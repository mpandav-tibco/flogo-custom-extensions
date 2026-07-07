package vectordb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"tibco.com/tibdg"
)

// activeSpacesNativeClient implements VectorDBClient by talking DIRECTLY to a
// TIBCO ActiveSpaces data grid through the native tibdg (CGO) client. Unlike the
// pure-Go connector, there is no gateway hop: this links the ActiveSpaces native
// libraries and therefore builds/runs only where the AS SDK is present (see
// README + Dockerfile). A collection maps to an AS table with columns:
//
//	id VARCHAR PRIMARY KEY, content VARCHAR, embedding VECTOR_FLOAT32(dim), metadata VARCHAR(JSON)
type activeSpacesNativeClient struct {
	mu       sync.Mutex
	conn     *tibdg.Connection
	session  *tibdg.Session
	tables   map[string]*tibdg.Table
	cfg      ConnectionConfig
	realmURL string
}

var _ VectorDBClient = (*activeSpacesNativeClient)(nil)

const embeddingCol = "embedding"

// newActiveSpacesClient establishes a native ActiveSpaces connection + session.
func newActiveSpacesClient(cfg ConnectionConfig) (VectorDBClient, error) {
	scheme := "http"
	if cfg.UseTLS {
		scheme = "https"
	}
	realmURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port)
	props := buildProps(cfg)

	conn, err := tibdg.NewConnection(realmURL, cfg.GridName, props)
	if err != nil {
		return nil, newError(ErrCodeConnectionFailed, fmt.Sprintf("ActiveSpaces: connect realm %q grid %q", realmURL, cfg.GridName), err)
	}
	sess, err := conn.NewSession(props)
	if err != nil {
		conn.Close()
		return nil, newError(ErrCodeConnectionFailed, "ActiveSpaces: create session", err)
	}
	return &activeSpacesNativeClient{
		conn:     conn,
		session:  sess,
		tables:   make(map[string]*tibdg.Table),
		cfg:      cfg,
		realmURL: realmURL,
	}, nil
}

func buildProps(cfg ConnectionConfig) tibdg.Props {
	props := tibdg.Props{}
	if cfg.TimeoutSeconds > 0 {
		props[tibdg.ConnectionPropertyDoubleTimeout] = float64(cfg.TimeoutSeconds)
	}
	if cfg.ClientCert != "" {
		if b, err := os.ReadFile(cfg.ClientCert); err == nil {
			props[tibdg.ConnectionPropertyStringClientCert] = string(b)
		} else {
			props[tibdg.ConnectionPropertyStringClientCert] = cfg.ClientCert
		}
	}
	if cfg.ClientKey != "" {
		if b, err := os.ReadFile(cfg.ClientKey); err == nil {
			props[tibdg.ConnectionPropertyStringClientPrivateKey] = string(b)
		} else {
			props[tibdg.ConnectionPropertyStringClientPrivateKey] = cfg.ClientKey
		}
	}
	if len(props) == 0 {
		return nil
	}
	return props
}

func (c *activeSpacesNativeClient) DBType() string { return "activespaces" }

func (c *activeSpacesNativeClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.tables {
		_ = t.Close()
	}
	if c.session != nil {
		_ = c.session.Destroy()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *activeSpacesNativeClient) HealthCheck(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.conn.GetGridMetadata(nil); err != nil {
		return newError(ErrCodeConnectionFailed, "ActiveSpaces health check failed", err)
	}
	return nil
}

// --- Collection management ---

func (c *activeSpacesNativeClient) CreateCollection(_ context.Context, cfg CollectionConfig) error {
	if err := validateCollectionConfig(cfg); err != nil {
		return err
	}
	if err := validateIdentifier(cfg.Name); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ddl := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (id VARCHAR PRIMARY KEY, content VARCHAR, %s VECTOR_FLOAT32(%d), metadata VARCHAR)",
		cfg.Name, embeddingCol, cfg.Dimensions,
	)
	if err := c.withRetry(func() error {
		_, e := c.session.ExecuteUpdate(ddl, nil)
		return e
	}); err != nil {
		return newError(ErrCodeProviderError, fmt.Sprintf("create collection %q", cfg.Name), err)
	}
	return nil
}

func (c *activeSpacesNativeClient) DeleteCollection(_ context.Context, name string) error {
	if err := validateIdentifier(name); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.withRetry(func() error {
		_, e := c.session.ExecuteUpdate("DROP TABLE IF EXISTS "+name, nil)
		return e
	}); err != nil {
		return newError(ErrCodeProviderError, fmt.Sprintf("delete collection %q", name), err)
	}
	key := strings.ToLower(name)
	if t, ok := c.tables[key]; ok {
		_ = t.Close()
		delete(c.tables, key)
	}
	return nil
}

