package vectordb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// azureAISearchClient implements VectorDBClient for Azure AI Search via pure stdlib HTTP.
type azureAISearchClient struct {
	cfg        ConnectionConfig
	httpClient *http.Client
}

// Compile-time check.
var _ VectorDBClient = (*azureAISearchClient)(nil)

func newAzureAISearchClient(cfg ConnectionConfig) (VectorDBClient, error) {
	return &azureAISearchClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second},
	}, nil
}

func (c *azureAISearchClient) DBType() string { return "azureaisearch" }
func (c *azureAISearchClient) Close() error   { return nil }

// ── URL helpers ──────────────────────────────────────────────────────────────

func (c *azureAISearchClient) apiURL(path string) string {
	return fmt.Sprintf("%s%s?api-version=%s", c.cfg.Endpoint, path, c.cfg.APIVersion)
}

// ── HTTP helper ──────────────────────────────────────────────────────────────

func (c *azureAISearchClient) doRequest(ctx context.Context, method, url string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("azureaisearch: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("azureaisearch: create request: %w", err)
	}
	req.Header.Set("api-key", c.cfg.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, newError(ErrCodeConnectionFailed, "Azure AI Search HTTP request failed", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("azureaisearch: read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

func (c *azureAISearchClient) handleHTTPError(statusCode int, body []byte, docNotFound bool) error {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return newError(ErrCodeAuthFailed,
			fmt.Sprintf("Azure AI Search authentication failed (HTTP %d)", statusCode), nil)
	case http.StatusNotFound:
		if docNotFound {
			return newError(ErrCodeDocumentNotFound, "document not found", nil)
		}
		return newError(ErrCodeCollectionNotFound, "index not found", nil)
	case http.StatusConflict:
		return newError(ErrCodeCollectionExists, "index already exists", nil)
	default:
		// Try to parse Azure error body
		var azErr struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := string(body)
		if json.Unmarshal(body, &azErr) == nil && azErr.Error.Message != "" {
			msg = azErr.Error.Message
		}
		return newError(ErrCodeProviderError,
			fmt.Sprintf("Azure AI Search API error (HTTP %d): %s", statusCode, msg), nil)
	}
}

// ── Metadata helpers ─────────────────────────────────────────────────────────

func payloadToJSON(payload map[string]interface{}) string {
	if payload == nil {
		return "{}"
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func jsonToPayload(s string) map[string]interface{} {
	if s == "" || s == "{}" {
		return nil
	}
	m := make(map[string]interface{})
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

// ── Collection management ────────────────────────────────────────────────────

func (c *azureAISearchClient) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("%s/indexes?api-version=%s&$top=1", c.cfg.Endpoint, c.cfg.APIVersion)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			return newError(ErrCodeAuthFailed, fmt.Sprintf("Azure AI Search health check auth failed (HTTP %d)", statusCode), nil)
		}
		if statusCode != http.StatusOK {
			return newError(ErrCodeConnectionFailed, fmt.Sprintf("Azure AI Search health check failed (HTTP %d)", statusCode), nil)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (c *azureAISearchClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	url := c.apiURL("/indexes/" + name)
	var exists bool
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if statusCode == http.StatusOK {
			exists = true
			return nil
		}
		if statusCode == http.StatusNotFound {
			exists = false
			return nil
		}
		return newError(ErrCodeProviderError, fmt.Sprintf("CollectionExists HTTP %d", statusCode), nil)
	}); err != nil {
		return false, err
	}
	return exists, nil
}

func (c *azureAISearchClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}

	metric := "cosine"
	switch strings.ToLower(cfg.DistanceMetric) {
	case "dot":
		metric = "dotProduct"
	case "euclidean":
		metric = "euclidean"
	}

	body := map[string]interface{}{
		"name": cfg.Name,
		"fields": []map[string]interface{}{
			{
				"name": "id", "type": "Edm.String", "key": true,
				"retrievable": true, "filterable": true, "sortable": false,
				"facetable": false, "searchable": false,
			},
			{
				"name": "content", "type": "Edm.String",
				"searchable": true, "retrievable": true,
				"filterable": false, "sortable": false, "facetable": false,
			},
			{
				"name": "metadata", "type": "Edm.String",
				"searchable": false, "retrievable": true,
				"filterable": false, "sortable": false, "facetable": false,
			},
			{
				"name": "embedding", "type": "Collection(Edm.Single)",
				"dimensions": cfg.Dimensions, "vectorSearchProfile": "hnsw-profile",
				"retrievable": true, "stored": true, "searchable": true,
			},
		},
		"vectorSearch": map[string]interface{}{
			"profiles": []map[string]interface{}{
				{"name": "hnsw-profile", "algorithm": "hnsw-config", "vectorizer": nil},
			},
			"algorithms": []map[string]interface{}{
				{
					"name": "hnsw-config", "kind": "hnsw",
					"hnswParameters": map[string]interface{}{
						"metric":         metric,
						"m":              4,
						"efConstruction": 400,
						"efSearch":       500,
					},
					"exhaustiveKnnParameters": nil,
				},
			},
		},
	}

	url := c.apiURL("/indexes/" + cfg.Name)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, err := c.doRequest(ctx, http.MethodPut, url, body)
		if err != nil {
			return err
		}
		if statusCode == http.StatusCreated || statusCode == http.StatusOK {
			return nil
		}
		if statusCode == http.StatusConflict {
			return newError(ErrCodeCollectionExists, "", nil)
		}
		return c.handleHTTPError(statusCode, respBody, false)
	}); err != nil {
		return err
	}
	return nil
}

func (c *azureAISearchClient) DeleteCollection(ctx context.Context, name string) error {
	url := c.apiURL("/indexes/" + name)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, err := c.doRequest(ctx, http.MethodDelete, url, nil)
		if err != nil {
			return err
		}
		if statusCode == http.StatusNoContent || statusCode == http.StatusOK {
			return nil
		}
		if statusCode == http.StatusNotFound {
			return newError(ErrCodeCollectionNotFound, "", nil)
		}
		return c.handleHTTPError(statusCode, respBody, false)
	}); err != nil {
		return err
	}
	return nil
}

