package ragQuery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mpandav-tibco/flogo-extensions/vectordb"
	vectordbconnector "github.com/mpandav-tibco/flogo-extensions/vectordb/connector"
	mockclient "github.com/mpandav-tibco/flogo-extensions/vectordb/testutil/mock"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/trace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// fake activity context
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// local JSON fixture types (mirrors of vdbembed unexported response structs)
// ---------------------------------------------------------------------------

type openAIEmbedData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}
type openAIEmbedUsage struct {
	TotalTokens int `json:"total_tokens"`
}
type openAIEmbedResponse struct {
	Data  []openAIEmbedData `json:"data"`
	Usage openAIEmbedUsage  `json:"usage"`
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func makeEmbedServer(vec []float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIEmbedResponse{
			Data:  []openAIEmbedData{{Embedding: vec, Index: 0}},
			Usage: openAIEmbedUsage{TotalTokens: 10},
		})
	}))
}

func newTestConn(client vectordb.VectorDBClient) *vectordbconnector.VectorDBConnection {
	s := &vectordbconnector.Settings{DBType: "qdrant", Host: "localhost", TimeoutSeconds: 5}
	return vectordbconnector.NewConnectionForTest("test-conn", client, s)
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestRAGQuery_HappyPath_NumberedFormat(t *testing.T) {
	vec := []float64{0.1, 0.2, 0.3}
	embedServer := makeEmbedServer(vec)
	defer embedServer.Close()

	mc := &mockclient.VectorDBClient{}
	searchResults := []vectordb.SearchResult{
		{ID: "doc1", Score: 0.95, Payload: map[string]interface{}{"text": "Flogo is an event-driven integration platform."}},
		{ID: "doc2", Score: 0.88, Payload: map[string]interface{}{"text": "TIBCO provides enterprise software solutions."}},
	}
	mc.On("VectorSearch", mock.Anything, mock.MatchedBy(func(r vectordb.SearchRequest) bool {
		return r.CollectionName == "docs" && r.TopK == 5
	})).Return(searchResults, nil)

	a := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingBaseURL:  embedServer.URL,
			EmbeddingModel:    "text-embedding-3-small",
			DefaultCollection: "docs",
			DefaultTopK:       5,
			ContentField:      "text",
			ContextFormat:     "numbered",
			TimeoutSeconds:    10,
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"queryText": "what is Flogo?",
		"topK":      0,
	}}

	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, 2, ctx.outputs["totalFound"])

	context, _ := ctx.outputs["formattedContext"].(string)
	assert.Contains(t, context, "1.")
	assert.Contains(t, context, "Flogo is an event-driven")
	assert.Contains(t, context, "2.")

	qEmb, _ := ctx.outputs["queryEmbedding"].([]interface{})
	assert.Len(t, qEmb, 3)

	mc.AssertExpectations(t)
}

func TestRAGQuery_HappyPath_MarkdownFormat(t *testing.T) {
	vec := []float64{0.5, 0.6}
	embedServer := makeEmbedServer(vec)
	defer embedServer.Close()

	mc := &mockclient.VectorDBClient{}
	mc.On("VectorSearch", mock.Anything, mock.Anything).Return(
		[]vectordb.SearchResult{{ID: "1", Score: 0.9, Payload: map[string]interface{}{"text": "some content"}}}, nil)

	a := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI", EmbeddingBaseURL: embedServer.URL,
			EmbeddingModel: "text-embedding-3-small", DefaultCollection: "col",
			DefaultTopK: 3, ContentField: "text", ContextFormat: "markdown", TimeoutSeconds: 5,
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"queryText": "query"}}

	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	ctxStr, _ := ctx.outputs["formattedContext"].(string)
	assert.Contains(t, ctxStr, "**[1]**")
	assert.Contains(t, ctxStr, "0.9000")
}

func TestRAGQuery_XMLFormat(t *testing.T) {
	vec := []float64{0.1}
	embedServer := makeEmbedServer(vec)
	defer embedServer.Close()

	mc := &mockclient.VectorDBClient{}
	mc.On("VectorSearch", mock.Anything, mock.Anything).Return(
		[]vectordb.SearchResult{{ID: "1", Score: 0.85, Payload: map[string]interface{}{"text": "xml content"}}}, nil)

	a := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI", EmbeddingBaseURL: embedServer.URL,
			EmbeddingModel: "text-embedding-3-small", DefaultCollection: "col",
			DefaultTopK: 3, ContentField: "text", ContextFormat: "xml", TimeoutSeconds: 5,
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"queryText": "query"}}

	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	ctxStr, _ := ctx.outputs["formattedContext"].(string)
	assert.Contains(t, ctxStr, "<context>")
	assert.Contains(t, ctxStr, "<document id=\"1\"")
	assert.Contains(t, ctxStr, "</context>")
}

