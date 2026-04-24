package vectordb

import (
	"context"
	"fmt"
	"strings"
	"time"

	qdrant "github.com/qdrant/go-client/qdrant"
)

// qdrantClient implements VectorDBClient for Qdrant via gRPC.
type qdrantClient struct {
	client *qdrant.Client
	cfg    ConnectionConfig
}

func newQdrantClient(cfg ConnectionConfig) (VectorDBClient, error) {
	qCfg := &qdrant.Config{
		Host:   cfg.Host,
		Port:   cfg.GRPCPort,
		APIKey: cfg.APIKey,
		UseTLS: cfg.UseTLS,
	}

	c, err := qdrant.NewClient(qCfg)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "failed to connect to Qdrant", err)
	}

	return &qdrantClient{client: c, cfg: cfg}, nil
}

func (c *qdrantClient) DBType() string { return "qdrant" }

func (c *qdrantClient) Close() error {
	return c.client.Close()
}

func (c *qdrantClient) HealthCheck(ctx context.Context) error {
	_, err := c.client.HealthCheck(ctx)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "Qdrant health check failed", err)
	}
	return nil
}

// --- Collection management ---

func (c *qdrantClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}

	dist := qdrant.Distance_Cosine
	switch strings.ToLower(cfg.DistanceMetric) {
	case "dot":
		dist = qdrant.Distance_Dot
	case "euclidean":
		dist = qdrant.Distance_Euclid
	}

	onDisk := cfg.OnDisk
	err := c.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: cfg.Name,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(cfg.Dimensions),
			Distance: dist,
			OnDisk:   &onDisk,
		}),
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return newError(ErrCodeCollectionExists, fmt.Sprintf("collection %q already exists", cfg.Name), err)
		}
		return newError(ErrCodeProviderError, "CreateCollection failed", err)
	}
	return nil
}

func (c *qdrantClient) DeleteCollection(ctx context.Context, name string) error {
	err := c.client.DeleteCollection(ctx, name)
	if err != nil {
		return newError(ErrCodeProviderError, "DeleteCollection failed", err)
	}
	return nil
}

func (c *qdrantClient) ListCollections(ctx context.Context) ([]string, error) {
	resp, err := c.client.ListCollections(ctx)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	names := make([]string, len(resp))
	for i, col := range resp {
		names[i] = col
	}
	return names, nil
}

func (c *qdrantClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	collections, err := c.ListCollections(ctx)
	if err != nil {
		return false, err
	}
	for _, col := range collections {
		if col == name {
			return true, nil
		}
	}
	return false, nil
}

// --- Document operations ---

func (c *qdrantClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}

	points := make([]*qdrant.PointStruct, len(docs))
	for i, doc := range docs {
		payload := qdrantPayload(doc.Payload)
		if doc.Content != "" {
			payload["_content"] = &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: doc.Content}}
		}
		points[i] = &qdrant.PointStruct{
			Id:      qdrant.NewID(doc.ID),
			Vectors: qdrant.NewVectorsDense(toFloat32Slice(doc.Vector)),
			Payload: payload,
		}
	}

	wait := true
	_, err := c.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collectionName,
		Points:         points,
		Wait:           &wait,
	})
	if err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments failed", err)
	}
	return nil
}

func (c *qdrantClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	withPayload := true
	results, err := c.client.Get(ctx, &qdrant.GetPoints{
		CollectionName: collectionName,
		Ids:            []*qdrant.PointId{qdrant.NewID(id)},
		WithPayload:    qdrant.NewWithPayload(withPayload),
	})
	if err != nil {
		return nil, newError(ErrCodeProviderError, "GetDocument failed", err)
	}
	if len(results) == 0 {
		return nil, newError(ErrCodeDocumentNotFound,
			fmt.Sprintf("document %q not found in collection %q", id, collectionName), nil)
	}
	return qdrantPointToDocument(results[0]), nil
}

func (c *qdrantClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	pointIDs := make([]*qdrant.PointId, len(ids))
	for i, id := range ids {
		pointIDs[i] = qdrant.NewID(id)
	}
	wait := true
	_, err := c.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: collectionName,
		Points:         qdrant.NewPointsSelector(pointIDs...),
		Wait:           &wait,
	})
	if err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

func (c *qdrantClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	// TODO: translate generic filter map to qdrant.Filter — currently deletes all matching must-conditions.
	// For now, only simple equality filters are supported via the Qdrant filter DSL.
	wait := true
	_, err := c.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: collectionName,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Filter{
				Filter: buildQdrantFilter(filters),
			},
		},
		Wait: &wait,
	})
	if err != nil {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter failed", err)
	}
	return -1, nil // Qdrant does not return delete count in this path
}

func (c *qdrantClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	limit := uint32(req.Limit)
	if limit == 0 {
		limit = 100
	}

	withPayload := true
	params := &qdrant.ScrollPoints{
		CollectionName: req.CollectionName,
		Filter:         buildQdrantFilter(req.Filters),
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayload(withPayload),
		WithVectors:    &qdrant.WithVectorsSelector{SelectorOptions: &qdrant.WithVectorsSelector_Enable{Enable: req.WithVectors}},
	}
	if req.Offset != "" {
		params.Offset = qdrant.NewID(req.Offset)
	}

	results, err := c.client.Scroll(ctx, params)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}

	docs := make([]Document, len(results))
	for i, p := range results {
		docs[i] = *qdrantPointToDocument(p)
	}

	nextOffset := ""

	return &ScrollResult{Documents: docs, NextOffset: nextOffset, Total: -1}, nil
}

