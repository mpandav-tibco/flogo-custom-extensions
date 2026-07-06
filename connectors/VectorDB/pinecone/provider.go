package vectordb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// pineconeClient implements VectorDBClient for Pinecone via pure stdlib HTTP.
type pineconeClient struct {
	cfg        ConnectionConfig
	httpClient *http.Client
	hostCache  sync.Map // indexName → hostURL string
}

// Compile-time proof that pineconeClient satisfies the full VectorDBClient interface.
var _ VectorDBClient = (*pineconeClient)(nil)

func newPineconeClient(cfg ConnectionConfig) (VectorDBClient, error) {
	return &pineconeClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second},
	}, nil
}

func (c *pineconeClient) DBType() string { return "pinecone" }

func (c *pineconeClient) Close() error {
	// HTTP client does not require explicit closing.
	return nil
}

// ── HTTP helper ──────────────────────────────────────────────────────────────

// doRequest performs a single HTTP request against the Pinecone API.
// method: "GET", "POST", "DELETE", etc.
// endpoint: full URL
// body: JSON payload (nil for GET/DELETE with no body)
// Returns (responseBody, error).
func (c *pineconeClient) doRequest(ctx context.Context, method, endpoint string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("pinecone: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return nil, fmt.Errorf("pinecone: create request: %w", err)
	}
	req.Header.Set("Api-Key", c.cfg.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "Pinecone HTTP request failed", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB cap
	if err != nil {
		return nil, fmt.Errorf("pinecone: read response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		return respBody, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, newError(ErrCodeAuthFailed,
			fmt.Sprintf("Pinecone authentication failed (HTTP %d)", resp.StatusCode), nil)
	case http.StatusNotFound:
		return nil, newError(ErrCodeCollectionNotFound,
			fmt.Sprintf("Pinecone resource not found (HTTP %d): %s", resp.StatusCode, string(respBody)), nil)
	case http.StatusConflict:
		return nil, newError(ErrCodeCollectionExists,
			fmt.Sprintf("Pinecone resource already exists (HTTP %d)", resp.StatusCode), nil)
	default:
		return nil, newError(ErrCodeProviderError,
			fmt.Sprintf("Pinecone API error (HTTP %d): %s", resp.StatusCode, string(respBody)), nil)
	}
}

// scheme returns the URL scheme from config, defaulting to "https".
func (c *pineconeClient) scheme() string {
	if c.cfg.Scheme != "" {
		return c.cfg.Scheme
	}
	return "https"
}

// controlPlaneURL returns the base URL for the control plane (index management).
func (c *pineconeClient) controlPlaneURL() string {
	return fmt.Sprintf("%s://%s", c.scheme(), c.cfg.Host)
}

// ── Index host resolution ────────────────────────────────────────────────────

// getIndexHost resolves the data-plane host URL for the given index name.
// Results are cached in hostCache (sync.Map) to avoid repeated lookups.
func (c *pineconeClient) getIndexHost(ctx context.Context, indexName string) (string, error) {
	if v, ok := c.hostCache.Load(indexName); ok {
		return v.(string), nil
	}

	endpoint := fmt.Sprintf("%s/indexes/%s", c.controlPlaneURL(), indexName)
	body, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("pinecone: getIndexHost for %q: %w", indexName, err)
	}

	var resp struct {
		Host string `json:"host"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("pinecone: parse index host response: %w", err)
	}
	if resp.Host == "" {
		return "", newError(ErrCodeProviderError,
			fmt.Sprintf("pinecone: index %q returned empty host", indexName), nil)
	}
	// The local emulator returns bare host:port without a scheme; production returns
	// a hostname with no scheme either. Prepend our configured scheme.
	hostURL := resp.Host
	if !strings.HasPrefix(hostURL, "http://") && !strings.HasPrefix(hostURL, "https://") {
		hostURL = fmt.Sprintf("%s://%s", c.scheme(), hostURL)
	}
	c.hostCache.Store(indexName, hostURL)
	return hostURL, nil
}

// ── Collection (Index) management ───────────────────────────────────────────

// CreateCollection creates a new Pinecone serverless index and polls until ready.
func (c *pineconeClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}

	// Map generic metric to Pinecone metric name.
	metric := "cosine"
	switch strings.ToLower(cfg.DistanceMetric) {
	case "dot":
		metric = "dotproduct"
	case "euclidean":
		metric = "euclidean"
	}

	reqBody := map[string]interface{}{
		"name":      cfg.Name,
		"dimension": cfg.Dimensions,
		"metric":    metric,
		"spec": map[string]interface{}{
			"serverless": map[string]interface{}{
				"cloud":  c.cfg.PineconeCloud,
				"region": c.cfg.PineconeRegion,
			},
		},
	}

	endpoint := fmt.Sprintf("%s/indexes", c.controlPlaneURL())

	var createErr error
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, createErr = c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		if createErr != nil {
			// 409 = already exists — treat as idempotent success
			if vErr, ok := createErr.(*VDBError); ok && vErr.Code == ErrCodeCollectionExists {
				createErr = nil
				return nil // break retry
			}
		}
		return createErr
	}); err != nil {
		return err
	}
	if createErr != nil {
		return createErr
	}

	// Poll until the index is ready.
	return c.pollUntilReady(ctx, cfg.Name)
}

// pollUntilReady polls GET /indexes/{name} every 3 seconds up to 2 minutes
// until status.state == "Ready".
func (c *pineconeClient) pollUntilReady(ctx context.Context, indexName string) error {
	deadline := time.Now().Add(2 * time.Minute)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return newError(ErrCodeProviderError,
					fmt.Sprintf("pinecone: index %q did not become Ready within 2 minutes", indexName), nil)
			}
			endpoint := fmt.Sprintf("%s/indexes/%s", c.controlPlaneURL(), indexName)
			body, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
			if err != nil {
				// Not yet created — continue polling
				continue
			}
			var resp struct {
				Status struct {
					State string `json:"state"`
				} `json:"status"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				continue
			}
			if resp.Status.State == "Ready" {
				return nil
			}
		}
	}
}

