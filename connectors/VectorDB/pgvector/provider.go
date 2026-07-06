package vectordb

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

// pgvectorClient implements VectorDBClient using PostgreSQL + pgvector extension.
type pgvectorClient struct {
	pool *pgxpool.Pool
	cfg  ConnectionConfig
}

// Compile-time assertion: pgvectorClient implements VectorDBClient.
var _ VectorDBClient = (*pgvectorClient)(nil)

// newPgvectorClient creates a new pgvector client with a connection pool.
func newPgvectorClient(ctx context.Context, cfg ConnectionConfig) (VectorDBClient, error) {
	dsn := buildPgvectorDSN(cfg)
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "invalid DSN", err)
	}
	// Ensure the vector extension is installed, then register Go types.
	// CREATE EXTENSION is idempotent (IF NOT EXISTS) and safe to run per-connection.
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
			return fmt.Errorf("install pgvector extension: %w", err)
		}
		return pgxvec.RegisterTypes(ctx, conn)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "pool creation failed", err)
	}
	return &pgvectorClient{pool: pool, cfg: cfg}, nil
}

// buildPgvectorDSN builds a PostgreSQL DSN from the ConnectionConfig.
func buildPgvectorDSN(cfg ConnectionConfig) string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.DBName, cfg.SSLMode,
	)
}

// DBType returns "pgvector".
func (c *pgvectorClient) DBType() string { return "pgvector" }

// Close closes all connections in the pool.
func (c *pgvectorClient) Close() error {
	c.pool.Close()
	return nil
}

// HealthCheck verifies the PostgreSQL connection is alive.
func (c *pgvectorClient) HealthCheck(ctx context.Context) error {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "health check: acquire connection failed", err)
	}
	defer conn.Release()
	if err := conn.QueryRow(ctx, "SELECT 1").Scan(new(int)); err != nil {
		return newError(ErrCodeConnectionFailed, "health check: ping failed", err)
	}
	return nil
}

