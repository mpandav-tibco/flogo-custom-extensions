package vectordb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/auth"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/filters"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/graphql"
	"github.com/weaviate/weaviate/entities/models"
)

// weaviateClient implements VectorDBClient for Weaviate.
type weaviateClient struct {
	client *weaviate.Client
	cfg    ConnectionConfig
}

// Compile-time proof that weaviateClient satisfies the full VectorDBClient interface.
var _ VectorDBClient = (*weaviateClient)(nil)

func newWeaviateClient(cfg ConnectionConfig) (VectorDBClient, error) {
	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "Weaviate: invalid TLS configuration", err)
	}

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

	if tlsCfg != nil {
		wcfg.ConnectionClient = &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		}
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

// weaviateClassName converts a caller-supplied collection name to the Weaviate
// class name that is actually stored in the schema.
//
// CONTRACT: Weaviate requires all class names to start with an uppercase
// letter (PascalCase). This function capitalises the first character so that
// callers can use any casing (e.g. "myCollection", "my_collection") without
// worrying about the Weaviate constraint.  The transformed name is surfaced in
// all error messages returned by this provider so callers can always see the
// name that was used when debugging schema or query issues.
//
// Example: "products" → "Products", "my_index" → "My_index"
func weaviateClassName(name string) string {
	if len(name) == 0 {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// weaviateSearchFields builds the GraphQL field list for search operations.
// scoreField must be the _additional clause appropriate for the query type:
//   - VectorSearch:   "_additional { id certainty distance }"
//   - HybridSearch:   "_additional { id score }"
//
// When skipPayload is false (the zero-value safe default), the content, _docId
// and _metadata fields are appended so callers receive full document payloads.
// Set skipPayload=true for ranking-only passes where only ID and score are needed.
func weaviateSearchFields(skipPayload bool, scoreField string) []graphql.Field {
	fields := []graphql.Field{{Name: scoreField}}
	if !skipPayload {
		fields = append(fields,
			graphql.Field{Name: "content"},
			graphql.Field{Name: "_docId"},
			graphql.Field{Name: "_metadata"},
		)
	}
	return fields
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
			// _metadata stores arbitrary document payload as a JSON string.
			// The schema stays fixed regardless of what keys callers pass in;
			// we serialize/deserialize at the connector edges.
			{Name: "_metadata", DataType: []string{"text"}},
		},
		VectorIndexConfig: map[string]interface{}{
			"distance": distance,
		},
		ReplicationConfig: &models.ReplicationConfig{
			Factor: int64(replication),
		},
	}

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		retryErr := c.client.Schema().ClassCreator().WithClass(classObj).Do(ctx)
		if retryErr != nil && strings.Contains(retryErr.Error(), "already exists") {
			return newError(ErrCodeCollectionExists, fmt.Sprintf("collection %q already exists", cfg.Name), retryErr)
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

func (c *weaviateClient) DeleteCollection(ctx context.Context, name string) error {
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		return c.client.Schema().ClassDeleter().WithClassName(weaviateClassName(name)).Do(ctx)
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteCollection failed", err)
	}
	return nil
}

func (c *weaviateClient) ListCollections(ctx context.Context) ([]string, error) {
	var names []string
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		schemaDump, retryErr := c.client.Schema().Getter().Do(ctx)
		if retryErr != nil {
			return retryErr
		}
		names = make([]string, len(schemaDump.Classes))
		for i, cls := range schemaDump.Classes {
			names[i] = cls.Class
		}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	return names, nil
}

func (c *weaviateClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	className := weaviateClassName(name)
	var exists bool
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, retryErr := c.client.Schema().ClassGetter().WithClassName(className).Do(ctx)
		if retryErr != nil {
			// Weaviate returns 404 when the class does not exist — not retryable.
			if strings.Contains(retryErr.Error(), "404") || strings.Contains(retryErr.Error(), "not found") {
				exists = false
				return nil
			}
			return retryErr
		}
		exists = true
		return nil
	}); err != nil {
		return false, newError(ErrCodeProviderError, "CollectionExists failed", err)
	}
	return exists, nil
}

// --- Document operations ---

