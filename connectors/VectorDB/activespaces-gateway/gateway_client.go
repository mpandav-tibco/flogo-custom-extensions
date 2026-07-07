package vectordb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// activeSpacesClient implements VectorDBClient by calling the ActiveSpaces vector
// gateway over HTTP/JSON. Keeping the client pure-Go (no CGO / native libraries)
// is what lets this connector build with the standard Flogo toolchain and run on
// any OS/architecture; all native ActiveSpaces concerns live in the gateway.
type activeSpacesClient struct {
	baseURL string
	http    *http.Client
	cfg     ConnectionConfig
}

// Compile-time proof that activeSpacesClient satisfies the full interface.
var _ VectorDBClient = (*activeSpacesClient)(nil)

func newActiveSpacesClient(cfg ConnectionConfig) (VectorDBClient, error) {
	scheme := "http"
	if cfg.UseTLS {
		scheme = "https"
	}
	transport := &http.Transport{}
	if cfg.UseTLS {
		tlsCfg, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, newError(ErrCodeConnectionFailed, "activespaces: invalid TLS configuration", err)
		}
		transport.TLSClientConfig = tlsCfg
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &activeSpacesClient{
		baseURL: fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port),
		http:    &http.Client{Timeout: timeout, Transport: transport},
		cfg:     cfg,
	}, nil
}

func (c *activeSpacesClient) DBType() string { return "activespaces" }

func (c *activeSpacesClient) Close() error {
	c.http.CloseIdleConnections()
	return nil
}

func (c *activeSpacesClient) HealthCheck(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/readyz", nil, nil)
}

// --- Collection management ---

func (c *activeSpacesClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}
	body := map[string]any{
		"name":       cfg.Name,
		"dimension":  cfg.Dimensions,
		"similarity": toSimilarity(cfg.DistanceMetric),
	}
	return c.retry(ctx, func() error {
		return c.do(ctx, http.MethodPost, "/v1/collections", body, nil)
	})
}

func (c *activeSpacesClient) DeleteCollection(ctx context.Context, name string) error {
	return c.retry(ctx, func() error {
		return c.do(ctx, http.MethodDelete, "/v1/collections/"+url.PathEscape(name), nil, nil)
	})
}

func (c *activeSpacesClient) ListCollections(ctx context.Context) ([]string, error) {
	var out struct {
		Collections []struct {
			Name string `json:"name"`
		} `json:"collections"`
	}
	if err := c.retry(ctx, func() error {
		return c.do(ctx, http.MethodGet, "/v1/collections", nil, &out)
	}); err != nil {
		return nil, err
	}
	names := make([]string, len(out.Collections))
	for i, col := range out.Collections {
		names[i] = col.Name
	}
	return names, nil
}

func (c *activeSpacesClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	names, err := c.ListCollections(ctx)
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return true, nil
		}
	}
	return false, nil
}

// --- Document operations ---

func (c *activeSpacesClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	body := map[string]any{"documents": toGatewayDocs(docs)}
	return c.retry(ctx, func() error {
		return c.do(ctx, http.MethodPost, c.docPath(collectionName, "upsert"), body, nil)
	})
}

func (c *activeSpacesClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	var gd gatewayDoc
	if err := c.retry(ctx, func() error {
		return c.do(ctx, http.MethodPost, c.docPath(collectionName, "get"),
			map[string]any{"id": id, "includeVector": true}, &gd)
	}); err != nil {
		return nil, err
	}
	doc := gd.toDocument()
	return &doc, nil
}

func (c *activeSpacesClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	return c.retry(ctx, func() error {
		return c.do(ctx, http.MethodPost, c.docPath(collectionName, "delete"),
			map[string]any{"ids": ids}, nil)
	})
}

