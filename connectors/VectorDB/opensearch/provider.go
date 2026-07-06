package vectordb

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// openSearchClient implements VectorDBClient for OpenSearch via pure stdlib HTTP.
type openSearchClient struct {
	cfg        ConnectionConfig
	httpClient *http.Client
	baseURL    string
}

// Compile-time proof that openSearchClient satisfies the full VectorDBClient interface.
var _ VectorDBClient = (*openSearchClient)(nil)

func newOpenSearchClient(cfg ConnectionConfig) (VectorDBClient, error) {
	scheme := "http"
	if cfg.UseTLS {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.TLSInsecureSkipVerify}, //nolint:gosec
	}
	httpClient := &http.Client{
		Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
		Transport: transport,
	}

	return &openSearchClient{
		cfg:        cfg,
		httpClient: httpClient,
		baseURL:    baseURL,
	}, nil
}

func (c *openSearchClient) DBType() string { return "opensearch" }

func (c *openSearchClient) Close() error {
	// HTTP client does not require explicit closing.
	return nil
}

// ── HTTP helper ──────────────────────────────────────────────────────────────

// doRequest performs a single HTTP request against the OpenSearch API.
// Returns (responseBody, statusCode, error).
func (c *openSearchClient) doRequest(ctx context.Context, method, endpoint string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("opensearch: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("opensearch: create request: %w", err)
	}

	if c.cfg.Username != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, newError(ErrCodeConnectionFailed, "OpenSearch HTTP request failed", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB cap
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("opensearch: read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// doRequestHandleErrors performs a request and maps status codes to VDBErrors.
func (c *openSearchClient) doRequestHandleErrors(ctx context.Context, method, endpoint string, body interface{}) ([]byte, error) {
	respBody, statusCode, err := c.doRequest(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	return c.handleStatusCode(statusCode, respBody)
}

// handleStatusCode maps HTTP status codes to VDBErrors.
func (c *openSearchClient) handleStatusCode(statusCode int, body []byte) ([]byte, error) {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return body, nil
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return nil, newError(ErrCodeAuthFailed,
			fmt.Sprintf("OpenSearch authentication failed (HTTP %d)", statusCode), nil)
	case statusCode == http.StatusNotFound:
		return nil, newError(ErrCodeCollectionNotFound,
			fmt.Sprintf("OpenSearch resource not found (HTTP %d): %s", statusCode, string(body)), nil)
	case statusCode == http.StatusBadRequest:
		// Check for resource_already_exists_exception
		if strings.Contains(string(body), "resource_already_exists_exception") {
			return nil, newError(ErrCodeCollectionExists,
				"OpenSearch index already exists", nil)
		}
		return nil, newError(ErrCodeProviderError,
			fmt.Sprintf("OpenSearch bad request (HTTP %d): %s", statusCode, string(body)), nil)
	default:
		return nil, newError(ErrCodeProviderError,
			fmt.Sprintf("OpenSearch API error (HTTP %d): %s", statusCode, string(body)), nil)
	}
}

// ── Health Check ─────────────────────────────────────────────────────────────

// HealthCheck calls GET /_cluster/health?timeout=5s and returns nil if the
// cluster is reachable (any status).
func (c *openSearchClient) HealthCheck(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/_cluster/health?timeout=5s", c.baseURL)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, statusCode, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		if statusCode != http.StatusOK {
			return newError(ErrCodeConnectionFailed,
				fmt.Sprintf("OpenSearch health check returned HTTP %d", statusCode), nil)
		}
		return nil
	}); err != nil {
		return newError(ErrCodeConnectionFailed, "OpenSearch health check failed", err)
	}
	return nil
}

// ── Collection (Index) management ───────────────────────────────────────────