func TestRAGQuery_EmbeddingFails(t *testing.T) {
	// Server returns HTTP 500
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server error"}`))
	}))
	defer errorServer.Close()

	a := &Activity{
		conn: newTestConn(&mockclient.VectorDBClient{}),
		settings: &Settings{
			EmbeddingProvider: "OpenAI", EmbeddingBaseURL: errorServer.URL,
			EmbeddingModel: "text-embedding-3-small", DefaultCollection: "col",
			DefaultTopK: 5, ContentField: "text", ContextFormat: "numbered", TimeoutSeconds: 5,
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"queryText": "some query"}}

	ok, err := a.Eval(ctx)
	assert.False(t, ok)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embedding failed")
}

func TestRAGQuery_VectorSearchFails(t *testing.T) {
	vec := []float64{0.1, 0.2}
	embedServer := makeEmbedServer(vec)
	defer embedServer.Close()

	mc := &mockclient.VectorDBClient{}
	mc.On("VectorSearch", mock.Anything, mock.Anything).Return(
		[]vectordb.SearchResult(nil), fmt.Errorf("collection not found"))

	a := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI", EmbeddingBaseURL: embedServer.URL,
			EmbeddingModel: "text-embedding-3-small", DefaultCollection: "missing",
			DefaultTopK: 5, ContentField: "text", ContextFormat: "numbered", TimeoutSeconds: 5,
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"queryText": "query"}}

	ok, err := a.Eval(ctx)
	assert.False(t, ok)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vector search failed")
}

func TestRAGQuery_MissingQueryText(t *testing.T) {
	a := &Activity{
		conn: newTestConn(&mockclient.VectorDBClient{}),
		settings: &Settings{
			EmbeddingProvider: "OpenAI", EmbeddingModel: "text-embedding-3-small",
			DefaultCollection: "col", DefaultTopK: 5, ContentField: "text",
			ContextFormat: "numbered", TimeoutSeconds: 5,
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"queryText": ""}}

	_, err := a.Eval(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "queryText is required")
}

func TestRAGQuery_MissingCollection(t *testing.T) {
	a := &Activity{
		conn: newTestConn(&mockclient.VectorDBClient{}),
		settings: &Settings{
			EmbeddingProvider: "OpenAI", EmbeddingModel: "text-embedding-3-small",
			DefaultCollection: "", DefaultTopK: 5, ContentField: "text",
			ContextFormat: "numbered", TimeoutSeconds: 5,
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"queryText": "hello"}}

	_, err := a.Eval(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "collectionName is required")
}

func TestRAGQuery_EmptyResults_EmptyContext(t *testing.T) {
	vec := []float64{0.1}
	embedServer := makeEmbedServer(vec)
	defer embedServer.Close()

	mc := &mockclient.VectorDBClient{}
	mc.On("VectorSearch", mock.Anything, mock.Anything).Return([]vectordb.SearchResult{}, nil)

	a := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI", EmbeddingBaseURL: embedServer.URL,
			EmbeddingModel: "text-embedding-3-small", DefaultCollection: "col",
			DefaultTopK: 5, ContentField: "text", ContextFormat: "numbered", TimeoutSeconds: 5,
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"queryText": "obscure query"}}

	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, 0, ctx.outputs["totalFound"])
	assert.Equal(t, "", ctx.outputs["formattedContext"])
}

func TestRAGQuery_InputOverridesDefaults(t *testing.T) {
	vec := []float64{0.1, 0.2}
	embedServer := makeEmbedServer(vec)
	defer embedServer.Close()

	mc := &mockclient.VectorDBClient{}
	mc.On("VectorSearch", mock.Anything, mock.MatchedBy(func(r vectordb.SearchRequest) bool {
		return r.CollectionName == "override-col" && r.TopK == 10
	})).Return([]vectordb.SearchResult{}, nil)

	a := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI", EmbeddingBaseURL: embedServer.URL,
			EmbeddingModel: "text-embedding-3-small", DefaultCollection: "default-col",
			DefaultTopK: 5, ContentField: "text", ContextFormat: "numbered", TimeoutSeconds: 5,
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"queryText":      "test",
		"collectionName": "override-col",
		"topK":           10,
	}}

	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	mc.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Pure function unit tests — no Flogo runtime needed
