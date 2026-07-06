//go:build !integration

package vectordb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient creates an azureAISearchClient pointing at a test server.
func newTestClient(t *testing.T, serverURL string) *azureAISearchClient {
	t.Helper()
	cfg := ConnectionConfig{
		DBType:         "azureaisearch",
		Endpoint:       serverURL,
		APIKey:         "test-api-key",
		APIVersion:     "2024-05-01-Preview",
		TimeoutSeconds: 10,
		MaxRetries:     0, // no retries in unit tests
		RetryBackoffMs: 100,
	}
	client, err := newAzureAISearchClient(cfg)
	require.NoError(t, err)
	return client.(*azureAISearchClient)
}

// respondJSON writes a JSON response with the given status code.
func respondJSON(w http.ResponseWriter, statusCode int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(body)
}

// ─── HealthCheck ─────────────────────────────────────────────────────────────

func TestHealthCheck_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/indexes")
		respondJSON(w, http.StatusOK, map[string]interface{}{"value": []interface{}{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestHealthCheck_AuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.HealthCheck(context.Background())
	require.Error(t, err)
	// HealthCheck wraps the auth error in a connection-failed error at the outer level.
	// Check that it's a VDBError (either auth or connection-failed).
	var vdbErr *VDBError
	assert.ErrorAs(t, err, &vdbErr)
	assert.NotEmpty(t, vdbErr.Code)
}

// ─── CollectionExists ─────────────────────────────────────────────────────────

func TestCollectionExists_True(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{"name": "myindex"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	exists, err := c.CollectionExists(context.Background(), "myindex")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestCollectionExists_False(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]interface{}{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	exists, err := c.CollectionExists(context.Background(), "noindex")
	require.NoError(t, err)
	assert.False(t, exists)
}

// ─── CreateCollection ─────────────────────────────────────────────────────────

func TestCreateCollection_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		respondJSON(w, http.StatusCreated, map[string]interface{}{"name": "testindex"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.CreateCollection(context.Background(), CollectionConfig{
		Name:           "testindex",
		Dimensions:     1536,
		DistanceMetric: "cosine",
	})
	assert.NoError(t, err)
}

func TestCreateCollection_AlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusConflict, map[string]interface{}{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.CreateCollection(context.Background(), CollectionConfig{
		Name:       "existing",
		Dimensions: 768,
	})
	require.Error(t, err)
	var vdbErr *VDBError
	assert.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeCollectionExists, vdbErr.Code)
}

// ─── ListCollections ──────────────────────────────────────────────────────────

func TestListCollections_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"value": []map[string]interface{}{
				{"name": "index1"},
				{"name": "index2"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	names, err := c.ListCollections(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"index1", "index2"}, names)
}

// ─── UpsertDocuments ─────────────────────────────────────────────────────────

func TestUpsertDocuments_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/docs/index")
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"value": []map[string]interface{}{
				{"key": "doc1", "status": true, "statusCode": 200},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.UpsertDocuments(context.Background(), "myindex", []Document{
		{ID: "doc1", Vector: make([]float64, 4), Content: "hello world"},
	})
	assert.NoError(t, err)
}

// ─── GetDocument ─────────────────────────────────────────────────────────────

func TestGetDocument_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"id":       "doc42",
			"content":  "some text",
			"metadata": `{"source":"test"}`,
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	doc, err := c.GetDocument(context.Background(), "myindex", "doc42")
	require.NoError(t, err)
	assert.Equal(t, "doc42", doc.ID)
	assert.Equal(t, "some text", doc.Content)
}

func TestGetDocument_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]interface{}{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.GetDocument(context.Background(), "myindex", "missing")
	require.Error(t, err)
	var vdbErr *VDBError
	assert.ErrorAs(t, err, &vdbErr)
	assert.Equal(t, ErrCodeDocumentNotFound, vdbErr.Code)
}

// ─── CountDocuments ───────────────────────────────────────────────────────────

func TestCountDocuments_NoFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /docs/$count returns a plain integer
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "42")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	count, err := c.CountDocuments(context.Background(), "myindex", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(42), count)
}

// ─── VectorSearch ─────────────────────────────────────────────────────────────

func TestVectorSearch_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/docs/search")
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"value": []map[string]interface{}{
				{
					"id":            "doc1",
					"content":       "result text",
					"metadata":      `{"category":"tech"}`,
					"@search.score": 0.95,
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	results, err := c.VectorSearch(context.Background(), SearchRequest{
		CollectionName: "myindex",
		QueryVector:    []float64{0.1, 0.2, 0.3, 0.4},
		TopK:           5,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "doc1", results[0].ID)
	assert.Equal(t, "result text", results[0].Content)
	assert.InDelta(t, 0.95, results[0].Score, 0.001)
}

// ─── HybridSearch ─────────────────────────────────────────────────────────────

func TestHybridSearch_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate hybrid search payload has search text
		var payload map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		assert.NotEmpty(t, payload["search"])

		respondJSON(w, http.StatusOK, map[string]interface{}{
			"value": []map[string]interface{}{
				{
					"id":            "docH1",
					"content":       "hybrid result",
					"metadata":      "{}",
					"@search.score": 0.88,
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	results, err := c.HybridSearch(context.Background(), HybridSearchRequest{
		CollectionName: "myindex",
		QueryText:      "find something",
		QueryVector:    []float64{0.1, 0.2, 0.3, 0.4},
		TopK:           3,
		Alpha:          0.5,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "docH1", results[0].ID)
}

// ─── ScrollDocuments ──────────────────────────────────────────────────────────

func TestScrollDocuments_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"value": []map[string]interface{}{
				{"id": "s1", "content": "scroll result", "metadata": "{}"},
			},
			"@odata.count": 1,
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	result, err := c.ScrollDocuments(context.Background(), ScrollRequest{
		CollectionName: "myindex",
		Limit:          10,
		Offset:         "",
	})
	require.NoError(t, err)
	assert.Len(t, result.Documents, 1)
	assert.Equal(t, "s1", result.Documents[0].ID)
}

// ─── DeleteCollection ─────────────────────────────────────────────────────────

func TestDeleteCollection_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.DeleteCollection(context.Background(), "myindex")
	assert.NoError(t, err)
}

// ─── DBType / Close ───────────────────────────────────────────────────────────

func TestDBType(t *testing.T) {
	c := &azureAISearchClient{}
	assert.Equal(t, "azureaisearch", c.DBType())
}

func TestClose(t *testing.T) {
	c := &azureAISearchClient{}
	assert.NoError(t, c.Close())
}