// CreateCollection creates a PostgreSQL table with a pgvector column.
// The table includes: id (TEXT PK), content (TEXT), metadata (JSONB),
// embedding (vector(N)), and a generated tsvector column for full-text search.
// An HNSW index is created for ANN search based on the distance metric.
func (c *pgvectorClient) CreateCollection(ctx context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}
	metric := normalizeDistanceMetric(cfg.DistanceMetric)
	tableName := pgvectorSafeTableName(cfg.Name)

	// Determine HNSW operator class based on distance metric.
	var opsClass string
	switch metric {
	case "cosine":
		opsClass = "vector_cosine_ops"
	case "euclidean":
		opsClass = "vector_l2_ops"
	case "dot":
		opsClass = "vector_ip_ops"
	default:
		opsClass = "vector_cosine_ops"
	}

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "create collection: acquire connection failed", err)
	}
	defer conn.Release()

	// Create the table if it does not exist.
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			content TEXT,
			metadata JSONB,
			embedding vector(%d),
			content_tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', coalesce(content, ''))) STORED
		)`, tableName, cfg.Dimensions)

	if _, err := conn.Exec(ctx, createSQL); err != nil {
		return newError(ErrCodeProviderError, fmt.Sprintf("create table %q failed", tableName), err)
	}

	// Create HNSW index for ANN search.
	indexName := fmt.Sprintf("idx_%s_embedding", tableName)
	indexSQL := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s ON %s USING hnsw (embedding %s)`,
		indexName, tableName, opsClass)

	if _, err := conn.Exec(ctx, indexSQL); err != nil {
		return newError(ErrCodeProviderError, fmt.Sprintf("create HNSW index on %q failed", tableName), err)
	}

	// Create GIN index for full-text search.
	ftsIndexName := fmt.Sprintf("idx_%s_content_tsv", tableName)
	ftsIndexSQL := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s ON %s USING gin(content_tsv)`,
		ftsIndexName, tableName)

	if _, err := conn.Exec(ctx, ftsIndexSQL); err != nil {
		return newError(ErrCodeProviderError, fmt.Sprintf("create FTS index on %q failed", tableName), err)
	}

	return nil
}

// DeleteCollection drops the PostgreSQL table for the named collection.
func (c *pgvectorClient) DeleteCollection(ctx context.Context, name string) error {
	if name == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	tableName := pgvectorSafeTableName(name)
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "delete collection: acquire connection failed", err)
	}
	defer conn.Release()

	sql := fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)
	if _, err := conn.Exec(ctx, sql); err != nil {
		return newError(ErrCodeProviderError, fmt.Sprintf("drop table %q failed", tableName), err)
	}
	return nil
}

// ListCollections returns the names of all tables in the public schema.
func (c *pgvectorClient) ListCollections(ctx context.Context) ([]string, error) {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "list collections: acquire connection failed", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		"SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename")
	if err != nil {
		return nil, newError(ErrCodeProviderError, "list collections: query failed", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, newError(ErrCodeProviderError, "list collections: scan failed", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, newError(ErrCodeProviderError, "list collections: rows error", err)
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

// CollectionExists returns true if the named table exists in the public schema.
func (c *pgvectorClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return false, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	tableName := pgvectorSafeTableName(name)
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return false, newError(ErrCodeConnectionFailed, "collection exists: acquire connection failed", err)
	}
	defer conn.Release()

	var count int
	err = conn.QueryRow(ctx,
		"SELECT COUNT(*) FROM pg_tables WHERE tablename = $1 AND schemaname = 'public'",
		tableName).Scan(&count)
	if err != nil {
		return false, newError(ErrCodeProviderError, "collection exists: query failed", err)
	}
	return count > 0, nil
}

// UpsertDocuments inserts or updates documents in the collection using batch operations.
// Uses ON CONFLICT (id) DO UPDATE to implement upsert semantics.
func (c *pgvectorClient) UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateBatchSize(len(docs), maxUpsertBatchPgvector, "pgvector"); err != nil {
		return err
	}
	tableName := pgvectorSafeTableName(collectionName)

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "upsert: acquire connection failed", err)
	}
	defer conn.Release()

	upsertSQL := fmt.Sprintf(`
		INSERT INTO %s (id, content, metadata, embedding)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			content = EXCLUDED.content,
			metadata = EXCLUDED.metadata,
			embedding = EXCLUDED.embedding`, tableName)

	batch := &pgx.Batch{}
	for _, doc := range docs {
		metaJSON, err := json.Marshal(doc.Payload)
		if err != nil {
			return newError(ErrCodeProviderError, fmt.Sprintf("upsert: marshal metadata for doc %s failed", doc.ID), err)
		}
		vec := pgvector.NewVector(toFloat32Slice(doc.Vector))
		batch.Queue(upsertSQL, doc.ID, doc.Content, string(metaJSON), vec)
	}

	br := conn.SendBatch(ctx, batch)
	defer br.Close()

	for range docs {
		if _, err := br.Exec(); err != nil {
			return newError(ErrCodeProviderError, "upsert: batch exec failed", err)
		}
	}
	return nil
}

// GetDocument retrieves a single document by ID.
func (c *pgvectorClient) GetDocument(ctx context.Context, collectionName, id string) (*Document, error) {
	if collectionName == "" {
		return nil, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if id == "" {
		return nil, newError(ErrCodeInvalidDocumentID, "", nil)
	}
	tableName := pgvectorSafeTableName(collectionName)

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "get document: acquire connection failed", err)
	}
	defer conn.Release()

	sql := fmt.Sprintf("SELECT id, content, metadata, embedding FROM %s WHERE id = $1", tableName)
	row := conn.QueryRow(ctx, sql, id)

	var (
		docID    string
		content  string
		metaJSON string
		vec      pgvector.Vector
	)
	if err := row.Scan(&docID, &content, &metaJSON, &vec); err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, newError(ErrCodeDocumentNotFound, fmt.Sprintf("document %q not found in %q", id, collectionName), nil)
		}
		return nil, newError(ErrCodeProviderError, "get document: scan failed", err)
	}

	doc := &Document{
		ID:      docID,
		Content: content,
		Vector:  toFloat64Slice(vec.Slice()),
	}
	if metaJSON != "" && metaJSON != "null" {
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(metaJSON), &payload); err == nil {
			doc.Payload = payload
		}
	}
	return doc, nil
}

// DeleteDocuments removes documents by their IDs.
func (c *pgvectorClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	if collectionName == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if len(ids) == 0 {
		return nil
	}
	tableName := pgvectorSafeTableName(collectionName)

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return newError(ErrCodeConnectionFailed, "delete documents: acquire connection failed", err)
	}
	defer conn.Release()

	sql := fmt.Sprintf("DELETE FROM %s WHERE id = ANY($1::text[])", tableName)
	if _, err := conn.Exec(ctx, sql, ids); err != nil {
		return newError(ErrCodeProviderError, "delete documents: exec failed", err)
	}
	return nil
}

// DeleteByFilter removes all documents matching the metadata filter.
// Returns the number of documents deleted.
func (c *pgvectorClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if collectionName == "" {
		return -1, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	tableName := pgvectorSafeTableName(collectionName)

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return -1, newError(ErrCodeConnectionFailed, "delete by filter: acquire connection failed", err)
	}
	defer conn.Release()

	whereClause, args := buildPgvectorFilter(filters, 1)
	var sql string
	if whereClause == "" {
		sql = fmt.Sprintf("DELETE FROM %s", tableName)
	} else {
		sql = fmt.Sprintf("DELETE FROM %s WHERE %s", tableName, whereClause)
	}

	tag, err := conn.Exec(ctx, sql, args...)
	if err != nil {
		return -1, newError(ErrCodeProviderError, "delete by filter: exec failed", err)
	}
	return tag.RowsAffected(), nil
}

// ScrollDocuments paginates through all documents in a collection.
// Uses OFFSET-based pagination; NextOffset is the next numeric offset as a string.
func (c *pgvectorClient) ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error) {
	if req.CollectionName == "" {
		return nil, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	tableName := pgvectorSafeTableName(req.CollectionName)

	offset := 0
	if req.Offset != "" {
		if n, err := strconv.Atoi(req.Offset); err == nil && n > 0 {
			offset = n
		}
	}

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "scroll: acquire connection failed", err)
	}
	defer conn.Release()

	whereClause, filterArgs := buildPgvectorFilter(req.Filters, 1)
	var selectCols string
	if req.WithVectors {
		selectCols = "id, content, metadata, embedding"
	} else {
		selectCols = "id, content, metadata"
	}

	var sql string
	// LIMIT and OFFSET are appended as literal values (safe: both are integers).
	if whereClause == "" {
		sql = fmt.Sprintf("SELECT %s FROM %s ORDER BY id LIMIT %d OFFSET %d",
			selectCols, tableName, limit, offset)
	} else {
		sql = fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY id LIMIT %d OFFSET %d",
			selectCols, tableName, whereClause, limit, offset)
	}

	rows, err := conn.Query(ctx, sql, filterArgs...)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "scroll: query failed", err)
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var (
			id       string
			content  string
			metaJSON string
		)
		if req.WithVectors {
			var vec pgvector.Vector
			if err := rows.Scan(&id, &content, &metaJSON, &vec); err != nil {
				return nil, newError(ErrCodeProviderError, "scroll: scan failed", err)
			}
			doc := Document{
				ID:      id,
				Content: content,
				Vector:  toFloat64Slice(vec.Slice()),
			}
			if metaJSON != "" && metaJSON != "null" {
				var payload map[string]interface{}
				if err := json.Unmarshal([]byte(metaJSON), &payload); err == nil {
					doc.Payload = payload
				}
			}
			docs = append(docs, doc)
		} else {
			if err := rows.Scan(&id, &content, &metaJSON); err != nil {
				return nil, newError(ErrCodeProviderError, "scroll: scan failed", err)
			}
			doc := Document{
				ID:      id,
				Content: content,
			}
			if metaJSON != "" && metaJSON != "null" {
				var payload map[string]interface{}
				if err := json.Unmarshal([]byte(metaJSON), &payload); err == nil {
					doc.Payload = payload
				}
			}
			docs = append(docs, doc)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, newError(ErrCodeProviderError, "scroll: rows error", err)
	}
	if docs == nil {
		docs = []Document{}
	}

	// Determine next offset.
	nextOffset := ""
	if len(docs) == limit {
		nextOffset = strconv.Itoa(offset + len(docs))
	}

	return &ScrollResult{
		Documents:  docs,
		NextOffset: nextOffset,
		Total:      -1, // pgvector scroll does not return total count; use CountDocuments
	}, nil
}

// CountDocuments returns the count of documents matching the filter.
func (c *pgvectorClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if collectionName == "" {
		return 0, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	tableName := pgvectorSafeTableName(collectionName)

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return 0, newError(ErrCodeConnectionFailed, "count: acquire connection failed", err)
	}
	defer conn.Release()

	whereClause, args := buildPgvectorFilter(filters, 1)
	var sql string
	if whereClause == "" {
		sql = fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	} else {
		sql = fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", tableName, whereClause)
	}

	var count int64
	if err := conn.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		return 0, newError(ErrCodeProviderError, "count: query failed", err)
	}
	return count, nil
}

// VectorSearch performs ANN (approximate nearest neighbour) search using pgvector operators.
// Operators: cosine=<=> euclidean=<-> dot=<#>
func (c *pgvectorClient) VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := validateSearchRequest(req.CollectionName, req.QueryVector, req.TopK); err != nil {
		return nil, err
	}
	tableName := pgvectorSafeTableName(req.CollectionName)

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "vector search: acquire connection failed", err)
	}
	defer conn.Release()

	// Determine distance operator and score expression.
	distOp, scoreExpr := pgvectorDistanceOps("cosine")
	// We store the collection's metric in the HNSW index, but we don't persist it.
	// The caller can pass a collectionName with a known metric; for now we default to cosine
	// and allow override via the Filters map with special key "_metric" (internal use).
	if metricRaw, ok := req.Filters["_metric"]; ok {
		if metric, ok := metricRaw.(string); ok {
			distOp, scoreExpr = pgvectorDistanceOps(metric)
			// Remove internal key from filter map.
			delete(req.Filters, "_metric")
		}
	}

	vec := pgvector.NewVector(toFloat32Slice(req.QueryVector))

	// Build WHERE clause from filters.
	whereClause, filterArgs := buildPgvectorFilter(req.Filters, 2) // $1 is the query vector
	args := append([]interface{}{vec}, filterArgs...)

	var selectCols string
	if req.WithVectors {
		selectCols = fmt.Sprintf("id, content, metadata, embedding, %s AS dist", fmt.Sprintf(distOp, "$1::vector"))
	} else {
		selectCols = fmt.Sprintf("id, content, metadata, %s AS dist", fmt.Sprintf(distOp, "$1::vector"))
	}

	var sql string
	if whereClause == "" {
		sql = fmt.Sprintf(`SELECT %s FROM %s ORDER BY embedding %s $1::vector LIMIT %d`,
			selectCols, tableName, pgvectorDistOpSymbol(distOp), req.TopK)
	} else {
		sql = fmt.Sprintf(`SELECT %s FROM %s WHERE %s ORDER BY embedding %s $1::vector LIMIT %d`,
			selectCols, tableName, whereClause, pgvectorDistOpSymbol(distOp), req.TopK)
	}

	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "vector search: query failed", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var (
			id       string
			content  string
			metaJSON string
			dist     float64
		)
		if req.WithVectors {
			var vec pgvector.Vector
			if err := rows.Scan(&id, &content, &metaJSON, &vec, &dist); err != nil {
				return nil, newError(ErrCodeProviderError, "vector search: scan failed", err)
			}
			score := computeScore(scoreExpr, dist)
			if req.ScoreThreshold > 0 && score < req.ScoreThreshold {
				continue
			}
			sr := SearchResult{ID: id, Score: score}
			if !req.SkipPayload {
				sr.Content = content
				if metaJSON != "" && metaJSON != "null" {
					var payload map[string]interface{}
					if err := json.Unmarshal([]byte(metaJSON), &payload); err == nil {
						sr.Payload = payload
					}
				}
			}
			sr.Vector = toFloat64Slice(vec.Slice())
			results = append(results, sr)
		} else {
			if err := rows.Scan(&id, &content, &metaJSON, &dist); err != nil {
				return nil, newError(ErrCodeProviderError, "vector search: scan failed", err)
			}
			score := computeScore(scoreExpr, dist)
			if req.ScoreThreshold > 0 && score < req.ScoreThreshold {
				continue
			}
			sr := SearchResult{ID: id, Score: score}
			if !req.SkipPayload {
				sr.Content = content
				if metaJSON != "" && metaJSON != "null" {
					var payload map[string]interface{}
					if err := json.Unmarshal([]byte(metaJSON), &payload); err == nil {
						sr.Payload = payload
					}
				}
			}
			results = append(results, sr)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, newError(ErrCodeProviderError, "vector search: rows error", err)
	}
	if results == nil {
		results = []SearchResult{}
	}
	return results, nil
}

// HybridSearch combines dense vector search with PostgreSQL full-text search (BM25/tsvector).
// Uses Reciprocal Rank Fusion (RRF) to blend dense and sparse results with the given Alpha weight.
// Alpha=1.0 is pure dense, Alpha=0.0 is pure BM25, Alpha=0.5 is balanced.
func (c *pgvectorClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	if req.CollectionName == "" {
		return nil, newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if req.TopK <= 0 {
		return nil, newError(ErrCodeInvalidTopK, fmt.Sprintf("topK=%d must be > 0", req.TopK), nil)
	}
	if req.Alpha < 0 || req.Alpha > 1 {
		return nil, newError(ErrCodeInvalidAlpha, "", nil)
	}

	// If no vector provided, fall back to text-only search.
	if len(req.QueryVector) == 0 && req.QueryText != "" {
		return c.textOnlySearch(ctx, req)
	}
	// If no text provided, fall back to vector-only search.
	if req.QueryText == "" && len(req.QueryVector) > 0 {
		return c.VectorSearch(ctx, SearchRequest{
			CollectionName: req.CollectionName,
			QueryVector:    req.QueryVector,
			TopK:           req.TopK,
			ScoreThreshold: req.ScoreThreshold,
			Filters:        req.Filters,
			SkipPayload:    req.SkipPayload,
		})
	}

	tableName := pgvectorSafeTableName(req.CollectionName)
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "hybrid search: acquire connection failed", err)
	}
	defer conn.Release()

	whereClause, filterArgs := buildPgvectorFilter(req.Filters, 3) // $1=vec, $2=querytext, $3+=filters
	vec := pgvector.NewVector(toFloat32Slice(req.QueryVector))
	candidateLimit := req.TopK * 10
	if candidateLimit < 100 {
		candidateLimit = 100
	}

	// Build the RRF hybrid search CTE.
	var filterSQL string
	if whereClause != "" {
		filterSQL = "AND " + whereClause
	}

	rrfSQL := fmt.Sprintf(`
		WITH vec_ranked AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY embedding <=> $1::vector) AS vrank
			FROM %s
			WHERE true %s
			LIMIT %d
		),
		txt_ranked AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY ts_rank_cd(content_tsv, query) DESC) AS trank
			FROM %s, plainto_tsquery('english', $2) query
			WHERE content_tsv @@ query %s
			LIMIT %d
		),
		fused AS (
			SELECT COALESCE(v.id, t.id) AS id,
				$3::float8 * COALESCE(1.0/(60+v.vrank), 0) + (1-$3::float8) * COALESCE(1.0/(60+t.trank), 0) AS score
			FROM vec_ranked v FULL OUTER JOIN txt_ranked t ON v.id = t.id
			ORDER BY score DESC
			LIMIT %d
		)
		SELECT f.id, d.content, d.metadata, f.score
		FROM fused f
		JOIN %s d ON f.id = d.id
		ORDER BY f.score DESC`,
		tableName, filterSQL, candidateLimit,
		tableName, filterSQL, candidateLimit,
		req.TopK,
		tableName,
	)

	args := append([]interface{}{vec, req.QueryText, req.Alpha}, filterArgs...)
	rows, err := conn.Query(ctx, rrfSQL, args...)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "hybrid search: query failed", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var (
			id       string
			content  string
			metaJSON string
			score    float64
		)
		if err := rows.Scan(&id, &content, &metaJSON, &score); err != nil {
			return nil, newError(ErrCodeProviderError, "hybrid search: scan failed", err)
		}
		if req.ScoreThreshold > 0 && score < req.ScoreThreshold {
			continue
		}
		sr := SearchResult{ID: id, Score: score}
		if !req.SkipPayload {
			sr.Content = content
			if metaJSON != "" && metaJSON != "null" {
				var payload map[string]interface{}
				if err := json.Unmarshal([]byte(metaJSON), &payload); err == nil {
					sr.Payload = payload
				}
			}
		}
		results = append(results, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, newError(ErrCodeProviderError, "hybrid search: rows error", err)
	}
	if results == nil {
		results = []SearchResult{}
	}
	return results, nil
}

// textOnlySearch performs BM25/tsvector full-text search without vector ranking.
func (c *pgvectorClient) textOnlySearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	tableName := pgvectorSafeTableName(req.CollectionName)
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, "text search: acquire connection failed", err)
	}
	defer conn.Release()

	whereClause, filterArgs := buildPgvectorFilter(req.Filters, 2)
	var filterSQL string
	if whereClause != "" {
		filterSQL = "AND " + whereClause
	}

	sql := fmt.Sprintf(`
		SELECT id, content, metadata, ts_rank_cd(content_tsv, query) AS score
		FROM %s, plainto_tsquery('english', $1) query
		WHERE content_tsv @@ query %s
		ORDER BY score DESC
		LIMIT %d`,
		tableName, filterSQL, req.TopK)

	args := append([]interface{}{req.QueryText}, filterArgs...)
	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "text search: query failed", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var (
			id       string
			content  string
			metaJSON string
			score    float64
		)
		if err := rows.Scan(&id, &content, &metaJSON, &score); err != nil {
			return nil, newError(ErrCodeProviderError, "text search: scan failed", err)
		}
		if req.ScoreThreshold > 0 && score < req.ScoreThreshold {
			continue
		}
		sr := SearchResult{ID: id, Score: score}
		if !req.SkipPayload {
			sr.Content = content
			if metaJSON != "" && metaJSON != "null" {
				var payload map[string]interface{}
				if err := json.Unmarshal([]byte(metaJSON), &payload); err == nil {
					sr.Payload = payload
				}
			}
		}
		results = append(results, sr)
	}
	if err := rows.Err(); err != nil {
		return nil, newError(ErrCodeProviderError, "text search: rows error", err)
	}
	if results == nil {
		results = []SearchResult{}
	}
	return results, nil
}

// --- Helper functions ---

// pgvectorSafeTableName sanitizes a collection name for use as a PostgreSQL table name.
// Keeps only alphanumeric characters and underscores, lowercases.
var safeNameRe = regexp.MustCompile(`[^a-z0-9_]`)

func pgvectorSafeTableName(name string) string {
	lower := strings.ToLower(name)
	safe := safeNameRe.ReplaceAllString(lower, "_")
	// Ensure it doesn't start with a digit.
	if len(safe) > 0 && safe[0] >= '0' && safe[0] <= '9' {
		safe = "t_" + safe
	}
	return safe
}

// pgvectorSafeFieldName sanitizes a metadata field name for JSONB access.
var safeFieldRe = regexp.MustCompile(`[^a-z0-9_]`)

func pgvectorSafeFieldName(name string) string {
	lower := strings.ToLower(name)
	return safeFieldRe.ReplaceAllString(lower, "_")
}

// pgvectorDistanceOps returns the distance operator pattern and score expression type for a metric.
// The operator pattern uses %s as a placeholder for the query vector argument expression.
func pgvectorDistanceOps(metric string) (opPattern string, scoreExpr string) {
	switch normalizeDistanceMetric(metric) {
	case "euclidean":
		return "embedding <-> %s", "euclidean"
	case "dot":
		return "embedding <#> %s", "dot"
	default: // cosine
		return "embedding <=> %s", "cosine"
	}
}

// pgvectorDistOpSymbol extracts the operator symbol from the opPattern string.
func pgvectorDistOpSymbol(opPattern string) string {
	// Pattern is like "embedding <=> %s", extract the operator.
	parts := strings.Fields(opPattern)
	if len(parts) >= 2 {
		return parts[1]
	}
	return "<=>"
}

// computeScore converts a distance value to a similarity score.
func computeScore(scoreExpr string, dist float64) float64 {
	switch scoreExpr {
	case "euclidean":
		return 1.0 / (1.0 + dist)
	case "dot":
		// For dot product, pgvector returns negative inner product.
		return -dist
	default: // cosine
		return 1.0 - dist
	}
}

// buildPgvectorFilter converts a generic filter map to a parameterized PostgreSQL WHERE clause.
// startIdx is the starting parameter index (e.g., 1 for $1, 2 for $2 if $1 is used by the query vector).
//
// Supported filter operators: $eq, $ne, $gt, $gte, $lt, $lte, $in.
// Metadata fields are accessed as JSONB: metadata->>'fieldname'.
// Numeric comparisons cast the JSONB text to numeric: (metadata->>'fieldname')::numeric.
//
// Example:
//
//	{"category": {"$eq": "science"}} → "metadata->>'category' = $1"
//	{"year": {"$gt": 2020}} → "(metadata->>'year')::numeric > $1"
//	{"tags": {"$in": ["a","b"]}} → "metadata->>'tags' = ANY($1)"
func buildPgvectorFilter(filters map[string]interface{}, startIdx int) (string, []interface{}) {
	if len(filters) == 0 {
		return "", nil
	}

	var clauses []string
	var args []interface{}
	idx := startIdx

	for key, value := range filters {
		fieldName := pgvectorSafeFieldName(key)
		jsonbField := fmt.Sprintf("metadata->>'%s'", fieldName)
		numericField := fmt.Sprintf("(metadata->>'%s')::numeric", fieldName)

		switch v := value.(type) {
		case map[string]interface{}:
			// Operator-based filter.
			for op, opVal := range v {
				switch op {
				case "$eq":
					clauses = append(clauses, fmt.Sprintf("%s = $%d", jsonbField, idx))
					args = append(args, fmt.Sprintf("%v", opVal))
					idx++
				case "$ne":
					clauses = append(clauses, fmt.Sprintf("%s != $%d", jsonbField, idx))
					args = append(args, fmt.Sprintf("%v", opVal))
					idx++
				case "$gt":
					clauses = append(clauses, fmt.Sprintf("%s > $%d", numericField, idx))
					args = append(args, opVal)
					idx++
				case "$gte":
					clauses = append(clauses, fmt.Sprintf("%s >= $%d", numericField, idx))
					args = append(args, opVal)
					idx++
				case "$lt":
					clauses = append(clauses, fmt.Sprintf("%s < $%d", numericField, idx))
					args = append(args, opVal)
					idx++
				case "$lte":
					clauses = append(clauses, fmt.Sprintf("%s <= $%d", numericField, idx))
					args = append(args, opVal)
					idx++
				case "$in":
					// Convert value to []string for ANY() operator.
					if arr, ok := opVal.([]interface{}); ok {
						strs := make([]string, len(arr))
						for i, item := range arr {
							strs[i] = fmt.Sprintf("%v", item)
						}
						clauses = append(clauses, fmt.Sprintf("%s = ANY($%d)", jsonbField, idx))
						args = append(args, strs)
						idx++
					}
				}
			}
		default:
			// Simple equality: {"field": "value"}.
			clauses = append(clauses, fmt.Sprintf("%s = $%d", jsonbField, idx))
			args = append(args, fmt.Sprintf("%v", v))
			idx++
		}
	}

	return strings.Join(clauses, " AND "), args
}
