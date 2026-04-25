package createEmbeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/trace"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// fake activity context (same pattern as other activities in this connector)
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
type ollamaEmbedResponse struct {
	Embeddings      [][]float64 `json:"embeddings"`
	PromptEvalCount int         `json:"prompt_eval_count"`
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func makeOpenAIServer(embeddings [][]float64, statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		data := make([]openAIEmbedData, len(embeddings))
		for i, e := range embeddings {
			data[i] = openAIEmbedData{Embedding: e, Index: i}
		}
		_ = json.NewEncoder(w).Encode(openAIEmbedResponse{
			Data:  data,
			Usage: openAIEmbedUsage{TotalTokens: 8},
		})
	}))
}

func makeCohereServer(embeddings [][]float64, statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		type resp struct {
			Embeddings struct {
				Float [][]float64 `json:"float"`
			} `json:"embeddings"`
		}
		var body resp
		body.Embeddings.Float = embeddings
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func makeOllamaServer(embeddings [][]float64, statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(ollamaEmbedResponse{Embeddings: embeddings, PromptEvalCount: 5})
	}))
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestCreateEmbeddings_SingleText_OpenAI(t *testing.T) {
	vec := []float64{0.1, 0.2, 0.3, 0.4}
	server := makeOpenAIServer([][]float64{vec}, http.StatusOK)
	defer server.Close()

	a := &Activity{settings: &Settings{
		Provider:       "OpenAI",
		Model:          "text-embedding-3-small",
		BaseURL:        server.URL,
		TimeoutSeconds: 5,
	}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"inputText":  "hello world",
		"inputTexts": []interface{}{},
	}}

	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, 4, ctx.outputs["dimensions"])
	assert.Equal(t, 8, ctx.outputs["tokensUsed"])
	emb, _ := ctx.outputs["embedding"].([]interface{})
	assert.Len(t, emb, 4)
}

func TestCreateEmbeddings_BatchTexts_OpenAI(t *testing.T) {
	vecs := [][]float64{{0.1, 0.2}, {0.3, 0.4}}
	server := makeOpenAIServer(vecs, http.StatusOK)
	defer server.Close()

	a := &Activity{settings: &Settings{
		Provider: "OpenAI", Model: "text-embedding-3-small",
		BaseURL: server.URL, TimeoutSeconds: 5,
	}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"inputText":  "",
		"inputTexts": []interface{}{"text A", "text B"},
	}}

	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	allEmbs, _ := ctx.outputs["embeddings"].([]interface{})
	assert.Len(t, allEmbs, 2)
}

func TestCreateEmbeddings_Cohere(t *testing.T) {
	vec := []float64{0.5, 0.6, 0.7}
	server := makeCohereServer([][]float64{vec}, http.StatusOK)
	defer server.Close()

	a := &Activity{settings: &Settings{
		Provider: "Cohere", Model: "embed-english-v3.0",
		BaseURL: server.URL, TimeoutSeconds: 5,
	}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"inputText": "what is AI?", "inputTexts": []interface{}{},
	}}

	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, 3, ctx.outputs["dimensions"])
}

func TestCreateEmbeddings_Ollama(t *testing.T) {
	vec := []float64{0.9, 0.8, 0.7, 0.6}
	server := makeOllamaServer([][]float64{vec}, http.StatusOK)
	defer server.Close()

	a := &Activity{settings: &Settings{
		Provider: "Ollama", Model: "nomic-embed-text",
		BaseURL: server.URL, TimeoutSeconds: 5,
	}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"inputText": "local text", "inputTexts": []interface{}{},
	}}

	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, 4, ctx.outputs["dimensions"])
}

func TestCreateEmbeddings_ProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "invalid api key"}`))
	}))
	defer server.Close()

	a := &Activity{settings: &Settings{
		Provider: "OpenAI", Model: "text-embedding-3-small",
		BaseURL: server.URL, TimeoutSeconds: 5,
	}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"inputText": "hello", "inputTexts": []interface{}{},
	}}

	ok, err := a.Eval(ctx)
	assert.True(t, ok) // activity-level error → success=false, no Go error
	assert.NoError(t, err)
	assert.Equal(t, false, ctx.outputs["success"])
	assert.Contains(t, ctx.outputs["error"], "401")
}

func TestCreateEmbeddings_NoInput(t *testing.T) {
	a := &Activity{settings: &Settings{
		Provider: "OpenAI", Model: "text-embedding-3-small", TimeoutSeconds: 5,
	}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"inputText": "", "inputTexts": []interface{}{},
	}}

	_, err := a.Eval(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "inputText or inputTexts is required")
}
