package vectordb

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	chromago "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
)

// chromaClient implements VectorDBClient for Chroma (chroma-go v0.4.x, Chroma 1.0+ v2 API).
type chromaClient struct {
	client chromago.Client
	cfg    ConnectionConfig
}

// Compile-time proof that chromaClient satisfies the full VectorDBClient interface.
var _ VectorDBClient = (*chromaClient)(nil)

func newChromaClient(cfg ConnectionConfig) (VectorDBClient, error) {
	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "Chroma: invalid TLS configuration", err)
	}

	scheme := cfg.Scheme
	if scheme == "" {
		if tlsCfg != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port)

	opts := []chromago.ClientOption{chromago.WithBaseURL(baseURL)}
	if cfg.APIKey != "" {
		opts = append(opts, chromago.WithAuth(
			chromago.NewTokenAuthCredentialsProvider(cfg.APIKey, chromago.AuthorizationTokenHeader),
		))
	}
	if tlsCfg != nil {
		opts = append(opts, chromago.WithHTTPClient(&http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		}))
	}

	c, err := chromago.NewHTTPClient(opts...)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "failed to create Chroma client", err)
	}
	return &chromaClient{client: c, cfg: cfg}, nil
}

func (c *chromaClient) DBType() string { return "chroma" }

// Close is a no-op for the Chroma HTTP client. The underlying *http.Client
// has no persistent connection pool that requires explicit shutdown; idle
// connections are reclaimed automatically by the runtime.
func (c *chromaClient) Close() error { return nil }

func (c *chromaClient) HealthCheck(ctx context.Context) error {
	if err := c.client.Heartbeat(ctx); err != nil {
		return newError(ErrCodeConnectionFailed, "Chroma health check failed", err)
	}
	return nil
}

func (c *chromaClient) chromaDistance(metric string) embeddings.DistanceMetric {
	switch strings.ToLower(metric) {
	case "dot":
		return embeddings.IP
	case "euclidean":
		return embeddings.L2
	default:
		return embeddings.COSINE
	}
}

func (c *chromaClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, retryErr := c.client.CreateCollection(ctx, cfg.Name,
			chromago.WithHNSWSpaceCreate(c.chromaDistance(cfg.DistanceMetric)),
		)
		if retryErr != nil {
			if strings.Contains(retryErr.Error(), "already exists") || strings.Contains(retryErr.Error(), "UniqueConstraintError") {
				return newError(ErrCodeCollectionExists, fmt.Sprintf("collection %q already exists", cfg.Name), retryErr)
			}
		}
		return retryErr
	}); err != nil {
		if ve, ok := err.(*VDBError); ok {
			return ve
		}
		return newError(ErrCodeProviderError, "CreateCollection failed", err)
	}
	return nil
}

func (c *chromaClient) DeleteCollection(ctx context.Context, name string) error {
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		return c.client.DeleteCollection(ctx, name)
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteCollection failed", err)
	}
	return nil
}

func (c *chromaClient) ListCollections(ctx context.Context) ([]string, error) {
	var cols []chromago.Collection
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var retryErr error
		cols, retryErr = c.client.ListCollections(ctx)
		return retryErr
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.Name()
	}
	return names, nil
}

func (c *chromaClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, retryErr := c.client.GetCollection(ctx, name)
		if retryErr == nil {
			exists = true
			return nil
		}
		// Chroma returns "Error (404) NotFoundError: collection <name> does not exist"
		// when the collection is absent.  Only suppress that; propagate real failures
		// (network, auth, 5xx) so the caller is not misled into calling CreateCollection.
		errMsg := retryErr.Error()
		if strings.Contains(errMsg, "not found") ||
			strings.Contains(errMsg, "NotFound") ||
			strings.Contains(errMsg, "(404)") {
			exists = false
			return nil
		}
		return retryErr
	}); err != nil {
		return false, newError(ErrCodeProviderError, "CollectionExists failed", err)
	}
	return exists, nil
}

