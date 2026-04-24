package vectordb

import (
	"context"
	"fmt"
	"strings"
	"time"

	chromago "github.com/amikos-tech/chroma-go"
	"github.com/amikos-tech/chroma-go/types"
)

// chromaClient implements VectorDBClient for Chroma.
type chromaClient struct {
	client *chromago.Client
	cfg    ConnectionConfig
}

func newChromaClient(cfg ConnectionConfig) (VectorDBClient, error) {
	scheme := cfg.Scheme
	if scheme == "" {
		scheme = "http"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port)

	opts := []chromago.ClientOption{}
	if cfg.APIKey != "" {
		opts = append(opts, chromago.WithDefaultHeaders(map[string]string{
			"Authorization": "Bearer " + cfg.APIKey,
		}))
	}

	c, err := chromago.NewClient(baseURL, opts...)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "failed to create Chroma client", err)
	}

	return &chromaClient{client: c, cfg: cfg}, nil
}

func (c *chromaClient) DBType() string { return "chroma" }
func (c *chromaClient) Close() error   { return nil }

func (c *chromaClient) HealthCheck(ctx context.Context) error {
	_, err := c.client.Heartbeat(ctx)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "Chroma health check failed", err)
	}
	return nil
}

func (c *chromaClient) chromaDistance(metric string) types.DistanceFunction {
	switch strings.ToLower(metric) {
	case "dot":
		return types.IP
	case "euclidean":
		return types.L2
	default:
		return types.COSINE
	}
}

func (c *chromaClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}
	metadata := map[string]interface{}{"hnsw:space": strings.ToLower(cfg.DistanceMetric)}
	_, err := c.client.CreateCollection(ctx, cfg.Name, metadata, false, nil, c.chromaDistance(cfg.DistanceMetric))
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "UniqueConstraintError") {
			return newError(ErrCodeCollectionExists, fmt.Sprintf("collection %q already exists", cfg.Name), err)
		}
		return newError(ErrCodeProviderError, "CreateCollection failed", err)
	}
	return nil
}

func (c *chromaClient) DeleteCollection(ctx context.Context, name string) error {
	_, err := c.client.DeleteCollection(ctx, name)
	if err != nil {
		return newError(ErrCodeProviderError, "DeleteCollection failed", err)
	}
	return nil
}

func (c *chromaClient) ListCollections(ctx context.Context) ([]string, error) {
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

func (c *chromaClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	cols, err := c.ListCollections(ctx)
	if err != nil {
		return false, err
	}
	for _, col := range cols {
		if col == name {
			return true, nil
		}
	}
	return false, nil
}

func (c *chromaClient) getCollection(ctx context.Context, name string) (*chromago.Collection, error) {
	col, err := c.client.GetCollection(ctx, name, nil)
	if err != nil {
		return nil, newError(ErrCodeCollectionNotFound, fmt.Sprintf("collection %q not found", name), err)
	}
	return col, nil
}

func (c *chromaClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	col, err := c.getCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	ids := make([]string, len(docs))
	embeddings := make([]*types.Embedding, len(docs))
	metadatas := make([]map[string]interface{}, len(docs))
	documents := make([]string, len(docs))
	for i, doc := range docs {
		ids[i] = doc.ID
		f32 := toFloat32Slice(doc.Vector)
		embeddings[i] = &types.Embedding{ArrayOfFloat32: &f32}
		documents[i] = doc.Content
		meta := make(map[string]interface{})
		for k, v := range doc.Payload {
			meta[k] = v
		}
		metadatas[i] = meta
	}
	_, err = col.Upsert(ctx, embeddings, metadatas, documents, ids)
	if err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments failed", err)
	}
	return nil
}