func (c *weaviateClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatchWeaviate, "Weaviate"); err != nil {
		return err
	}

	className := weaviateClassName(collectionName)

	// Pre-build the objects slice from docs. The batcher is reconstructed
	// inside the retry closure because the Weaviate batcher accumulates
	// internal state and must start fresh after a failed Do() call.
	objects := make([]*models.Object, len(docs))
	for i, doc := range docs {
		// Serialize arbitrary payload to JSON so the Weaviate schema stays
		// fixed — unknown property keys no longer cause schema-strict errors.
		metaJSON := "{}"
		if len(doc.Payload) > 0 {
			if b, jsonErr := json.Marshal(doc.Payload); jsonErr == nil {
				metaJSON = string(b)
			}
		}
		objects[i] = &models.Object{
			Class: className,
			ID:    weaviateResolveUUID(doc.ID),
			Properties: map[string]interface{}{
				"content":   doc.Content,
				"_docId":    doc.ID,
				"_metadata": metaJSON,
			},
			Vector: toFloat32Slice(doc.Vector),
		}
	}

	var resp interface{}
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		batcher := c.client.Batch().ObjectsBatcher()
		for _, obj := range objects {
			batcher = batcher.WithObjects(obj)
		}
		var doErr error
		resp, doErr = batcher.Do(ctx)
		return doErr
	}); err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments batch failed", err)
	}

	if responses, ok := resp.([]models.ObjectsGetResponse); ok {
		for _, r := range responses {
			if r.Result != nil && r.Result.Errors != nil && len(r.Result.Errors.Error) > 0 {
				return newError(ErrCodeProviderError,
					fmt.Sprintf("UpsertDocuments partial error (class=%s): %s", className, r.Result.Errors.Error[0].Message), nil)
			}
		}
	}
	return nil
}

func (c *weaviateClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	className := weaviateClassName(collectionName)
	var doc *Document
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		obj, retryErr := c.client.Data().ObjectsGetter().
			WithClassName(className).
			WithID(weaviateResolveUUID(id).String()).
			WithVector().
			Do(ctx)
		if retryErr != nil {
			if strings.Contains(retryErr.Error(), "404") {
				return newError(ErrCodeDocumentNotFound,
					fmt.Sprintf("document %q not found in %q", id, collectionName), retryErr)
			}
			return retryErr
		}
		if len(obj) == 0 {
			return newError(ErrCodeDocumentNotFound,
				fmt.Sprintf("document %q not found in %q", id, collectionName), nil)
		}
		doc = weaviateObjectToDocument(obj[0])
		return nil
	}); err != nil {
		if ve, ok := err.(*VDBError); ok {
			return nil, ve
		}
		return nil, newError(ErrCodeProviderError, "GetDocument failed", err)
	}
	return doc, nil
}

func (c *weaviateClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	className := weaviateClassName(collectionName)

	// Build a WHERE filter on the _docId property to batch-delete all IDs in one
	// HTTP call instead of firing N individual DELETE requests.
	var whereFilter *filters.WhereBuilder
	if len(ids) == 1 {
		whereFilter = filters.Where().
			WithPath([]string{"_docId"}).
			WithOperator(filters.Equal).
			WithValueText(ids[0])
	} else {
		operands := make([]*filters.WhereBuilder, len(ids))
		for i, id := range ids {
			operands[i] = filters.Where().
				WithPath([]string{"_docId"}).
				WithOperator(filters.Equal).
				WithValueText(id)
		}
		whereFilter = filters.Where().
			WithOperator(filters.Or).
			WithOperands(operands)
	}

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, retryErr := c.client.Batch().ObjectsBatchDeleter().
			WithClassName(className).
			WithWhere(whereFilter).
			Do(ctx)
		return retryErr
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

func (c *weaviateClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if len(filters) == 0 {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter: filter map must not be empty", nil)
	}
	className := weaviateClassName(collectionName)

	whereFilter, err := buildWeaviateWhereFilter(filters)
	if err != nil {
		return 0, newError(ErrCodeProviderError, fmt.Sprintf("DeleteByFilter: cannot build filter: %s", err), nil)
	}

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, retryErr := c.client.Batch().ObjectsBatchDeleter().
			WithClassName(className).
			WithWhere(whereFilter).
			Do(ctx)
		return retryErr
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "DeleteByFilter failed", err)
	}
	// Weaviate does not return a deleted count via this API path.
	return -1, nil
}