func (c *chromaClient) getCollection(ctx context.Context, name string) (chromago.Collection, error) {
	var col chromago.Collection
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var retryErr error
		col, retryErr = c.client.GetCollection(ctx, name)
		return retryErr
	}); err != nil {
		return nil, newError(ErrCodeCollectionNotFound, fmt.Sprintf("collection %q not found", name), err)
	}
	return col, nil
}

func (c *chromaClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatchChroma, "Chroma"); err != nil {
		return err
	}
	col, err := c.getCollection(ctx, collectionName)
	if err != nil {
		return err
	}

	ids := make([]chromago.DocumentID, len(docs))
	embs := make([]embeddings.Embedding, len(docs))
	texts := make([]string, len(docs))
	metas := make([]chromago.DocumentMetadata, len(docs))

	for i, doc := range docs {
		ids[i] = chromago.DocumentID(doc.ID)
		embs[i] = embeddings.NewEmbeddingFromFloat32(toFloat32Slice(doc.Vector))
		texts[i] = doc.Content
		meta := make(map[string]interface{}, len(doc.Payload))
		for k, v := range doc.Payload {
			meta[k] = v
		}
		m, merr := chromago.NewDocumentMetadataFromMap(meta)
		if merr != nil {
			return newError(ErrCodeProviderError, fmt.Sprintf("UpsertDocuments: invalid metadata for doc %q", doc.ID), merr)
		}
		metas[i] = m
	}

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		return col.Upsert(ctx,
			chromago.WithIDs(ids...),
			chromago.WithEmbeddings(embs...),
			chromago.WithTexts(texts...),
			chromago.WithMetadatas(metas...),
		)
	}); err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments failed", err)
	}
	return nil
}

func (c *chromaClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	col, err := c.getCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	var doc *Document
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		result, retryErr := col.Get(ctx,
			chromago.WithIDs(chromago.DocumentID(id)),
			chromago.WithInclude(chromago.IncludeDocuments, chromago.IncludeMetadatas, chromago.IncludeEmbeddings),
		)
		if retryErr != nil {
			return retryErr
		}
		if result.Count() == 0 {
			return newError(ErrCodeDocumentNotFound,
				fmt.Sprintf("document %q not found in collection %q", id, collectionName), nil)
		}
		docIDs := result.GetIDs()
		docsList := result.GetDocuments()
		metasList := result.GetMetadatas()
		embsList := result.GetEmbeddings()
		doc = &Document{ID: string(docIDs[0]), Payload: make(map[string]interface{})}
		if len(docsList) > 0 && docsList[0] != nil {
			doc.Content = docsList[0].ContentString()
		}
		if len(metasList) > 0 && metasList[0] != nil {
			doc.Payload = chromaMetadataToMap(metasList[0])
		}
		if len(embsList) > 0 && embsList[0] != nil {
			doc.Vector = toFloat64Slice(embsList[0].ContentAsFloat32())
		}
		return nil
	}); err != nil {
		if ve, ok := err.(*VDBError); ok {
			return nil, ve
		}
		return nil, newError(ErrCodeProviderError, "GetDocument failed", err)
	}
	return doc, nil
}

func (c *chromaClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	col, err := c.getCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	docIDs := make([]chromago.DocumentID, len(ids))
	for i, id := range ids {
		docIDs[i] = chromago.DocumentID(id)
	}
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		return col.Delete(ctx, chromago.WithIDs(docIDs...))
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

func (c *chromaClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if len(filters) == 0 {
		// An empty filter would delete ALL documents in the collection.
		// Require at least one filter to prevent accidental data loss.
		return 0, newError(ErrCodeProviderError, "DeleteByFilter requires at least one filter", nil)
	}
	col, err := c.getCollection(ctx, collectionName)
	if err != nil {
		return 0, err
	}
	opts := []chromago.CollectionDeleteOption{}
	if where := chromaWhereFilter(filters); where != nil {
		opts = append(opts, chromago.WithWhere(where))
	}
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		return col.Delete(ctx, opts...)
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter failed", err)
	}
	return -1, nil
}

