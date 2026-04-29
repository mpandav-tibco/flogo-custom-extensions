package vectordb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	google_grpc "google.golang.org/grpc"
	grpc_credentials "google.golang.org/grpc/credentials"
)

// milvusClient implements VectorDBClient for Milvus / Zilliz.
type milvusClient struct {
	client milvusclient.Client
	cfg    ConnectionConfig
}

// Compile-time proof that milvusClient satisfies the full VectorDBClient interface.
var _ VectorDBClient = (*milvusClient)(nil)

// milvusMaxVarCharLen is the maximum VarChar field length configured for
// the content and _metadata schema fields. Documents exceeding this limit
// will be silently truncated by Milvus — we enforce it here to fail fast
// with a clear error rather than lose data.
const milvusMaxVarCharLen = 65535

func newMilvusClient(ctx context.Context, cfg ConnectionConfig) (VectorDBClient, error) {
	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "Milvus: invalid TLS configuration", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	mCfg := milvusclient.Config{
		Address: addr,
	}
	if cfg.Username != "" {
		mCfg.Username = cfg.Username
		mCfg.Password = cfg.Password
	}
	if cfg.DBName != "" {
		mCfg.DBName = cfg.DBName
	}
	if tlsCfg != nil {
		mCfg.DialOptions = append(mCfg.DialOptions,
			google_grpc.WithTransportCredentials(grpc_credentials.NewTLS(tlsCfg)))
	}

	connCtx, connCancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer connCancel()

	c, err := milvusclient.NewClient(connCtx, mCfg)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "failed to connect to Milvus", err)
	}

	return &milvusClient{client: c, cfg: cfg}, nil
}

func (c *milvusClient) DBType() string { return "milvus" }

func (c *milvusClient) Close() error {
	return c.client.Close()
}

func (c *milvusClient) HealthCheck(ctx context.Context) error {
	// Milvus SDK v2 does not expose a dedicated ping endpoint.
	// ListCollections with a short-timeout context exercises the full gRPC
	// path — it verifies both connectivity and credential validity.
	hCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := c.client.ListCollections(hCtx)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "Milvus health check failed", err)
	}
	return nil
}

// --- Collection management ---

func milvusMetricType(metric string) entity.MetricType {
	switch strings.ToLower(metric) {
	case "dot":
		return entity.IP
	case "euclidean":
		return entity.L2
	default:
		return entity.COSINE
	}
}

func (c *milvusClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}

	replication := cfg.ReplicationFactor
	if replication <= 0 {
		replication = 1
	}

	schema := entity.NewSchema().
		WithName(cfg.Name).
		WithDescription(fmt.Sprintf("VectorDB connector collection: %s", cfg.Name)).
		WithField(entity.NewField().
			WithName("_id").
			WithDataType(entity.FieldTypeVarChar).
			WithIsPrimaryKey(true).
			WithIsAutoID(false).
			WithMaxLength(512)).
		WithField(entity.NewField().
			WithName("content").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(65535)).
		WithField(entity.NewField().
			// _metadata stores arbitrary document payload as a JSON string so the
			// schema stays fixed regardless of what keys callers pass in.
			WithName("_metadata").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(65535)).
		WithField(entity.NewField().
			WithName("vector").
			WithDataType(entity.FieldTypeFloatVector).
			WithDim(int64(cfg.Dimensions)))

	// Milvus 2.4 does not return an error when a collection with the same name
	// already exists — it silently succeeds. We check explicitly so callers
	// can distinguish between a fresh creation and a pre-existing one.
	var exists bool
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var retryErr error
		exists, retryErr = c.client.HasCollection(ctx, cfg.Name)
		return retryErr
	}); err != nil {
		return newError(ErrCodeProviderError, "CreateCollection: failed to check existence", err)
	}
	if exists {
		return newError(ErrCodeCollectionExists, fmt.Sprintf("collection %q already exists", cfg.Name), nil)
	}

	err := c.client.CreateCollection(ctx, schema, int32(replication))
	if err != nil {
		if strings.Contains(err.Error(), "already exist") {
			return newError(ErrCodeCollectionExists, fmt.Sprintf("collection %q already exists", cfg.Name), err)
		}
		return newError(ErrCodeProviderError, "CreateCollection failed", err)
	}

	// Create HNSW index on the vector field
	idx, err := entity.NewIndexHNSW(milvusMetricType(cfg.DistanceMetric), 8, 64)
	if err != nil {
		return newError(ErrCodeProviderError, "CreateCollection: failed to build HNSW index params", err)
	}
	if err = c.client.CreateIndex(ctx, cfg.Name, "vector", idx, false); err != nil {
		return newError(ErrCodeProviderError, "CreateCollection: failed to create vector index", err)
	}

	// Load collection into memory so it is immediately queryable
	if err = c.client.LoadCollection(ctx, cfg.Name, false); err != nil {
		return newError(ErrCodeProviderError, "CreateCollection: failed to load collection", err)
	}

	return nil
}