func (c *chromaClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	col, err := c.getCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	results, err := col.Get(ctx, nil, nil, []string{id}, []types.QueryEnum{types.IDocuments, types.IMetadatas, types.IEmbeddings})
	if err != nil {
		return nil, newError(ErrCodeProviderError, "GetDocument failed", err)
	}
	if len(results.Ids) == 0 {
		return nil, newError(ErrCodeDocumentNotFound,
			fmt.Sprintf("document %q not found in collection %q", id, collectionName), nil)
	}
	doc := &Document{ID: results.Ids[0], Payload: make(map[string]interface{})}
	if len(results.Documents) > 0 {
		doc.Content = results.Documents[0]
	}
	if len(results.Metadatas) > 0 {
		doc.Payload = results.Metadatas[0]
	}
	if len(results.Embeddings) > 0 && results.Embeddings[0] != nil && results.Embeddings[0].ArrayOfFloat32 != nil {
		doc.Vector = toFloat64Slice(*results.Embeddings[0].ArrayOfFloat32)
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
	_, err = col.Delete(ctx, ids, nil, nil)
	if err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

func (c *chromaClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	col, err := c.getCollection(ctx, collectionName)
	if err != nil {
		return 0, err
	}
	_, err = col.Delete(ctx, nil, chromaWhereFilter(filters), nil)
	if err != nil {
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
		fmt.Sscanf(req.Offset, "%d", &offset)
	}
	include := []types.QueryEnum{types.IDocuments, types.IMetadatas}
	if req.WithVectors {
		include = append(include, types.IEmbeddings)
	}
	results, err := col.Get(ctx, nil, chromaWhereFilter(req.Filters), nil, include)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}
	end := offset + limit
	if end > len(results.Ids) {
		end = len(results.Ids)
	}
	docs := make([]Document, 0, end-offset)
	for i := offset; i < end; i++ {
		doc := Document{ID: results.Ids[i], Payload: make(map[string]interface{})}
		if i < len(results.Documents) {
			doc.Content = results.Documents[i]
		}
		if i < len(results.Metadatas) && results.Metadatas[i] != nil {
			doc.Payload = results.Metadatas[i]
		}
		if req.WithVectors && i < len(results.Embeddings) && results.Embeddings[i] != nil && results.Embeddings[i].ArrayOfFloat32 != nil {
			doc.Vector = toFloat64Slice(*results.Embeddings[i].ArrayOfFloat32)
		}
		docs = append(docs, doc)
	}
	nextOffset := ""
	if end < len(results.Ids) {
		nextOffset = fmt.Sprintf("%d", end)
	}
	return &ScrollResult{Documents: docs, NextOffset: nextOffset, Total: int64(len(results.Ids))}, nil
}

func (c *chromaClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	col, err := c.getCollection(ctx, collectionName)
	if err != nil {
		return 0, err
	}
	if len(filters) == 0 {
		count, err := col.Count(ctx)
		if err != nil {
			return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
		}
		return int64(count), nil
	}
	results, err := col.Get(ctx, nil, chromaWhereFilter(filters), nil, nil)
	if err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments (filtered) failed", err)
	}
	return int64(len(results.Ids)), nil
}

func (c *chromaClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}
	col, err := c.getCollection(ctx, req.CollectionName)
	if err != nil {
		return nil, err
	}
	_, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	include := []types.QueryEnum{types.IDocuments, types.IMetadatas, types.IDistances}
	if req.WithVectors {
		include = append(include, types.IEmbeddings)
	}
	f32 := toFloat32Slice(req.QueryVector)
	emb := &types.Embedding{ArrayOfFloat32: &f32}
	results, err := col.QueryWithOptions(ctx,
		types.WithQueryEmbedding(emb),
		types.WithNResults(int32(req.TopK)),
		types.WithWhereMap(chromaWhereFilter(req.Filters)),
		types.WithInclude(include...),
	)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}
	return chromaQueryResultsToSearchResults(results, req.ScoreThreshold, req.WithVectors), nil
}

func (c *chromaClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	logger.Warnf("Chroma does not support hybrid search; falling back to vector-only search")
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

func chromaQueryResultsToSearchResults(results *chromago.QueryResults, threshold float64, withVectors bool) []SearchResult {
	if results == nil || len(results.Ids) == 0 || len(results.Ids[0]) == 0 {
		return nil
	}
	out := make([]SearchResult, 0, len(results.Ids[0]))
	for i, id := range results.Ids[0] {
		distance := 0.0
		if len(results.Distances) > 0 && i < len(results.Distances[0]) {
			distance = float64(results.Distances[0][i])
		}
		score := 1.0 - distance
		if threshold > 0 && score < threshold {
			continue
		}
		sr := SearchResult{ID: id, Score: score, Payload: make(map[string]interface{})}
		if len(results.Documents) > 0 && i < len(results.Documents[0]) {
			sr.Content = results.Documents[0][i]
		}
		if len(results.Metadatas) > 0 && i < len(results.Metadatas[0]) && results.Metadatas[0][i] != nil {
			sr.Payload = results.Metadatas[0][i]
		}
		if withVectors && len(results.Metadatas) > 0 && i < len(results.Metadatas[0]) {
			// QueryResults does not carry Embeddings in Chroma v0.1.4 — vector not available in query results
			_ = withVectors
		}
		out = append(out, sr)
	}
	return out
}

func chromaWhereFilter(filters map[string]interface{}) map[string]interface{} {
	if len(filters) == 0 {
		return nil
	}
	where := make(map[string]interface{}, len(filters))
	for k, v := range filters {
		where[k] = map[string]interface{}{"$eq": v}
	}
	return where
}