func (c *chromaClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	col, err := c.getCollection(ctx, req.CollectionName)
	if err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := 0
	if req.Offset != "" {
		if n, parseErr := fmt.Sscanf(req.Offset, "%d", &offset); n != 1 || parseErr != nil {
			return nil, newError(ErrCodeProviderError,
				fmt.Sprintf("ScrollDocuments: invalid offset cursor %q", req.Offset), parseErr)
		}
	}

	// Hard cap: Chroma does not support server-side cursor pagination — this call
	// loads ALL matching documents into memory before slicing by offset/limit.
	// We pre-check the total collection size (only when no filter is active) so
	// that page-1 requests on a large unfiltered collection are rejected before
	// any data is transferred. When filters are active the cap cannot be
	// determined cheaply, so we fall through to the offset+limit gate instead.
	const maxChromaScrollDocs = 50_000
	if len(req.Filters) == 0 {
		if collectionCount, countErr := col.Count(ctx); countErr == nil && collectionCount > maxChromaScrollDocs {
			return nil, newError(ErrCodeProviderError,
				fmt.Sprintf("ScrollDocuments: collection %q has %d documents which exceeds the Chroma client-side scroll limit (%d); use Qdrant or Milvus for large collections",
					req.CollectionName, collectionCount, maxChromaScrollDocs), nil)
		}
	}
	if offset+limit > maxChromaScrollDocs {
		return nil, newError(ErrCodeProviderError,
			fmt.Sprintf("ScrollDocuments: offset+limit=%d exceeds Chroma client-side scroll limit (%d); use Qdrant or Milvus for large collections",
				offset+limit, maxChromaScrollDocs), nil)
	}
	logger.Warnf("Chroma ScrollDocuments: client-side pagination — fetches entire collection into memory (collection=%s)", req.CollectionName)

	include := []chromago.Include{chromago.IncludeDocuments, chromago.IncludeMetadatas}
	if req.WithVectors {
		include = append(include, chromago.IncludeEmbeddings)
	}
	getOpts := []chromago.CollectionGetOption{chromago.WithInclude(include...)}
	if where := chromaWhereFilter(req.Filters); where != nil {
		getOpts = append(getOpts, chromago.WithWhere(where))
	}

	var scrollResult *ScrollResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		result, retryErr := col.Get(ctx, getOpts...)
		if retryErr != nil {
			return retryErr
		}
		allIDs := result.GetIDs()
		allDocs := result.GetDocuments()
		allMetas := result.GetMetadatas()
		allEmbs := result.GetEmbeddings()
		total := len(allIDs)
		end := offset + limit
		if end > total {
			end = total
		}
		docs := make([]Document, 0, end-offset)
		for i := offset; i < end; i++ {
			doc := Document{ID: string(allIDs[i]), Payload: make(map[string]interface{})}
			if i < len(allDocs) && allDocs[i] != nil {
				doc.Content = allDocs[i].ContentString()
			}
			if i < len(allMetas) && allMetas[i] != nil {
				doc.Payload = chromaMetadataToMap(allMetas[i])
			}
			if req.WithVectors && i < len(allEmbs) && allEmbs[i] != nil {
				doc.Vector = toFloat64Slice(allEmbs[i].ContentAsFloat32())
			}
			docs = append(docs, doc)
		}
		nextOffset := ""
		if end < total {
			nextOffset = fmt.Sprintf("%d", end)
		}
		scrollResult = &ScrollResult{Documents: docs, NextOffset: nextOffset, Total: int64(total)}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}
	return scrollResult, nil
}

func (c *chromaClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	col, err := c.getCollection(ctx, collectionName)
	if err != nil {
		return 0, err
	}
	if len(filters) == 0 {
		var count int
		if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
			var retryErr error
			count, retryErr = col.Count(ctx)
			return retryErr
		}); err != nil {
			return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
		}
		return int64(count), nil
	}
	// Filtered count: load only documents (IDs are always returned regardless
	// of Include, but at least one include field is required by Chroma API).
	getOpts := []chromago.CollectionGetOption{
		chromago.WithInclude(chromago.IncludeDocuments),
	}
	if where := chromaWhereFilter(filters); where != nil {
		getOpts = append(getOpts, chromago.WithWhere(where))
	}
	var result chromago.GetResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var retryErr error
		result, retryErr = col.Get(ctx, getOpts...)
		return retryErr
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments (filtered) failed", err)
	}
	return int64(result.Count()), nil
}