func (c *weaviateClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	className := weaviateClassName(req.CollectionName)
	offset := 0
	if req.Offset != "" {
		if n, parseErr := fmt.Sscanf(req.Offset, "%d", &offset); n != 1 || parseErr != nil {
			return nil, newError(ErrCodeProviderError,
				fmt.Sprintf("ScrollDocuments: invalid offset cursor %q", req.Offset), parseErr)
		}
	}

	var scrollResult *ScrollResult
	if len(req.Filters) > 0 {
		// The Data API (ObjectsGetter) does not support WHERE filters.
		// Switch to GraphQL Get with WithWhere + WithOffset + WithLimit.
		wf, buildErr := buildWeaviateWhereFilter(req.Filters)
		if buildErr != nil {
			return nil, newError(ErrCodeProviderError,
				fmt.Sprintf("ScrollDocuments: cannot build filter: %s", buildErr), nil)
		}
		gqlFields := []graphql.Field{
			{Name: "_additional { id }"},
			{Name: "content"},
			{Name: "_docId"},
			{Name: "_metadata"},
		}
		var gqlResult *models.GraphQLResponse
		if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
			var gqlErr error
			gqlResult, gqlErr = c.client.GraphQL().Get().
				WithClassName(className).
				WithFields(gqlFields...).
				WithWhere(wf).
				WithLimit(limit).
				WithOffset(offset).
				Do(ctx)
			return gqlErr
		}); err != nil {
			return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
		}
		rawDocs := weaviateGraphQLToResults(gqlResult, className)
		docs := make([]Document, len(rawDocs))
		for i, sr := range rawDocs {
			docs[i] = Document{ID: sr.ID, Content: sr.Content, Payload: sr.Payload}
		}
		nextOffset := ""
		if len(docs) == limit {
			nextOffset = fmt.Sprintf("%d", offset+limit)
		}
		return &ScrollResult{Documents: docs, NextOffset: nextOffset, Total: -1}, nil
	}

	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		objects, retryErr := c.client.Data().ObjectsGetter().
			WithClassName(className).
			WithLimit(limit).
			WithOffset(offset).
			WithVector().
			Do(ctx)
		if retryErr != nil {
			return retryErr
		}
		docs := make([]Document, len(objects))
		for i, obj := range objects {
			docs[i] = *weaviateObjectToDocument(obj)
		}
		nextOffset := ""
		if len(objects) == limit {
			nextOffset = fmt.Sprintf("%d", offset+limit)
		}
		scrollResult = &ScrollResult{Documents: docs, NextOffset: nextOffset, Total: -1}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}
	return scrollResult, nil
}

func (c *weaviateClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	className := weaviateClassName(collectionName)

	// Weaviate's generic filter-to-Where translation requires knowing each
	// property's type (string / int / float / bool) at query time — information
	// we do not carry in the provider-agnostic filter map.  Rather than
	// silently returning the full-collection count (which would be wrong), we
	// surface ErrCodeNotImplemented so callers can handle the gap explicitly.
	if len(filters) > 0 {
		return 0, newError(ErrCodeNotImplemented,
			"CountDocuments with filters is not yet implemented for Weaviate; omit filters to count all documents", nil)
	}

	field := graphql.Field{Name: "meta { count }"}
	var count int64
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		result, retryErr := c.client.GraphQL().Aggregate().
			WithClassName(className).
			WithFields(field).
			Do(ctx)
		if retryErr != nil {
			return retryErr
		}
		aggData, ok := result.Data["Aggregate"].(map[string]interface{})
		if !ok {
			return nil
		}
		clsData, ok := aggData[className].([]interface{})
		if !ok || len(clsData) == 0 {
			return nil
		}
		row, ok := clsData[0].(map[string]interface{})
		if !ok {
			return nil
		}
		meta, ok := row["meta"].(map[string]interface{})
		if !ok {
			return nil
		}
		v, _ := meta["count"].(float64)
		count = int64(v)
		return nil
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
	}
	return count, nil
}

// --- Search ---

func (c *weaviateClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	className := weaviateClassName(req.CollectionName)

	tCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	nearVector := c.client.GraphQL().NearVectorArgBuilder().
		WithVector(toFloat32Slice(req.QueryVector))
	if req.ScoreThreshold > 0 {
		// WithDistance is correct for all Weaviate metric types (available since v1.14).
		// Distance threshold = 1 - score_threshold for cosine (and a reasonable
		// approximation for dot/euclidean). WithCertainty only works for cosine.
		distanceThreshold := float32(1.0 - req.ScoreThreshold)
		nearVector = nearVector.WithDistance(distanceThreshold)
	}

	fields := weaviateSearchFields(req.SkipPayload, "_additional { id certainty distance }")

	var result *models.GraphQLResponse
	if err := withRetry(tCtx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var gqlErr error
		q := c.client.GraphQL().Get().
			WithClassName(className).
			WithFields(fields...).
			WithNearVector(nearVector).
			WithLimit(req.TopK)
		if len(req.Filters) > 0 {
			wf, buildErr := buildWeaviateWhereFilter(req.Filters)
			if buildErr != nil {
				gqlErr = newError(ErrCodeProviderError, fmt.Sprintf("VectorSearch: cannot build filter: %s", buildErr), nil)
				return gqlErr
			}
			q = q.WithWhere(wf)
		}
		result, gqlErr = q.Do(tCtx)
		return gqlErr
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}

	return weaviateGraphQLToResults(result, className), nil
}

