package vectordb

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type elasticsearchClient struct {
	cfg        ConnectionConfig
	httpClient *http.Client
	baseURL    string
}

var _ VectorDBClient = (*elasticsearchClient)(nil)

func newElasticsearchClient(cfg ConnectionConfig) (VectorDBClient, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.TLSInsecureSkipVerify}, //nolint:gosec
	}
	httpClient := &http.Client{
		Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
		Transport: transport,
	}
	scheme := "http"
	if cfg.UseTLS {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port)
	return &elasticsearchClient{cfg: cfg, httpClient: httpClient, baseURL: baseURL}, nil
}

func (c *elasticsearchClient) DBType() string { return "elasticsearch" }

func (c *elasticsearchClient) Close() error { return nil }

// doRequest performs a single HTTP request. method: GET/POST/PUT/DELETE/HEAD
// endpoint: full URL path appended to baseURL
// body: JSON payload (nil for GET/HEAD/DELETE with no body)
// Returns (responseBody, statusCode, error).
func (c *elasticsearchClient) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("elasticsearch: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("elasticsearch: create request: %w", err)
	}

	// Auth
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "ApiKey "+c.cfg.APIKey)
	} else if c.cfg.Username != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, newError(ErrCodeConnectionFailed, "Elasticsearch HTTP request failed", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("elasticsearch: read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// mapError converts non-2xx status codes to VDBError.
func (c *elasticsearchClient) mapError(statusCode int, body []byte, context string) error {
	// Parse ES error response
	var esErr struct {
		Error struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &esErr)

	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return newError(ErrCodeAuthFailed, fmt.Sprintf("%s: authentication failed (HTTP %d)", context, statusCode), nil)
	case http.StatusNotFound:
		return newError(ErrCodeCollectionNotFound, fmt.Sprintf("%s: not found (HTTP %d)", context, statusCode), nil)
	case http.StatusConflict:
		if strings.Contains(esErr.Error.Type, "resource_already_exists_exception") {
			return newError(ErrCodeCollectionExists, fmt.Sprintf("%s: already exists", context), nil)
		}
		return newError(ErrCodeProviderError, fmt.Sprintf("%s: conflict (HTTP %d): %s", context, statusCode, string(body)), nil)
	default:
		reason := esErr.Error.Reason
		if reason == "" {
			reason = string(body)
		}
		return newError(ErrCodeProviderError, fmt.Sprintf("%s: Elasticsearch error (HTTP %d): %s", context, statusCode, reason), nil)
	}
}

func (c *elasticsearchClient) HealthCheck(ctx context.Context) error {
	_, statusCode, err := c.doRequest(ctx, http.MethodGet, "/_cluster/health?timeout=5s", nil)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "Elasticsearch health check failed", err)
	}
	if statusCode != http.StatusOK {
		return newError(ErrCodeConnectionFailed, fmt.Sprintf("Elasticsearch health check returned HTTP %d", statusCode), nil)
	}
	return nil
}

func (c *elasticsearchClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return false, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	_, statusCode, err := c.doRequest(ctx, http.MethodHead, "/"+name, nil)
	if err != nil {
		return false, err
	}
	if statusCode == http.StatusOK {
		return true, nil
	}
	if statusCode == http.StatusNotFound {
		return false, nil
	}
	return false, newError(ErrCodeProviderError, fmt.Sprintf("CollectionExists: unexpected status %d", statusCode), nil)
}