// DeleteCollection deletes a Pinecone index and evicts it from the host cache.
func (c *pineconeClient) DeleteCollection(ctx context.Context, name string) error {
	endpoint := fmt.Sprintf("%s/indexes/%s", c.controlPlaneURL(), name)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, err := c.doRequest(ctx, http.MethodDelete, endpoint, nil)
		// 404 on delete = already gone, treat as success
		if err != nil {
			if vErr, ok := err.(*VDBError); ok && vErr.Code == ErrCodeCollectionNotFound {
				return nil
			}
		}
		return err
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteCollection failed", err)
	}
	c.hostCache.Delete(name)
	return nil
}

// ListCollections returns the names of all Pinecone indexes.
func (c *pineconeClient) ListCollections(ctx context.Context) ([]string, error) {
	endpoint := fmt.Sprintf("%s/indexes", c.controlPlaneURL())
	var names []string
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		var resp struct {
			Indexes []struct {
				Name string `json:"name"`
			} `json:"indexes"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("pinecone: parse list indexes response: %w", err)
		}
		names = make([]string, len(resp.Indexes))
		for i, idx := range resp.Indexes {
			names[i] = idx.Name
		}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	return names, nil
}

// CollectionExists reports whether the named Pinecone index exists.
func (c *pineconeClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	endpoint := fmt.Sprintf("%s/indexes/%s", c.controlPlaneURL(), name)
	var exists bool
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			if vErr, ok := err.(*VDBError); ok && vErr.Code == ErrCodeCollectionNotFound {
				exists = false
				return nil
			}
			return err
		}
		exists = true
		return nil
	}); err != nil {
		return false, newError(ErrCodeProviderError, "CollectionExists failed", err)
	}
	return exists, nil
}

// ── Document operations ──────────────────────────────────────────────────────

// UpsertDocuments inserts or updates documents in the given Pinecone index.
func (c *pineconeClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatchPinecone, "pinecone"); err != nil {
		return err
	}

	indexHost, err := c.getIndexHost(ctx, collectionName)
	if err != nil {
		return err
	}

	// Build Pinecone vectors array.
	vectors := make([]map[string]interface{}, len(docs))
	for i, doc := range docs {
		metadata := make(map[string]interface{})
		// Store content in metadata for retrieval.
		if doc.Content != "" {
			metadata["_content"] = doc.Content
		}
		for k, v := range doc.Payload {
			metadata[k] = v
		}
		vectors[i] = map[string]interface{}{
			"id":       doc.ID,
			"values":   toFloat32Slice(doc.Vector),
			"metadata": metadata,
		}
	}

	reqBody := map[string]interface{}{
		"vectors": vectors,
	}

	endpoint := fmt.Sprintf("%s/vectors/upsert", indexHost)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, err := c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		return err
	}); err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments failed", err)
	}
	return nil
}

// GetDocument retrieves a single document by ID from the given Pinecone index.
func (c *pineconeClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	indexHost, err := c.getIndexHost(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/vectors/fetch?ids=%s", indexHost, url.QueryEscape(id))
	var doc *Document
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		var resp struct {
			Vectors map[string]struct {
				ID       string                 `json:"id"`
				Values   []float32              `json:"values"`
				Metadata map[string]interface{} `json:"metadata"`
			} `json:"vectors"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("pinecone: parse fetch response: %w", err)
		}
		v, ok := resp.Vectors[id]
		if !ok {
			return newError(ErrCodeDocumentNotFound,
				fmt.Sprintf("document %q not found in index %q", id, collectionName), nil)
		}
		doc = pineconeVectorToDocument(v.ID, v.Values, v.Metadata)
		return nil
	}); err != nil {
		if vErr, ok := err.(*VDBError); ok {
			return nil, vErr
		}
		return nil, newError(ErrCodeProviderError, "GetDocument failed", err)
	}
	return doc, nil
}