func (c *activeSpacesClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	var out struct {
		Deleted int64 `json:"deleted"`
	}
	if err := c.retry(ctx, func() error {
		return c.do(ctx, http.MethodPost, c.docPath(collectionName, "delete"),
			map[string]any{"filter": translateFilters(filters)}, &out)
	}); err != nil {
		return 0, err
	}
	return out.Deleted, nil
}

func (c *activeSpacesClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	offset := 0
	if req.Offset != "" {
		offset, _ = strconv.Atoi(req.Offset)
	}
	body := map[string]any{
		"limit":         req.Limit,
		"offset":        offset,
		"includeVector": req.WithVectors,
	}
	if f := translateFilters(req.Filters); f != nil {
		body["filter"] = f
	}
	var out struct {
		Documents  []gatewayDoc `json:"documents"`
		NextOffset int          `json:"nextOffset"`
	}
	if err := c.retry(ctx, func() error {
		return c.do(ctx, http.MethodPost, c.docPath(req.CollectionName, "scroll"), body, &out)
	}); err != nil {
		return nil, err
	}
	res := &ScrollResult{Documents: make([]Document, len(out.Documents)), Total: -1}
	for i, gd := range out.Documents {
		res.Documents[i] = gd.toDocument()
	}
	if out.NextOffset > 0 {
		res.NextOffset = strconv.Itoa(out.NextOffset)
	}
	return res, nil
}

func (c *activeSpacesClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	body := map[string]any{}
	if f := translateFilters(filters); f != nil {
		body["filter"] = f
	}
	var out struct {
		Count int64 `json:"count"`
	}
	if err := c.retry(ctx, func() error {
		return c.do(ctx, http.MethodPost, c.docPath(collectionName, "count"), body, &out)
	}); err != nil {
		return 0, err
	}
	return out.Count, nil
}

// --- Search ---

func (c *activeSpacesClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if len(req.QueryVector) == 0 {
		return nil, newError(ErrCodeInvalidQueryVector, "", nil)
	}
	body := map[string]any{
		"vector":          req.QueryVector,
		"topK":            req.TopK,
		"minScore":        req.ScoreThreshold,
		"includeVector":   req.WithVectors,
		"includeMetadata": !req.SkipPayload,
	}
	if f := translateFilters(req.Filters); f != nil {
		body["filter"] = f
	}
	var out struct {
		Results []gatewaySearchResult `json:"results"`
	}
	if err := c.retry(ctx, func() error {
		return c.do(ctx, http.MethodPost, "/v1/collections/"+url.PathEscape(req.CollectionName)+"/search", body, &out)
	}); err != nil {
		return nil, err
	}
	return toSearchResults(out.Results), nil
}

// HybridSearch falls back to dense VectorSearch: ActiveSpaces does not provide a
// native sparse/BM25 index, so the dense component is used.
func (c *activeSpacesClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	return c.VectorSearch(ctx, SearchRequest{
		CollectionName: req.CollectionName,
		QueryVector:    req.QueryVector,
		TopK:           req.TopK,
		ScoreThreshold: req.ScoreThreshold,
		Filters:        req.Filters,
		SkipPayload:    req.SkipPayload,
	})
}

// --- helpers ---

func (c *activeSpacesClient) docPath(collection, action string) string {
	return "/v1/collections/" + url.PathEscape(collection) + "/documents/" + action
}

func (c *activeSpacesClient) retry(ctx context.Context, op func() error) error {
	return withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, op)
}

