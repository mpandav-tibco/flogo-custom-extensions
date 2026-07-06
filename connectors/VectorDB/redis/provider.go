package vectordb

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

// redisClient implements VectorDBClient for Redis Stack (RediSearch + vector module).
type redisClient struct {
	client *redis.Client
	cfg    ConnectionConfig
}

// Compile-time proof that redisClient satisfies the full VectorDBClient interface.
var _ VectorDBClient = (*redisClient)(nil)

func newRedisClient(cfg ConnectionConfig) (VectorDBClient, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.RedisDB,
		Protocol: 2, // Force RESP2; RESP3 changes FT.SEARCH response to map format, breaking Slice() parsing
	})
	return &redisClient{client: rdb, cfg: cfg}, nil
}

func (c *redisClient) DBType() string { return "redis" }

func (c *redisClient) Close() error {
	return c.client.Close()
}

func (c *redisClient) HealthCheck(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// ── Vector encoding ──────────────────────────────────────────────────────────

// encodeVector encodes a float64 slice as little-endian IEEE 754 float32 bytes
// for Redis Stack VECTOR fields.
func encodeVector(v []float64) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(float32(f)))
	}
	return buf
}

// decodeVector decodes little-endian IEEE 754 float32 bytes back to float64 slice.
func decodeVector(b []byte) []float64 {
	n := len(b) / 4
	v := make([]float64, n)
	for i := 0; i < n; i++ {
		v[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:])))
	}
	return v
}

// ── Collection Management ────────────────────────────────────────────────────

// CreateCollection creates a RediSearch index with HNSW vector support.
// Schema: id (TAG), content (TEXT), metadata (TEXT), embedding (VECTOR HNSW).
func (c *redisClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}

	metric := redisMetric(normalizeDistanceMetric(cfg.DistanceMetric))

	// FT.CREATE {indexName} ON HASH PREFIX 1 {indexName}:
	// SCHEMA id TAG SORTABLE content TEXT metadata TEXT embedding VECTOR HNSW 6 TYPE FLOAT32 DIM {dim} DISTANCE_METRIC {metric}
	args := []interface{}{
		"FT.CREATE", cfg.Name,
		"ON", "HASH",
		"PREFIX", "1", cfg.Name + ":",
		"SCHEMA",
		"id", "TAG", "SORTABLE",
		"content", "TEXT",
		"metadata", "TEXT",
		"embedding", "VECTOR", "HNSW", "6",
		"TYPE", "FLOAT32",
		"DIM", cfg.Dimensions,
		"DISTANCE_METRIC", metric,
	}

	err := c.client.Do(ctx, args...).Err()
	if err != nil {
		if strings.Contains(err.Error(), "Index already exists") {
			return newError(ErrCodeCollectionExists,
				fmt.Sprintf("index %q already exists", cfg.Name), err)
		}
		return newError(ErrCodeProviderError,
			fmt.Sprintf("FT.CREATE %q failed", cfg.Name), err)
	}
	return nil
}

// redisMetric maps canonical metric names to Redis Stack distance metric strings.
func redisMetric(metric string) string {
	switch metric {
	case "dot":
		return "IP"
	case "euclidean":
		return "L2"
	default: // cosine
		return "COSINE"
	}
}

// DeleteCollection drops the RediSearch index AND all associated hash keys (DD flag).
func (c *redisClient) DeleteCollection(ctx context.Context, name string) error {
	if name == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	err := c.client.Do(ctx, "FT.DROPINDEX", name, "DD").Err()
	if err != nil {
		if isRedisIndexNotFound(err) {
			return newError(ErrCodeCollectionNotFound,
				fmt.Sprintf("index %q does not exist", name), err)
		}
		return newError(ErrCodeProviderError,
			fmt.Sprintf("FT.DROPINDEX %q failed", name), err)
	}
	return nil
}

// CollectionExists returns true if the named RediSearch index exists.
// isRedisIndexNotFound reports whether err means the RediSearch index does not exist.
// Redis Stack returns "Unknown index name" (case varies by version), older builds
// return "no such index" — normalise to lowercase for a reliable check.
func isRedisIndexNotFound(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "unknown index") || strings.Contains(lower, "no such index")
}

