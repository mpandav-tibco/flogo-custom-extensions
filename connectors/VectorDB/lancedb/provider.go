package vectordb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// lanceDBClient implements VectorDBClient for LanceDB via pure stdlib HTTP.
type lanceDBClient struct {
	cfg        ConnectionConfig
	httpClient *http.Client
}

// Compile-time proof that lanceDBClient satisfies the full VectorDBClient interface.
var _ VectorDBClient = (*lanceDBClient)(nil)

func newLanceDBClient(_ context.Context, cfg ConnectionConfig) (VectorDBClient, error) {
	return &lanceDBClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second},
	}, nil
}

func (c *lanceDBClient) DBType() string { return "lancedb" }

func (c *lanceDBClient) Close() error {
	// HTTP client does not require explicit closing.
	return nil
}

// ── Base URL ─────────────────────────────────────────────────────────────────

// baseURL builds the base URL for the LanceDB server.
// Self-hosted: {scheme}://{host}:{port}
// Cloud (port==0): {scheme}://{host}
func (c *lanceDBClient) baseURL() string {
	if c.cfg.Port == 0 {
		return fmt.Sprintf("%s://%s", c.cfg.Scheme, c.cfg.Host)
	}
	return fmt.Sprintf("%s://%s:%d", c.cfg.Scheme, c.cfg.Host, c.cfg.Port)
}

// ── HTTP helper ───────────────────────────────────────────────────────────────

// doRequest performs a single HTTP request against the LanceDB REST API.
func (c *lanceDBClient) doRequest(ctx context.Context, method, endpoint string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("lancedb: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("lancedb: create request: %w", err)
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, newError(ErrCodeConnectionFailed, "LanceDB HTTP request failed", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB cap
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("lancedb: read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// doRequestChecked performs an HTTP request and maps status codes to VDBErrors.
// notFoundCode overrides the default "collection not found" to allow callers to
// distinguish between "table not found" vs "document not found" contexts.
func (c *lanceDBClient) doRequestChecked(ctx context.Context, method, endpoint string, body interface{}, notFoundCode string) ([]byte, error) {
	if notFoundCode == "" {
		notFoundCode = ErrCodeCollectionNotFound
	}
	respBody, status, err := c.doRequest(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	switch {
	case status >= 200 && status < 300:
		return respBody, nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return nil, newError(ErrCodeAuthFailed,
			fmt.Sprintf("LanceDB authentication failed (HTTP %d)", status), nil)
	case status == http.StatusNotFound:
		return nil, newError(notFoundCode,
			fmt.Sprintf("LanceDB resource not found (HTTP %d): %s", status, string(respBody)), nil)
	case status == http.StatusConflict:
		return nil, newError(ErrCodeCollectionExists,
			fmt.Sprintf("LanceDB resource already exists (HTTP %d)", status), nil)
	default:
		return nil, newError(ErrCodeProviderError,
			fmt.Sprintf("LanceDB API error (HTTP %d): %s", status, string(respBody)), nil)
	}
}

// ── HealthCheck ───────────────────────────────────────────────────────────────

// HealthCheck verifies that the LanceDB server is reachable.
func (c *lanceDBClient) HealthCheck(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/v1/table/", c.baseURL())
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, status, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		if status >= 200 && status < 300 {
			return nil
		}
		return newError(ErrCodeConnectionFailed,
			fmt.Sprintf("LanceDB health check returned HTTP %d", status), nil)
	}); err != nil {
		return newError(ErrCodeConnectionFailed, "LanceDB health check failed", err)
	}
	return nil
}

// ── Collection management ─────────────────────────────────────────────────────

// CollectionExists reports whether the named LanceDB table exists.
func (c *lanceDBClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return false, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	endpoint := fmt.Sprintf("%s/v1/table/%s/", c.baseURL(), name)
	var exists bool
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, status, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		switch status {
		case http.StatusOK:
			exists = true
			return nil
		case http.StatusNotFound:
			exists = false
			return nil
		default:
			return newError(ErrCodeProviderError,
				fmt.Sprintf("CollectionExists: unexpected HTTP %d", status), nil)
		}
	}); err != nil {
		return false, newError(ErrCodeProviderError, "CollectionExists failed", err)
	}
	return exists, nil
}

// CreateCollection creates a new LanceDB table with the given schema.
func (c *lanceDBClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/v1/table/%s/", c.baseURL(), cfg.Name)

	// Build Arrow-like schema body for LanceDB REST API.
	schemaBody := map[string]interface{}{
		"schema": map[string]interface{}{
			"type": "struct",
			"fields": []interface{}{
				map[string]interface{}{
					"name":     "id",
					"type":     map[string]interface{}{"type": "utf8"},
					"nullable": false,
				},
				map[string]interface{}{
					"name":     "content",
					"type":     map[string]interface{}{"type": "utf8"},
					"nullable": true,
				},
				map[string]interface{}{
					"name":     "metadata",
					"type":     map[string]interface{}{"type": "utf8"},
					"nullable": true,
				},
				map[string]interface{}{
					"name": "embedding",
					"type": map[string]interface{}{
						"type": "fixed_size_list",
						"children": []interface{}{
							map[string]interface{}{
								"name":     "item",
								"type":     map[string]interface{}{"type": "float32"},
								"nullable": true,
							},
						},
						"list_size": cfg.Dimensions,
					},
					"nullable": true,
				},
			},
		},
	}

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, status, reqErr := c.doRequest(ctx, http.MethodPost, endpoint, schemaBody)
		if reqErr != nil {
			return reqErr
		}
		switch {
		case status == http.StatusOK || status == http.StatusCreated || status == http.StatusNoContent:
			return nil
		case status == http.StatusConflict:
			return newError(ErrCodeCollectionExists, "", nil)
		case status == http.StatusNotFound || status == http.StatusUnprocessableEntity:
			// Some LanceDB versions do not support schema-only creation.
			// Fallback: create with empty data.
			emptyBody := map[string]interface{}{
				"data": []interface{}{},
				"mode": "create",
			}
			_, status2, err2 := c.doRequest(ctx, http.MethodPost, endpoint, emptyBody)
			if err2 != nil {
				return err2
			}
			if status2 == http.StatusConflict {
				return newError(ErrCodeCollectionExists, "", nil)
			}
			if status2 < 200 || status2 >= 300 {
				return newError(ErrCodeProviderError,
					fmt.Sprintf("CreateCollection fallback failed (HTTP %d)", status2), nil)
			}
			return nil
		default:
			return newError(ErrCodeProviderError,
				fmt.Sprintf("CreateCollection failed (HTTP %d)", status), nil)
		}
	}); err != nil {
		// 409 Conflict = already exists
		if vErr, ok := err.(*VDBError); ok && vErr.Code == ErrCodeCollectionExists {
			return err
		}
		return newError(ErrCodeProviderError, "CreateCollection failed", err)
	}
	return nil
}