// CollectionExists reports whether the named OpenSearch index exists.
// Uses HEAD /{index}: 200 = true, 404 = false.
func (c *openSearchClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	endpoint := fmt.Sprintf("%s/%s", c.baseURL, name)
	var exists bool
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, statusCode, err := c.doRequest(ctx, http.MethodHead, endpoint, nil)
		if err != nil {
			// If it's a connection error, propagate
			if vErr, ok := err.(*VDBError); ok && vErr.Code == ErrCodeConnectionFailed {
				return err
			}
			return err
		}
		switch statusCode {
		case http.StatusOK:
			exists = true
		case http.StatusNotFound:
			exists = false
		default:
			return newError(ErrCodeProviderError,
				fmt.Sprintf("OpenSearch CollectionExists returned unexpected HTTP %d", statusCode), nil)
		}
		return nil
	}); err != nil {
		return false, newError(ErrCodeProviderError, "CollectionExists failed", err)
	}
	return exists, nil
}

// CreateCollection creates a new OpenSearch index with knn_vector mapping.
func (c *openSearchClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}

	// Map generic metric to OpenSearch space_type.
	spaceType := "cosinesimil"
	switch strings.ToLower(cfg.DistanceMetric) {
	case "dot":
		spaceType = "innerproduct"
	case "euclidean":
		spaceType = "l2"
	case "cosine":
		spaceType = "cosinesimil"
	}

	reqBody := map[string]interface{}{
		"settings": map[string]interface{}{
			"index.knn": true,
		},
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type": "keyword",
				},
				"content": map[string]interface{}{
					"type":     "text",
					"analyzer": "standard",
				},
				"metadata": map[string]interface{}{
					"type":    "object",
					"dynamic": true,
				},
				"embedding": map[string]interface{}{
					"type":      "knn_vector",
					"dimension": cfg.Dimensions,
					"method": map[string]interface{}{
						"name":       "hnsw",
						"space_type": spaceType,
						"engine":     "lucene",
						"parameters": map[string]interface{}{
							"ef_construction": 128,
							"m":               16,
						},
					},
				},
			},
		},
	}

	endpoint := fmt.Sprintf("%s/%s", c.baseURL, cfg.Name)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, reqErr := c.doRequest(ctx, http.MethodPut, endpoint, reqBody)
		if reqErr != nil {
			return reqErr
		}
		if statusCode >= 200 && statusCode < 300 {
			return nil
		}
		if statusCode == http.StatusBadRequest && strings.Contains(string(respBody), "resource_already_exists_exception") {
			return newError(ErrCodeCollectionExists, "OpenSearch index already exists", nil)
		}
		_, handleErr := c.handleStatusCode(statusCode, respBody)
		return handleErr
	}); err != nil {
		if vErr, ok := err.(*VDBError); ok {
			return vErr
		}
		return newError(ErrCodeProviderError, "CreateCollection failed", err)
	}
	return nil
}

// DeleteCollection deletes an OpenSearch index.
func (c *openSearchClient) DeleteCollection(ctx context.Context, name string) error {
	endpoint := fmt.Sprintf("%s/%s", c.baseURL, name)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, reqErr := c.doRequest(ctx, http.MethodDelete, endpoint, nil)
		if reqErr != nil {
			return reqErr
		}
		if statusCode == http.StatusNotFound {
			return newError(ErrCodeCollectionNotFound, "index not found", nil)
		}
		_, err := c.handleStatusCode(statusCode, respBody)
		return err
	}); err != nil {
		if vErr, ok := err.(*VDBError); ok {
			return vErr
		}
		return newError(ErrCodeProviderError, "DeleteCollection failed", err)
	}
	return nil
}

// ListCollections returns the names of all user-visible OpenSearch indexes.
// System indexes (starting with '.') are excluded.
func (c *openSearchClient) ListCollections(ctx context.Context) ([]string, error) {
	endpoint := fmt.Sprintf("%s/_cat/indices?format=json&h=index", c.baseURL)
	var names []string
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, reqErr := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		if reqErr != nil {
			return reqErr
		}
		if _, err := c.handleStatusCode(statusCode, respBody); err != nil {
			return err
		}
		var indices []struct {
			Index string `json:"index"`
		}
		if err := json.Unmarshal(respBody, &indices); err != nil {
			return fmt.Errorf("opensearch: parse list indices response: %w", err)
		}
		names = make([]string, 0, len(indices))
		for _, idx := range indices {
			// Filter out system indexes (prefixed with '.')
			if !strings.HasPrefix(idx.Index, ".") {
				names = append(names, idx.Index)
			}
		}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	return names, nil
}

