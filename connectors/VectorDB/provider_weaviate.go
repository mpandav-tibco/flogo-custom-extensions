package vectordb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/weaviate/weaviate-go-client/v4/weaviate"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/auth"
	"github.com/weaviate/weaviate-go-client/v4/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
)

// weaviateClient implements VectorDBClient for Weaviate.
type weaviateClient struct {
	client *weaviate.Client
	cfg    ConnectionConfig
}

func newWeaviateClient(cfg ConnectionConfig) (VectorDBClient, error) {
	scheme := cfg.Scheme
	if scheme == "" {
		scheme = "http"
	}

	wcfg := weaviate.Config{
		Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Scheme: scheme,
	}

	if cfg.APIKey != "" {
		wcfg.AuthConfig = auth.ApiKey{Value: cfg.APIKey}
	}

	c, err := weaviate.NewClient(wcfg)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "failed to create Weaviate client", err)
	}

	return &weaviateClient{client: c, cfg: cfg}, nil
}

func (c *weaviateClient) DBType() string { return "weaviate" }
func (c *weaviateClient) Close() error   { return nil } // Weaviate client is stateless HTTP

func (c *weaviateClient) HealthCheck(ctx context.Context) error {
	ok, err := c.client.Misc().ReadyChecker().Do(ctx)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "Weaviate health check failed", err)
	}
	if !ok {
		return newError(ErrCodeConnectionFailed, "Weaviate is not ready", nil)
	}
	return nil
}

// --- Collection management ---

// Weaviate collections are called "classes". Names must be PascalCase.
func weaviateClassName(name string) string {
	if len(name) == 0 {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func (c *weaviateClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}

	distance := "cosine"
	switch strings.ToLower(cfg.DistanceMetric) {
	case "dot":
		distance = "dot"
	case "euclidean":
		distance = "l2-squared"
	}

	replication := cfg.ReplicationFactor
	if replication <= 0 {
		replication = 1
	}

	classObj := &models.Class{
		Class:       weaviateClassName(cfg.Name),
		Description: fmt.Sprintf("VectorDB connector collection: %s", cfg.Name),
		Properties: []*models.Property{
			{Name: "content", DataType: []string{"text"}},
			{Name: "_docId", DataType: []string{"text"}},
		},
		VectorIndexConfig: map[string]interface{}{
			"distance": distance,
		},
		ReplicationConfig: &models.ReplicationConfig{
			Factor: int64(replication),
		},
	}

	err := c.client.Schema().ClassCreator().WithClass(classObj).Do(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return newError(ErrCodeCollectionExists, fmt.Sprintf("collection %q already exists", cfg.Name), err)
		}
		return newError(ErrCodeProviderError, "CreateCollection failed", err)
	}
	return nil
}

func (c *weaviateClient) DeleteCollection(ctx context.Context, name string) error {
	err := c.client.Schema().ClassDeleter().WithClassName(weaviateClassName(name)).Do(ctx)
	if err != nil {
		return newError(ErrCodeProviderError, "DeleteCollection failed", err)
	}
	return nil
}

func (c *weaviateClient) ListCollections(ctx context.Context) ([]string, error) {
	schema, err := c.client.Schema().Getter().Do(ctx)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	names := make([]string, len(schema.Classes))
	for i, cls := range schema.Classes {
		names[i] = cls.Class
	}
	return names, nil
}

func (c *weaviateClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	collections, err := c.ListCollections(ctx)
	if err != nil {
		return false, err
	}
	className := weaviateClassName(name)
	for _, col := range collections {
		if col == className {
			return true, nil
		}
	}
	return false, nil
}

// --- Document operations ---

func (c *weaviateClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}

	className := weaviateClassName(collectionName)
	batcher := c.client.Batch().ObjectsBatcher()

	for _, doc := range docs {
		props := map[string]interface{}{
			"content": doc.Content,
			"_docId":  doc.ID,
		}
		for k, v := range doc.Payload {
			props[k] = v
		}

		obj := &models.Object{
			Class:      className,
			ID:         strfmt.UUID(doc.ID),
			Properties: props,
			Vector:     toFloat32Slice(doc.Vector),
		}
		batcher = batcher.WithObjects(obj)
	}

	resp, err := batcher.Do(ctx)
	if err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments batch failed", err)
	}

	for _, r := range resp {
		if r.Result != nil && r.Result.Errors != nil && len(r.Result.Errors.Error) > 0 {
			return newError(ErrCodeProviderError,
				fmt.Sprintf("UpsertDocuments partial error: %s", r.Result.Errors.Error[0].Message), nil)
		}
	}
	return nil
}

func (c *weaviateClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	className := weaviateClassName(collectionName)
	obj, err := c.client.Data().ObjectsGetter().
		WithClassName(className).
		WithID(id).
		WithVector().
		Do(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, newError(ErrCodeDocumentNotFound,
				fmt.Sprintf("document %q not found in %q", id, collectionName), err)
		}
		return nil, newError(ErrCodeProviderError, "GetDocument failed", err)
	}
	if len(obj) == 0 {
		return nil, newError(ErrCodeDocumentNotFound,
			fmt.Sprintf("document %q not found in %q", id, collectionName), nil)
	}

	return weaviateObjectToDocument(obj[0]), nil
}