// DeleteCollection deletes a LanceDB table.
func (c *lanceDBClient) DeleteCollection(ctx context.Context, name string) error {
	if name == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	endpoint := fmt.Sprintf("%s/v1/table/%s/", c.baseURL(), name)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, status, err := c.doRequest(ctx, http.MethodDelete, endpoint, nil)
		if err != nil {
			return err
		}
		switch {
		case status == http.StatusOK || status == http.StatusNoContent:
			return nil
		case status == http.StatusNotFound:
			return newError(ErrCodeCollectionNotFound, fmt.Sprintf("table %q not found", name), nil)
		default:
			return newError(ErrCodeProviderError,
				fmt.Sprintf("DeleteCollection failed (HTTP %d)", status), nil)
		}
	}); err != nil {
		return err
	}
	return nil
}

// ListCollections returns the names of all LanceDB tables.
func (c *lanceDBClient) ListCollections(ctx context.Context) ([]string, error) {
	endpoint := fmt.Sprintf("%s/v1/table/", c.baseURL())
	var names []string
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequestChecked(ctx, http.MethodGet, endpoint, nil, "")
		if err != nil {
			return err
		}
		// Try {"tables": [...]} format first
		var resp1 struct {
			Tables []string `json:"tables"`
		}
		if err := json.Unmarshal(body, &resp1); err == nil && resp1.Tables != nil {
			names = resp1.Tables
			return nil
		}
		// Try plain array format: ["name1","name2",...]
		var arr []string
		if err := json.Unmarshal(body, &arr); err == nil {
			names = arr
			return nil
		}
		// Try array of objects: [{"name":"..."}]
		var objArr []map[string]interface{}
		if err := json.Unmarshal(body, &objArr); err == nil {
			names = make([]string, 0, len(objArr))
			for _, obj := range objArr {
				if n, ok := obj["name"].(string); ok {
					names = append(names, n)
				}
			}
			return nil
		}
		return fmt.Errorf("lancedb: unexpected ListCollections response format: %s", string(body))
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

// ── Document operations ───────────────────────────────────────────────────────

// UpsertDocuments inserts or updates documents in the given LanceDB table.
func (c *lanceDBClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatchLanceDB, "lancedb"); err != nil {
		return err
	}

	// Build records array for LanceDB.
	records := make([]map[string]interface{}, len(docs))
	for i, doc := range docs {
		rec := map[string]interface{}{
			"id":        doc.ID,
			"content":   doc.Content,
			"metadata":  payloadToJSON(doc.Payload),
			"embedding": toFloat32Slice(doc.Vector),
		}
		records[i] = rec
	}

	// Try merge_insert endpoint for upsert semantics.
	mergeEndpoint := fmt.Sprintf("%s/v1/table/%s/merge_insert/", c.baseURL(), collectionName)
	insertEndpoint := fmt.Sprintf("%s/v1/table/%s/insert/", c.baseURL(), collectionName)

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body := map[string]interface{}{
			"data": records,
			"on":   "id",
		}
		_, status, err := c.doRequest(ctx, http.MethodPost, mergeEndpoint, body)
		if err != nil {
			return err
		}
		if status >= 200 && status < 300 {
			return nil
		}
		// Fallback to insert endpoint
		body2 := map[string]interface{}{
			"data": records,
			"mode": "append",
		}
		_, status2, err2 := c.doRequest(ctx, http.MethodPost, insertEndpoint, body2)
		if err2 != nil {
			return err2
		}
		if status2 >= 200 && status2 < 300 {
			return nil
		}
		return newError(ErrCodeProviderError,
			fmt.Sprintf("UpsertDocuments failed (HTTP merge=%d insert=%d)", status, status2), nil)
	}); err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments failed", err)
	}
	return nil
}