// ── Document operations ──────────────────────────────────────────────────────

// UpsertDocuments inserts or updates documents in the given OpenSearch index via _bulk API.
func (c *openSearchClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatchOpenSearch, "opensearch"); err != nil {
		return err
	}

	// Build NDJSON bulk body
	var buf bytes.Buffer
	for _, doc := range docs {
		// Action line
		actionLine := map[string]interface{}{
			"index": map[string]interface{}{"_id": doc.ID},
		}
		actionBytes, _ := json.Marshal(actionLine)
		buf.Write(actionBytes)
		buf.WriteByte('\n')

		// Document line
		docLine := map[string]interface{}{
			"id":        doc.ID,
			"content":   doc.Content,
			"metadata":  doc.Payload,
			"embedding": doc.Vector,
		}
		docBytes, _ := json.Marshal(docLine)
		buf.Write(docBytes)
		buf.WriteByte('\n')
	}

	endpoint := fmt.Sprintf("%s/%s/_bulk", c.baseURL, collectionName)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf.Bytes()))
		if err != nil {
			return fmt.Errorf("opensearch: create bulk request: %w", err)
		}
		if c.cfg.Username != "" {
			req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
		}
		req.Header.Set("Content-Type", "application/x-ndjson")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return newError(ErrCodeConnectionFailed, "OpenSearch bulk request failed", err)
		}
		defer resp.Body.Close()

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if readErr != nil {
			return newError(ErrCodeProviderError, "UpsertDocuments: read response body failed", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, handleErr := c.handleStatusCode(resp.StatusCode, respBody)
			return handleErr
		}

		// Check for bulk errors in response body
		var bulkResp struct {
			Errors bool `json:"errors"`
			Items  []map[string]struct {
				Error *struct {
					Reason string `json:"reason"`
				} `json:"error,omitempty"`
			} `json:"items"`
		}
		if err := json.Unmarshal(respBody, &bulkResp); err != nil {
			return fmt.Errorf("opensearch: parse bulk response: %w", err)
		}
		if bulkResp.Errors {
			var errs []string
			for _, item := range bulkResp.Items {
				for _, op := range item {
					if op.Error != nil && op.Error.Reason != "" {
						errs = append(errs, op.Error.Reason)
					}
				}
			}
			if len(errs) > 0 {
				return newError(ErrCodeProviderError, fmt.Sprintf("OpenSearch bulk upsert: %d item(s) failed: %s", len(errs), strings.Join(errs, "; ")), nil)
			}
			return newError(ErrCodeProviderError, "OpenSearch bulk upsert reported errors", nil)
		}
		return nil
	}); err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments failed", err)
	}
	return nil
}

