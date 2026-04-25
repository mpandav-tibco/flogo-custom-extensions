package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	vectordbconnector "github.com/mpandav-tibco/flogo-extensions/vectordb/connector"
	mockclient "github.com/mpandav-tibco/flogo-extensions/vectordb/testutil/mock"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/trace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeActivityContext struct {
	inputs  map[string]interface{}
	outputs map[string]interface{}
}

func (f *fakeActivityContext) ActivityHost() activity.Host        { return nil }
func (f *fakeActivityContext) Name() string                       { return "test" }
func (f *fakeActivityContext) GetSetting(name string) interface{} { return nil }
func (f *fakeActivityContext) GetInput(name string) interface{} {
	if f.inputs == nil {
		return nil
	}
	return f.inputs[name]
}
func (f *fakeActivityContext) SetOutput(name string, value interface{}) error {
	if f.outputs == nil {
		f.outputs = make(map[string]interface{})
	}
	f.outputs[name] = value
	return nil
}
func (f *fakeActivityContext) GetInputObject(obj data.StructValue) error {
	return obj.FromMap(f.inputs)
}
func (f *fakeActivityContext) SetOutputObject(obj data.StructValue) error {
	if f.outputs == nil {
		f.outputs = make(map[string]interface{})
	}
	for k, v := range obj.ToMap() {
		f.outputs[k] = v
	}
	return nil
}
func (f *fakeActivityContext) GetSharedTempData() map[string]interface{} { return nil }
func (f *fakeActivityContext) Logger() log.Logger                        { return log.RootLogger() }
func (f *fakeActivityContext) GetTracingContext() trace.TracingContext   { return nil }
func (f *fakeActivityContext) GoContext() context.Context                { return context.Background() }

func makeRerankServer(results []rerankResult, statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(rerankResponseBody{Results: results})
	}))
}

func newTestConn() *vectordbconnector.VectorDBConnection {
	s := &vectordbconnector.Settings{DBType: "qdrant", Host: "localhost", TimeoutSeconds: 5}
	mc := &mockclient.VectorDBClient{}
	return vectordbconnector.NewConnectionForTest("test-conn", mc, s)
}

func TestRerank_HappyPath(t *testing.T) {
	server := makeRerankServer([]rerankResult{
		{Index: 0, RelevanceScore: 0.95},
		{Index: 1, RelevanceScore: 0.72},
	}, http.StatusOK)
	defer server.Close()

	a := &Activity{
		conn:     newTestConn(),
		settings: &Settings{RerankEndpoint: server.URL, Model: "rerank-v3", TopN: 2, TimeoutSeconds: 5},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"query":     "what is AI?",
		"documents": []interface{}{"AI is ...", "Artificial Intelligence ..."},
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, 2, ctx.outputs["totalCount"])
}

func TestRerank_EmptyDocuments(t *testing.T) {
	a := &Activity{
		conn:     newTestConn(),
		settings: &Settings{RerankEndpoint: "http://example.com", TimeoutSeconds: 5},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"query":     "what?",
		"documents": []interface{}{},
	}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "documents must not be empty")
}

func TestRerank_EmptyQuery(t *testing.T) {
	a := &Activity{
		conn:     newTestConn(),
		settings: &Settings{RerankEndpoint: "http://example.com", TimeoutSeconds: 5},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"query":     "",
		"documents": []interface{}{"doc1"},
	}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "query is required")
}

func TestRerank_ServerError(t *testing.T) {
	server := makeRerankServer(nil, http.StatusUnauthorized)
	defer server.Close()

	a := &Activity{
		conn:     newTestConn(),
		settings: &Settings{RerankEndpoint: server.URL, TimeoutSeconds: 5},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"query":     "test",
		"documents": []interface{}{"doc1"},
	}}
	ok, err := a.Eval(ctx)
	assert.False(t, ok)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 401")
}

func TestRerank_TopNDefault(t *testing.T) {
	server := makeRerankServer([]rerankResult{
		{Index: 0, RelevanceScore: 0.9},
		{Index: 1, RelevanceScore: 0.8},
	}, http.StatusOK)
	defer server.Close()

	// TopN=0 should default to len(documents)
	a := &Activity{
		conn:     newTestConn(),
		settings: &Settings{RerankEndpoint: server.URL, TopN: 0, TimeoutSeconds: 5},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"query":     "query",
		"documents": []interface{}{"d1", "d2"},
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
}

// ---------------------------------------------------------------------------
// Pure function unit tests — callRerankAPI
// ---------------------------------------------------------------------------

func TestCallRerankAPI_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer testkey", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rerankResponseBody{
			Results: []rerankResult{
				{Index: 1, RelevanceScore: 0.95},
				{Index: 0, RelevanceScore: 0.72},
			},
		})
	}))
	defer server.Close()

	out, err := callRerankAPI(context.Background(), rerankAPIRequest{
		Endpoint:  server.URL,
		APIKey:    "testkey",
		Model:     "rerank-v3",
		Query:     "what is AI?",
		Documents: []interface{}{"first doc", "second doc"},
		TopN:      2,
	})
	assert.NoError(t, err)
	assert.Len(t, out, 2)
	first := out[0].(map[string]interface{})
	assert.Equal(t, 1, first["index"])
	assert.InDelta(t, 0.95, first["relevance_score"], 0.001)
}

func TestCallRerankAPI_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"overloaded"}`))
	}))
	defer server.Close()

	_, err := callRerankAPI(context.Background(), rerankAPIRequest{
		Endpoint:  server.URL,
		Query:     "test",
		Documents: []interface{}{"doc"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestCallRerankAPI_FallsBackToDataField(t *testing.T) {
	// Some providers (Cohere) return "data" instead of "results"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rerankResponseBody{
			Data: []rerankResult{{Index: 0, RelevanceScore: 0.88}},
		})
	}))
	defer server.Close()

	out, err := callRerankAPI(context.Background(), rerankAPIRequest{
		Endpoint:  server.URL,
		Query:     "q",
		Documents: []interface{}{"doc0"},
	})
	assert.NoError(t, err)
	assert.Len(t, out, 1)
	m := out[0].(map[string]interface{})
	assert.InDelta(t, 0.88, m["relevance_score"], 0.001)
}

func TestCallRerankAPI_DocumentFallbackToInput(t *testing.T) {
	// When rerankResult.Document is nil, the original document by index is used
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rerankResponseBody{
			Results: []rerankResult{{Index: 0, RelevanceScore: 0.5, Document: nil}},
		})
	}))
	defer server.Close()

	out, err := callRerankAPI(context.Background(), rerankAPIRequest{
		Endpoint:  server.URL,
		Query:     "q",
		Documents: []interface{}{"original doc text"},
	})
	assert.NoError(t, err)
	require.Len(t, out, 1)
	m := out[0].(map[string]interface{})
	assert.Equal(t, "original doc text", m["document"])
}
