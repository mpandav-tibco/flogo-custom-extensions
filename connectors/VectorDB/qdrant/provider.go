package vectordb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
)

// qdrantClient implements VectorDBClient for Qdrant via gRPC.
type qdrantClient struct {
	client *qdrant.Client
	cfg    ConnectionConfig
}

// Compile-time proof that qdrantClient satisfies the full VectorDBClient interface.
var _ VectorDBClient = (*qdrantClient)(nil)

func newQdrantClient(cfg ConnectionConfig) (VectorDBClient, error) {
	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "Qdrant: invalid TLS configuration", err)
	}

	qCfg := &qdrant.Config{
		Host:      cfg.Host,
		Port:      cfg.GRPCPort,
		APIKey:    cfg.APIKey,
		UseTLS:    cfg.UseTLS,
		TLSConfig: tlsCfg,
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
	collCfg := &qdrant.CreateCollection{
		CollectionName: cfg.Name,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(cfg.Dimensions),
			Distance: dist,
			OnDisk:   &onDisk,
		}),
	}
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		retryErr := c.client.CreateCollection(ctx, collCfg)
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

func (c *qdrantClient) DeleteCollection(ctx context.Context, name string) error {
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		return c.client.DeleteCollection(ctx, name)
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteCollection failed", err)
	}
	return nil
}

func (c *qdrantClient) ListCollections(ctx context.Context) ([]string, error) {
	var resp []string
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var retryErr error
		resp, retryErr = c.client.ListCollections(ctx)
		return retryErr
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ListCollections failed", err)
	}
	names := make([]string, len(resp))
	for i, col := range resp {
		names[i] = col
	}
	return names, nil
}

func (c *qdrantClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var retryErr error
		exists, retryErr = c.client.CollectionExists(ctx, name)
		return retryErr
	}); err != nil {
		return false, newError(ErrCodeProviderError, "CollectionExists failed", err)
	}
	return exists, nil
}

// --- Document operations ---

func (c *qdrantClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatchQdrant, "Qdrant"); err != nil {
		return err
	}

	points := make([]*qdrant.PointStruct, len(docs))
	for i, doc := range docs {
		payload := qdrantPayload(doc.Payload)
		if doc.Content != "" {
			payload["_content"] = &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: doc.Content}}
		}
		// Store the original string ID so it can be recovered on reads.
		// Qdrant only accepts UUID or uint64; arbitrary strings are converted to a
		// deterministic UUID v5 by qdrantResolveID.
		payload["_original_id"] = &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: doc.ID}}
		points[i] = &qdrant.PointStruct{
			Id:      qdrantResolveID(doc.ID),
			Vectors: qdrant.NewVectorsDense(toFloat32Slice(doc.Vector)),
			Payload: payload,
		}
	}

	wait := true
	upsertReq := &qdrant.UpsertPoints{
		CollectionName: collectionName,
		Points:         points,
		Wait:           &wait,
	}
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, retryErr := c.client.Upsert(ctx, upsertReq)
		return retryErr
	}); err != nil {
		return newError(ErrCodeProviderError, "UpsertDocuments failed", err)
	}
	return nil
}

func (c *qdrantClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	withPayload := true
	withVectors := true
	getReq := &qdrant.GetPoints{
		CollectionName: collectionName,
		Ids:            []*qdrant.PointId{qdrantResolveID(id)},
		WithPayload:    qdrant.NewWithPayload(withPayload),
		WithVectors:    &qdrant.WithVectorsSelector{SelectorOptions: &qdrant.WithVectorsSelector_Enable{Enable: withVectors}},
	}
	var doc *Document
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		results, retryErr := c.client.Get(ctx, getReq)
		if retryErr != nil {
			return retryErr
		}
		if len(results) == 0 {
			return newError(ErrCodeDocumentNotFound,
				fmt.Sprintf("document %q not found in collection %q", id, collectionName), nil)
		}
		doc = qdrantPointToDocument(results[0])
		return nil
	}); err != nil {
		if ve, ok := err.(*VDBError); ok {
			return nil, ve
		}
		return nil, newError(ErrCodeProviderError, "GetDocument failed", err)
	}
	return doc, nil
}

func (c *qdrantClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	pointIDs := make([]*qdrant.PointId, len(ids))
	for i, id := range ids {
		pointIDs[i] = qdrantResolveID(id)
	}
	wait := true
	delReq := &qdrant.DeletePoints{
		CollectionName: collectionName,
		Points:         qdrant.NewPointsSelector(pointIDs...),
		Wait:           &wait,
	}
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, retryErr := c.client.Delete(ctx, delReq)
		return retryErr
	}); err != nil {
		return newError(ErrCodeProviderError, "DeleteDocuments failed", err)
	}
	return nil
}