// DeleteDocuments removes documents by ID list from the given Pinecone index.
func (c *pineconeClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	indexHost, err := c.getIndexHost(ctx, collectionName)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"ids": ids,
	}
	endpoint := fmt.Sprintf("%s/vectors/delete", indexHost)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, err := c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		return err
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

// DeleteByFilter removes documents matching the given filter from a Pinecone index.
// Returns -1 because Pinecone does not report a delete count.
func (c *pineconeClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if len(filters) == 0 {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter requires at least one filter", nil)
	}
	indexHost, err := c.getIndexHost(ctx, collectionName)
	if err != nil {
		return 0, err
	}

	reqBody := map[string]interface{}{
		"filter":    buildPineconeFilter(filters),
		"deleteAll": false,
	}
	endpoint := fmt.Sprintf("%s/vectors/delete", indexHost)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, err := c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		return err
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter failed", err)
	}
	return -1, nil // Pinecone does not report count
}

// ScrollDocuments paginates through documents using the Pinecone list + fetch approach.
// Step 1: list IDs via /vectors/list?prefix=&limit=N&paginationToken=X
// Step 2: fetch full vectors+metadata via /vectors/fetch?ids=...
func (c *pineconeClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	indexHost, err := c.getIndexHost(ctx, req.CollectionName)
	if err != nil {
		return nil, err
	}

	// Step 1: list IDs
	listURL := fmt.Sprintf("%s/vectors/list?limit=%d", indexHost, limit)
	if req.Offset != "" {
		listURL += "&paginationToken=" + url.QueryEscape(req.Offset)
	}

	var ids []string
	var nextToken string

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequest(ctx, http.MethodGet, listURL, nil)
		if err != nil {
			return err
		}
		var resp struct {
			Vectors []struct {
				ID string `json:"id"`
			} `json:"vectors"`
			Pagination struct {
				Next string `json:"next"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("pinecone: parse list response: %w", err)
		}
		ids = make([]string, len(resp.Vectors))
		for i, v := range resp.Vectors {
			ids[i] = v.ID
		}
		nextToken = resp.Pagination.Next
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments (list) failed", err)
	}

	if len(ids) == 0 {
		return &ScrollResult{Documents: []Document{}, NextOffset: "", Total: -1}, nil
	}

	// Step 2: fetch full vectors+metadata for the listed IDs.
	docs, err := c.fetchByIDs(ctx, indexHost, ids)
	if err != nil {
		return nil, err
	}

	return &ScrollResult{
		Documents:  docs,
		NextOffset: nextToken,
		Total:      -1,
	}, nil
}

// fetchByIDs retrieves vectors+metadata from Pinecone's /vectors/fetch endpoint.
func (c *pineconeClient) fetchByIDs(ctx context.Context, indexHost string, ids []string) ([]Document, error) {
	// Build URL with repeated ids query params.
	fetchURL := fmt.Sprintf("%s/vectors/fetch", indexHost)
	params := make([]string, len(ids))
	for i, id := range ids {
		params[i] = "ids=" + url.QueryEscape(id)
	}
	fetchURL += "?" + strings.Join(params, "&")

	var docs []Document
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequest(ctx, http.MethodGet, fetchURL, nil)
		if err != nil {
			return err
		}
		var resp struct {
			Vectors map[string]struct {
				ID       string                 `json:"id"`
				Values   []float32              `json:"values"`
				Metadata map[string]interface{} `json:"metadata"`
			} `json:"vectors"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("pinecone: parse fetch response: %w", err)
		}
		docs = make([]Document, 0, len(resp.Vectors))
		for _, v := range resp.Vectors {
			docs = append(docs, *pineconeVectorToDocument(v.ID, v.Values, v.Metadata))
		}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "fetchByIDs failed", err)
	}
	return docs, nil
}