// GetDocument retrieves a single document by ID from the given OpenSearch index.
func (c *openSearchClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	endpoint := fmt.Sprintf("%s/%s/_doc/%s", c.baseURL, collectionName, id)
	var doc *Document
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, reqErr := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		if reqErr != nil {
			return reqErr
		}
		if statusCode == http.StatusNotFound {
			return newError(ErrCodeDocumentNotFound,
				fmt.Sprintf("document %q not found in index %q", id, collectionName), nil)
		}
		if _, err := c.handleStatusCode(statusCode, respBody); err != nil {
			return err
		}

		var resp struct {
			Found  bool                   `json:"found"`
			Source map[string]interface{} `json:"_source"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return fmt.Errorf("opensearch: parse get document response: %w", err)
		}
		if !resp.Found {
			return newError(ErrCodeDocumentNotFound,
				fmt.Sprintf("document %q not found in index %q", id, collectionName), nil)
		}

		doc = openSearchSourceToDocument(id, resp.Source)
		return nil
	}); err != nil {
		if vErr, ok := err.(*VDBError); ok {
			return nil, vErr
		}
		return nil, newError(ErrCodeProviderError, "GetDocument failed", err)
	}
	return doc, nil
}

// DeleteDocuments removes documents by ID list from the given OpenSearch index via _bulk delete.
func (c *openSearchClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// Build NDJSON bulk body with delete actions
	var buf bytes.Buffer
	for _, id := range ids {
		actionLine := map[string]interface{}{
			"delete": map[string]interface{}{"_id": id},
		}
		actionBytes, _ := json.Marshal(actionLine)
		buf.Write(actionBytes)
		buf.WriteByte('\n')
	}

	endpoint := fmt.Sprintf("%s/%s/_bulk", c.baseURL, collectionName)
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf.Bytes()))
		if err != nil {
			return fmt.Errorf("opensearch: create bulk delete request: %w", err)
		}
		if c.cfg.Username != "" {
			req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
		}
		req.Header.Set("Content-Type", "application/x-ndjson")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return newError(ErrCodeConnectionFailed, "OpenSearch bulk delete request failed", err)
		}
		defer resp.Body.Close()

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if readErr != nil {
			return newError(ErrCodeProviderError, "DeleteDocuments: read response body failed", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, handleErr := c.handleStatusCode(resp.StatusCode, respBody)
			return handleErr
		}
		return nil
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

// DeleteByFilter removes documents matching the given filter using _delete_by_query.
func (c *openSearchClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if collectionName == "" {
		return 0, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if len(filters) == 0 {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter requires at least one filter to prevent accidental full-collection deletion", nil)
	}
	query := buildOpenSearchFilter(filters)
	reqBody := map[string]interface{}{
		"query": query,
	}

	endpoint := fmt.Sprintf("%s/%s/_delete_by_query", c.baseURL, collectionName)
	var deleted int64
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, reqErr := c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		if reqErr != nil {
			return reqErr
		}
		if _, err := c.handleStatusCode(statusCode, respBody); err != nil {
			return err
		}
		var resp struct {
			Deleted int64 `json:"deleted"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return fmt.Errorf("opensearch: parse delete_by_query response: %w", err)
		}
		deleted = resp.Deleted
		return nil
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter failed", err)
	}
	return deleted, nil
}