func (c *qdrantClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if len(filters) == 0 {
		// An empty filter would delete ALL documents in the collection.
		// Require at least one filter to prevent accidental data loss.
		return 0, newError(ErrCodeProviderError, "DeleteByFilter requires at least one filter", nil)
	}
	wait := true
	delReq := &qdrant.DeletePoints{
		CollectionName: collectionName,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Filter{
				Filter: buildQdrantFilter(filters),
			},
		},
		Wait: &wait,
	}
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		_, retryErr := c.client.Delete(ctx, delReq)
		return retryErr
	}); err != nil {
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

	// Use the raw gRPC Scroll call so we can extract both the result points AND
	// the next-page offset from a single round-trip.  The high-level
	// c.client.Scroll() discards NextPageOffset, which would force a second call.
	var scrollResult *ScrollResult
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		rawResp, retryErr := c.client.GetPointsClient().Scroll(ctx, params)
		if retryErr != nil {
			return retryErr
		}
		pts := rawResp.GetResult()
		docs := make([]Document, len(pts))
		for i, p := range pts {
			docs[i] = *qdrantPointToDocument(p)
		}
		nextOffset := ""
		if npo := rawResp.GetNextPageOffset(); npo != nil {
			if uuid := npo.GetUuid(); uuid != "" {
				nextOffset = uuid
			} else if num := npo.GetNum(); num != 0 {
				nextOffset = fmt.Sprintf("%d", num)
			}
		}
		scrollResult = &ScrollResult{Documents: docs, NextOffset: nextOffset, Total: -1}
		return nil
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "ScrollDocuments failed", err)
	}
	return scrollResult, nil
}

func (c *qdrantClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	exact := true
	countReq := &qdrant.CountPoints{
		CollectionName: collectionName,
		Filter:         buildQdrantFilter(filters),
		Exact:          &exact,
	}
	var count uint64
	if err := withRetry(ctx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var retryErr error
		count, retryErr = c.client.Count(ctx, countReq)
		return retryErr
	}); err != nil {
		return 0, newError(ErrCodeProviderError, "CountDocuments failed", err)
	}
	return int64(count), nil
}

// --- Search ---

func (c *qdrantClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	tCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	limit := uint64(req.TopK)
	// SkipPayload=false (zero value) means include payload — the safe default.
	// Set req.SkipPayload=true for ranking-only passes that don't need metadata.
	withPayload := !req.SkipPayload

	qparams := &qdrant.QueryPoints{
		CollectionName: req.CollectionName,
		Query:          qdrant.NewQuery(toFloat32Slice(req.QueryVector)...),
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayload(withPayload),
		WithVectors:    &qdrant.WithVectorsSelector{SelectorOptions: &qdrant.WithVectorsSelector_Enable{Enable: req.WithVectors}},
		Filter:         buildQdrantFilter(req.Filters),
	}
	// Only set ScoreThreshold when explicitly requested (> 0).
	// Passing 0.0 to Qdrant is treated as a literal threshold and would filter
	// all negative-cosine results even when the caller intends "no threshold".
	if req.ScoreThreshold > 0 {
		st := float32(req.ScoreThreshold)
		qparams.ScoreThreshold = &st
	}

	var results []*qdrant.ScoredPoint
	if err := withRetry(tCtx, c.cfg.MaxRetries, c.cfg.RetryBackoffMs, func() error {
		var qErr error
		results, qErr = c.client.Query(tCtx, qparams)
		return qErr
	}); err != nil {
		return nil, newError(ErrCodeProviderError, "VectorSearch failed", err)
	}

	return qdrantScoredPointsToResults(results), nil
}

func (c *qdrantClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	// Qdrant supports native hybrid search via Prefetch + Fusion, but that requires
	// a sparse vector field configured on the collection at creation time.
	// Until sparse indexing is supported by this connector, we fall back to dense
	// vector search so callers get best-effort results rather than an error.
	// TODO: Implement qdrant.QueryPoints with Prefetch for true sparse+dense hybrid.
	logger.Warnf("Qdrant: HybridSearch falling back to dense vector search — sparse indexing is not configured " +
		"on this connector; BM25/keyword component is absent. Set alpha=1.0 or use a provider with native hybrid support.")
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

// --- Helpers ---

// qdrantResolveID converts any string ID to a Qdrant-compatible PointId.
// Qdrant only supports UUID strings and unsigned 64-bit integers as point IDs.
// For any other string, a deterministic UUID v5 is derived so that the same
// input always maps to the same Qdrant ID. The original string is stored in
// the "_original_id" payload field and recovered transparently on reads.
func qdrantResolveID(id string) *qdrant.PointId {
	// Try valid UUID first.
	if _, err := uuid.Parse(id); err == nil {
		return qdrant.NewID(id)
	}
	// Try uint64 — Qdrant supports native numeric IDs.
	if n, err := strconv.ParseUint(id, 10, 64); err == nil {
		return qdrant.NewIDNum(n)
	}
	// Derive a deterministic UUID v5 from the arbitrary string.
	derived := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(id))
	return qdrant.NewID(derived.String())
}

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