func (c *activeSpacesClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return newError(ErrCodeProviderError, "activespaces: failed to encode request", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return newError(ErrCodeProviderError, "activespaces: failed to build request", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "activespaces gateway request failed", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= http.StatusBadRequest {
		return httpStatusError(resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return newError(ErrCodeProviderError, "activespaces: failed to decode response", err)
		}
	}
	return nil
}

func httpStatusError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		msg = e.Error
	}
	switch status {
	case http.StatusNotFound:
		if strings.Contains(strings.ToLower(msg), "document") {
			return newError(ErrCodeDocumentNotFound, msg, nil)
		}
		return newError(ErrCodeCollectionNotFound, msg, nil)
	case http.StatusConflict:
		return newError(ErrCodeCollectionExists, msg, nil)
	case http.StatusBadRequest:
		return newError(ErrCodeProviderError, "activespaces gateway: "+msg, nil)
	case http.StatusUnauthorized, http.StatusForbidden:
		return newError(ErrCodeAuthFailed, msg, nil)
	default:
		return newError(ErrCodeProviderError, fmt.Sprintf("activespaces gateway returned HTTP %d: %s", status, msg), nil)
	}
}

func toSimilarity(metric string) string {
	switch strings.ToLower(metric) {
	case "dot", "dotproduct", "dot_product":
		return "dot"
	case "euclidean", "l2", "l2_distance":
		return "l2"
	default:
		return "cosine"
	}
}

// gatewayDoc mirrors the gateway document JSON (metadata, not payload).
type gatewayDoc struct {
	ID       string                 `json:"id"`
	Content  string                 `json:"content,omitempty"`
	Vector   []float64              `json:"vector,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

func (g gatewayDoc) toDocument() Document {
	return Document{ID: g.ID, Content: g.Content, Vector: g.Vector, Payload: g.Metadata}
}

func toGatewayDocs(docs []Document) []gatewayDoc {
	out := make([]gatewayDoc, len(docs))
	for i, d := range docs {
		out[i] = gatewayDoc{ID: d.ID, Content: d.Content, Vector: d.Vector, Metadata: d.Payload}
	}
	return out
}

type gatewaySearchResult struct {
	ID       string                 `json:"id"`
	Content  string                 `json:"content,omitempty"`
	Score    float64                `json:"score"`
	Vector   []float64              `json:"vector,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

func toSearchResults(in []gatewaySearchResult) []SearchResult {
	out := make([]SearchResult, len(in))
	for i, r := range in {
		out[i] = SearchResult{ID: r.ID, Score: r.Score, Content: r.Content, Vector: r.Vector, Payload: r.Metadata}
	}
	return out
}

// gwFilter is the gateway's structured, injection-safe filter representation.
type gwFilter struct {
	Op      string     `json:"op"`
	Field   string     `json:"field,omitempty"`
	Value   any        `json:"value,omitempty"`
	Filters []gwFilter `json:"filters,omitempty"`
}

// translateFilters converts the connector's map-based filter into the gateway's
// structured filter. Supported per-key forms:
//
//	"field": scalar                       -> field == scalar
//	"field": [a, b]                        -> field IN (a, b)
//	"field": {"$gte": 1, "$lt": 10}        -> field >= 1 AND field < 10
func translateFilters(m map[string]interface{}) *gwFilter {
	if len(m) == 0 {
		return nil
	}
	var conds []gwFilter
	for field, v := range m {
		conds = append(conds, translateField(field, v)...)
	}
	if len(conds) == 1 {
		return &conds[0]
	}
	return &gwFilter{Op: "and", Filters: conds}
}

func translateField(field string, v any) []gwFilter {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make([]gwFilter, 0, len(val))
		for op, ov := range val {
			out = append(out, gwFilter{Op: mapFilterOp(op), Field: field, Value: ov})
		}
		return out
	case []interface{}:
		return []gwFilter{{Op: "in", Field: field, Value: val}}
	default:
		return []gwFilter{{Op: "eq", Field: field, Value: v}}
	}
}

func mapFilterOp(op string) string {
	switch strings.ToLower(op) {
	case "$ne", "ne":
		return "ne"
	case "$gt", "gt":
		return "gt"
	case "$gte", "gte":
		return "gte"
	case "$lt", "lt":
		return "lt"
	case "$lte", "lte":
		return "lte"
	case "$in", "in":
		return "in"
	case "$like", "like":
		return "like"
	default:
		return "eq"
	}
}