// CountDocuments returns the total count of documents matching the optional filter.
func (c *openSearchClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	query := buildOpenSearchFilter(filters)
	reqBody := map[string]interface{}{
		"query": query,
	}

	endpoint := fmt.Sprintf("%s/%s/_count", c.baseURL, collectionName)
	var count int64
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, reqErr := c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		if reqErr != nil {
			return reqErr
		}
		if _, err := c.handleStatusCode(statusCode, respBody); err != nil {
			return err
		}
		var resp struct {
			Count int64 `json:"count"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return fmt.Errorf("opensearch: parse count response: %w", err)
		}
		count = resp.Count
		return nil
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
	}
	return count, nil
}

// ScrollDocuments paginates through documents using from/size pagination.
func (c *openSearchClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	from := 0
	if req.Offset != "" {
		fmt.Sscanf(req.Offset, "%d", &from)
	}

	query := buildOpenSearchFilter(req.Filters)
	reqBody := map[string]interface{}{
		"from":  from,
		"size":  limit,
		"query": query,
	}

	endpoint := fmt.Sprintf("%s/%s/_search", c.baseURL, req.CollectionName)
	var result *ScrollResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, reqErr := c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		if reqErr != nil {
			return reqErr
		}
		if _, err := c.handleStatusCode(statusCode, respBody); err != nil {
			return err
		}

		var searchResp struct {
			Hits struct {
				Total struct {
					Value int64 `json:"value"`
				} `json:"total"`
				Hits []struct {
					ID     string                 `json:"_id"`
					Source map[string]interface{} `json:"_source"`
				} `json:"hits"`
			} `json:"hits"`
		}
		if err := json.Unmarshal(respBody, &searchResp); err != nil {
			return fmt.Errorf("opensearch: parse scroll response: %w", err)
		}

		docs := make([]Document, 0, len(searchResp.Hits.Hits))
		for _, hit := range searchResp.Hits.Hits {
			docs = append(docs, *openSearchSourceToDocument(hit.ID, hit.Source))
		}

		nextOffset := ""
		newFrom := from + len(searchResp.Hits.Hits)
		if int64(newFrom) < searchResp.Hits.Total.Value {
			nextOffset = fmt.Sprintf("%d", newFrom)
		}

		result = &ScrollResult{
			Documents:  docs,
			NextOffset: nextOffset,
			Total:      searchResp.Hits.Total.Value,
		}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}
	return result, nil
}

// ── Search ───────────────────────────────────────────────────────────────────

// VectorSearch performs k-NN vector similarity search in an OpenSearch index.
func (c *openSearchClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	knnQuery := map[string]interface{}{
		"knn": map[string]interface{}{
			"embedding": map[string]interface{}{
				"vector": req.QueryVector,
				"k":      req.TopK,
			},
		},
	}

	var query map[string]interface{}
	if len(req.Filters) > 0 {
		filterQuery := buildOpenSearchFilter(req.Filters)
		// Wrap knn + filter in bool.must
		query = map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []interface{}{
					knnQuery,
					filterQuery,
				},
			},
		}
	} else {
		query = knnQuery
	}

	reqBody := map[string]interface{}{
		"size":  req.TopK,
		"query": query,
	}

	endpoint := fmt.Sprintf("%s/%s/_search", c.baseURL, req.CollectionName)
	var results []SearchResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, reqErr := c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		if reqErr != nil {
			return reqErr
		}
		if _, err := c.handleStatusCode(statusCode, respBody); err != nil {
			return err
		}

		results = parseSearchResponse(respBody, req.ScoreThreshold, req.SkipPayload, req.WithVectors)
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}
	return results, nil
}

// HybridSearch performs combined dense + BM25 full-text search using bool/should.
func (c *openSearchClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	if len(req.QueryVector) == 0 && req.QueryText == "" {
		return nil, newError(ErrCodeInvalidQueryVector, "HybridSearch requires queryVector or queryText", nil)
	}
	if req.TopK <= 0 {
		return nil, newError(ErrCodeInvalidTopK, fmt.Sprintf("topK=%d must be > 0", req.TopK), nil)
	}

	// Alpha: 0.0 = pure BM25, 1.0 = pure dense vector, 0.5 = balanced
	alpha := req.Alpha
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	sparseBoost := 1 - alpha
	denseBoost := alpha

	shouldClauses := make([]interface{}, 0, 2)

	if len(req.QueryVector) > 0 {
		knnClause := map[string]interface{}{
			"knn": map[string]interface{}{
				"embedding": map[string]interface{}{
					"vector": req.QueryVector,
					"k":      req.TopK,
					"boost":  denseBoost,
				},
			},
		}
		shouldClauses = append(shouldClauses, knnClause)
	}

	if req.QueryText != "" {
		textClause := map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":  req.QueryText,
				"fields": []string{"content"},
				"boost":  sparseBoost,
			},
		}
		shouldClauses = append(shouldClauses, textClause)
	}

	boolQuery := map[string]interface{}{
		"should":               shouldClauses,
		"minimum_should_match": 1,
	}
	if len(req.Filters) > 0 {
		boolQuery["filter"] = []interface{}{buildOpenSearchFilter(req.Filters)}
	}
	query := map[string]interface{}{
		"bool": boolQuery,
	}

	reqBody := map[string]interface{}{
		"size":  req.TopK,
		"query": query,
	}

	endpoint := fmt.Sprintf("%s/%s/_search", c.baseURL, req.CollectionName)
	var results []SearchResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		respBody, statusCode, reqErr := c.doRequest(ctx, http.MethodPost, endpoint, reqBody)
		if reqErr != nil {
			return reqErr
		}
		if _, err := c.handleStatusCode(statusCode, respBody); err != nil {
			return err
		}

		results = parseSearchResponse(respBody, req.ScoreThreshold, req.SkipPayload, false)
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "HybridSearch failed", err)
	}
	return results, nil
}

// ── Filter builder ───────────────────────────────────────────────────────────

// buildOpenSearchFilter converts a generic filter map to OpenSearch query syntax.
// nil or empty filters → {"match_all": {}}
// Single entry → term/range/terms query
// Multiple entries → {"bool": {"must": [...]}}
func buildOpenSearchFilter(filters map[string]interface{}) map[string]interface{} {
	if len(filters) == 0 {
		return map[string]interface{}{"match_all": map[string]interface{}{}}
	}

	clauses := make([]interface{}, 0, len(filters))
	for k, v := range filters {
		fieldPath := "metadata." + k
		switch val := v.(type) {
		case map[string]interface{}:
			// Operator map
			clause := buildOperatorClause(fieldPath, val)
			clauses = append(clauses, clause)
		default:
			// Direct value → term query
			clauses = append(clauses, map[string]interface{}{
				"term": map[string]interface{}{fieldPath: val},
			})
		}
	}

	if len(clauses) == 1 {
		return clauses[0].(map[string]interface{})
	}
	return map[string]interface{}{
		"bool": map[string]interface{}{
			"must": clauses,
		},
	}
}

// buildOperatorClause converts a single operator map to an OpenSearch query clause.
func buildOperatorClause(field string, ops map[string]interface{}) map[string]interface{} {
	rangeOps := map[string]interface{}{}
	hasRange := false

	for op, val := range ops {
		switch op {
		case "$eq":
			return map[string]interface{}{
				"term": map[string]interface{}{field: val},
			}
		case "$ne":
			return map[string]interface{}{
				"bool": map[string]interface{}{
					"must_not": []interface{}{
						map[string]interface{}{
							"term": map[string]interface{}{field: val},
						},
					},
				},
			}
		case "$in":
			return map[string]interface{}{
				"terms": map[string]interface{}{field: val},
			}
		case "$gt":
			rangeOps["gt"] = val
			hasRange = true
		case "$gte":
			rangeOps["gte"] = val
			hasRange = true
		case "$lt":
			rangeOps["lt"] = val
			hasRange = true
		case "$lte":
			rangeOps["lte"] = val
			hasRange = true
		}
	}

	if hasRange {
		return map[string]interface{}{
			"range": map[string]interface{}{field: rangeOps},
		}
	}

	// Fallback: use term with the raw map
	return map[string]interface{}{
		"term": map[string]interface{}{field: ops},
	}
}

// ── Response parsing helpers ─────────────────────────────────────────────────

// parseSearchResponse parses an OpenSearch _search response body into SearchResult slice.
func parseSearchResponse(body []byte, scoreThreshold float64, skipPayload, withVectors bool) []SearchResult {
	var searchResp struct {
		Hits struct {
			Hits []struct {
				ID     string                 `json:"_id"`
				Score  float64                `json:"_score"`
				Source map[string]interface{} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil
	}

	results := make([]SearchResult, 0, len(searchResp.Hits.Hits))
	for _, hit := range searchResp.Hits.Hits {
		if scoreThreshold > 0 && hit.Score < scoreThreshold {
			continue
		}
		sr := SearchResult{
			ID:    hit.ID,
			Score: hit.Score,
		}

		if !skipPayload && hit.Source != nil {
			if content, ok := hit.Source["content"].(string); ok {
				sr.Content = content
			}
			if meta, ok := hit.Source["metadata"].(map[string]interface{}); ok {
				sr.Payload = meta
			}
		}

		if withVectors {
			if embRaw, ok := hit.Source["embedding"]; ok {
				switch embVal := embRaw.(type) {
				case []interface{}:
					vec := make([]float64, len(embVal))
					for i, f := range embVal {
						if fv, ok := f.(float64); ok {
							vec[i] = fv
						}
					}
					sr.Vector = vec
				}
			}
		}

		results = append(results, sr)
	}
	return results
}

// openSearchSourceToDocument converts an OpenSearch _source map to a Document.
func openSearchSourceToDocument(id string, source map[string]interface{}) *Document {
	doc := &Document{
		ID:      id,
		Payload: make(map[string]interface{}),
	}
	if source == nil {
		return doc
	}
	if content, ok := source["content"].(string); ok {
		doc.Content = content
	}
	if meta, ok := source["metadata"].(map[string]interface{}); ok {
		doc.Payload = meta
	}
	if embRaw, ok := source["embedding"]; ok {
		switch embVal := embRaw.(type) {
		case []interface{}:
			vec := make([]float64, len(embVal))
			for i, f := range embVal {
				if fv, ok := f.(float64); ok {
					vec[i] = fv
				}
			}
			doc.Vector = vec
		}
	}
	return doc
}