// GetDocument retrieves a single document by ID using a filter query.
func (c *lanceDBClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	if collectionName == "" {
		return nil, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if id == "" {
		return nil, newError(ErrCodeInvalidDocumentID, "", nil)
	}

	endpoint := fmt.Sprintf("%s/v1/table/%s/query/", c.baseURL(), collectionName)
	reqBody := map[string]interface{}{
		"prefilter": true,
		"filter":    fmt.Sprintf("id = '%s'", strings.ReplaceAll(id, "'", "''")),
		"limit":     1,
	}

	var doc *Document
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequestChecked(ctx, http.MethodPost, endpoint, reqBody, ErrCodeCollectionNotFound)
		if err != nil {
			return err
		}
		results, parseErr := parseQueryResponse(body)
		if parseErr != nil {
			return parseErr
		}
		if len(results) == 0 {
			return newError(ErrCodeDocumentNotFound,
				fmt.Sprintf("document %q not found in collection %q", id, collectionName), nil)
		}
		doc = &results[0]
		return nil
	}); err != nil {
		return nil, err
	}
	return doc, nil
}

// DeleteDocuments removes documents by their IDs using a SQL-like predicate.
func (c *lanceDBClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if collectionName == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if len(ids) == 0 {
		return nil
	}

	// Build SQL predicate: id IN ('id1','id2',...)
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "'" + strings.ReplaceAll(id, "'", "''") + "'"
	}
	predicate := fmt.Sprintf("id IN (%s)", strings.Join(quoted, ","))

	endpoint := fmt.Sprintf("%s/v1/table/%s/delete/", c.baseURL(), collectionName)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, err := c.doRequestChecked(ctx, http.MethodPost, endpoint,
			map[string]interface{}{"predicate": predicate}, ErrCodeCollectionNotFound)
		return err
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

// DeleteByFilter removes documents matching the given filter.
// Returns -1 because LanceDB does not report a delete count.
func (c *lanceDBClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if collectionName == "" {
		return 0, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if len(filters) == 0 {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter requires at least one filter", nil)
	}

	predicate := buildLanceDBPredicate(filters)
	endpoint := fmt.Sprintf("%s/v1/table/%s/delete/", c.baseURL(), collectionName)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, err := c.doRequestChecked(ctx, http.MethodPost, endpoint,
			map[string]interface{}{"predicate": predicate}, ErrCodeCollectionNotFound)
		return err
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter failed", err)
	}
	return -1, nil // LanceDB does not report count
}