func (c *milvusClient) DeleteCollection(ctx context.Context, name string) error {
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		return c.client.DropCollection(ctx, name)
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteCollection failed", err)
	}
	return nil
}

func (c *milvusClient) ListCollections(ctx context.Context) ([]string, error) {
	var cols []*entity.Collection
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var retryErr error
		cols, retryErr = c.client.ListCollections(ctx)
		return retryErr
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.Name
	}
	return names, nil
}

func (c *milvusClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	var ok bool
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var retryErr error
		ok, retryErr = c.client.HasCollection(ctx, name)
		return retryErr
	}); err != nil {
		return false, newError(ErrCodeProviderError, "CollectionExists failed", err)
	}
	return ok, nil
}

// --- Document operations ---

func (c *milvusClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatchMilvus, "Milvus"); err != nil {
		return err
	}

	ids := make([]string, len(docs))
	contents := make([]string, len(docs))
	metadatas := make([]string, len(docs))
	vectors := make([][]float32, len(docs))

	for i, doc := range docs {
		ids[i] = doc.ID
		contents[i] = doc.Content
		vectors[i] = toFloat32Slice(doc.Vector)
		// Serialize arbitrary payload to JSON so the Milvus schema stays fixed.
		metaJSON := "{}"
		if len(doc.Payload) > 0 {
			if b, jsonErr := json.Marshal(doc.Payload); jsonErr == nil {
				metaJSON = string(b)
			}
		}
		metadatas[i] = metaJSON
		// Enforce the VarChar field size limits defined in the collection schema.
		// Milvus silently truncates values that exceed MaxLength — we fail fast
		// with a clear error instead of losing data.
		if len(doc.Content) > milvusMaxVarCharLen {
			return newError(ErrCodeProviderError,
				fmt.Sprintf("UpsertDocuments: document[%d] (id=%s) content length %d exceeds Milvus VarChar limit %d",
					i, doc.ID, len(doc.Content), milvusMaxVarCharLen), nil)
		}
		if len(metaJSON) > milvusMaxVarCharLen {
			return newError(ErrCodeProviderError,
				fmt.Sprintf("UpsertDocuments: document[%d] (id=%s) serialized metadata length %d exceeds Milvus VarChar limit %d",
					i, doc.ID, len(metaJSON), milvusMaxVarCharLen), nil)
		}
	}

	idCol := entity.NewColumnVarChar("_id", ids)
	contentCol := entity.NewColumnVarChar("content", contents)
	metaCol := entity.NewColumnVarChar("_metadata", metadatas)
	vectorCol := entity.NewColumnFloatVector("vector", len(docs[0].Vector), vectors)

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, insertErr := c.client.Upsert(ctx, collectionName, "", idCol, contentCol, metaCol, vectorCol)
		return insertErr
	}); err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments failed", err)
	}
	// Flush ensures the written segment is sealed and visible to searches.
	// A transient flush failure leaves data in an unflushed (invisible) state,
	// so we retry with the same policy as the upsert.
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		return c.client.Flush(ctx, collectionName, false)
	}); err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments: flush failed", err)
	}

	return nil
}

func (c *milvusClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	expr := fmt.Sprintf(`_id == "%s"`, milvusEscapeString(id))
	// Fetch all fields including the vector so callers receive the full document.
	// The vector field is ~6 KB for 1536-dim embeddings; if you need metadata-only
	// lookups at scale, use ScrollDocuments with a filter instead.
	var doc *Document
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		results, retryErr := c.client.Query(ctx, collectionName, nil, expr, []string{"_id", "content", "_metadata", "vector"})
		if retryErr != nil {
			return retryErr
		}
		if len(results) == 0 || results[0].Len() == 0 {
			return newError(ErrCodeDocumentNotFound,
				fmt.Sprintf("document %q not found in collection %q", id, collectionName), nil)
		}
		d := &Document{ID: id, Payload: make(map[string]interface{})}
		for _, col := range results {
			if col.Len() == 0 {
				continue
			}
			switch col.Name() {
			case "content":
				v, _ := col.GetAsString(0)
				d.Content = v
			case "_metadata":
				v, _ := col.GetAsString(0)
				if v != "" && v != "{}" {
					var m map[string]interface{}
					if json.Unmarshal([]byte(v), &m) == nil {
						d.Payload = m
					}
				}
			case "vector":
				if fvCol, ok := col.(*entity.ColumnFloatVector); ok {
					data := fvCol.Data()
					if len(data) > 0 {
						d.Vector = toFloat64Slice(data[0])
					}
				}
			}
		}
		doc = d
		return nil
	}); err != nil {
		if ve, ok := err.(*VDBError); ok {
			return nil, ve
		}
		return nil, newError(ErrCodeProviderError, "GetDocument failed", err)
	}
	return doc, nil
}