func (c *activeSpacesNativeClient) ListCollections(_ context.Context) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	gmd, err := c.conn.GetGridMetadata(nil)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "list collections", err)
	}
	var names []string
	for _, tm := range gmd.Tables {
		for _, col := range tm.Columns {
			if col.Type == tibdg.ColumnTypeVectorFloat32 {
				names = append(names, tm.Name)
				break
			}
		}
	}
	return names, nil
}

func (c *activeSpacesNativeClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	names, err := c.ListCollections(ctx)
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return true, nil
		}
	}
	return false, nil
}

// --- Document operations ---

func (c *activeSpacesNativeClient) UpsertDocuments(_ context.Context, collectionName string, docs []Document) error {
	if err := validateUpsertDocuments(docs); err != nil {
		return err
	}
	if err := validateIdentifier(collectionName); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	tbl, err := c.openTable(collectionName)
	if err != nil {
		return err
	}
	rows := make([]*tibdg.Row, 0, len(docs))
	defer func() {
		for _, r := range rows {
			_ = r.Destroy()
		}
	}()
	for _, d := range docs {
		id := d.ID
		if id == "" {
			id = uuid.NewString()
		}
		row, err := tbl.NewRow()
		if err != nil {
			return newError(ErrCodeProviderError, "new row", err)
		}
		// Track the row immediately so the deferred cleanup destroys it even if Set fails.
		rows = append(rows, row)
		content := tibdg.RowContent{"id": id, "content": d.Content, embeddingCol: toFloat32(d.Vector)}
		if len(d.Payload) > 0 {
			mj, _ := json.Marshal(d.Payload)
			content["metadata"] = string(mj)
		}
		if err := row.Set(content); err != nil {
			return newError(ErrCodeProviderError, "set row", err)
		}
		rows = append(rows, row)
	}
	if err := c.withRetry(func() error {
		br, e := c.session.PutRows(rows, nil)
		if e != nil {
			return e
		}
		if ok, _ := br.AllSucceeded(); !ok {
			return fmt.Errorf("one or more documents failed to upsert")
		}
		return nil
	}); err != nil {
		return newError(ErrCodeProviderError, "upsert documents", err)
	}
	return nil
}

func (c *activeSpacesNativeClient) GetDocument(_ context.Context, collectionName, id string) (*Document, error) {
	if err := validateIdentifier(collectionName); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	sql := fmt.Sprintf("SELECT id, content, metadata, %s FROM %s WHERE id = ?", embeddingCol, collectionName)
	stmt, err := c.session.NewStatement(sql, nil)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "prepare get", err)
	}
	defer stmt.Close()
	if err := stmt.SetString(1, id); err != nil {
		return nil, newError(ErrCodeProviderError, "bind id", err)
	}
	rs, err := stmt.ExecuteQuery(nil)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "execute get", err)
	}
	defer rs.Close()
	has, err := rs.HasNext()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, newError(ErrCodeDocumentNotFound, fmt.Sprintf("document %q not found", id), nil)
	}
	row, err := rs.Next()
	if err != nil {
		return nil, err
	}
	defer row.Destroy()
	rc, err := row.Get()
	if err != nil {
		return nil, err
	}
	doc := rowToDocument(rc, true)
	return &doc, nil
}

func (c *activeSpacesNativeClient) DeleteDocuments(_ context.Context, collectionName string, ids []string) error {
	if err := validateIdentifier(collectionName); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.deleteByIDs(collectionName, ids)
	return err
}

func (c *activeSpacesNativeClient) DeleteByFilter(_ context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if err := validateIdentifier(collectionName); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	where, err := buildFilterSQL(filters)
	if err != nil {
		return 0, err
	}
	if where == "" {
		return 0, nil
	}
	ids, err := c.selectIDs(collectionName, where)
	if err != nil {
		return 0, err
	}
	return c.deleteByIDs(collectionName, ids)
}