// CountDocuments returns the total vector count for the index.
// For filtered queries returns -1 (Pinecone doesn't support filtered count without a query).
// describe_index_stats is a data-plane endpoint on the index host, NOT the control plane.
func (c *pineconeClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if len(filters) > 0 {
		return -1, nil
	}

	indexHost, err := c.getIndexHost(ctx, collectionName)
	if err != nil {
		return 0, err
	}
	endpoint := fmt.Sprintf("%s/describe_index_stats", indexHost)

	var count int64
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequest(ctx, http.MethodPost, endpoint, map[string]interface{}{})
		if err != nil {
			return err
		}
		var resp struct {
			TotalVectorCount int64 `json:"totalVectorCount"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("pinecone: parse describe_index_stats response: %w", err)
		}
		count = resp.TotalVectorCount
		return nil
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
	}
	return count, nil
}

// ── Search ───────────────────────────────────────────────────────────────────

// VectorSearch performs an approximate nearest-neighbour search in a Pinecone index.
func (c *pineconeClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	indexHost, err := c.getIndexHost(ctx, req.CollectionName)
	if err != nil {
		return nil, err
	}

	reqBody := map[string]interface{}{
		"vector":          toFloat32Slice(req.QueryVector),
		"topK":            req.TopK,
		"includeMetadata": !req.SkipPayload,
		"includeValues":   req.WithVectors,
	}
	if len(req.Filters) > 0 {
		reqBody["filter"] = buildPineconeFilter(req.Filters)
	}

	endpoint := fmt.Sprintf("%s/query", indexHost)
	var results []SearchResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		if err != nil {
			return err
		}
		var resp struct {
			Matches []struct {
				ID       string                 `json:"id"`
				Score    float64                `json:"score"`
				Metadata map[string]interface{} `json:"metadata"`
				Values   []float32              `json:"values"`
			} `json:"matches"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("pinecone: parse query response: %w", err)
		}
		results = make([]SearchResult, 0, len(resp.Matches))
		for _, m := range resp.Matches {
			if req.ScoreThreshold > 0 && m.Score < req.ScoreThreshold {
				continue
			}
			sr := SearchResult{
				ID:    m.ID,
				Score: m.Score,
			}
			if !req.SkipPayload && m.Metadata != nil {
				if content, ok := m.Metadata["_content"].(string); ok {
					sr.Content = content
				}
				payload := make(map[string]interface{})
				for k, v := range m.Metadata {
					if k != "_content" {
						payload[k] = v
					}
				}
				if len(payload) > 0 {
					sr.Payload = payload
				}
			}
			if req.WithVectors && len(m.Values) > 0 {
				sr.Vector = toFloat64Slice(m.Values)
			}
			results = append(results, sr)
		}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}
	return results, nil
}