func (c *milvusClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf(`"%s"`, milvusEscapeString(id))
	}
	expr := fmt.Sprintf(`_id in [%s]`, strings.Join(quoted, ","))
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		return c.client.Delete(ctx, collectionName, "", expr)
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

func (c *milvusClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	expr := buildMilvusExpr(filters)
	if expr == "" {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter requires at least one filter", nil)
	}
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		return c.client.Delete(ctx, collectionName, "", expr)
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter failed", err)
	}
	return -1, nil // Milvus does not return delete count in this path
}

func (c *milvusClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	expr := buildMilvusExpr(req.Filters)
	if expr == "" {
		expr = "_id != \"\""
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

	queryFields := []string{"_id", "content", "_metadata"}
	var scrollResult *ScrollResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		results, retryErr := c.client.Query(ctx, req.CollectionName, nil, expr, queryFields,
			milvusclient.WithOffset(int64(offset)),
			milvusclient.WithLimit(int64(limit)),
		)
		if retryErr != nil {
			return retryErr
		}
		docs := milvusQueryResultsToDocs(results)
		nextOffset := ""
		if len(docs) == limit {
			nextOffset = fmt.Sprintf("%d", offset+limit)
		}
		scrollResult = &ScrollResult{Documents: docs, NextOffset: nextOffset, Total: -1}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}
	return scrollResult, nil
}

func (c *milvusClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	expr := buildMilvusExpr(filters)
	if expr == "" {
		expr = "_id != \"\""
	}

	var count int64
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		stats, retryErr := c.client.Query(ctx, collectionName, nil, expr, []string{"count(*)"})
		if retryErr != nil {
			return retryErr
		}
		if len(stats) > 0 {
			v, err := stats[0].GetAsInt64(0)
			if err == nil {
				count = v
			}
		}
		return nil
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
	}
	return count, nil
}

// --- Search ---

func (c *milvusClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	sp, err := entity.NewIndexHNSWSearchParam(64)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch: failed to build search params", err)
	}

	queryVector := entity.FloatVector(toFloat32Slice(req.QueryVector))
	outputFields := []string{"_id"}
	if !req.SkipPayload {
		outputFields = append(outputFields, "content", "_metadata")
	}

	expr := buildMilvusExpr(req.Filters)

	tCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	var searchResults []milvusclient.SearchResult
	if err := withRetry(tCtx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var sErr error
		searchResults, sErr = c.client.Search(tCtx,
			req.CollectionName,
			nil,
			expr,
			outputFields,
			[]entity.Vector{queryVector},
			"vector",
			milvusMetricType(c.cfg.DefaultMetricType),
			req.TopK,
			sp,
		)
		return sErr
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}

	return milvusSearchResultsToResults(searchResults, req.ScoreThreshold), nil
}

func (c *milvusClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	// Milvus 2.4+ supports hybrid search with sparse+dense vectors, but that
	// requires a sparse vector field to be configured on the collection at
	// creation time — which this connector does not yet provision.
	// Fall back to dense vector search so the interface contract is honoured:
	// "Providers without native hybrid support fall back to VectorSearch."
	logger.Debugf("Milvus: HybridSearch falling back to dense VectorSearch (sparse+dense not provisioned)")
	return c.VectorSearch(ctx, SearchRequest{
		CollectionName: req.CollectionName,
		QueryVector:    req.QueryVector,
		TopK:           req.TopK,
		ScoreThreshold: req.ScoreThreshold,
		Filters:        req.Filters,
		SkipPayload:    req.SkipPayload,
	})
}

// --- Helpers ---

// milvusSchemaFields is the set of top-level Milvus collection fields created
// by CreateCollection. Any filter key NOT in this set is a payload key stored
// inside the _metadata VarChar JSON blob and must be queried via a LIKE
// substring match rather than a direct field expression.
var milvusSchemaFields = map[string]bool{
	"_id":       true,
	"content":   true,
	"_metadata": true,
}