func (c *activeSpacesNativeClient) ScrollDocuments(_ context.Context, req ScrollRequest) (*ScrollResult, error) {
	if err := validateIdentifier(req.CollectionName); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := 0
	if req.Offset != "" {
		offset, _ = strconv.Atoi(req.Offset)
	}
	sql := fmt.Sprintf("SELECT id, content, metadata%s FROM %s", vecSelect(req.WithVectors), req.CollectionName)
	where, err := buildFilterSQL(req.Filters)
	if err != nil {
		return nil, err
	}
	if where != "" {
		sql += " WHERE " + where
	}
	sql += fmt.Sprintf(" ORDER BY id LIMIT %d", limit)
	if offset > 0 {
		sql += fmt.Sprintf(" OFFSET %d", offset)
	}
	docs, err := c.queryDocs(sql, req.WithVectors)
	if err != nil {
		return nil, err
	}
	res := &ScrollResult{Documents: docs, Total: -1}
	if len(docs) == limit {
		res.NextOffset = strconv.Itoa(offset + limit)
	}
	return res, nil
}

func (c *activeSpacesNativeClient) CountDocuments(_ context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	if err := validateIdentifier(collectionName); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	sql := "SELECT COUNT(*) AS cnt FROM " + collectionName
	where, err := buildFilterSQL(filters)
	if err != nil {
		return 0, err
	}
	if where != "" {
		sql += " WHERE " + where
	}
	stmt, err := c.session.NewStatement(sql, nil)
	if err != nil {
		return 0, newError(ErrCodeProviderError, "prepare count", err)
	}
	defer stmt.Close()
	rs, err := stmt.ExecuteQuery(nil)
	if err != nil {
		return 0, newError(ErrCodeProviderError, "execute count", err)
	}
	defer rs.Close()
	has, err := rs.HasNext()
	if err != nil || !has {
		return 0, err
	}
	row, err := rs.Next()
	if err != nil {
		return 0, err
	}
	defer row.Destroy()
	rc, err := row.Get()
	if err != nil {
		return 0, err
	}
	return asInt64(rc["cnt"]), nil
}

// --- Search ---

func (c *activeSpacesNativeClient) VectorSearch(_ context.Context, req SearchRequest) ([]SearchResult, error) {
	if len(req.QueryVector) == 0 {
		return nil, newError(ErrCodeInvalidQueryVector, "", nil)
	}
	if err := validateIdentifier(req.CollectionName); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fn, order := simFunc("cosine")
	sql := fmt.Sprintf("SELECT id, content, %s(%s, ?) AS score, metadata%s FROM %s",
		fn, embeddingCol, vecSelect(req.WithVectors), req.CollectionName)

	var conds []string
	if where, err := buildFilterSQL(req.Filters); err != nil {
		return nil, err
	} else if where != "" {
		conds = append(conds, where)
	}
	if req.ScoreThreshold != 0 {
		conds = append(conds, fmt.Sprintf("score >= %s", sqlLiteral(req.ScoreThreshold)))
	}
	if len(conds) > 0 {
		sql += " WHERE " + strings.Join(conds, " AND ")
	}
	sql += " ORDER BY score " + order
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	sql += fmt.Sprintf(" LIMIT %d", topK)

	stmt, err := c.session.NewStatement(sql, nil)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "prepare search", err)
	}
	defer stmt.Close()
	if err := stmt.SetVectorFloat32(1, toFloat32(req.QueryVector)); err != nil {
		return nil, newError(ErrCodeProviderError, "bind vector", err)
	}
	rs, err := stmt.ExecuteQuery(nil)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "execute search", err)
	}
	defer rs.Close()

	var out []SearchResult
	for {
		has, err := rs.HasNext()
		if err != nil {
			return nil, err
		}
		if !has {
			break
		}
		row, err := rs.Next()
		if err != nil {
			return nil, err
		}
		rc, err := row.Get()
		if err != nil {
			row.Destroy()
			return nil, err
		}
		d := rowToDocument(rc, req.WithVectors)
		res := SearchResult{ID: d.ID, Content: d.Content, Score: asFloat64(rc["score"]), Vector: d.Vector}
		if !req.SkipPayload {
			res.Payload = d.Payload
		}
		out = append(out, res)
		row.Destroy()
	}
	return out, nil
}

// HybridSearch falls back to dense VectorSearch (ActiveSpaces has no native BM25 index).
func (c *activeSpacesNativeClient) HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error) {
	return c.VectorSearch(ctx, SearchRequest{
		CollectionName: req.CollectionName,
		QueryVector:    req.QueryVector,
		TopK:           req.TopK,
		ScoreThreshold: req.ScoreThreshold,
		Filters:        req.Filters,
		SkipPayload:    req.SkipPayload,
	})
}

// --- helpers ---

func (c *activeSpacesNativeClient) withRetry(op func() error) error {
	return withRetry(context.Background(), c.cfg.MaxRetries, c.cfg.RetryBackoffMs, op)
}