func (c *azureAISearchClient) ListCollections(ctx context.Context) ([]string, error) {
	url := c.apiURL("/indexes")
	var names []string
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if statusCode != http.StatusOK {
			return c.handleHTTPError(statusCode, respBody, false)
		}
		var resp struct {
			Value []struct {
				Name string `json:"name"`
			} `json:"value"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return fmt.Errorf("azureaisearch: parse list indexes: %w", err)
		}
		names = make([]string, len(resp.Value))
		for i, idx := range resp.Value {
			names[i] = idx.Name
		}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	return names, nil
}

// ── Document operations ──────────────────────────────────────────────────────

func (c *azureAISearchClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatch, "azureaisearch"); err != nil {
		return err
	}

	type azDoc struct {
		SearchAction string    `json:"@search.action"`
		ID           string    `json:"id"`
		Content      string    `json:"content"`
		Metadata     string    `json:"metadata"`
		Embedding    []float32 `json:"embedding"`
	}

	azDocs := make([]azDoc, len(docs))
	for i, d := range docs {
		emb := make([]float32, len(d.Vector))
		for j, v := range d.Vector {
			emb[j] = float32(v)
		}
		azDocs[i] = azDoc{
			SearchAction: "mergeOrUpload",
			ID:           d.ID,
			Content:      d.Content,
			Metadata:     payloadToJSON(d.Payload),
			Embedding:    emb,
		}
	}

	body := map[string]interface{}{"value": azDocs}
	url := c.apiURL("/indexes/" + collectionName + "/docs/index")

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, url, body)
		if err != nil {
			return err
		}
		if statusCode == http.StatusOK || statusCode == http.StatusCreated {
			// Parse response to check per-doc status
			var resp struct {
				Value []struct {
					Key    string `json:"key"`
					Status bool   `json:"status"`
					Err    string `json:"errorMessage"`
				} `json:"value"`
			}
			if json.Unmarshal(respBody, &resp) == nil {
				for _, r := range resp.Value {
					if !r.Status {
						return newError(ErrCodeProviderError,
							fmt.Sprintf("upsert failed for doc %q: %s", r.Key, r.Err), nil)
					}
				}
			}
			return nil
		}
		return c.handleHTTPError(statusCode, respBody, false)
	}); err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments failed", err)
	}
	return nil
}

func (c *azureAISearchClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	url := c.apiURL("/indexes/" + collectionName + "/docs/" + id)
	var doc *Document
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if statusCode == http.StatusNotFound {
			return newError(ErrCodeDocumentNotFound, fmt.Sprintf("document %q not found", id), nil)
		}
		if statusCode != http.StatusOK {
			return c.handleHTTPError(statusCode, respBody, true)
		}
		var raw struct {
			ID        string    `json:"id"`
			Content   string    `json:"content"`
			Metadata  string    `json:"metadata"`
			Embedding []float32 `json:"embedding"`
		}
		if err := json.Unmarshal(respBody, &raw); err != nil {
			return fmt.Errorf("azureaisearch: parse get document: %w", err)
		}
		doc = &Document{
			ID:      raw.ID,
			Content: raw.Content,
			Payload: jsonToPayload(raw.Metadata),
		}
		if len(raw.Embedding) > 0 {
			doc.Vector = make([]float64, len(raw.Embedding))
			for i, v := range raw.Embedding {
				doc.Vector[i] = float64(v)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return doc, nil
}

func (c *azureAISearchClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	type azDoc struct {
		SearchAction string `json:"@search.action"`
		ID           string `json:"id"`
	}
	azDocs := make([]azDoc, len(ids))
	for i, id := range ids {
		azDocs[i] = azDoc{SearchAction: "delete", ID: id}
	}

	body := map[string]interface{}{"value": azDocs}
	url := c.apiURL("/indexes/" + collectionName + "/docs/index")

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, url, body)
		if err != nil {
			return err
		}
		if statusCode == http.StatusOK || statusCode == http.StatusCreated {
			return nil
		}
		return c.handleHTTPError(statusCode, respBody, false)
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

// DeleteByFilter scrolls all docs with pagination (client-side filter on payload), then deletes matching IDs.
// Note: metadata is stored as a JSON string (not filterable via OData), so filtering is done client-side.
func (c *azureAISearchClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	var matchingIDs []string
	offset := ""

	for {
		scrollResult, err := c.ScrollDocuments(ctx, ScrollRequest{
			CollectionName: collectionName,
			Limit:          1000,
			Offset:         offset,
		})
		if err != nil {
			return 0, err
		}

		for _, doc := range scrollResult.Documents {
			if matchesFilters(doc.Payload, filters) {
				matchingIDs = append(matchingIDs, doc.ID)
			}
		}

		if scrollResult.NextOffset == "" {
			break
		}
		offset = scrollResult.NextOffset
	}

	if len(matchingIDs) == 0 {
		return 0, nil
	}

	if err := c.DeleteDocuments(ctx, collectionName, matchingIDs); err != nil {
		return 0, err
	}
	return int64(len(matchingIDs)), nil
}

// matchesFilters checks if a payload matches all filters (simple equality).
func matchesFilters(payload map[string]interface{}, filters map[string]interface{}) bool {
	if len(filters) == 0 {
		return true
	}
	for k, v := range filters {
		pv, ok := payload[k]
		if !ok {
			return false
		}
		// Simple equality check
		if fmt.Sprintf("%v", pv) != fmt.Sprintf("%v", v) {
			return false
		}
	}
	return true
}

func (c *azureAISearchClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	var count int64

	if len(filters) == 0 {
		// Simple count endpoint
		url := c.apiURL("/indexes/" + collectionName + "/docs/$count")
		if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
			respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			if statusCode != http.StatusOK {
				return c.handleHTTPError(statusCode, respBody, false)
			}
			n, err := strconv.ParseInt(strings.TrimSpace(string(respBody)), 10, 64)
			if err != nil {
				return fmt.Errorf("azureaisearch: parse count response %q: %w", string(respBody), err)
			}
			count = n
			return nil
		}); err != nil {
			return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
		}
		return count, nil
	}

	// Filtered count: metadata is stored as a JSON string (not OData-filterable),
	// so we must scroll the full collection and count matching docs client-side.
	offset := ""
	for {
		scrollResult, err := c.ScrollDocuments(ctx, ScrollRequest{
			CollectionName: collectionName,
			Limit:          1000,
			Offset:         offset,
		})
		if err != nil {
			return 0, newError(ErrCodeProviderError, "CountDocuments (filtered) failed", err)
		}
		for _, doc := range scrollResult.Documents {
			if matchesFilters(doc.Payload, filters) {
				count++
			}
		}
		if scrollResult.NextOffset == "" {
			break
		}
		offset = scrollResult.NextOffset
	}
	return count, nil
}

func (c *azureAISearchClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	skip := 0
	if req.Offset != "" {
		if n, err := strconv.Atoi(req.Offset); err == nil {
			skip = n
		}
	}

	url := c.apiURL("/indexes/" + req.CollectionName + "/docs/search")
	body := map[string]interface{}{
		"search": "*",
		"top":    limit,
		"skip":   skip,
		"count":  true,
	}

	var result *ScrollResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, url, body)
		if err != nil {
			return err
		}
		if statusCode != http.StatusOK {
			return c.handleHTTPError(statusCode, respBody, false)
		}

		var resp struct {
			Count int64 `json:"@odata.count"`
			Value []struct {
				ID        string    `json:"id"`
				Content   string    `json:"content"`
				Metadata  string    `json:"metadata"`
				Embedding []float32 `json:"embedding"`
			} `json:"value"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return fmt.Errorf("azureaisearch: parse scroll response: %w", err)
		}

		docs := make([]Document, len(resp.Value))
		for i, v := range resp.Value {
			doc := Document{
				ID:      v.ID,
				Content: v.Content,
				Payload: jsonToPayload(v.Metadata),
			}
			if req.WithVectors && len(v.Embedding) > 0 {
				doc.Vector = make([]float64, len(v.Embedding))
				for j, f := range v.Embedding {
					doc.Vector[j] = float64(f)
				}
			}
			docs[i] = doc
		}

		nextOffset := ""
		newSkip := skip + len(resp.Value)
		if int64(newSkip) < resp.Count {
			nextOffset = strconv.Itoa(newSkip)
		}

		result = &ScrollResult{
			Documents:  docs,
			NextOffset: nextOffset,
			Total:      resp.Count,
		}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}
	return result, nil
}