func (c *redisClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return false, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	err := c.client.Do(ctx, "FT.INFO", name).Err()
	if err != nil {
		if isRedisIndexNotFound(err) {
			return false, nil
		}
		return false, newError(ErrCodeProviderError,
			fmt.Sprintf("FT.INFO %q failed", name), err)
	}
	return true, nil
}

// ListCollections returns names of all RediSearch indexes via FT._LIST.
func (c *redisClient) ListCollections(ctx context.Context) ([]string, error) {
	result, err := c.client.Do(ctx, "FT._LIST").StringSlice()
	if err != nil {
		return nil, newError(ErrCodeProviderError, "FT._LIST failed", err)
	}
	return result, nil
}

// ── Document Operations ──────────────────────────────────────────────────────

// docKey returns the Redis hash key for a document: "{indexName}:{docID}".
func docKey(indexName, id string) string {
	return indexName + ":" + id
}

// UpsertDocuments writes documents as Redis hashes using a pipeline for efficiency.
func (c *redisClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if collectionName == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatchRedis, "redis"); err != nil {
		return err
	}

	pipe := c.client.Pipeline()
	for _, doc := range docs {
		key := docKey(collectionName, doc.ID)
		metaJSON, _ := json.Marshal(doc.Payload)
		pipe.HSet(ctx, key,
			"id", doc.ID,
			"content", doc.Content,
			"metadata", string(metaJSON),
			"embedding", encodeVector(doc.Vector),
		)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return newError(ErrCodeProviderError, "pipeline upsert failed", err)
	}
	return nil
}

// GetDocument retrieves a single document by its hash key.
func (c *redisClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	if collectionName == "" {
		return nil, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if id == "" {
		return nil, newError(ErrCodeInvalidDocumentID, "", nil)
	}

	key := docKey(collectionName, id)
	vals, err := c.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, newError(ErrCodeProviderError,
			fmt.Sprintf("HGETALL %q failed", key), err)
	}
	if len(vals) == 0 {
		return nil, newError(ErrCodeDocumentNotFound,
			fmt.Sprintf("document %q not found in collection %q", id, collectionName), nil)
	}

	doc := &Document{
		ID:      vals["id"],
		Content: vals["content"],
	}
	if doc.ID == "" {
		doc.ID = id
	}
	if metaStr, ok := vals["metadata"]; ok && metaStr != "" {
		var payload map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(metaStr), &payload); jsonErr == nil {
			doc.Payload = payload
		}
	}
	if embBytes, ok := vals["embedding"]; ok && embBytes != "" {
		doc.Vector = decodeVector([]byte(embBytes))
	}
	return doc, nil
}

// DeleteDocuments removes documents by their hash keys.
func (c *redisClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if collectionName == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = docKey(collectionName, id)
	}
	err := c.client.Del(ctx, keys...).Err()
	if err != nil {
		return newError(ErrCodeProviderError, "DEL failed", err)
	}
	return nil
}