func (c *activeSpacesNativeClient) openTable(name string) (*tibdg.Table, error) {
	key := strings.ToLower(name)
	if t, ok := c.tables[key]; ok {
		return t, nil
	}
	t, err := c.session.OpenTable(name, nil)
	if err != nil {
		return nil, newError(ErrCodeProviderError, fmt.Sprintf("open table %q", name), err)
	}
	c.tables[key] = t
	return t, nil
}

func (c *activeSpacesNativeClient) selectIDs(collection, where string) ([]string, error) {
	stmt, err := c.session.NewStatement(fmt.Sprintf("SELECT id FROM %s WHERE %s", collection, where), nil)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "prepare select ids", err)
	}
	defer stmt.Close()
	rs, err := stmt.ExecuteQuery(nil)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "execute select ids", err)
	}
	defer rs.Close()
	var ids []string
	for {
		has, err := rs.HasNext()
		if err != nil {
			return nil, err
		}
		if !has {
			break
		}
		row, err := rs.Next()
		if err != nil {
			return nil, err
		}
		rc, err := row.Get()
		if err != nil {
			row.Destroy()
			return nil, err
		}
		if id, ok := rc["id"].(string); ok {
			ids = append(ids, id)
		}
		row.Destroy()
	}
	return ids, nil
}

// deleteByIDs deletes rows via the table API (ActiveSpaces does not support SQL DELETE).
func (c *activeSpacesNativeClient) deleteByIDs(collection string, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tbl, err := c.openTable(collection)
	if err != nil {
		return 0, err
	}
	rows := make([]*tibdg.Row, 0, len(ids))
	defer func() {
		for _, r := range rows {
			_ = r.Destroy()
		}
	}()
	for _, id := range ids {
		row, err := tbl.NewRow()
		if err != nil {
			return 0, newError(ErrCodeProviderError, "new row", err)
		}
		// Track the row immediately so the deferred cleanup destroys it even if Set fails.
		rows = append(rows, row)
		if err := row.Set(tibdg.RowContent{"id": id}); err != nil {
			return 0, newError(ErrCodeProviderError, "set row", err)
		}
	}
	if _, err := c.session.DeleteRows(rows, nil); err != nil {
		return 0, newError(ErrCodeProviderError, "delete rows", err)
	}
	return int64(len(rows)), nil
}

func (c *activeSpacesNativeClient) queryDocs(sql string, withVectors bool) ([]Document, error) {
	stmt, err := c.session.NewStatement(sql, nil)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "prepare query", err)
	}
	defer stmt.Close()
	rs, err := stmt.ExecuteQuery(nil)
	if err != nil {
		return nil, newError(ErrCodeProviderError, "execute query", err)
	}
	defer rs.Close()
	var docs []Document
	for {
		has, err := rs.HasNext()
		if err != nil {
			return nil, err
		}
		if !has {
			break
		}
		row, err := rs.Next()
		if err != nil {
			return nil, err
		}
		rc, err := row.Get()
		if err != nil {
			row.Destroy()
			return nil, err
		}
		docs = append(docs, rowToDocument(rc, withVectors))
		row.Destroy()
	}
	return docs, nil
}

func rowToDocument(rc tibdg.RowContent, withVectors bool) Document {
	d := Document{}
	if v, ok := rc["id"].(string); ok {
		d.ID = v
	}
	if v, ok := rc["content"].(string); ok {
		d.Content = v
	}
	if v, ok := rc["metadata"].(string); ok && v != "" {
		_ = json.Unmarshal([]byte(v), &d.Payload)
	}
	if withVectors {
		if v, ok := rc[embeddingCol].([]float32); ok {
			d.Vector = toFloat64(v)
		}
	}
	return d
}

func vecSelect(include bool) string {
	if include {
		return ", " + embeddingCol
	}
	return ""
}

func simFunc(metric string) (fn, order string) {
	switch strings.ToLower(metric) {
	case "l2", "euclidean", "l2_distance":
		return "l2_distance", "ASC"
	case "dot", "dotproduct", "dot_product":
		return "dot_product_similarity", "DESC"
	default:
		return "cosine_similarity", "DESC"
	}
}

func toFloat32(in []float64) []float32 {
	out := make([]float32, len(in))
	for i, v := range in {
		out[i] = float32(v)
	}
	return out
}

func toFloat64(in []float32) []float64 {
	out := make([]float64, len(in))
	for i, v := range in {
		out[i] = float64(v)
	}
	return out
}

func asInt64(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	default:
		return 0
	}
}

func asFloat64(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}