// ── Search ───────────────────────────────────────────────────────────────────

func (c *azureAISearchClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	emb := make([]float32, len(req.QueryVector))
	for i, v := range req.QueryVector {
		emb[i] = float32(v)
	}

	body := map[string]interface{}{
		"vectorQueries": []map[string]interface{}{
			{
				"kind":       "vector",
				"vector":     emb,
				"fields":     "embedding",
				"k":          req.TopK,
				"exhaustive": false,
			},
		},
		"top":    req.TopK,
		"select": "id,content,metadata",
	}

	url := c.apiURL("/indexes/" + req.CollectionName + "/docs/search")
	var results []SearchResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, url, body)
		if err != nil {
			return err
		}
		if statusCode != http.StatusOK {
			return c.handleHTTPError(statusCode, respBody, false)
		}
		results, err = parseSearchResponse(respBody, req.ScoreThreshold, req.SkipPayload)
		return err
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}
	return results, nil
}

func (c *azureAISearchClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	if len(req.QueryVector) == 0 && req.QueryText == "" {
		return nil, newError(ErrCodeInvalidQueryVector, "HybridSearch requires queryVector or queryText", nil)
	}
	if req.TopK <= 0 {
		return nil, newError(ErrCodeInvalidTopK, fmt.Sprintf("topK=%d must be > 0", req.TopK), nil)
	}

	body := map[string]interface{}{
		"search": req.QueryText,
		"top":    req.TopK,
		"select": "id,content,metadata",
	}

	if len(req.QueryVector) > 0 {
		emb := make([]float32, len(req.QueryVector))
		for i, v := range req.QueryVector {
			emb[i] = float32(v)
		}
		body["vectorQueries"] = []map[string]interface{}{
			{
				"kind":       "vector",
				"vector":     emb,
				"fields":     "embedding",
				"k":          req.TopK,
				"exhaustive": false,
			},
		}
	}

	url := c.apiURL("/indexes/" + req.CollectionName + "/docs/search")
	var results []SearchResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, url, body)
		if err != nil {
			return err
		}
		if statusCode != http.StatusOK {
			return c.handleHTTPError(statusCode, respBody, false)
		}
		var parseErr error
		results, parseErr = parseSearchResponse(respBody, req.ScoreThreshold, req.SkipPayload)
		return parseErr
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "HybridSearch failed", err)
	}
	return results, nil
}

// parseSearchResponse parses the Azure AI Search search response body.
func parseSearchResponse(respBody []byte, scoreThreshold float64, skipPayload bool) ([]SearchResult, error) {
	var resp struct {
		Value []struct {
			ID       string  `json:"id"`
			Content  string  `json:"content"`
			Metadata string  `json:"metadata"`
			Score    float64 `json:"@search.score"`
		} `json:"value"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("azureaisearch: parse search response: %w", err)
	}

	results := make([]SearchResult, 0, len(resp.Value))
	for _, v := range resp.Value {
		if scoreThreshold > 0 && v.Score < scoreThreshold {
			continue
		}
		sr := SearchResult{
			ID:      v.ID,
			Score:   v.Score,
			Content: v.Content,
		}
		if !skipPayload {
			sr.Payload = jsonToPayload(v.Metadata)
		}
		results = append(results, sr)
	}
	return results, nil
}