// CountDocuments returns the row count for the collection.
func (c *lanceDBClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if collectionName == "" {
		return 0, newError(ErrCodeInvalidCollectionName, "", nil)
	}

	// If filters are provided, use query to count results.
	if len(filters) > 0 {
		predicate := buildLanceDBPredicate(filters)
		endpoint := fmt.Sprintf("%s/v1/table/%s/query/", c.baseURL(), collectionName)
		reqBody := map[string]interface{}{
			"prefilter": true,
			"filter":    predicate,
			"limit":     1_000_000, // large limit to count all
		}
		var count int64
		if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
			body, err := c.doRequestChecked(ctx, http.MethodPost, endpoint, reqBody, ErrCodeCollectionNotFound)
			if err != nil {
				return err
			}
			results, parseErr := parseQueryResponse(body)
			if parseErr != nil {
				return parseErr
			}
			count = int64(len(results))
			return nil
		}); err != nil {
			return 0, newError(ErrCodeProviderError, "CountDocuments (filtered) failed", err)
		}
		return count, nil
	}

	// No filter: use count_rows endpoint.
	endpoint := fmt.Sprintf("%s/v1/table/%s/count_rows/", c.baseURL(), collectionName)
	var count int64
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequestChecked(ctx, http.MethodGet, endpoint, nil, ErrCodeCollectionNotFound)
		if err != nil {
			return err
		}
		// Try plain integer
		if n, err2 := strconv.ParseInt(strings.TrimSpace(string(body)), 10, 64); err2 == nil {
			count = n
			return nil
		}
		// Try {"count": N}
		var resp struct {
			Count int64 `json:"count"`
		}
		if err2 := json.Unmarshal(body, &resp); err2 == nil {
			count = resp.Count
			return nil
		}
		// Try plain number in JSON
		var n float64
		if err2 := json.Unmarshal(body, &n); err2 == nil {
			count = int64(n)
			return nil
		}
		return fmt.Errorf("lancedb: unexpected CountDocuments response: %s", string(body))
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
	}
	return count, nil
}

// ScrollDocuments paginates through all documents.
func (c *lanceDBClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	if req.CollectionName == "" {
		return nil, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	offset := 0
	if req.Offset != "" {
		if n, err := strconv.Atoi(req.Offset); err == nil && n > 0 {
			offset = n
		}
	}

	endpoint := fmt.Sprintf("%s/v1/table/%s/query/", c.baseURL(), req.CollectionName)
	reqBody := map[string]interface{}{
		"limit":        limit,
		"offset":       offset,
		"with_row_id":  false,
	}
	if len(req.Filters) > 0 {
		reqBody["filter"] = buildLanceDBPredicate(req.Filters)
		reqBody["prefilter"] = true
	}

	var result *ScrollResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequestChecked(ctx, http.MethodPost, endpoint, reqBody, ErrCodeCollectionNotFound)
		if err != nil {
			return err
		}
		docs, parseErr := parseQueryResponse(body)
		if parseErr != nil {
			return parseErr
		}

		nextOffset := ""
		if len(docs) == limit {
			nextOffset = strconv.Itoa(offset + len(docs))
		}
		result = &ScrollResult{
			Documents:  docs,
			NextOffset: nextOffset,
			Total:      -1,
		}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}
	return result, nil
}

// ── Search ────────────────────────────────────────────────────────────────────

// VectorSearch performs ANN search in LanceDB.
func (c *lanceDBClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/v1/table/%s/query/", c.baseURL(), req.CollectionName)

	// Copy filters to avoid mutating the caller's map.
	filters := make(map[string]interface{}, len(req.Filters))
	for k, v := range req.Filters {
		filters[k] = v
	}
	metric := lanceDBMetric(filters)
	delete(filters, "_metric")

	reqBody := map[string]interface{}{
		"vector":      toFloat32Slice(req.QueryVector),
		"limit":       req.TopK,
		"metric":      metric,
		"with_row_id": false,
	}
	if len(filters) > 0 {
		reqBody["filter"] = buildLanceDBPredicate(filters)
		reqBody["prefilter"] = true
	}

	var results []SearchResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequestChecked(ctx, http.MethodPost, endpoint, reqBody, ErrCodeCollectionNotFound)
		if err != nil {
			return err
		}
		results = parseSearchResponse(body, req.ScoreThreshold, req.SkipPayload, req.WithVectors, metric)
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}
	return results, nil
}