// qdrantDecodeDocument reconstructs a Document from the raw Qdrant point components.
// Shared by qdrantPointToDocument (RetrievedPoint) and qdrantScoredPointsToResults
// (ScoredPoint) to keep payload/ID/vector extraction logic in one place.
// vec is the pre-extracted float32 slice from either VectorsOutput or Vectors; nil = no vector.
func qdrantDecodeDocument(pointID *qdrant.PointId, payload map[string]*qdrant.Value, vec []float32) Document {
	doc := Document{
		ID:      pointID.GetUuid(),
		Payload: make(map[string]interface{}),
	}
	if doc.ID == "" {
		doc.ID = fmt.Sprintf("%d", pointID.GetNum())
	}
	for k, v := range payload {
		switch val := v.GetKind().(type) {
		case *qdrant.Value_StringValue:
			switch k {
			case "_content":
				doc.Content = val.StringValue
			case "_original_id":
				// Recover the user-provided string ID.
				doc.ID = val.StringValue
			default:
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
	if vec != nil {
		doc.Vector = toFloat64Slice(vec)
	}
	return doc
}

func qdrantPointToDocument(p *qdrant.RetrievedPoint) *Document {
	var vec []float32
	if p.GetVectors() != nil {
		vec = p.GetVectors().GetVector().GetData()
	}
	doc := qdrantDecodeDocument(p.GetId(), p.GetPayload(), vec)
	return &doc
}

func qdrantScoredPointsToResults(pts []*qdrant.ScoredPoint) []SearchResult {
	out := make([]SearchResult, len(pts))
	for i, p := range pts {
		var vec []float32
		if p.GetVectors() != nil {
			vec = p.GetVectors().GetVector().GetData()
		}
		doc := qdrantDecodeDocument(p.GetId(), p.GetPayload(), vec)
		out[i] = SearchResult{
			ID:      doc.ID,
			Score:   float64(p.GetScore()),
			Payload: doc.Payload,
			Content: doc.Content,
			Vector:  doc.Vector,
		}
	}
	return out
}

// buildQdrantFilter builds a Qdrant gRPC Filter from a generic key/value map.
//
// Each map entry can be either a scalar equality or an operator map:
//
//	Scalar:   {"field": "foo"}               → keyword match
//	Operator: {"field": {"$gt": 5.0}}        → numeric range
//	          {"field": {"$in": ["a","b"]}}   → keyword/integer set match
//	          {"field": {"$nin": [1,2,3]}}    → except-integers
//	          {"field": {"$ne": "foo"}}       → except-keywords
//
// Multiple top-level entries are ANDed via Must[].
// Unrecognised operators are silently skipped (log a warning upstream if needed).
func buildQdrantFilter(filters map[string]interface{}) *qdrant.Filter {
	if len(filters) == 0 {
		return nil
	}
	conds := make([]*qdrant.Condition, 0, len(filters))
	for k, v := range filters {
		switch val := v.(type) {
		case map[string]interface{}:
			// Operator map: {"$gt": 5} etc.
			if cond := qdrantOperatorCondition(k, val); cond != nil {
				conds = append(conds, cond)
			}
		case bool:
			conds = append(conds, qdrantMatchCond(k,
				&qdrant.Match{MatchValue: &qdrant.Match_Boolean{Boolean: val}}))
		case int:
			conds = append(conds, qdrantMatchCond(k,
				&qdrant.Match{MatchValue: &qdrant.Match_Integer{Integer: int64(val)}}))
		case int64:
			conds = append(conds, qdrantMatchCond(k,
				&qdrant.Match{MatchValue: &qdrant.Match_Integer{Integer: val}}))
		case float64:
			// Qdrant has no float equality match; fall back to a tight range.
			conds = append(conds, qdrantRangeCond(k, &qdrant.Range{
				Gte: float64Ptr(val),
				Lte: float64Ptr(val),
			}))
		default:
			conds = append(conds, qdrantMatchCond(k,
				&qdrant.Match{MatchValue: &qdrant.Match_Keyword{Keyword: fmt.Sprintf("%v", v)}}))
		}
	}
	if len(conds) == 0 {
		return nil
	}
	return &qdrant.Filter{Must: conds}
}

// qdrantOperatorCondition converts a single {"$op": operand} map into a Condition.
// Returns nil for unsupported operators so the caller can skip gracefully.
func qdrantOperatorCondition(key string, ops map[string]interface{}) *qdrant.Condition {
	// Collect range fields first (multiple range ops on one field share one Range struct).
	r := &qdrant.Range{}
	hasRange := false

	for op, operand := range ops {
		switch op {
		case "$eq":
			return qdrantEqCondition(key, operand)
		case "$ne":
			return qdrantNeCondition(key, operand)
		case "$in":
			return qdrantInCondition(key, operand, false)
		case "$nin":
			return qdrantInCondition(key, operand, true)
		case "$gt":
			if f, ok := toFloat64(operand); ok {
				r.Gt = float64Ptr(f)
				hasRange = true
			}
		case "$gte":
			if f, ok := toFloat64(operand); ok {
				r.Gte = float64Ptr(f)
				hasRange = true
			}
		case "$lt":
			if f, ok := toFloat64(operand); ok {
				r.Lt = float64Ptr(f)
				hasRange = true
			}
		case "$lte":
			if f, ok := toFloat64(operand); ok {
				r.Lte = float64Ptr(f)
				hasRange = true
			}
		}
	}
	if hasRange {
		return qdrantRangeCond(key, r)
	}
	return nil
}

// qdrantEqCondition builds an equality Condition.
func qdrantEqCondition(key string, v interface{}) *qdrant.Condition {
	switch val := v.(type) {
	case bool:
		return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_Boolean{Boolean: val}})
	case int:
		return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_Integer{Integer: int64(val)}})
	case int64:
		return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_Integer{Integer: val}})
	case float64:
		return qdrantRangeCond(key, &qdrant.Range{Gte: float64Ptr(val), Lte: float64Ptr(val)})
	default:
		return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_Keyword{Keyword: fmt.Sprintf("%v", v)}})
	}
}