func (c *weaviateClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	// Hybrid search requires collectionName and topK but NOT a queryVector (text-only is valid).
	if req.CollectionName == "" {
		return nil, newError(ErrCodeCollectionNotFound, "collectionName must not be empty", nil)
	}
	if req.TopK <= 0 {
		return nil, newError(ErrCodeInvalidTopK, fmt.Sprintf("topK=%d must be > 0", req.TopK), nil)
	}

	className := weaviateClassName(req.CollectionName)

	fields := weaviateSearchFields(req.SkipPayload, "_additional { id score }")

	tCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	// Weaviate hybrid search uses the Hybrid operator (BM25 + vector fusion)
	hybrid := c.client.GraphQL().HybridArgumentBuilder().
		WithQuery(req.QueryText).
		WithVector(toFloat32Slice(req.QueryVector)).
		WithAlpha(float32(req.Alpha))

	var result *models.GraphQLResponse
	if err := withRetry(tCtx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var gqlErr error
		q := c.client.GraphQL().Get().
			WithClassName(className).
			WithFields(fields...).
			WithHybrid(hybrid).
			WithLimit(req.TopK)
		if len(req.Filters) > 0 {
			wf, buildErr := buildWeaviateWhereFilter(req.Filters)
			if buildErr != nil {
				gqlErr = newError(ErrCodeProviderError, fmt.Sprintf("HybridSearch: cannot build filter: %s", buildErr), nil)
				return gqlErr
			}
			q = q.WithWhere(wf)
		}
		result, gqlErr = q.Do(tCtx)
		return gqlErr
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "HybridSearch failed", err)
	}

	return weaviateGraphQLToResults(result, className), nil
}

// --- Helpers ---

// weaviateResolveUUID converts any string ID to a valid strfmt.UUID.
// Weaviate requires UUID-format object IDs.
// If the given ID is already a valid UUID it is used as-is; otherwise a
// deterministic UUID v5 is derived so the same string always maps to the
// same UUID. The original string is stored in the "_docId" property and
// recovered transparently on reads.
func weaviateResolveUUID(id string) strfmt.UUID {
	if _, err := uuid.Parse(id); err == nil {
		return strfmt.UUID(id)
	}
	// For all non-UUID strings (including numeric strings) derive a deterministic
	// UUID v5. Weaviate does not support raw numeric IDs — unlike Qdrant.
	derived := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(id))
	return strfmt.UUID(derived.String())
}

func weaviateObjectToDocument(obj *models.Object) *Document {
	doc := &Document{
		ID:      obj.ID.String(), // fallback: use internal Weaviate UUID
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
				// Recover the user-provided original string ID.
				if s, ok := v.(string); ok && s != "" {
					doc.ID = s
				}
			case "_metadata":
				// Deserialize the JSON payload stored by UpsertDocuments.
				if s, ok := v.(string); ok && s != "" && s != "{}" {
					var m map[string]interface{}
					if json.Unmarshal([]byte(s), &m) == nil {
						for mk, mv := range m {
							doc.Payload[mk] = mv
						}
					}
				}
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
			// Prefer distance-based score — works across all Weaviate metric types.
			// score = 1.0 - distance (cosine) is the universal approximation;
			// certainty is kept as fallback for environments where distance is absent.
			if dist, ok := add["distance"].(float64); ok {
				sr.Score = 1.0 - dist
			} else if cert, ok := add["certainty"].(float64); ok {
				sr.Score = cert
			}
			// HybridSearch returns "score" (BM25+vector fusion rank score).
			if hybridScore, ok := add["score"].(float64); ok {
				sr.Score = hybridScore
			}
		}

		for k, v := range row {
			switch k {
			case "_additional":
				// handled above
			case "content":
				sr.Content, _ = v.(string)
			case "_docId":
				// Recover the user-provided original string ID.
				if s, ok := v.(string); ok && s != "" {
					sr.ID = s
				}
			case "_metadata":
				// Deserialize the JSON payload stored by UpsertDocuments.
				if s, ok := v.(string); ok && s != "" && s != "{}" {
					var m map[string]interface{}
					if json.Unmarshal([]byte(s), &m) == nil {
						for mk, mv := range m {
							sr.Payload[mk] = mv
						}
					}
				}
			default:
				sr.Payload[k] = v
			}
		}
		out = append(out, sr)
	}
	return out
}