// DeleteByFilter searches for documents matching the filter and deletes them.
// Returns the count of deleted documents.
func (c *redisClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if collectionName == "" {
		return -1, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if len(filters) == 0 {
		return -1, newError(ErrCodeProviderError,
			"DeleteByFilter: filters must not be empty (would delete all documents)", nil)
	}

	query := buildRedisFilter(filters)
	// Use FT.SEARCH to find matching document IDs
	// LIMIT 0 10000 to get a large batch (practical limit for filter-based delete)
	raw, err := c.client.Do(ctx, "FT.SEARCH", collectionName, query,
		"RETURN", "1", "id",
		"LIMIT", "0", "10000",
	).Slice()
	if err != nil {
		return -1, newError(ErrCodeProviderError, "FT.SEARCH for delete-by-filter failed", err)
	}

	keys, parseErr := extractKeysFromSearch(raw)
	if parseErr != nil {
		return -1, parseErr
	}
	if len(keys) == 0 {
		return 0, nil
	}

	delErr := c.client.Del(ctx, keys...).Err()
	if delErr != nil {
		return -1, newError(ErrCodeProviderError, "DEL in delete-by-filter failed", delErr)
	}
	return int64(len(keys)), nil
}

// extractKeysFromSearch pulls the key strings from a raw FT.SEARCH Slice() result.
// FT.SEARCH returns: [totalCount, key1, [fields...], key2, [fields...], ...]
func extractKeysFromSearch(raw []interface{}) ([]string, error) {
	if len(raw) < 1 {
		return nil, nil
	}
	var keys []string
	// Elements at index 1, 3, 5, ... are the key strings; even indices hold field arrays.
	for i := 1; i < len(raw); i += 2 {
		key, ok := raw[i].(string)
		if !ok {
			continue
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// ScrollDocuments paginates through documents using FT.SEARCH with LIMIT (offset, count).
func (c *redisClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	if req.CollectionName == "" {
		return nil, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	offset := 0
	if req.Offset != "" {
		if n, err := strconv.Atoi(req.Offset); err == nil && n >= 0 {
			offset = n
		}
	}

	query := buildRedisFilter(req.Filters)

	var returnFields []interface{}
	if req.WithVectors {
		returnFields = []interface{}{"RETURN", "4", "id", "content", "metadata", "embedding"}
	} else {
		returnFields = []interface{}{"RETURN", "3", "id", "content", "metadata"}
	}

	args := []interface{}{"FT.SEARCH", req.CollectionName, query}
	args = append(args, returnFields...)
	args = append(args, "LIMIT", offset, limit)

	raw, err := c.client.Do(ctx, args...).Slice()
	if err != nil {
		return nil, newError(ErrCodeProviderError, "FT.SEARCH (scroll) failed", err)
	}

	docs, total, parseErr := parseDocumentsFromSearch(raw, req.WithVectors)
	if parseErr != nil {
		return nil, parseErr
	}

	nextOffset := ""
	if len(docs) == limit {
		nextOffset = strconv.Itoa(offset + len(docs))
	}

	return &ScrollResult{
		Documents:  docs,
		NextOffset: nextOffset,
		Total:      total,
	}, nil
}

// CountDocuments counts documents matching the filter using FT.SEARCH LIMIT 0 0.
// The first element of the response is the total count.
func (c *redisClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if collectionName == "" {
		return 0, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	query := buildRedisFilter(filters)

	raw, err := c.client.Do(ctx, "FT.SEARCH", collectionName, query,
		"LIMIT", "0", "0",
	).Slice()
	if err != nil {
		return 0, newError(ErrCodeProviderError, "FT.SEARCH (count) failed", err)
	}
	if len(raw) == 0 {
		return 0, nil
	}
	count, err := toInt64(raw[0])
	if err != nil {
		return 0, nil
	}
	return count, nil
}

// ── Search ───────────────────────────────────────────────────────────────────

// VectorSearch performs KNN dense-vector search using FT.SEARCH with DIALECT 2.
func (c *redisClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	vecBytes := encodeVector(req.QueryVector)
	query := fmt.Sprintf("*=>[KNN %d @embedding $vec AS __score__]", req.TopK)

	var returnFields []interface{}
	if req.WithVectors {
		returnFields = []interface{}{"RETURN", "5", "id", "content", "metadata", "embedding", "__score__"}
	} else {
		returnFields = []interface{}{"RETURN", "4", "id", "content", "metadata", "__score__"}
	}

	args := []interface{}{"FT.SEARCH", req.CollectionName, query}
	args = append(args, returnFields...)
	args = append(args, "PARAMS", "2", "vec", vecBytes)
	args = append(args, "SORTBY", "__score__")
	args = append(args, "LIMIT", "0", req.TopK)
	args = append(args, "DIALECT", "2")

	raw, err := c.client.Do(ctx, args...).Slice()
	if err != nil {
		return nil, newError(ErrCodeProviderError, "FT.SEARCH (vector) failed", err)
	}

	results, parseErr := parseSearchResults(raw, req.SkipPayload, req.WithVectors, req.ScoreThreshold, true)
	if parseErr != nil {
		return nil, parseErr
	}
	return results, nil
}

// HybridSearch combines keyword (BM25) and vector (KNN) search using RediSearch DIALECT 2.
// When Alpha >= 1.0 or QueryText is empty, falls back to pure VectorSearch.
func (c *redisClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	// Fall back to pure vector search when no text or pure dense
	if req.Alpha >= 1.0 || req.QueryText == "" {
		return c.VectorSearch(ctx, SearchRequest{
			CollectionName: req.CollectionName,
			QueryVector:    req.QueryVector,
			TopK:           req.TopK,
			ScoreThreshold: req.ScoreThreshold,
			Filters:        req.Filters,
			SkipPayload:    req.SkipPayload,
		})
	}

	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}

	vecBytes := encodeVector(req.QueryVector)

	// Build word list from query text for BM25 match
	words := strings.Fields(req.QueryText)
	wordQuery := strings.Join(words, " ")

	// Hybrid: (text match) pre-filter + KNN
	query := fmt.Sprintf("(@content:(%s))=>[KNN %d @embedding $vec AS __score__]", wordQuery, req.TopK)

	args := []interface{}{"FT.SEARCH", req.CollectionName, query,
		"RETURN", "4", "id", "content", "metadata", "__score__",
		"PARAMS", "2", "vec", vecBytes,
		"SORTBY", "__score__",
		"LIMIT", "0", req.TopK,
		"DIALECT", "2",
	}

	raw, err := c.client.Do(ctx, args...).Slice()
	if err != nil {
		// Fall back to pure vector search on syntax/parse errors from text component
		return c.VectorSearch(ctx, SearchRequest{
			CollectionName: req.CollectionName,
			QueryVector:    req.QueryVector,
			TopK:           req.TopK,
			ScoreThreshold: req.ScoreThreshold,
			Filters:        req.Filters,
			SkipPayload:    req.SkipPayload,
		})
	}

	results, parseErr := parseSearchResults(raw, req.SkipPayload, false, req.ScoreThreshold, true)
	if parseErr != nil {
		return nil, parseErr
	}
	return results, nil
}

// ── Filter building ──────────────────────────────────────────────────────────

// buildRedisFilter converts a filter map to a RediSearch query string.
// For nil/empty filters, returns "*" (match all).
// For metadata equality filters: (@metadata:*"key":"value"*)
// For multiple filters, combines with space (AND in RediSearch).
func buildRedisFilter(filters map[string]interface{}) string {
	if len(filters) == 0 {
		return "*"
	}

	var parts []string
	for k, v := range filters {
		switch tv := v.(type) {
		case string:
			// Escape quotes in the value for JSON substring match
			escaped := strings.ReplaceAll(tv, `"`, `\"`)
			parts = append(parts, fmt.Sprintf(`@metadata:*"%s":"%s"*`, k, escaped))
		case float64:
			parts = append(parts, fmt.Sprintf(`@metadata:*"%s":%v*`, k, tv))
		case int, int32, int64:
			parts = append(parts, fmt.Sprintf(`@metadata:*"%s":%v*`, k, tv))
		case bool:
			parts = append(parts, fmt.Sprintf(`@metadata:*"%s":%v*`, k, tv))
		default:
			valStr := fmt.Sprintf("%v", v)
			parts = append(parts, fmt.Sprintf(`@metadata:*"%s":"%s"*`, k, valStr))
		}
	}

	if len(parts) == 1 {
		return "(" + parts[0] + ")"
	}
	return "(" + strings.Join(parts, " ") + ")"
}

// ── Search result parsing ────────────────────────────────────────────────────

// parseSearchResults parses the raw FT.SEARCH Slice() output into SearchResult slice.
//
// FT.SEARCH returns: [totalCount, key1, [field1, val1, field2, val2, ...], key2, [...], ...]
//
// isVectorSearch=true → score field is distance (lower = more similar for L2/COSINE);
// we convert to similarity: score = 1 - dist for cosine, 1/(1+dist) for L2.
// For simplicity, we store the raw score negated so higher = better (cosine distance = 1 - similarity).
func parseSearchResults(raw []interface{}, skipPayload, withVectors bool, scoreThreshold float64, isVectorSearch bool) ([]SearchResult, error) {
	if len(raw) < 1 {
		return nil, nil
	}

	var results []SearchResult
	// Skip index 0 (total count), then iterate key/fields pairs
	for i := 1; i < len(raw); i += 2 {
		if i+1 >= len(raw) {
			break
		}
		fields, ok := raw[i+1].([]interface{})
		if !ok {
			continue
		}

		fieldMap := parseFieldPairs(fields)

		sr := SearchResult{}

		// ID
		if idVal, ok := fieldMap["id"]; ok {
			sr.ID = fmt.Sprintf("%v", idVal)
		} else {
			// Fall back to key name: strip "indexName:" prefix
			if keyStr, ok := raw[i].(string); ok {
				parts := strings.SplitN(keyStr, ":", 2)
				if len(parts) == 2 {
					sr.ID = parts[1]
				} else {
					sr.ID = keyStr
				}
			}
		}

		// Score — Redis returns distance; convert to similarity (1 - dist for cosine)
		if scoreVal, ok := fieldMap["__score__"]; ok {
			dist := toFloat64Safe(scoreVal)
			// Store as 1 - distance so higher = better (for COSINE and L2)
			// For IP (dot product), Redis returns negative dot product; keep as is.
			sr.Score = 1.0 - dist
		}

		// Apply score threshold (after conversion)
		if scoreThreshold > 0 && sr.Score < scoreThreshold {
			continue
		}

		// Content
		if contentVal, ok := fieldMap["content"]; ok {
			sr.Content = fmt.Sprintf("%v", contentVal)
		}

		// Payload (metadata JSON)
		if !skipPayload {
			if metaVal, ok := fieldMap["metadata"]; ok {
				metaStr := fmt.Sprintf("%v", metaVal)
				if metaStr != "" && metaStr != "<nil>" {
					var payload map[string]interface{}
					if jsonErr := json.Unmarshal([]byte(metaStr), &payload); jsonErr == nil {
						sr.Payload = payload
					}
				}
			}
		}

		// Vector (only when requested)
		if withVectors {
			if embVal, ok := fieldMap["embedding"]; ok {
				embStr := fmt.Sprintf("%v", embVal)
				if embStr != "" {
					sr.Vector = decodeVector([]byte(embStr))
				}
			}
		}

		results = append(results, sr)
	}
	return results, nil
}

// parseDocumentsFromSearch parses FT.SEARCH results into Document slice.
func parseDocumentsFromSearch(raw []interface{}, withVectors bool) ([]Document, int64, error) {
	if len(raw) < 1 {
		return nil, 0, nil
	}
	total, _ := toInt64(raw[0])

	var docs []Document
	for i := 1; i < len(raw); i += 2 {
		if i+1 >= len(raw) {
			break
		}
		fields, ok := raw[i+1].([]interface{})
		if !ok {
			continue
		}

		fieldMap := parseFieldPairs(fields)
		doc := Document{}

		if idVal, ok := fieldMap["id"]; ok {
			doc.ID = fmt.Sprintf("%v", idVal)
		} else if keyStr, ok := raw[i].(string); ok {
			parts := strings.SplitN(keyStr, ":", 2)
			if len(parts) == 2 {
				doc.ID = parts[1]
			} else {
				doc.ID = keyStr
			}
		}

		if contentVal, ok := fieldMap["content"]; ok {
			doc.Content = fmt.Sprintf("%v", contentVal)
		}

		if metaVal, ok := fieldMap["metadata"]; ok {
			metaStr := fmt.Sprintf("%v", metaVal)
			if metaStr != "" && metaStr != "<nil>" {
				var payload map[string]interface{}
				if jsonErr := json.Unmarshal([]byte(metaStr), &payload); jsonErr == nil {
					doc.Payload = payload
				}
			}
		}

		if withVectors {
			if embVal, ok := fieldMap["embedding"]; ok {
				embStr := fmt.Sprintf("%v", embVal)
				if embStr != "" {
					doc.Vector = decodeVector([]byte(embStr))
				}
			}
		}

		docs = append(docs, doc)
	}
	return docs, total, nil
}

// parseFieldPairs converts alternating [field, value, field, value, ...] to map.
func parseFieldPairs(fields []interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		key := fmt.Sprintf("%v", fields[i])
		m[key] = fields[i+1]
	}
	return m
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func toFloat64Safe(v interface{}) float64 {
	switch tv := v.(type) {
	case float64:
		return tv
	case float32:
		return float64(tv)
	case string:
		f, _ := strconv.ParseFloat(tv, 64)
		return f
	case int64:
		return float64(tv)
	case int:
		return float64(tv)
	default:
		f, _ := strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
		return f
	}
}

func toInt64(v interface{}) (int64, error) {
	switch tv := v.(type) {
	case int64:
		return tv, nil
	case int:
		return int64(tv), nil
	case float64:
		return int64(tv), nil
	case string:
		return strconv.ParseInt(tv, 10, 64)
	default:
		return strconv.ParseInt(fmt.Sprintf("%v", v), 10, 64)
	}
}