func (c *weaviateClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	className := weaviateClassName(collectionName)
	for _, id := range ids {
		err := c.client.Data().Deleter().
			WithClassName(className).
			WithID(id).
			Do(ctx)
		if err != nil && !strings.Contains(err.Error(), "404") {
			return newError(ErrCodeProviderError, fmt.Sprintf("delete of document %q failed", id), err)
		}
	}
	return nil
}

func (c *weaviateClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	// TODO: Weaviate batch delete requires building a Where filter expression.
	// Returning not-supported for complex filters until the Where builder is wired in.
	return 0, newError(ErrCodeHybridNotSupported,
		"DeleteByFilter is not yet implemented for Weaviate; use DeleteDocuments with explicit IDs", nil)
}

func (c *weaviateClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	className := weaviateClassName(req.CollectionName)
	offset := 0
	if req.Offset != "" {
		fmt.Sscanf(req.Offset, "%d", &offset)
	}

	objects, err := c.client.Data().ObjectsGetter().
		WithClassName(className).
		WithLimit(limit).
		WithOffset(offset).
		WithVector().
		Do(ctx)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}

	docs := make([]Document, len(objects))
	for i, obj := range objects {
		docs[i] = *weaviateObjectToDocument(obj)
	}

	nextOffset := ""
	if len(objects) == limit {
		nextOffset = fmt.Sprintf("%d", offset+limit)
	}

	return &ScrollResult{Documents: docs, NextOffset: nextOffset, Total: -1}, nil
}

func (c *weaviateClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	className := weaviateClassName(collectionName)

	field := graphql.Field{Name: "meta { count }"}
	result, err := c.client.GraphQL().Aggregate().
		WithClassName(className).
		WithFields(field).
		Do(ctx)
	if err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
	}

	aggData, ok := result.Data["Aggregate"].(map[string]interface{})
	if !ok {
		return 0, nil
	}
	clsData, ok := aggData[className].([]interface{})
	if !ok || len(clsData) == 0 {
		return 0, nil
	}
	row, ok := clsData[0].(map[string]interface{})
	if !ok {
		return 0, nil
	}
	meta, ok := row["meta"].(map[string]interface{})
	if !ok {
		return 0, nil
	}
	count, _ := meta["count"].(float64)
	return int64(count), nil
}

// --- Search ---

func (c *weaviateClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	className := weaviateClassName(req.CollectionName)

	_, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	nearVector := c.client.GraphQL().NearVectorArgBuilder().
		WithVector(toFloat32Slice(req.QueryVector))
	if req.ScoreThreshold > 0 {
		certainty := float32(req.ScoreThreshold)
		nearVector = nearVector.WithCertainty(certainty)
	}

	fields := []graphql.Field{
		{Name: "_additional { id certainty distance }"},
		{Name: "content"},
		{Name: "_docId"},
	}

	result, err := c.client.GraphQL().Get().
		WithClassName(className).
		WithFields(fields...).
		WithNearVector(nearVector).
		WithLimit(req.TopK).
		Do(ctx)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}

	return weaviateGraphQLToResults(result, className), nil
}

func (c *weaviateClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	className := weaviateClassName(req.CollectionName)

	fields := []graphql.Field{
		{Name: "_additional { id score }"},
		{Name: "content"},
		{Name: "_docId"},
	}

	// Weaviate hybrid search uses the Hybrid operator (BM25 + vector fusion)
	hybrid := c.client.GraphQL().HybridArgumentBuilder().
		WithQuery(req.QueryText).
		WithVector(toFloat32Slice(req.QueryVector)).
		WithAlpha(float32(req.Alpha))

	result, err := c.client.GraphQL().Get().
		WithClassName(className).
		WithFields(fields...).
		WithHybrid(hybrid).
		WithLimit(req.TopK).
		Do(ctx)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "HybridSearch failed", err)
	}

	return weaviateGraphQLToResults(result, className), nil
}

// --- Helpers ---

func weaviateObjectToDocument(obj *models.Object) *Document {
	doc := &Document{
		ID:      obj.ID.String(),
		Payload: make(map[string]interface{}),
	}
	if obj.Vector != nil {
		doc.Vector = toFloat64Slice([]float32(obj.Vector))
	}
	if props, ok := obj.Properties.(map[string]interface{}); ok {
		for k, v := range props {
			switch k {
			case "content":
				doc.Content, _ = v.(string)
			case "_docId":
				// already stored as ID
			default:
				doc.Payload[k] = v
			}
		}
	}
	return doc
}

func weaviateGraphQLToResults(result *models.GraphQLResponse, className string) []SearchResult {
	if result == nil || result.Data == nil {
		return nil
	}
	getData, ok := result.Data["Get"].(map[string]interface{})
	if !ok {
		return nil
	}
	items, ok := getData[className].([]interface{})
	if !ok {
		return nil
	}

	out := make([]SearchResult, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		sr := SearchResult{Payload: make(map[string]interface{})}

		if add, ok := row["_additional"].(map[string]interface{}); ok {
			sr.ID, _ = add["id"].(string)
			if cert, ok := add["certainty"].(float64); ok {
				sr.Score = cert
			}
			if score, ok := add["score"].(float64); ok {
				sr.Score = score
			}
		}

		for k, v := range row {
			switch k {
			case "_additional":
				// handled above
			case "content":
				sr.Content, _ = v.(string)
			case "_docId":
				// skip internal field
			default:
				sr.Payload[k] = v
			}
		}
		out = append(out, sr)
	}
	return out
}