func (c *qdrantClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	exact := true
	resp, err := c.client.Count(ctx, &qdrant.CountPoints{
		CollectionName: collectionName,
		Filter:         buildQdrantFilter(filters),
		Exact:          &exact,
	})
	if err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
	}
	return int64(resp), nil
}

// --- Search ---

func (c *qdrantClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	limit := uint64(req.TopK)
	withPayload := true
	scoreThreshold := float32(req.ScoreThreshold)

	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	results, err := c.client.Query(queryCtx, &qdrant.QueryPoints{
		CollectionName: req.CollectionName,
		Query:          qdrant.NewQuery(toFloat32Slice(req.QueryVector)...),
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayload(withPayload),
		WithVectors:    &qdrant.WithVectorsSelector{SelectorOptions: &qdrant.WithVectorsSelector_Enable{Enable: req.WithVectors}},
		ScoreThreshold: &scoreThreshold,
		Filter:         buildQdrantFilter(req.Filters),
	})
	if err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}

	return qdrantScoredPointsToResults(results), nil
}

func (c *qdrantClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	// Qdrant supports hybrid search via the Prefetch + Fusion API.
	// For simplicity, we implement it as weighted vector-only search.
	// Full sparse+dense hybrid requires sparse vector indexing configured on the collection.
	// TODO: Implement qdrant.QueryPoints with Prefetch for true hybrid search.
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

func qdrantPayload(m map[string]interface{}) map[string]*qdrant.Value {
	out := make(map[string]*qdrant.Value, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			out[k] = &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: val}}
		case int:
			out[k] = qdrant.NewValueInt(int64(val))
		case int64:
			out[k] = qdrant.NewValueInt(val)
		case float64:
			out[k] = qdrant.NewValueDouble(val)
		case bool:
			out[k] = qdrant.NewValueBool(val)
		default:
			out[k] = &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: fmt.Sprintf("%v", val)}}
		}
	}
	return out
}

func qdrantPointToDocument(p *qdrant.RetrievedPoint) *Document {
	doc := &Document{
		ID:      p.GetId().GetUuid(),
		Payload: make(map[string]interface{}),
	}
	if doc.ID == "" {
		doc.ID = fmt.Sprintf("%d", p.GetId().GetNum())
	}
	for k, v := range p.GetPayload() {
		switch val := v.GetKind().(type) {
		case *qdrant.Value_StringValue:
			if k == "_content" {
				doc.Content = val.StringValue
			} else {
				doc.Payload[k] = val.StringValue
			}
		case *qdrant.Value_IntegerValue:
			doc.Payload[k] = val.IntegerValue
		case *qdrant.Value_DoubleValue:
			doc.Payload[k] = val.DoubleValue
		case *qdrant.Value_BoolValue:
			doc.Payload[k] = val.BoolValue
		}
	}
	return doc
}

func qdrantScoredPointsToResults(pts []*qdrant.ScoredPoint) []SearchResult {
	out := make([]SearchResult, len(pts))
	for i, p := range pts {
		doc := &Document{
			ID:      p.GetId().GetUuid(),
			Payload: make(map[string]interface{}),
		}
		if doc.ID == "" {
			doc.ID = fmt.Sprintf("%d", p.GetId().GetNum())
		}
		for k, v := range p.GetPayload() {
			switch val := v.GetKind().(type) {
			case *qdrant.Value_StringValue:
				if k == "_content" {
					doc.Content = val.StringValue
				} else {
					doc.Payload[k] = val.StringValue
				}
			case *qdrant.Value_IntegerValue:
				doc.Payload[k] = val.IntegerValue
			case *qdrant.Value_DoubleValue:
				doc.Payload[k] = val.DoubleValue
			case *qdrant.Value_BoolValue:
				doc.Payload[k] = val.BoolValue
			}
		}
		out[i] = SearchResult{
			ID:      doc.ID,
			Score:   float64(p.GetScore()),
			Payload: doc.Payload,
			Content: doc.Content,
		}
		if p.GetVectors() != nil {
			out[i].Vector = toFloat64Slice(p.GetVectors().GetVector().GetData())
		}
	}
	return out
}

// buildQdrantFilter builds a minimal Qdrant filter from a generic key=value map.
// Only equality matches are implemented. Complex filter DSL is left as a TODO.
func buildQdrantFilter(filters map[string]interface{}) *qdrant.Filter {
	if len(filters) == 0 {
		return nil
	}
	conds := make([]*qdrant.Condition, 0, len(filters))
	for k, v := range filters {
		conds = append(conds, &qdrant.Condition{
			ConditionOneOf: &qdrant.Condition_Field{
				Field: &qdrant.FieldCondition{
					Key: k,
					Match: &qdrant.Match{
						MatchValue: &qdrant.Match_Keyword{
							Keyword: fmt.Sprintf("%v", v),
						},
					},
				},
			},
		})
	}
	return &qdrant.Filter{Must: conds}
}