// weaviateSchemaProps is the fixed set of first-class properties declared in
// the schema created by CreateCollection.  Any payload field NOT in this set
// is stored inside the _metadata JSON blob and must be queried via a LIKE
// wildcard substring match on the _metadata property.
var weaviateSchemaProps = map[string]bool{
	"_docId":    true,
	"content":   true,
	"_metadata": true,
}

// buildWeaviateWhereFilter converts the provider-agnostic filter map to a
// Weaviate *filters.WhereBuilder. Supported operators: $eq (default), $ne,
// $gt, $gte, $lt, $lte. String values use WithValueText; numeric values use
// WithValueNumber; booleans use WithValueBoolean.
//
// Payload fields that are not first-class Weaviate schema properties are
// automatically routed to a LIKE wildcard filter on _metadata so that callers
// can filter on arbitrary document payload keys without knowing the schema
// layout. Only $eq semantics are supported for non-schema payload fields.
//
// Each key in the map may be:
//   - a scalar value: interpreted as field == value (implicit $eq)
//   - a map[string]interface{} with a single operator key
func buildWeaviateWhereFilter(f map[string]interface{}) (*filters.WhereBuilder, error) {
	type clause struct {
		field     string
		op        filters.WhereOperator
		valueStr  string
		valueNum  float64
		valueBool bool
		kind      string // "text", "number", "bool"
	}

	opMap := map[string]filters.WhereOperator{
		"$eq":  filters.Equal,
		"$ne":  filters.NotEqual,
		"$gt":  filters.GreaterThan,
		"$gte": filters.GreaterThanEqual,
		"$lt":  filters.LessThan,
		"$lte": filters.LessThanEqual,
	}

	clauses := make([]clause, 0, len(f))
	for field, raw := range f {
		op := filters.Equal
		value := raw

		if opMap2, ok := raw.(map[string]interface{}); ok {
			for opKey, opVal := range opMap2 {
				mapped, ok := opMap[opKey]
				if !ok {
					return nil, fmt.Errorf("unsupported operator %q for Weaviate filter on field %q", opKey, field)
				}
				op = mapped
				value = opVal
				break
			}
		}

		c := clause{field: field, op: op}
		switch v := value.(type) {
		case string:
			c.kind = "text"
			c.valueStr = v
		case float64:
			c.kind = "number"
			c.valueNum = v
		case int:
			c.kind = "number"
			c.valueNum = float64(v)
		case int64:
			c.kind = "number"
			c.valueNum = float64(v)
		case bool:
			c.kind = "bool"
			c.valueBool = v
		default:
			c.kind = "text"
			c.valueStr = fmt.Sprintf("%v", v)
		}
		clauses = append(clauses, c)
	}

	builder := func(c clause) *filters.WhereBuilder {
		// Payload fields that are not first-class schema properties are stored
		// as JSON inside _metadata.  Route them to a LIKE substring match so
		// that callers can filter on arbitrary payload keys.
		if !weaviateSchemaProps[c.field] {
			// Build the JSON fragment we expect to find inside _metadata.
			// json.Marshal produces compact, deterministic output so
			// `"key":"value"` or `"key":42` or `"key":true` will always match.
			var fragment string
			switch c.kind {
			case "text":
				// JSON-encode string: "field":"value"
				keyBytes, _ := json.Marshal(c.field)
				valBytes, _ := json.Marshal(c.valueStr)
				fragment = fmt.Sprintf("*%s:%s*", string(keyBytes), string(valBytes))
			case "number":
				keyBytes, _ := json.Marshal(c.field)
				fragment = fmt.Sprintf("*%s:%v*", string(keyBytes), c.valueNum)
			case "bool":
				keyBytes, _ := json.Marshal(c.field)
				fragment = fmt.Sprintf("*%s:%v*", string(keyBytes), c.valueBool)
			default:
				keyBytes, _ := json.Marshal(c.field)
				valBytes, _ := json.Marshal(c.valueStr)
				fragment = fmt.Sprintf("*%s:%s*", string(keyBytes), string(valBytes))
			}
			return filters.Where().
				WithPath([]string{"_metadata"}).
				WithOperator(filters.Like).
				WithValueText(fragment)
		}
		w := filters.Where().WithPath([]string{c.field}).WithOperator(c.op)
		switch c.kind {
		case "number":
			return w.WithValueNumber(c.valueNum)
		case "bool":
			return w.WithValueBoolean(c.valueBool)
		default:
			return w.WithValueText(c.valueStr)
		}
	}

	if len(clauses) == 1 {
		return builder(clauses[0]), nil
	}

	operands := make([]*filters.WhereBuilder, len(clauses))
	for i, c := range clauses {
		operands[i] = builder(c)
	}
	return filters.Where().WithOperator(filters.And).WithOperands(operands), nil
}