// buildMilvusExpr converts a generic filter map to a Milvus boolean expression.
//
// Each entry can be a scalar (equality) or an operator map:
//
//	Scalar:   {"field": "foo"}               → field == "foo"
//	Operator: {"field": {"$gt": 5}}          → field > 5
//	          {"field": {"$in": ["a","b"]}}   → field in ["a","b"]
//	          {"field": {"$nin": [1,2,3]}}    → field not in [1,2,3]
//	          {"field": {"$ne": "foo"}}       → field != "foo"
//
// Payload fields that are not first-class Milvus schema fields are
// automatically routed to a LIKE wildcard on _metadata so that callers can
// filter on arbitrary document payload keys. Only equality semantics
// are used for non-schema payload fields.
//
// String values are quote-escaped to prevent expression injection.
// Multiple entries are ANDed.
func buildMilvusExpr(filters map[string]interface{}) string {
	if len(filters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(filters))
	for k, v := range filters {
		// Field names: only allow alphanumeric + underscore to prevent injection.
		safeK := milvusSafeFieldName(k)

		// Payload fields that are not top-level schema columns are stored as
		// JSON inside _metadata.  For scalar equality values, route them to a
		// LIKE substring match.  Operator-map values ($gt, $in, …) fall through
		// to the direct-expression path below — the caller is responsible for
		// ensuring such fields exist as schema columns.
		if !milvusSchemaFields[k] {
			_, isOpMap := v.(map[string]interface{})
			if !isOpMap {
				var fragment string
				switch val := v.(type) {
				case string:
					keyBytes, _ := json.Marshal(k)
					valBytes, _ := json.Marshal(val)
					fragment = fmt.Sprintf(`%%"%s":%s%%`, string(keyBytes[1:len(keyBytes)-1]), string(valBytes))
				case int:
					keyBytes, _ := json.Marshal(k)
					fragment = fmt.Sprintf(`%%"%s":%d%%`, string(keyBytes[1:len(keyBytes)-1]), val)
				case int64:
					keyBytes, _ := json.Marshal(k)
					fragment = fmt.Sprintf(`%%"%s":%d%%`, string(keyBytes[1:len(keyBytes)-1]), val)
				case float64:
					keyBytes, _ := json.Marshal(k)
					fragment = fmt.Sprintf(`%%"%s":%g%%`, string(keyBytes[1:len(keyBytes)-1]), val)
				case bool:
					keyBytes, _ := json.Marshal(k)
					fragment = fmt.Sprintf(`%%"%s":%v%%`, string(keyBytes[1:len(keyBytes)-1]), val)
				default:
					keyBytes, _ := json.Marshal(k)
					valBytes, _ := json.Marshal(fmt.Sprintf("%v", val))
					fragment = fmt.Sprintf(`%%"%s":%s%%`, string(keyBytes[1:len(keyBytes)-1]), string(valBytes))
				}
				parts = append(parts, fmt.Sprintf("_metadata like '%s'", fragment))
				continue
			}
		}

		switch val := v.(type) {
		case map[string]interface{}:
			if expr := milvusOperatorExpr(safeK, val); expr != "" {
				parts = append(parts, expr)
			}
		case string:
			parts = append(parts, fmt.Sprintf(`%s == "%s"`, safeK, milvusEscapeString(val)))
		case int:
			parts = append(parts, fmt.Sprintf(`%s == %d`, safeK, val))
		case int64:
			parts = append(parts, fmt.Sprintf(`%s == %d`, safeK, val))
		case float64:
			parts = append(parts, fmt.Sprintf(`%s == %g`, safeK, val))
		case bool:
			parts = append(parts, fmt.Sprintf(`%s == %v`, safeK, val))
		default:
			parts = append(parts, fmt.Sprintf(`%s == "%s"`, safeK, milvusEscapeString(fmt.Sprintf("%v", val))))
		}
	}
	return strings.Join(parts, " && ")
}