func (c *chromaClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}
	col, err := c.getCollection(ctx, req.CollectionName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	include := []chromago.Include{chromago.IncludeDistances}
	if !req.SkipPayload {
		include = append(include, chromago.IncludeDocuments, chromago.IncludeMetadatas)
	}
	if req.WithVectors {
		include = append(include, chromago.IncludeEmbeddings)
	}

	queryOpts := []chromago.CollectionQueryOption{
		chromago.WithQueryEmbeddings(embeddings.NewEmbeddingFromFloat32(toFloat32Slice(req.QueryVector))),
		chromago.WithNResults(req.TopK),
		chromago.WithInclude(include...),
	}
	if where := chromaWhereFilter(req.Filters); where != nil {
		queryOpts = append(queryOpts, chromago.WithWhere(where))
	}

	var qr chromago.QueryResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var qErr error
		qr, qErr = col.Query(ctx, queryOpts...)
		return qErr
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}
	return chromaQueryResultToSearchResults(qr, req.ScoreThreshold, req.WithVectors), nil
}

func (c *chromaClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	logger.Debugf("Chroma does not support hybrid search; falling back to vector-only search")
	return c.VectorSearch(ctx, SearchRequest{
		CollectionName: req.CollectionName,
		QueryVector:    req.QueryVector,
		TopK:           req.TopK,
		ScoreThreshold: req.ScoreThreshold,
		Filters:        req.Filters,
		WithVectors:    false,
		SkipPayload:    req.SkipPayload,
	})
}

// chromaQueryResultToSearchResults converts a chroma v0.4.x QueryResult into our SearchResult slice.
// Chroma returns cosine distances (lower = closer); we convert to score = 1 - distance.
func chromaQueryResultToSearchResults(qr chromago.QueryResult, threshold float64, withVectors bool) []SearchResult {
	if qr == nil {
		return nil
	}
	idGroups := qr.GetIDGroups()
	if len(idGroups) == 0 || len(idGroups[0]) == 0 {
		return nil
	}
	ids := idGroups[0]
	var docGroup chromago.Documents
	if dg := qr.GetDocumentsGroups(); len(dg) > 0 {
		docGroup = dg[0]
	}
	var metaGroup chromago.DocumentMetadatas
	if mg := qr.GetMetadatasGroups(); len(mg) > 0 {
		metaGroup = mg[0]
	}
	var distGroup embeddings.Distances
	if dg := qr.GetDistancesGroups(); len(dg) > 0 {
		distGroup = dg[0]
	}
	var embGroup embeddings.Embeddings
	if withVectors {
		if eg := qr.GetEmbeddingsGroups(); len(eg) > 0 {
			embGroup = eg[0]
		}
	}

	out := make([]SearchResult, 0, len(ids))
	for i, id := range ids {
		distance := 0.0
		if i < len(distGroup) {
			distance = float64(distGroup[i])
		}
		score := 1.0 - distance
		if threshold > 0 && score < threshold {
			continue
		}
		sr := SearchResult{ID: string(id), Score: score, Payload: make(map[string]interface{})}
		if i < len(docGroup) && docGroup[i] != nil {
			sr.Content = docGroup[i].ContentString()
		}
		if i < len(metaGroup) && metaGroup[i] != nil {
			sr.Payload = chromaMetadataToMap(metaGroup[i])
		}
		if withVectors && i < len(embGroup) && embGroup[i] != nil {
			sr.Vector = toFloat64Slice(embGroup[i].ContentAsFloat32())
		}
		out = append(out, sr)
	}
	return out
}