func (c *elasticsearchClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}
	metric := normalizeDistanceMetric(cfg.DistanceMetric)
	var esSimilarity string
	switch metric {
	case "dot":
		esSimilarity = "dot_product"
	case "euclidean":
		esSimilarity = "l2_norm"
	default:
		esSimilarity = "cosine"
	}

	reqBody := map[string]interface{}{
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"id":      map[string]interface{}{"type": "keyword"},
				"content": map[string]interface{}{"type": "text", "analyzer": "standard"},
				"metadata": map[string]interface{}{
					"type":    "object",
					"dynamic": true,
				},
				"embedding": map[string]interface{}{
					"type":       "dense_vector",
					"dims":       cfg.Dimensions,
					"index":      true,
					"similarity": esSimilarity,
				},
			},
		},
	}

	body, statusCode, err := c.doRequest(ctx, http.MethodPut, "/"+cfg.Name, reqBody)
	if err != nil {
		return err
	}
	if statusCode == http.StatusOK || statusCode == http.StatusCreated {
		return nil
	}
	// Check for already-exists
	var esErr struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &esErr)
	if statusCode == http.StatusBadRequest && strings.Contains(esErr.Error.Type, "resource_already_exists_exception") {
		return newError(ErrCodeCollectionExists, fmt.Sprintf("index %q already exists", cfg.Name), nil)
	}
	return c.mapError(statusCode, body, "CreateCollection")
}

func (c *elasticsearchClient) DeleteCollection(ctx context.Context, name string) error {
	if name == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	body, statusCode, err := c.doRequest(ctx, http.MethodDelete, "/"+name, nil)
	if err != nil {
		return err
	}
	if statusCode == http.StatusOK {
		return nil
	}
	if statusCode == http.StatusNotFound {
		return newError(ErrCodeCollectionNotFound, fmt.Sprintf("index %q not found", name), nil)
	}
	return c.mapError(statusCode, body, "DeleteCollection")
}

func (c *elasticsearchClient) ListCollections(ctx context.Context) ([]string, error) {
	body, statusCode, err := c.doRequest(ctx, http.MethodGet, "/_cat/indices?format=json&h=index", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, c.mapError(statusCode, body, "ListCollections")
	}
	var indices []struct {
		Index string `json:"index"`
	}
	if err := json.Unmarshal(body, &indices); err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections: parse response failed", err)
	}
	var names []string
	for _, idx := range indices {
		if !strings.HasPrefix(idx.Index, ".") {
			names = append(names, idx.Index)
		}
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

func (c *elasticsearchClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatchElasticsearch, "elasticsearch"); err != nil {
		return err
	}

	// Build bulk request body (NDJSON)
	var sb strings.Builder
	for _, doc := range docs {
		metaLine := fmt.Sprintf(`{"index":{"_id":%q}}`, doc.ID)
		sb.WriteString(metaLine)
		sb.WriteString("\n")

		docBody := map[string]interface{}{
			"id":        doc.ID,
			"content":   doc.Content,
			"metadata":  doc.Payload,
			"embedding": doc.Vector,
		}
		docJSON, err := json.Marshal(docBody)
		if err != nil {
			return newError(ErrCodeProviderError, fmt.Sprintf("UpsertDocuments: marshal doc %s failed", doc.ID), err)
		}
		sb.Write(docJSON)
		sb.WriteString("\n")
	}

	url := c.baseURL + "/" + collectionName + "/_bulk"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(sb.String()))
	if err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments: create request failed", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "ApiKey "+c.cfg.APIKey)
	} else if c.cfg.Username != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "UpsertDocuments: HTTP request failed", err)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if readErr != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments: read response body failed", readErr)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return c.mapError(resp.StatusCode, respBody, "UpsertDocuments")
	}

	// Check for per-item errors
	var bulkResp struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Error *struct {
				Reason string `json:"reason"`
			} `json:"error,omitempty"`
		} `json:"items"`
	}
	if err := json.Unmarshal(respBody, &bulkResp); err == nil && bulkResp.Errors {
		var errs []string
		for _, item := range bulkResp.Items {
			for _, op := range item {
				if op.Error != nil {
					errs = append(errs, op.Error.Reason)
				}
			}
		}
		if len(errs) > 0 {
			return newError(ErrCodeProviderError, fmt.Sprintf("UpsertDocuments: %d item(s) failed: %s", len(errs), strings.Join(errs, "; ")), nil)
		}
	}
	return nil
}