// HybridSearch performs combined dense vector + full-text search using RRF fusion.
func (c *lanceDBClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	if len(req.QueryVector) == 0 && req.QueryText == "" {
		return nil, newError(ErrCodeInvalidQueryVector, "HybridSearch requires queryVector or queryText", nil)
	}
	if req.TopK <= 0 {
		return nil, newError(ErrCodeInvalidTopK, fmt.Sprintf("topK=%d must be > 0", req.TopK), nil)
	}

	// If no vector, fall back to text-only FTS.
	if len(req.QueryVector) == 0 {
		return c.fullTextSearch(ctx, req)
	}
	// If no text, fall back to vector-only search.
	if req.QueryText == "" {
		return c.VectorSearch(ctx, SearchRequest{
			CollectionName: req.CollectionName,
			QueryVector:    req.QueryVector,
			TopK:           req.TopK,
			ScoreThreshold: req.ScoreThreshold,
			Filters:        req.Filters,
			SkipPayload:    req.SkipPayload,
		})
	}

	// Run both searches in parallel.
	candidateK := req.TopK * 2
	if candidateK < 20 {
		candidateK = 20
	}

	// Dense search results.
	var vectorResults []SearchResult
	var textResults []SearchResult

	// Dense search.
	vEndpoint := fmt.Sprintf("%s/v1/table/%s/query/", c.baseURL(), req.CollectionName)
	vBody := map[string]interface{}{
		"vector":      toFloat32Slice(req.QueryVector),
		"limit":       candidateK,
		"metric":      "cosine",
		"with_row_id": false,
	}
	if len(req.Filters) > 0 {
		vBody["filter"] = buildLanceDBPredicate(req.Filters)
		vBody["prefilter"] = true
	}
	vRespBody, err := c.doRequestChecked(ctx, http.MethodPost, vEndpoint, vBody, ErrCodeCollectionNotFound)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "HybridSearch (dense) failed", err)
	}
	vectorResults = parseSearchResponse(vRespBody, 0, false, false, "cosine")

	// Full-text search (may fail if not indexed — fall back to vector-only).
	tEndpoint := fmt.Sprintf("%s/v1/table/%s/query/", c.baseURL(), req.CollectionName)
	tBody := map[string]interface{}{
		"full_text_search": map[string]interface{}{
			"query":   req.QueryText,
			"columns": []string{"content"},
		},
		"limit":       candidateK,
		"with_row_id": false,
	}
	if len(req.Filters) > 0 {
		tBody["filter"] = buildLanceDBPredicate(req.Filters)
		tBody["prefilter"] = true
	}
	tRespBody, tErr := c.doRequestChecked(ctx, http.MethodPost, tEndpoint, tBody, ErrCodeCollectionNotFound)
	if tErr == nil {
		textResults = parseSearchResponse(tRespBody, 0, false, false, "")
	}
	// If FTS fails, use vector results only.
	if tErr != nil || len(textResults) == 0 {
		// Return vector-only results filtered by threshold.
		out := make([]SearchResult, 0, len(vectorResults))
		for _, r := range vectorResults {
			if req.ScoreThreshold > 0 && r.Score < req.ScoreThreshold {
				continue
			}
			if req.SkipPayload {
				r.Payload = nil
				r.Content = ""
			}
			out = append(out, r)
		}
		if len(out) > req.TopK {
			out = out[:req.TopK]
		}
		return out, nil
	}

	// Apply RRF fusion.
	alpha := req.Alpha
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	results := rrfFuse(vectorResults, textResults, alpha, req.TopK, req.ScoreThreshold, req.SkipPayload)
	return results, nil
}

// fullTextSearch runs FTS-only search as a fallback.
func (c *lanceDBClient) fullTextSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	endpoint := fmt.Sprintf("%s/v1/table/%s/query/", c.baseURL(), req.CollectionName)
	reqBody := map[string]interface{}{
		"full_text_search": map[string]interface{}{
			"query":   req.QueryText,
			"columns": []string{"content"},
		},
		"limit":       req.TopK,
		"with_row_id": false,
	}
	var results []SearchResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		body, err := c.doRequestChecked(ctx, http.MethodPost, endpoint, reqBody, ErrCodeCollectionNotFound)
		if err != nil {
			return err
		}
		results = parseSearchResponse(body, req.ScoreThreshold, req.SkipPayload, false, "")
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "fullTextSearch failed", err)
	}
	return results, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// lanceDBMetric converts generic metric from filters or defaults to "cosine".