// chromaWhereFilter converts our generic map[string]interface{} filter into a
// chroma-go v0.4.x WhereClause.
//
// Supported formats per map entry:
//
//	Scalar value (equality):
//	  {"field": "foo"}  → EqString
//	  {"field": 42}     → EqInt
//	  {"field": 3.14}   → EqFloat
//	  {"field": true}   → EqBool
//
//	Operator map:
//	  {"field": {"$eq":  val}}  → Eq*
//	  {"field": {"$ne":  val}}  → NotEq*
//	  {"field": {"$gt":  val}}  → Gt*
//	  {"field": {"$gte": val}}  → Gte*
//	  {"field": {"$lt":  val}}  → Lt*
//	  {"field": {"$lte": val}}  → Lte*
//	  {"field": {"$in":  [...]}} → In*
//	  {"field": {"$nin": [...]}} → Nin*
//
// Multiple entries are ANDed. Returns nil when filters is empty.
func chromaWhereFilter(filters map[string]interface{}) chromago.WhereFilter {
	if len(filters) == 0 {
		return nil
	}
	clauses := make([]chromago.WhereClause, 0, len(filters))
	for k, v := range filters {
		switch val := v.(type) {
		case map[string]interface{}:
			// Operator filter: {"$gt": 5} or {"$ne": "foo"} etc.
			for op, operand := range val {
				if clause := chromaOperatorClause(k, op, operand); clause != nil {
					clauses = append(clauses, clause)
				}
			}
		case string:
			clauses = append(clauses, chromago.EqString(k, val))
		case int:
			clauses = append(clauses, chromago.EqInt(k, val))
		case int32:
			clauses = append(clauses, chromago.EqInt(k, int(val)))
		case int64:
			clauses = append(clauses, chromago.EqInt(k, int(val)))
		case float32:
			clauses = append(clauses, chromago.EqFloat(k, val))
		case float64:
			clauses = append(clauses, chromago.EqFloat(k, float32(val)))
		case bool:
			clauses = append(clauses, chromago.EqBool(k, val))
		default:
			clauses = append(clauses, chromago.EqString(k, fmt.Sprintf("%v", v)))
		}
	}
	if len(clauses) == 0 {
		return nil
	}
	if len(clauses) == 1 {
		return clauses[0]
	}
	return chromago.And(clauses...)
}

// chromaOperatorClause builds a single WhereClause for one $op / value pair.
// Returns nil if the operator or operand type is unsupported — the caller skips
// nil clauses so unrecognised operators are silently ignored rather than panicking.
func chromaOperatorClause(key, op string, operand interface{}) chromago.WhereClause {
	switch op {
	case "$eq":
		return chromaEqClause(key, operand)
	case "$ne":
		switch v := operand.(type) {
		case string:
			return chromago.NotEqString(key, v)
		case int:
			return chromago.NotEqInt(key, v)
		case int32:
			return chromago.NotEqInt(key, int(v))
		case int64:
			return chromago.NotEqInt(key, int(v))
		case float32:
			return chromago.NotEqFloat(key, v)
		case float64:
			return chromago.NotEqFloat(key, float32(v))
		case bool:
			return chromago.NotEqBool(key, v)
		}
	case "$gt":
		switch v := operand.(type) {
		case int:
			return chromago.GtInt(key, v)
		case int32:
			return chromago.GtInt(key, int(v))
		case int64:
			return chromago.GtInt(key, int(v))
		case float32:
			return chromago.GtFloat(key, v)
		case float64:
			return chromago.GtFloat(key, float32(v))
		}
	case "$gte":
		switch v := operand.(type) {
		case int:
			return chromago.GteInt(key, v)
		case int32:
			return chromago.GteInt(key, int(v))
		case int64:
			return chromago.GteInt(key, int(v))
		case float32:
			return chromago.GteFloat(key, v)
		case float64:
			return chromago.GteFloat(key, float32(v))
		}
	case "$lt":
		switch v := operand.(type) {
		case int:
			return chromago.LtInt(key, v)
		case int32:
			return chromago.LtInt(key, int(v))
		case int64:
			return chromago.LtInt(key, int(v))
		case float32:
			return chromago.LtFloat(key, v)
		case float64:
			return chromago.LtFloat(key, float32(v))
		}
	case "$lte":
		switch v := operand.(type) {
		case int:
			return chromago.LteInt(key, v)
		case int32:
			return chromago.LteInt(key, int(v))
		case int64:
			return chromago.LteInt(key, int(v))
		case float32:
			return chromago.LteFloat(key, v)
		case float64:
			return chromago.LteFloat(key, float32(v))
		}
	case "$in":
		return chromaSetClause(key, operand, false)
	case "$nin":
		return chromaSetClause(key, operand, true)
	}
	return nil
}