// ---------------------------------------------------------------------------

func TestFormatContext_Empty(t *testing.T) {
	assert.Equal(t, "", formatContext(nil, "text", "numbered"))
	assert.Equal(t, "", formatContext([]vectordb.SearchResult{}, "text", "numbered"))
}

func TestFormatContext_Numbered(t *testing.T) {
	results := []vectordb.SearchResult{
		{ID: "1", Score: 0.9, Payload: map[string]interface{}{"text": "hello"}},
		{ID: "2", Score: 0.8, Payload: map[string]interface{}{"text": "world"}},
	}
	out := formatContext(results, "text", "numbered")
	assert.Contains(t, out, "1. hello")
	assert.Contains(t, out, "2. world")
}

func TestFormatContext_Markdown(t *testing.T) {
	results := []vectordb.SearchResult{
		{ID: "1", Score: 0.75, Payload: map[string]interface{}{"text": "md content"}},
	}
	out := formatContext(results, "text", "markdown")
	assert.Contains(t, out, "**[1]**")
	assert.Contains(t, out, "0.7500")
	assert.Contains(t, out, "---")
	assert.Contains(t, out, "md content")
}

func TestFormatContext_XML(t *testing.T) {
	results := []vectordb.SearchResult{
		{ID: "1", Score: 0.6, Payload: map[string]interface{}{"text": "xml content"}},
	}
	out := formatContext(results, "text", "xml")
	assert.True(t, strings.HasPrefix(out, "<context>"), "should start with <context>")
	assert.Contains(t, out, `<document id="1"`)
	assert.Contains(t, out, "xml content")
	assert.Contains(t, out, "</context>")
}

func TestFormatContext_JSON(t *testing.T) {
	results := []vectordb.SearchResult{
		{ID: "doc-1", Score: 0.95, Payload: map[string]interface{}{"text": "json content"}},
	}
	out := formatContext(results, "text", "json")
	assert.True(t, strings.HasPrefix(out, "["), "should be a JSON array")
	assert.Contains(t, out, `"index"`)
	assert.Contains(t, out, `"score"`)
	assert.Contains(t, out, `"content"`)
	assert.Contains(t, out, "json content")
}

func TestFormatContext_Plain(t *testing.T) {
	results := []vectordb.SearchResult{
		{ID: "1", Score: 0.5, Payload: map[string]interface{}{"text": "plain content"}},
	}
	out := formatContext(results, "text", "plain")
	assert.Contains(t, out, "plain content")
	assert.NotContains(t, out, "1.")
	assert.NotContains(t, out, "**")
}

func TestExtractContent_FieldPresent(t *testing.T) {
	payload := map[string]interface{}{"text": "hello world", "other": "ignore"}
	assert.Equal(t, "hello world", extractContent(payload, "text"))
}

func TestExtractContent_NilPayload(t *testing.T) {
	assert.Equal(t, "", extractContent(nil, "text"))
}

func TestExtractContent_FieldMissing_FallbackJoin(t *testing.T) {
	payload := map[string]interface{}{"description": "fallback value"}
	out := extractContent(payload, "text")
	assert.Contains(t, out, "fallback value")
}

func TestSearchResultsToInterface_Empty(t *testing.T) {
	out := searchResultsToInterface(nil)
	assert.Empty(t, out)
}

func TestSearchResultsToInterface_Fields(t *testing.T) {
	results := []vectordb.SearchResult{
		{ID: "doc-1", Score: 0.95, Payload: map[string]interface{}{"tag": "test"}},
	}
	out := searchResultsToInterface(results)
	require.Len(t, out, 1)
	m, ok := out[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "doc-1", m["id"])
	assert.InDelta(t, 0.95, m["score"], 0.0001)
	assert.NotNil(t, m["payload"])
}

func TestSettingsString_RedactsEmbeddingAPIKey(t *testing.T) {
	s := Settings{
		EmbeddingProvider: "openai",
		EmbeddingModel:    "text-embedding-3-small",
		EmbeddingAPIKey:   "sk-supersecretkey",
	}
	out := s.String()
	assert.NotContains(t, out, "sk-supersecretkey")
	assert.Contains(t, out, "[redacted]")
	assert.Contains(t, out, "openai")
}

func TestSettingsString_NoAPIKey(t *testing.T) {
	s := Settings{EmbeddingProvider: "ollama", EmbeddingModel: "nomic-embed-text"}
	out := s.String()
	assert.NotContains(t, out, "[redacted]")
	assert.Contains(t, out, "ollama")
}
