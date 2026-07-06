//go:build !integration

package vectordb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient creates an openSearchClient pointed at the given test server URL.
func newTestClient(serverURL string) *openSearchClient {
	cfg := ConnectionConfig{
		Host:           "localhost",
		Port:           9200,
		TimeoutSeconds: 10,
		MaxRetries:     1,
		RetryBackoffMs: 10,
	}
	client := &openSearchClient{
		cfg:        cfg,
		httpClient: http.DefaultClient,
		baseURL:    serverURL,
	}
	return client
}

// ---------------------------------------------------------------------------
// CollectionExists
// ---------------------------------------------------------------------------

func TestCollectionExists_True(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodHead, r.Method)
		assert.Equal(t, "/my-index", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	exists, err := client.CollectionExists(context.Background(), "my-index")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestCollectionExists_False(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	exists, err := client.CollectionExists(context.Background(), "non-existent")
	require.NoError(t, err)
	assert.False(t, exists)
}

// ---------------------------------------------------------------------------
// CreateCollection
// ---------------------------------------------------------------------------

func TestCreateCollection_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/test-collection", r.URL.Path)

		// Verify it's valid JSON with knn settings
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		settings, ok := body["settings"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, true, settings["index.knn"])

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"acknowledged":true,"shards_acknowledged":true,"index":"test-collection"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.CreateCollection(context.Background(), CollectionConfig{
		Name:           "test-collection",
		Dimensions:     128,
		DistanceMetric: "cosine",
	})
	require.NoError(t, err)
}

func TestCreateCollection_AlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"root_cause":[{"type":"resource_already_exists_exception","reason":"index [test-coll] already exists"}]}}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.CreateCollection(context.Background(), CollectionConfig{
		Name:           "test-coll",
		Dimensions:     128,
		DistanceMetric: "cosine",
	})
	require.Error(t, err)
	vErr, ok := err.(*VDBError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeCollectionExists, vErr.Code)
}

// ---------------------------------------------------------------------------
// ListCollections
// ---------------------------------------------------------------------------