// chromaEqClause builds an equality WhereClause for any scalar type.
func chromaEqClause(key string, v interface{}) chromago.WhereClause {
	switch val := v.(type) {
	case string:
		return chromago.EqString(key, val)
	case int:
		return chromago.EqInt(key, val)
	case int32:
		return chromago.EqInt(key, int(val))
	case int64:
		return chromago.EqInt(key, int(val))
	case float32:
		return chromago.EqFloat(key, val)
	case float64:
		return chromago.EqFloat(key, float32(val))
	case bool:
		return chromago.EqBool(key, val)
	default:
		return chromago.EqString(key, fmt.Sprintf("%v", v))
	}
}

// chromaSetClause builds an In/Nin WhereClause from a []interface{} operand.
// The element type is inferred from the first non-nil element.
func chromaSetClause(key string, operand interface{}, notIn bool) chromago.WhereClause {
	items, ok := operand.([]interface{})
	if !ok || len(items) == 0 {
		return nil
	}
	// Infer type from first element.
	switch items[0].(type) {
	case string:
		ss := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				ss = append(ss, s)
			}
		}
		if notIn {
			return chromago.NinString(key, ss...)
		}
		return chromago.InString(key, ss...)
	case int, int32, int64:
		is := make([]int, 0, len(items))
		for _, item := range items {
			switch n := item.(type) {
			case int:
				is = append(is, n)
			case int32:
				is = append(is, int(n))
			case int64:
				is = append(is, int(n))
			}
		}
		if notIn {
			return chromago.NinInt(key, is...)
		}
		return chromago.InInt(key, is...)
	case float32, float64:
		fs := make([]float32, 0, len(items))
		for _, item := range items {
			switch f := item.(type) {
			case float32:
				fs = append(fs, f)
			case float64:
				fs = append(fs, float32(f))
			}
		}
		if notIn {
			return chromago.NinFloat(key, fs...)
		}
		return chromago.InFloat(key, fs...)
	case bool:
		bs := make([]bool, 0, len(items))
		for _, item := range items {
			if b, ok := item.(bool); ok {
				bs = append(bs, b)
			}
		}
		if notIn {
			return chromago.NinBool(key, bs...)
		}
		return chromago.InBool(key, bs...)
	}
	return nil
}

// chromaMetadataToMap converts a chroma-go v0.4.x DocumentMetadata into a plain
// map[string]interface{} suitable for our Document.Payload field.
func chromaMetadataToMap(meta chromago.DocumentMetadata) map[string]interface{} {
	if meta == nil {
		return make(map[string]interface{})
	}
	impl, ok := meta.(*chromago.DocumentMetadataImpl)
	if !ok {
		return make(map[string]interface{})
	}
	result := make(map[string]interface{}, len(impl.Keys()))
	for _, k := range impl.Keys() {
		if s, ok := meta.GetString(k); ok {
			result[k] = s
		} else if i, ok := meta.GetInt(k); ok {
			result[k] = i
		} else if f, ok := meta.GetFloat(k); ok {
			result[k] = f
		} else if b, ok := meta.GetBool(k); ok {
			result[k] = b
		} else if sa, ok := meta.GetStringArray(k); ok {
			result[k] = sa
		} else if ia, ok := meta.GetIntArray(k); ok {
			result[k] = ia
		} else if fa, ok := meta.GetFloatArray(k); ok {
			result[k] = fa
		} else if ba, ok := meta.GetBoolArray(k); ok {
			result[k] = ba
		}
	}
	return result
}