// qdrantNeCondition builds a not-equal Condition using ExceptKeywords / ExceptIntegers.
func qdrantNeCondition(key string, v interface{}) *qdrant.Condition {
	switch val := v.(type) {
	case int:
		return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_ExceptIntegers{
			ExceptIntegers: &qdrant.RepeatedIntegers{Integers: []int64{int64(val)}}}})
	case int64:
		return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_ExceptIntegers{
			ExceptIntegers: &qdrant.RepeatedIntegers{Integers: []int64{val}}}})
	default:
		return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_ExceptKeywords{
			ExceptKeywords: &qdrant.RepeatedStrings{Strings: []string{fmt.Sprintf("%v", v)}}}})
	}
}

// qdrantInCondition builds a set-membership Condition (In / Nin).
func qdrantInCondition(key string, operand interface{}, notIn bool) *qdrant.Condition {
	items, ok := operand.([]interface{})
	if !ok || len(items) == 0 {
		return nil
	}
	// Infer type from first element.
	switch items[0].(type) {
	case int, int32, int64:
		ints := make([]int64, 0, len(items))
		for _, item := range items {
			switch n := item.(type) {
			case int:
				ints = append(ints, int64(n))
			case int32:
				ints = append(ints, int64(n))
			case int64:
				ints = append(ints, n)
			}
		}
		if notIn {
			return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_ExceptIntegers{
				ExceptIntegers: &qdrant.RepeatedIntegers{Integers: ints}}})
		}
		return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_Integers{
			Integers: &qdrant.RepeatedIntegers{Integers: ints}}})
	default:
		strs := make([]string, 0, len(items))
		for _, item := range items {
			strs = append(strs, fmt.Sprintf("%v", item))
		}
		if notIn {
			return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_ExceptKeywords{
				ExceptKeywords: &qdrant.RepeatedStrings{Strings: strs}}})
		}
		return qdrantMatchCond(key, &qdrant.Match{MatchValue: &qdrant.Match_Keywords{
			Keywords: &qdrant.RepeatedStrings{Strings: strs}}})
	}
}

func qdrantMatchCond(key string, m *qdrant.Match) *qdrant.Condition {
	return &qdrant.Condition{
		ConditionOneOf: &qdrant.Condition_Field{
			Field: &qdrant.FieldCondition{Key: key, Match: m},
		},
	}
}

func qdrantRangeCond(key string, r *qdrant.Range) *qdrant.Condition {
	return &qdrant.Condition{
		ConditionOneOf: &qdrant.Condition_Field{
			Field: &qdrant.FieldCondition{Key: key, Range: r},
		},
	}
}

func float64Ptr(f float64) *float64 { return &f }

// toFloat64 coerces numeric types to float64 for range conditions.
func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