func lanceDBMetric(filters map[string]interface{}) string {
	if metricRaw, ok := filters["_metric"]; ok {
		if metric, ok := metricRaw.(string); ok {
			switch strings.ToLower(metric) {
			case "cosine":
				return "cosine"
			case "dot":
				return "dot"
			case "euclidean", "l2":
				return "l2"
			}
		}
	}
	return "cosine"
}

// payloadToJSON serializes a payload map to a JSON string.
func payloadToJSON(payload map[string]interface{}) string {
	if len(payload) == 0 {
		return ""
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(b)
}

// jsonToPayload deserializes a JSON string to a payload map.
func jsonToPayload(s string) map[string]interface{} {
	if s == "" {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

// lanceDBRecord is the internal representation of a LanceDB query result row.
type lanceDBRecord struct {
	ID        string      `json:"id"`
	Content   string      `json:"content"`
	Metadata  string      `json:"metadata"`
	Embedding interface{} `json:"embedding"`
	Distance  float64     `json:"_distance"`
}

// parseQueryResponse parses a LanceDB query response into Documents.
func parseQueryResponse(body []byte) ([]Document, error) {
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	// Try {"data": [...]} format.
	if err := json.Unmarshal(body, &resp); err == nil && resp.Data != nil {
		return recordsToDocuments(resp.Data), nil
	}
	// Try plain array format.
	var arr []map[string]interface{}
	if err := json.Unmarshal(body, &arr); err == nil {
		return recordsToDocuments(arr), nil
	}
	return []Document{}, nil
}

// recordsToDocuments converts raw map records to Documents.
func recordsToDocuments(records []map[string]interface{}) []Document {
	docs := make([]Document, 0, len(records))
	for _, rec := range records {
		doc := Document{}
		if id, ok := rec["id"].(string); ok {
			doc.ID = id
		}
		if content, ok := rec["content"].(string); ok {
			doc.Content = content
		}
		if metadata, ok := rec["metadata"].(string); ok {
			doc.Payload = jsonToPayload(metadata)
		}
		if emb, ok := rec["embedding"]; ok {
			doc.Vector = extractVector(emb)
		}
		docs = append(docs, doc)
	}
	return docs
}

// extractVector extracts a float64 vector from various JSON formats.
func extractVector(v interface{}) []float64 {
	switch val := v.(type) {
	case []interface{}:
		out := make([]float64, len(val))
		for i, f := range val {
			switch fv := f.(type) {
			case float64:
				out[i] = fv
			case float32:
				out[i] = float64(fv)
			}
		}
		return out
	case []float64:
		return val
	case []float32:
		return toFloat64Slice(val)
	}
	return nil
}

// parseSearchResponse parses a LanceDB query response into SearchResults.
// metric must be "cosine" (default), "l2", or "dot" to compute the correct score.
func parseSearchResponse(body []byte, scoreThreshold float64, skipPayload, withVectors bool, metric string) []SearchResult {
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	var records []map[string]interface{}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Data != nil {
		records = resp.Data
	} else {
		// Plain array.
		_ = json.Unmarshal(body, &records)
	}

	results := make([]SearchResult, 0, len(records))
	for _, rec := range records {
		distance := 0.0
		if d, ok := rec["_distance"].(float64); ok {
			distance = d
		}
		var score float64
		switch metric {
		case "l2":
			// L2 distance: smaller is better; map to 0-1 similarity.
			score = 1.0 / (1.0 + distance)
		case "dot":
			// LanceDB negates dot product so smaller _distance = higher similarity.
			score = -distance
		default:
			// cosine distance (0 = identical, 2 = opposite) → cosine similarity.
			score = 1.0 - distance
		}

		if scoreThreshold > 0 && score < scoreThreshold {
			continue
		}

		sr := SearchResult{Score: score}
		if id, ok := rec["id"].(string); ok {
			sr.ID = id
		}
		if !skipPayload {
			if content, ok := rec["content"].(string); ok {
				sr.Content = content
			}
			if metadata, ok := rec["metadata"].(string); ok {
				sr.Payload = jsonToPayload(metadata)
			}
		}
		if withVectors {
			if emb, ok := rec["embedding"]; ok {
				sr.Vector = extractVector(emb)
			}
		}
		results = append(results, sr)
	}
	return results
}

// rrfFuse applies Reciprocal Rank Fusion to combine dense and text search results.
func rrfFuse(vectorResults, textResults []SearchResult, alpha float64, topK int, scoreThreshold float64, skipPayload bool) []SearchResult {
	type rrfEntry struct {
		id      string
		score   float64
		content string
		payload map[string]interface{}
	}

	scores := make(map[string]*rrfEntry)

	for rank, r := range vectorResults {
		entry, ok := scores[r.ID]
		if !ok {
			entry = &rrfEntry{id: r.ID, content: r.Content, payload: r.Payload}
			scores[r.ID] = entry
		}
		entry.score += alpha * (1.0 / float64(60+rank+1))
	}
	for rank, r := range textResults {
		entry, ok := scores[r.ID]
		if !ok {
			entry = &rrfEntry{id: r.ID, content: r.Content, payload: r.Payload}
			scores[r.ID] = entry
		}
		entry.score += (1.0 - alpha) * (1.0 / float64(60+rank+1))
	}

	// Sort by score descending.
	entries := make([]*rrfEntry, 0, len(scores))
	for _, e := range scores {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].score > entries[j].score
	})

	results := make([]SearchResult, 0, topK)
	for _, e := range entries {
		if len(results) >= topK {
			break
		}
		if scoreThreshold > 0 && e.score < scoreThreshold {
			continue
		}
		sr := SearchResult{ID: e.id, Score: e.score}
		if !skipPayload {
			sr.Content = e.content
			sr.Payload = e.payload
		}
		results = append(results, sr)
	}
	return results
}

// buildLanceDBPredicate converts a generic filter map to a LanceDB SQL-like predicate.
// Since metadata is stored as a JSON string column, string equality is matched with LIKE.
// Numeric range operators ($gt/$gte/$lt/$lte) are not supported and are skipped.
// $in is expanded as OR-joined LIKE clauses.
func buildLanceDBPredicate(filters map[string]interface{}) string {
	clauses := make([]string, 0, len(filters))
	for key, value := range filters {
		switch v := value.(type) {
		case map[string]interface{}:
			for op, opVal := range v {
				switch op {
				case "$eq":
					clauses = append(clauses, lanceDBLikeExpr(key, opVal))
				case "$ne":
					clauses = append(clauses, fmt.Sprintf(`metadata NOT LIKE '%%"%s":%s%%'`, key, lanceDBLikeValue(opVal)))
				case "$in":
					if orClause := lanceDBInClause(key, opVal); orClause != "" {
						clauses = append(clauses, "("+orClause+")")
					}
				case "$nin":
					if orClause := lanceDBInClause(key, opVal); orClause != "" {
						clauses = append(clauses, "NOT ("+orClause+")")
					}
				// $gt/$gte/$lt/$lte require JSON extraction which LanceDB/DataFusion
				// does not support on plain string columns — skip silently.
				}
			}
		default:
			clauses = append(clauses, lanceDBLikeExpr(key, v))
		}
	}
	if len(clauses) == 0 {
		return ""
	}
	return strings.Join(clauses, " AND ")
}

// lanceDBLikeExpr builds a LIKE clause for a single key=value equality match.
func lanceDBLikeExpr(key string, val interface{}) string {
	return fmt.Sprintf(`metadata LIKE '%%"%s":%s%%'`, key, lanceDBLikeValue(val))
}

// lanceDBLikeValue formats a value for use inside a LIKE pattern.
// Strings are quoted; numbers and booleans are unquoted (matching JSON serialisation).
func lanceDBLikeValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		escaped := strings.ReplaceAll(v, "'", "''")
		return fmt.Sprintf(`"%s"`, escaped)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// lanceDBInClause builds an OR-joined set of LIKE clauses for $in matching.
func lanceDBInClause(key string, val interface{}) string {
	var items []interface{}
	switch v := val.(type) {
	case []interface{}:
		items = v
	case []string:
		items = make([]interface{}, len(v))
		for i, s := range v {
			items[i] = s
		}
	default:
		return lanceDBLikeExpr(key, val)
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, lanceDBLikeExpr(key, item))
	}
	return strings.Join(parts, " OR ")
}
