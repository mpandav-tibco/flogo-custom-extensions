//go:build !integration

package vectordb

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClientForServer creates a lanceDBClient that points to the given httptest.Server.
func newTestClientForServer(srv *httptest.Server) *lanceDBClient {
	// Parse host and port from srv.URL (e.g. "http://127.0.0.1:PORT")
	trimmed := strings.TrimPrefix(srv.URL, "http://")
	host, portStr, _ := net.SplitHostPort(trimmed)
	port, _ := strconv.Atoi(portStr)
	return &lanceDBClient{
		cfg: ConnectionConfig{
			DBType:         "lancedb",
			Host:           host,
			Port:           port,
			Scheme:         "http",
			TimeoutSeconds: 5,
			MaxRetries:     0,
			RetryBackoffMs: 0,
		},
		httpClient: srv.Client(),
	}
}

func TestHealthCheck_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/table/", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"tables":[]}`))
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	err := c.HealthCheck(context.Background())
	require.NoError(t, err)
}

func TestHealthCheck_ServerDown(t *testing.T) {
	// Point at a port that's not listening.
	c := &lanceDBClient{
		cfg: ConnectionConfig{
			Host: "127.0.0.1", Port: 19999, Scheme: "http",
			TimeoutSeconds: 1, MaxRetries: 0, RetryBackoffMs: 0,
		},
		httpClient: &http.Client{Timeout: 1},
	}
	err := c.HealthCheck(context.Background())
	require.Error(t, err)
}

func TestCollectionExists_True(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name":"test","schema":{},"version":1}`))
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	exists, err := c.CollectionExists(context.Background(), "test")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestCollectionExists_False(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	exists, err := c.CollectionExists(context.Background(), "missing")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCreateCollection_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	err := c.CreateCollection(context.Background(), CollectionConfig{
		Name: "mytest", Dimensions: 8, DistanceMetric: "cosine",
	})
	require.NoError(t, err)
}

func TestListCollections_TablesFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"tables":["t1","t2"]}`))
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	names, err := c.ListCollections(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"t1", "t2"}, names)
}

func TestListCollections_PlainArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`["a","b","c"]`))
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	names, err := c.ListCollections(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, names)
}

func TestCountDocuments_JSONFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count":42}`))
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	count, err := c.CountDocuments(context.Background(), "mycol", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(42), count)
}

func TestCountDocuments_PlainInt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`7`))
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	count, err := c.CountDocuments(context.Background(), "mycol", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(7), count)
}

func TestGetDocument_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": "doc1", "content": "hello world", "metadata": `{"cat":"test"}`, "embedding": []float64{0.1, 0.2}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	doc, err := c.GetDocument(context.Background(), "mycol", "doc1")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "doc1", doc.ID)
	assert.Equal(t, "hello world", doc.Content)
}

func TestGetDocument_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	_, err := c.GetDocument(context.Background(), "mycol", "missing")
	require.Error(t, err)
	vdbErr, ok := err.(*VDBError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeDocumentNotFound, vdbErr.Code)
}

func TestVectorSearch_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": "r1", "content": "result one", "metadata": "", "_distance": 0.05},
				{"id": "r2", "content": "result two", "metadata": "", "_distance": 0.15},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	results, err := c.VectorSearch(context.Background(), SearchRequest{
		CollectionName: "mycol",
		QueryVector:    []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
		TopK:           5,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "r1", results[0].ID)
	assert.InDelta(t, 0.95, results[0].Score, 0.01) // 1.0 - 0.05
}

func TestUpsertDocuments_OK(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	err := c.UpsertDocuments(context.Background(), "mycol", []Document{
		{ID: "d1", Content: "text", Vector: []float64{0.1, 0.2, 0.3, 0.4}},
	})
	require.NoError(t, err)
}

func TestDeleteCollection_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	err := c.DeleteCollection(context.Background(), "gone")
	require.Error(t, err)
	vdbErr, ok := err.(*VDBError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeCollectionNotFound, vdbErr.Code)
}

func TestAuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClientForServer(srv)
	err := c.HealthCheck(context.Background())
	require.Error(t, err)
}

func TestValidateConnectionConfig_Defaults(t *testing.T) {
	cfg := &ConnectionConfig{Host: "localhost"}
	err := validateConnectionConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, "lancedb", cfg.DBType)
	assert.Equal(t, 30, cfg.TimeoutSeconds)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 500, cfg.RetryBackoffMs)
}

func TestValidateConnectionConfig_MissingHost(t *testing.T) {
	cfg := &ConnectionConfig{}
	err := validateConnectionConfig(cfg)
	require.Error(t, err)
	vdbErr, ok := err.(*VDBError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeMissingHost, vdbErr.Code)
}

func TestPayloadToJSON_RoundTrip(t *testing.T) {
	m := map[string]interface{}{"key": "value", "num": float64(42)}
	s := payloadToJSON(m)
	assert.NotEmpty(t, s)
	recovered := jsonToPayload(s)
	assert.Equal(t, m["key"], recovered["key"])
}

func TestBuildLanceDBPredicate(t *testing.T) {
	p := buildLanceDBPredicate(map[string]interface{}{"category": "ml"})
	assert.Contains(t, p, "category")
	assert.Contains(t, p, "ml")
}

func TestRRFFuse_Basic(t *testing.T) {
	vResults := []SearchResult{{ID: "a", Score: 0.9}, {ID: "b", Score: 0.8}}
	tResults := []SearchResult{{ID: "b", Score: 0.85}, {ID: "c", Score: 0.7}}
	out := rrfFuse(vResults, tResults, 0.5, 3, 0, false)
	// b should score highest (appears in both)
	require.NotEmpty(t, out)
	// b is in both lists and should rank well
	ids := make([]string, len(out))
	for i, r := range out {
		ids[i] = r.ID
	}
	assert.Contains(t, ids, "b")
}