// HybridSearch performs combined dense + sparse search using Pinecone's sparseVector support.
// Builds a FNV-32a sparse vector from QueryText and combines with the dense QueryVector.
func (c *pineconeClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	if len(req.QueryVector) == 0 && req.QueryText == "" {
		return nil, newError(ErrCodeInvalidQueryVector, "HybridSearch requires queryVector or queryText", nil)
	}
	if req.TopK <= 0 {
		return nil, newError(ErrCodeInvalidTopK, fmt.Sprintf("topK=%d must be > 0", req.TopK), nil)
	}

	indexHost, err := c.getIndexHost(ctx, req.CollectionName)
	if err != nil {
		return nil, err
	}

	reqBody := map[string]interface{}{
		"topK":            req.TopK,
		"includeMetadata": !req.SkipPayload,
	}

	if len(req.QueryVector) > 0 {
		reqBody["vector"] = toFloat32Slice(req.QueryVector)
	}

	// Build sparse vector from query text.
	if req.QueryText != "" {
		sparseIndices, sparseValues := buildSparseVector(req.QueryText)
		if len(sparseIndices) > 0 {
			reqBody["sparseVector"] = map[string]interface{}{
				"indices": sparseIndices,
				"values":  sparseValues,
			}
			// Alpha controls dense vs sparse weighting (used by Pinecone hybrid search).
			reqBody["alpha"] = req.Alpha
		}
	}

	if len(req.Filters) > 0 {
		reqBody["filter"] = buildPineconeFilter(req.Filters)
	}

	endpoint := fmt.Sprintf("%s/query", indexHost)
	var results []SearchResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		if err != nil {
			return err
		}
		var resp struct {
			Matches []struct {
				ID       string                 `json:"id"`
				Score    float64                `json:"score"`
				Metadata map[string]interface{} `json:"metadata"`
				Values   []float32              `json:"values"`
			} `json:"matches"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("pinecone: parse hybrid query response: %w", err)
		}
		results = make([]SearchResult, 0, len(resp.Matches))
		for _, m := range resp.Matches {
			if req.ScoreThreshold > 0 && m.Score < req.ScoreThreshold {
				continue
			}
			sr := SearchResult{
				ID:    m.ID,
				Score: m.Score,
			}
			if !req.SkipPayload && m.Metadata != nil {
				if content, ok := m.Metadata["_content"].(string); ok {
					sr.Content = content
				}
				payload := make(map[string]interface{})
				for k, v := range m.Metadata {
					if k != "_content" {
						payload[k] = v
					}
				}
				if len(payload) > 0 {
					sr.Payload = payload
				}
			}
			results = append(results, sr)
		}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "HybridSearch failed", err)
	}
	return results, nil
}

// HealthCheck verifies that the Pinecone control plane is reachable.
func (c *pineconeClient) HealthCheck(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/indexes", c.controlPlaneURL())
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		return err
	}); err != nil {
		return newError(ErrCodeConnectionFailed, "Pinecone health check failed", err)
	}
	return nil
}

// ── Sparse vector builder ────────────────────────────────────────────────────

// buildSparseVector builds a Pinecone-compatible sparse vector from plain text
// using FNV-32a hashing and term-frequency (TF) weighting.
//
// Algorithm:
//  1. Tokenize text into lower-case space-separated tokens.
//  2. Compute term frequency: count[token] / total_tokens.
//  3. Hash each token with FNV-32a → dimension index (uint32).
//  4. Normalize TF values so they sum to 1.0.
//  5. Sort entries by index (deterministic output for identical inputs).
//  6. Cap at 50 dimensions (Pinecone sparse vector limit for typical use).
//
// Returns nil, nil for empty input (callers must check before adding to request).
func buildSparseVector(text string) (indices []uint32, values []float32) {
	tokens := strings.Fields(strings.ToLower(text))
	if len(tokens) == 0 {
		return nil, nil
	}

	tf := make(map[uint32]int)
	h := fnv.New32a()
	for _, tok := range tokens {
		h.Reset()
		h.Write([]byte(tok))
		tf[h.Sum32()]++
	}

	total := float64(len(tokens))
	type entry struct {
		idx uint32
		val float32
	}
	entries := make([]entry, 0, len(tf))
	for idx, cnt := range tf {
		entries = append(entries, entry{idx, float32(float64(cnt) / total)})
	}

	// Sort by index for deterministic output.
	sort.Slice(entries, func(i, j int) bool { return entries[i].idx < entries[j].idx })

	// Cap at 50 dimensions.
	const maxDims = 50
	if len(entries) > maxDims {
		entries = entries[:maxDims]
	}

	// Normalize values to sum to 1.0.
	var sum float64
	for _, e := range entries {
		sum += float64(e.val)
	}
	if sum > 0 {
		for i := range entries {
			entries[i].val = float32(float64(entries[i].val) / sum)
		}
	}

	indices = make([]uint32, len(entries))
	values = make([]float32, len(entries))
	for i, e := range entries {
		indices[i] = e.idx
		values[i] = e.val
	}
	return indices, values
}

// ── Filter builder ───────────────────────────────────────────────────────────

// buildPineconeFilter converts a generic filter map to Pinecone metadata filter syntax.
// Pinecone uses MongoDB-style operators: $eq, $ne, $gt, $gte, $lt, $lte, $in.
//
// Multiple top-level entries are ANDed implicitly.
// Each value can be a scalar (equality) or an operator map.
func buildPineconeFilter(filters map[string]interface{}) map[string]interface{} {
	if len(filters) == 0 {
		return nil
	}
	result := make(map[string]interface{}, len(filters))
	for k, v := range filters {
		switch val := v.(type) {
		case map[string]interface{}:
			// Operator map — pass through as-is (Pinecone uses the same $ syntax).
			result[k] = val
		default:
			// Scalar equality → $eq wrapper.
			result[k] = map[string]interface{}{"$eq": v}
		}
	}
	return result
}

// ── Document conversion helpers ──────────────────────────────────────────────

// pineconeVectorToDocument reconstructs a Document from a Pinecone vector response.
func pineconeVectorToDocument(id string, values []float32, metadata map[string]interface{}) *Document {
	doc := &Document{
		ID:      id,
		Payload: make(map[string]interface{}),
	}
	if len(values) > 0 {
		doc.Vector = toFloat64Slice(values)
	}
	for k, v := range metadata {
		if k == "_content" {
			if s, ok := v.(string); ok {
				doc.Content = s
			}
		} else {
			doc.Payload[k] = v
		}
	}
	return doc
}

