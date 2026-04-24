package vectordb

import (
	"context"
	"fmt"
	"strings"
	"time"

	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// milvusClient implements VectorDBClient for Milvus / Zilliz.
type milvusClient struct {
	client milvusclient.Client
	cfg    ConnectionConfig
}

func newMilvusClient(cfg ConnectionConfig) (VectorDBClient, error) {
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	c, err := milvusclient.NewClient(ctx, mCfg)
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
	_, err := c.client.ListCollections(ctx)
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
			WithName("vector").
			WithDataType(entity.FieldTypeFloatVector).
			WithDim(int64(cfg.Dimensions)))

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
	if err := c.client.DropCollection(ctx, name); err != nil {
		return newError(ErrCodeProviderError, "DeleteCollection failed", err)
	}
	return nil
}

func (c *milvusClient) ListCollections(ctx context.Context) ([]string, error) {
	cols, err := c.client.ListCollections(ctx)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.Name
	}
	return names, nil
}

func (c *milvusClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	ok, err := c.client.HasCollection(ctx, name)
	if err != nil {
		return false, newError(ErrCodeProviderError, "CollectionExists failed", err)
	}
	return ok, nil
}

// --- Document operations ---

func (c *milvusClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}

	ids := make([]string, len(docs))
	contents := make([]string, len(docs))
	vectors := make([][]float32, len(docs))

	for i, doc := range docs {
		ids[i] = doc.ID
		contents[i] = doc.Content
		vectors[i] = toFloat32Slice(doc.Vector)
	}

	idCol := entity.NewColumnVarChar("_id", ids)
	contentCol := entity.NewColumnVarChar("content", contents)
	vectorCol := entity.NewColumnFloatVector("vector", len(docs[0].Vector), vectors)

	_, err := c.client.Upsert(ctx, collectionName, "", idCol, contentCol, vectorCol)
	if err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments failed", err)
	}

	// Flush to ensure data is persisted
	if err = c.client.Flush(ctx, collectionName, false); err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments: flush failed", err)
	}

	return nil
}

func (c *milvusClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	expr := fmt.Sprintf(`_id == "%s"`, id)

	results, err := c.client.Query(ctx, collectionName, nil, expr, []string{"_id", "content"})
	if err != nil {
		return nil, newError(ErrCodeProviderError, "GetDocument failed", err)
	}
	if len(results) == 0 || results[0].Len() == 0 {
		return nil, newError(ErrCodeDocumentNotFound,
			fmt.Sprintf("document %q not found in collection %q", id, collectionName), nil)
	}

	doc := &Document{ID: id, Payload: make(map[string]interface{})}
	for _, col := range results {
		switch col.Name() {
		case "content":
			if col.Len() > 0 {
				v, _ := col.GetAsString(0)
				doc.Content = v
			}
		}
	}
	return doc, nil
}

func (c *milvusClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf(`"%s"`, id)
	}
	expr := fmt.Sprintf(`_id in [%s]`, strings.Join(quoted, ","))
	err := c.client.Delete(ctx, collectionName, "", expr)
	if err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

func (c *milvusClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	expr := buildMilvusExpr(filters)
	if expr == "" {
		return 0, newError(ErrCodeInvalidQueryVector, "DeleteByFilter requires at least one filter", nil)
	}
	err := c.client.Delete(ctx, collectionName, "", expr)
	if err != nil {
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
		fmt.Sscanf(req.Offset, "%d", &offset)
	}

	queryFields := []string{"_id", "content"}
	results, err := c.client.Query(ctx, req.CollectionName, nil, expr, queryFields,
		milvusclient.WithOffset(int64(offset)),
		milvusclient.WithLimit(int64(limit)),
	)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}

	docs := milvusQueryResultsToDocs(results)

	nextOffset := ""
	if len(docs) == limit {
		nextOffset = fmt.Sprintf("%d", offset+limit)
	}

	return &ScrollResult{Documents: docs, NextOffset: nextOffset, Total: -1}, nil
}

func (c *milvusClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	expr := buildMilvusExpr(filters)
	if expr == "" {
		expr = "_id != \"\""
	}

	stats, err := c.client.Query(ctx, collectionName, nil, expr, []string{"count(*)"},
		milvusclient.WithIgnoreGrowing())
	if err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
	}
	if len(stats) > 0 {
		v, err := stats[0].GetAsInt64(0)
		if err == nil {
			return v, nil
		}
	}
	return 0, nil
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
	outputFields := []string{"_id", "content"}

	expr := buildMilvusExpr(req.Filters)

	_, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	results, err := c.client.Search(ctx,
		req.CollectionName,
		nil,
		expr,
		outputFields,
		[]entity.Vector{queryVector},
		"vector",
		milvusMetricType("cosine"),
		req.TopK,
		sp,
	)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}

	return milvusSearchResultsToResults(results, req.ScoreThreshold), nil
}

func (c *milvusClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	// Milvus 2.4+ supports hybrid search with sparse+dense.
	// TODO: Implement sparse vector field + AnnSearchRequest for full hybrid search.
	// For now fall back to dense vector search.
	logger.Warnf("Milvus hybrid search (sparse+dense) is not yet implemented; falling back to vector search")
	return c.VectorSearch(ctx, SearchRequest{
		CollectionName: req.CollectionName,
		QueryVector:    req.QueryVector,
		TopK:           req.TopK,
		ScoreThreshold: req.ScoreThreshold,
		Filters:        req.Filters,
		WithVectors:    false,
		WithPayload:    true,
	})
}

// --- Helpers ---

func buildMilvusExpr(filters map[string]interface{}) string {
	if len(filters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(filters))
	for k, v := range filters {
		switch val := v.(type) {
		case string:
			parts = append(parts, fmt.Sprintf(`%s == "%s"`, k, val))
		case int, int64, float64:
			parts = append(parts, fmt.Sprintf(`%s == %v`, k, val))
		case bool:
			parts = append(parts, fmt.Sprintf(`%s == %v`, k, val))
		default:
			parts = append(parts, fmt.Sprintf(`%s == "%v"`, k, val))
		}
	}
	return strings.Join(parts, " && ")
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
				}
			}
			out = append(out, sr)
		}
	}
	return out
}