func TestListCollections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "_cat/indices")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"index":"my-index"},{"index":".system-index"},{"index":"another-index"}]`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	names, err := client.ListCollections(context.Background())
	require.NoError(t, err)
	assert.Contains(t, names, "my-index")
	assert.Contains(t, names, "another-index")
	// System indexes should be filtered out
	assert.NotContains(t, names, ".system-index")
}

// ---------------------------------------------------------------------------
// CountDocuments
// ---------------------------------------------------------------------------

func TestCountDocuments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "_count")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"count":42,"_shards":{"total":1,"successful":1,"failed":0}}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	count, err := client.CountDocuments(context.Background(), "my-index", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(42), count)
}

func TestCountDocuments_WithFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		query, ok := body["query"].(map[string]interface{})
		assert.True(t, ok)
		// Should contain a term filter for category
		assert.NotNil(t, query)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"count":5}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	count, err := client.CountDocuments(context.Background(), "my-index", map[string]interface{}{
		"category": "technology",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
}

// ---------------------------------------------------------------------------
// GetDocument
// ---------------------------------------------------------------------------

func TestGetDocument_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/my-index/_doc/doc-123", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"_index": "my-index",
			"_id": "doc-123",
			"_version": 1,
			"_seq_no": 0,
			"_primary_term": 1,
			"found": true,
			"_source": {
				"id": "doc-123",
				"content": "Hello World",
				"metadata": {"tag": "test"},
				"embedding": [0.1, 0.2, 0.3]
			}
		}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	doc, err := client.GetDocument(context.Background(), "my-index", "doc-123")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "doc-123", doc.ID)
	assert.Equal(t, "Hello World", doc.Content)
	assert.Equal(t, "test", doc.Payload["tag"])
}

func TestGetDocument_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"_index":"my-index","_id":"missing-doc","found":false}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	doc, err := client.GetDocument(context.Background(), "my-index", "missing-doc")
	require.Error(t, err)
	assert.Nil(t, doc)
	vErr, ok := err.(*VDBError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeDocumentNotFound, vErr.Code)
}

// ---------------------------------------------------------------------------
// VectorSearch
// ---------------------------------------------------------------------------

func TestVectorSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "_search")

		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		// Verify knn query structure
		query, ok := body["query"].(map[string]interface{})
		require.True(t, ok)
		assert.NotNil(t, query["knn"])

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"hits": {
				"total": {"value": 2, "relation": "eq"},
				"hits": [
					{"_id": "doc-1", "_score": 0.95, "_source": {"content": "First result", "metadata": {"cat": "a"}}},
					{"_id": "doc-2", "_score": 0.82, "_source": {"content": "Second result", "metadata": {"cat": "b"}}}
				]
			}
		}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	results, err := client.VectorSearch(context.Background(), SearchRequest{
		CollectionName: "my-index",
		QueryVector:    []float64{0.1, 0.2, 0.3, 0.4},
		TopK:           5,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "doc-1", results[0].ID)
	assert.InDelta(t, 0.95, results[0].Score, 0.001)
	assert.Equal(t, "First result", results[0].Content)
}

func TestVectorSearch_WithScoreThreshold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"hits": {
				"total": {"value": 3, "relation": "eq"},
				"hits": [
					{"_id": "doc-1", "_score": 0.95, "_source": {"content": "High score"}},
					{"_id": "doc-2", "_score": 0.60, "_source": {"content": "Low score"}},
					{"_id": "doc-3", "_score": 0.45, "_source": {"content": "Too low"}}
				]
			}
		}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	results, err := client.VectorSearch(context.Background(), SearchRequest{
		CollectionName: "my-index",
		QueryVector:    []float64{0.1, 0.2, 0.3, 0.4},
		TopK:           5,
		ScoreThreshold: 0.7,
	})
	require.NoError(t, err)
	// Only results with score >= 0.7 should be returned
	require.Len(t, results, 1)
	assert.Equal(t, "doc-1", results[0].ID)
}

// ---------------------------------------------------------------------------
// HybridSearch
// ---------------------------------------------------------------------------

func TestHybridSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "_search")

		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		query, ok := body["query"].(map[string]interface{})
		require.True(t, ok)
		// Verify bool/should structure for hybrid
		boolQ, ok := query["bool"].(map[string]interface{})
		require.True(t, ok)
		should, ok := boolQ["should"].([]interface{})
		require.True(t, ok)
		assert.GreaterOrEqual(t, len(should), 1, "should have at least one clause")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"hits": {
				"total": {"value": 1, "relation": "eq"},
				"hits": [
					{"_id": "doc-1", "_score": 0.88, "_source": {"content": "Hybrid result"}}
				]
			}
		}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	results, err := client.HybridSearch(context.Background(), HybridSearchRequest{
		CollectionName: "my-index",
		QueryText:      "machine learning",
		QueryVector:    []float64{0.1, 0.2, 0.3, 0.4},
		TopK:           5,
		Alpha:          0.5,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "doc-1", results[0].ID)
}

// ---------------------------------------------------------------------------
// UpsertDocuments
// ---------------------------------------------------------------------------

func TestUpsertDocuments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "_bulk")
		assert.Equal(t, "application/x-ndjson", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"took":5,"errors":false,"items":[{"index":{"_id":"doc-1","result":"created","status":201}}]}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.UpsertDocuments(context.Background(), "my-index", []Document{
		{
			ID:      "doc-1",
			Content: "Test content",
			Vector:  []float64{0.1, 0.2, 0.3},
			Payload: map[string]interface{}{"tag": "test"},
		},
	})
	require.NoError(t, err)
}

func TestUpsertDocuments_BulkErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"took":5,"errors":true,"items":[{"index":{"_id":"doc-1","status":400,"error":{"type":"mapper_parsing_exception"}}}]}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.UpsertDocuments(context.Background(), "my-index", []Document{
		{ID: "doc-1", Content: "bad doc", Vector: []float64{0.1}},
	})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// DeleteDocuments
// ---------------------------------------------------------------------------

func TestDeleteDocuments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "_bulk")
		assert.Equal(t, "application/x-ndjson", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"took":3,"errors":false,"items":[{"delete":{"_id":"doc-1","result":"deleted","status":200}}]}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.DeleteDocuments(context.Background(), "my-index", []string{"doc-1"})
	require.NoError(t, err)
}

func TestDeleteDocuments_Empty(t *testing.T) {
	// Deleting empty list should be a no-op without any HTTP call
	requestMade := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMade = true
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.DeleteDocuments(context.Background(), "my-index", []string{})
	require.NoError(t, err)
	assert.False(t, requestMade, "no HTTP request should be made for empty IDs")
}

// ---------------------------------------------------------------------------
// DeleteCollection
// ---------------------------------------------------------------------------

func TestDeleteCollection_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/my-index", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"acknowledged":true}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.DeleteCollection(context.Background(), "my-index")
	require.NoError(t, err)
}

func TestDeleteCollection_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"type":"index_not_found_exception"}}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.DeleteCollection(context.Background(), "missing-index")
	require.Error(t, err)
	vErr, ok := err.(*VDBError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeCollectionNotFound, vErr.Code)
}

// ---------------------------------------------------------------------------
// HealthCheck
// ---------------------------------------------------------------------------

func TestHealthCheck_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "_cluster/health")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"green","cluster_name":"test"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.HealthCheck(context.Background())
	require.NoError(t, err)
}

func TestHealthCheck_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.HealthCheck(context.Background())
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Filter builder
// ---------------------------------------------------------------------------

func TestBuildOpenSearchFilter_Nil(t *testing.T) {
	result := buildOpenSearchFilter(nil)
	matchAll, ok := result["match_all"]
	assert.True(t, ok)
	assert.NotNil(t, matchAll)
}

func TestBuildOpenSearchFilter_DirectValue(t *testing.T) {
	result := buildOpenSearchFilter(map[string]interface{}{
		"category": "tech",
	})
	term, ok := result["term"]
	require.True(t, ok)
	termMap := term.(map[string]interface{})
	assert.Equal(t, "tech", termMap["metadata.category"])
}

func TestBuildOpenSearchFilter_OperatorGte(t *testing.T) {
	result := buildOpenSearchFilter(map[string]interface{}{
		"score": map[string]interface{}{"$gte": 10},
	})
	rangeQ, ok := result["range"]
	require.True(t, ok)
	rangeMap := rangeQ.(map[string]interface{})
	fieldRange := rangeMap["metadata.score"].(map[string]interface{})
	assert.Equal(t, 10, fieldRange["gte"])
}

func TestBuildOpenSearchFilter_OperatorIn(t *testing.T) {
	result := buildOpenSearchFilter(map[string]interface{}{
		"tags": map[string]interface{}{"$in": []string{"rag", "llm"}},
	})
	terms, ok := result["terms"]
	require.True(t, ok)
	termsMap := terms.(map[string]interface{})
	assert.NotNil(t, termsMap["metadata.tags"])
}

func TestBuildOpenSearchFilter_OperatorNe(t *testing.T) {
	result := buildOpenSearchFilter(map[string]interface{}{
		"status": map[string]interface{}{"$ne": "archived"},
	})
	boolQ, ok := result["bool"]
	require.True(t, ok)
	boolMap := boolQ.(map[string]interface{})
	assert.NotNil(t, boolMap["must_not"])
}

func TestBuildOpenSearchFilter_MultipleFilters(t *testing.T) {
	result := buildOpenSearchFilter(map[string]interface{}{
		"category": "tech",
		"year":     2024,
	})
	boolQ, ok := result["bool"]
	require.True(t, ok)
	boolMap := boolQ.(map[string]interface{})
	must, ok := boolMap["must"].([]interface{})
	require.True(t, ok)
	assert.Len(t, must, 2)
}

// ---------------------------------------------------------------------------
// ScrollDocuments
// ---------------------------------------------------------------------------

func TestScrollDocuments_FirstPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, float64(0), body["from"])
		assert.Equal(t, float64(2), body["size"])

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"hits": {
				"total": {"value": 5, "relation": "eq"},
				"hits": [
					{"_id": "d1", "_score": 1.0, "_source": {"content": "Doc 1"}},
					{"_id": "d2", "_score": 1.0, "_source": {"content": "Doc 2"}}
				]
			}
		}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	result, err := client.ScrollDocuments(context.Background(), ScrollRequest{
		CollectionName: "my-index",
		Limit:          2,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Documents, 2)
	assert.Equal(t, "2", result.NextOffset)
	assert.Equal(t, int64(5), result.Total)
}

func TestScrollDocuments_LastPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"hits": {
				"total": {"value": 3, "relation": "eq"},
				"hits": [
					{"_id": "d3", "_score": 1.0, "_source": {"content": "Doc 3"}}
				]
			}
		}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	result, err := client.ScrollDocuments(context.Background(), ScrollRequest{
		CollectionName: "my-index",
		Limit:          2,
		Offset:         "2",
	})
	require.NoError(t, err)
	assert.Len(t, result.Documents, 1)
	assert.Empty(t, result.NextOffset, "last page should have empty next offset")
}

// ---------------------------------------------------------------------------
// DBType
// ---------------------------------------------------------------------------

func TestDBType(t *testing.T) {
	client := newTestClient("http://localhost:9200")
	assert.Equal(t, "opensearch", client.DBType())
}

// ---------------------------------------------------------------------------
// DeleteByFilter
// ---------------------------------------------------------------------------

func TestDeleteByFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "_delete_by_query")

		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.NotNil(t, body["query"])

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deleted":7,"failures":[]}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	deleted, err := client.DeleteByFilter(context.Background(), "my-index", map[string]interface{}{
		"status": "archived",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(7), deleted)
}