// milvusOperatorExpr builds a single expression fragment for one {"$op": val} map.
func milvusOperatorExpr(key string, ops map[string]interface{}) string {
	parts := make([]string, 0, len(ops))
	for op, operand := range ops {
		var expr string
		switch op {
		case "$eq":
			expr = milvusEqExpr(key, operand)
		case "$ne":
			expr = milvusScalarExpr(key, "!=", operand)
		case "$gt":
			expr = milvusScalarExpr(key, ">", operand)
		case "$gte":
			expr = milvusScalarExpr(key, ">=", operand)
		case "$lt":
			expr = milvusScalarExpr(key, "<", operand)
		case "$lte":
			expr = milvusScalarExpr(key, "<=", operand)
		case "$in":
			expr = milvusSetExpr(key, operand, false)
		case "$nin":
			expr = milvusSetExpr(key, operand, true)
		}
		if expr != "" {
			parts = append(parts, expr)
		}
	}
	return strings.Join(parts, " && ")
}

func milvusEqExpr(key string, v interface{}) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf(`%s == "%s"`, key, milvusEscapeString(val))
	case int:
		return fmt.Sprintf(`%s == %d`, key, val)
	case int64:
		return fmt.Sprintf(`%s == %d`, key, val)
	case float64:
		return fmt.Sprintf(`%s == %g`, key, val)
	case bool:
		return fmt.Sprintf(`%s == %v`, key, val)
	default:
		return fmt.Sprintf(`%s == "%s"`, key, milvusEscapeString(fmt.Sprintf("%v", v)))
	}
}

func milvusScalarExpr(key, op string, v interface{}) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf(`%s %s "%s"`, key, op, milvusEscapeString(val))
	case int:
		return fmt.Sprintf(`%s %s %d`, key, op, val)
	case int64:
		return fmt.Sprintf(`%s %s %d`, key, op, val)
	case float64:
		return fmt.Sprintf(`%s %s %g`, key, op, val)
	case bool:
		return fmt.Sprintf(`%s %s %v`, key, op, val)
	}
	return ""
}

func milvusSetExpr(key string, operand interface{}, notIn bool) string {
	items, ok := operand.([]interface{})
	if !ok || len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		switch val := item.(type) {
		case string:
			parts = append(parts, fmt.Sprintf(`"%s"`, milvusEscapeString(val)))
		case int:
			parts = append(parts, fmt.Sprintf(`%d`, val))
		case int64:
			parts = append(parts, fmt.Sprintf(`%d`, val))
		case float64:
			parts = append(parts, fmt.Sprintf(`%g`, val))
		case bool:
			parts = append(parts, fmt.Sprintf(`%v`, val))
		default:
			parts = append(parts, fmt.Sprintf(`"%s"`, milvusEscapeString(fmt.Sprintf("%v", val))))
		}
	}
	op := "in"
	if notIn {
		op = "not in"
	}
	return fmt.Sprintf(`%s %s [%s]`, key, op, strings.Join(parts, ", "))
}

// milvusEscapeString escapes double-quote characters in a Milvus string literal.
func milvusEscapeString(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// milvusSafeFieldName strips any character that is not alphanumeric or underscore
// to prevent field-name injection in Milvus boolean expressions.
func milvusSafeFieldName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func milvusQueryResultsToDocs(cols []entity.Column) []Document {
	if len(cols) == 0 {
		return nil
	}
	n := cols[0].Len()
	docs := make([]Document, n)
	for i := range docs {
		docs[i] = Document{Payload: make(map[string]interface{})}
	}
	for _, col := range cols {
		for i := 0; i < n; i++ {
			switch col.Name() {
			case "_id":
				v, _ := col.GetAsString(i)
				docs[i].ID = v
			case "content":
				v, _ := col.GetAsString(i)
				docs[i].Content = v
			case "_metadata":
				v, _ := col.GetAsString(i)
				if v != "" && v != "{}" {
					var m map[string]interface{}
					if json.Unmarshal([]byte(v), &m) == nil {
						docs[i].Payload = m
					}
				}
			}
		}
	}
	return docs
}

func milvusSearchResultsToResults(results []milvusclient.SearchResult, threshold float64) []SearchResult {
	var out []SearchResult
	for _, res := range results {
		for i := 0; i < res.ResultCount; i++ {
			score := float64(res.Scores[i])
			if threshold > 0 && score < threshold {
				continue
			}
			sr := SearchResult{
				Score:   score,
				Payload: make(map[string]interface{}),
			}
			for _, col := range res.Fields {
				switch col.Name() {
				case "_id":
					v, _ := col.GetAsString(i)
					sr.ID = v
				case "content":
					v, _ := col.GetAsString(i)
					sr.Content = v
				case "_metadata":
					v, _ := col.GetAsString(i)
					if v != "" && v != "{}" {
						var m map[string]interface{}
						if json.Unmarshal([]byte(v), &m) == nil {
							sr.Payload = m
						}
					}
				}
			}
			out = append(out, sr)
		}
	}
	return out
}