func (c *elasticsearchClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	if collectionName == "" {
		return nil, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if id == "" {
		return nil, newError(ErrCodeInvalidDocumentID, "", nil)
	}

	body, statusCode, err := c.doRequest(ctx, http.MethodGet, "/"+collectionName+"/_doc/"+id, nil)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusNotFound {
		return nil, newError(ErrCodeDocumentNotFound, fmt.Sprintf("document %q not found in %q", id, collectionName), nil)
	}
	if statusCode != http.StatusOK {
		return nil, c.mapError(statusCode, body, "GetDocument")
	}

	var resp struct {
		Found  bool `json:"found"`
		Source struct {
			ID        string                 `json:"id"`
			Content   string                 `json:"content"`
			Metadata  map[string]interface{} `json:"metadata"`
			Embedding []float64              `json:"embedding"`
		} `json:"_source"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, newError(ErrCodeProviderError, "GetDocument: parse response failed", err)
	}
	if !resp.Found {
		return nil, newError(ErrCodeDocumentNotFound, fmt.Sprintf("document %q not found in %q", id, collectionName), nil)
	}

	return &Document{
		ID:      resp.Source.ID,
		Content: resp.Source.Content,
		Payload: resp.Source.Metadata,
		Vector:  resp.Source.Embedding,
	}, nil
}

func (c *elasticsearchClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if collectionName == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if len(ids) == 0 {
		return nil
	}

	// Build bulk delete request
	var sb strings.Builder
	for _, id := range ids {
		sb.WriteString(fmt.Sprintf(`{"delete":{"_id":%q}}`, id))
		sb.WriteString("\n")
	}

	url := c.baseURL + "/" + collectionName + "/_bulk"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(sb.String()))
	if err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments: create request failed", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "ApiKey "+c.cfg.APIKey)
	} else if c.cfg.Username != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "DeleteDocuments: HTTP request failed", err)
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if readErr != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments: read response body failed", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		return c.mapError(resp.StatusCode, respBody, "DeleteDocuments")
	}
	return nil
}

func (c *elasticsearchClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if collectionName == "" {
		return -1, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if len(filters) == 0 {
		return -1, newError(ErrCodeProviderError, "DeleteByFilter requires at least one filter to prevent accidental full-collection deletion", nil)
	}

	query := buildESFilter(filters)
	reqBody := map[string]interface{}{"query": query}

	body, statusCode, err := c.doRequest(ctx, http.MethodPost, "/"+collectionName+"/_delete_by_query", reqBody)
	if err != nil {
		return -1, err
	}
	if statusCode != http.StatusOK {
		return -1, c.mapError(statusCode, body, "DeleteByFilter")
	}

	var resp struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return -1, newError(ErrCodeProviderError, "DeleteByFilter: parse response failed", err)
	}
	return resp.Deleted, nil
}

func (c *elasticsearchClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if collectionName == "" {
		return 0, newError(ErrCodeInvalidCollectionName, "", nil)
	}

	query := buildESQuery(filters)
	reqBody := map[string]interface{}{"query": query}

	body, statusCode, err := c.doRequest(ctx, http.MethodPost, "/"+collectionName+"/_count", reqBody)
	if err != nil {
		return 0, err
	}
	if statusCode != http.StatusOK {
		return 0, c.mapError(statusCode, body, "CountDocuments")
	}

	var resp struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments: parse response failed", err)
	}
	return resp.Count, nil
}

func (c *elasticsearchClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	if req.CollectionName == "" {
		return nil, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	from := 0
	if req.Offset != "" {
		if n, err := strconv.Atoi(req.Offset); err == nil && n > 0 {
			from = n
		}
	}

	query := buildESQuery(req.Filters)
	reqBody := map[string]interface{}{
		"from":  from,
		"size":  limit,
		"query": query,
	}

	body, statusCode, err := c.doRequest(ctx, http.MethodPost, "/"+req.CollectionName+"/_search", reqBody)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, c.mapError(statusCode, body, "ScrollDocuments")
	}

	results, total, err := parseSearchHits(body)
	if err != nil {
		return nil, err
	}

	docs := make([]Document, len(results))
	for i, r := range results {
		docs[i] = Document{
			ID:      r.ID,
			Content: r.Content,
			Payload: r.Payload,
		}
	}

	nextOffset := ""
	if int64(from+len(docs)) < total {
		nextOffset = fmt.Sprintf("%d", from+len(docs))
	}

	return &ScrollResult{
		Documents:  docs,
		NextOffset: nextOffset,
		Total:      total,
	}, nil
}

func (c *elasticsearchClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	reqBody := map[string]interface{}{
		"size": req.TopK,
		"knn": map[string]interface{}{
			"field":          "embedding",
			"query_vector":   req.QueryVector,
			"k":              req.TopK,
			"num_candidates": req.TopK * 10,
		},
	}
	if len(req.Filters) > 0 {
		knn := reqBody["knn"].(map[string]interface{})
		knn["filter"] = buildESFilter(req.Filters)
	}

	body, statusCode, err := c.doRequest(ctx, http.MethodPost, "/"+req.CollectionName+"/_search", reqBody)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, c.mapError(statusCode, body, "VectorSearch")
	}

	results, _, err := parseSearchHits(body)
	if err != nil {
		return nil, err
	}

	// Apply score threshold
	if req.ScoreThreshold > 0 {
		filtered := results[:0]
		for _, r := range results {
			if r.Score >= req.ScoreThreshold {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if !req.SkipPayload {
		return results, nil
	}
	for i := range results {
		results[i].Payload = nil
		results[i].Content = ""
	}
	return results, nil
}

func (c *elasticsearchClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	if req.CollectionName == "" {
		return nil, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if req.TopK <= 0 {
		return nil, newError(ErrCodeInvalidTopK, fmt.Sprintf("topK=%d must be > 0", req.TopK), nil)
	}
	if req.Alpha < 0 || req.Alpha > 1 {
		return nil, newError(ErrCodeInvalidAlpha, "", nil)
	}

	reqBody := map[string]interface{}{
		"size": req.TopK,
	}

	if len(req.QueryVector) > 0 {
		knn := map[string]interface{}{
			"field":          "embedding",
			"query_vector":   req.QueryVector,
			"k":              req.TopK,
			"num_candidates": req.TopK * 10,
			"boost":          req.Alpha,
		}
		if len(req.Filters) > 0 {
			knn["filter"] = buildESFilter(req.Filters)
		}
		reqBody["knn"] = knn
	}

	if req.QueryText != "" {
		boolQuery := map[string]interface{}{
			"should": []interface{}{
				map[string]interface{}{
					"multi_match": map[string]interface{}{
						"query":  req.QueryText,
						"fields": []string{"content"},
						"boost":  1.0 - req.Alpha,
					},
				},
			},
		}
		if len(req.Filters) > 0 {
			boolQuery["filter"] = []interface{}{buildESFilter(req.Filters)}
		}
		reqBody["query"] = map[string]interface{}{"bool": boolQuery}
	}

	body, statusCode, err := c.doRequest(ctx, http.MethodPost, "/"+req.CollectionName+"/_search", reqBody)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, c.mapError(statusCode, body, "HybridSearch")
	}

	results, _, err := parseSearchHits(body)
	if err != nil {
		return nil, err
	}

	if req.ScoreThreshold > 0 {
		filtered := results[:0]
		for _, r := range results {
			if r.Score >= req.ScoreThreshold {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if req.SkipPayload {
		for i := range results {
			results[i].Payload = nil
			results[i].Content = ""
		}
	}

	if results == nil {
		results = []SearchResult{}
	}
	return results, nil
}

// --- Helpers ---

// buildESQuery converts filters to ES query. If filters nil/empty, returns match_all.
func buildESQuery(filters map[string]interface{}) map[string]interface{} {
	if len(filters) == 0 {
		return map[string]interface{}{"match_all": map[string]interface{}{}}
	}
	return buildESFilter(filters)
}

// buildESFilter converts filter map to ES bool/must query.
func buildESFilter(filters map[string]interface{}) map[string]interface{} {
	if len(filters) == 0 {
		return map[string]interface{}{"match_all": map[string]interface{}{}}
	}

	var must []interface{}
	for key, value := range filters {
		field := "metadata." + key
		switch v := value.(type) {
		case map[string]interface{}:
			for op, opVal := range v {
				switch op {
				case "$eq":
					must = append(must, map[string]interface{}{"term": map[string]interface{}{field: opVal}})
				case "$ne":
					must = append(must, map[string]interface{}{
						"bool": map[string]interface{}{
							"must_not": []interface{}{
								map[string]interface{}{"term": map[string]interface{}{field: opVal}},
							},
						},
					})
				case "$gt":
					must = append(must, map[string]interface{}{"range": map[string]interface{}{field: map[string]interface{}{"gt": opVal}}})
				case "$gte":
					must = append(must, map[string]interface{}{"range": map[string]interface{}{field: map[string]interface{}{"gte": opVal}}})
				case "$lt":
					must = append(must, map[string]interface{}{"range": map[string]interface{}{field: map[string]interface{}{"lt": opVal}}})
				case "$lte":
					must = append(must, map[string]interface{}{"range": map[string]interface{}{field: map[string]interface{}{"lte": opVal}}})
				case "$in":
					must = append(must, map[string]interface{}{"terms": map[string]interface{}{field: toInterfaceSlice(opVal)}})
				case "$nin":
					must = append(must, map[string]interface{}{
						"bool": map[string]interface{}{
							"must_not": []interface{}{
								map[string]interface{}{"terms": map[string]interface{}{field: toInterfaceSlice(opVal)}},
							},
						},
					})
				}
			}
		default:
			must = append(must, map[string]interface{}{"term": map[string]interface{}{field: v}})
		}
	}

	if len(must) == 1 {
		return must[0].(map[string]interface{})
	}
	return map[string]interface{}{
		"bool": map[string]interface{}{"must": must},
	}
}

// toInterfaceSlice converts typed slices to []interface{} for ES terms queries.
func toInterfaceSlice(v interface{}) []interface{} {
	switch val := v.(type) {
	case []interface{}:
		return val
	case []string:
		out := make([]interface{}, len(val))
		for i, s := range val {
			out[i] = s
		}
		return out
	case []int:
		out := make([]interface{}, len(val))
		for i, n := range val {
			out[i] = n
		}
		return out
	case []int64:
		out := make([]interface{}, len(val))
		for i, n := range val {
			out[i] = n
		}
		return out
	case []float64:
		out := make([]interface{}, len(val))
		for i, f := range val {
			out[i] = f
		}
		return out
	}
	return []interface{}{v}
}

// parseSearchHits parses an ES _search response and extracts SearchResult slice.
func parseSearchHits(body []byte) ([]SearchResult, int64, error) {
	var resp struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				ID     string  `json:"_id"`
				Score  float64 `json:"_score"`
				Source struct {
					Content   string                 `json:"content"`
					Metadata  map[string]interface{} `json:"metadata"`
					Embedding []float64              `json:"embedding"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, newError(ErrCodeProviderError, "parse search hits failed", err)
	}

	results := make([]SearchResult, len(resp.Hits.Hits))
	for i, hit := range resp.Hits.Hits {
		results[i] = SearchResult{
			ID:      hit.ID,
			Score:   hit.Score,
			Content: hit.Source.Content,
			Payload: hit.Source.Metadata,
			Vector:  hit.Source.Embedding,
		}
	}
	return results, resp.Hits.Total.Value, nil
}
