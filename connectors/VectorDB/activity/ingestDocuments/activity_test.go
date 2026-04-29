package ingestDocuments

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func newTestConn(client vectordb.VectorDBClient) *vectordbconnector.VectorDBConnection {
	s := &vectordbconnector.Settings{DBType: "qdrant", Host: "localhost", TimeoutSeconds: 5}
	return vectordbconnector.NewConnectionForTest("test-conn", client, s)
}

func singleEmbedServer(dims int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		emb := make([]float64, dims)
		for i := range emb {
			emb[i] = float64(i+1) * 0.1
		}
		resp := map[string]interface{}{
			"data":  []map[string]interface{}{{"embedding": emb, "index": 0}},
			"usage": map[string]interface{}{"total_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func batchEmbedServer(dims int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		n := len(req.Input)
		if n == 0 {
			n = 1
		}
		data := make([]map[string]interface{}, n)
		for i := 0; i < n; i++ {
			emb := make([]float64, dims)
			for j := range emb {
				emb[j] = float64(i+1) * 0.1
			}
			data[i] = map[string]interface{}{"embedding": emb, "index": i}
		}
		resp := map[string]interface{}{
			"data":  data,
			"usage": map[string]interface{}{"total_tokens": n * 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func getOutput(outputs map[string]interface{}) *Output {
	out := &Output{}
	if v, ok := outputs["success"].(bool); ok {
		out.Success = v
	}
	if v, ok := outputs["ingestedCount"].(int); ok {
		out.IngestedCount = v
	}
	if v, ok := outputs["dimensions"].(int); ok {
		out.Dimensions = v
	}
	if v, ok := outputs["duration"].(string); ok {
		out.Duration = v
	}
	if v, ok := outputs["error"].(string); ok {
		out.Error = v
	}
	if v, ok := outputs["ids"].([]string); ok {
		out.IDs = v
	}
	if v, ok := outputs["sourceDocumentCount"].(int); ok {
		out.SourceDocumentCount = v
	}
	if v, ok := outputs["chunksCreated"].(int); ok {
		out.ChunksCreated = v
	}
	return out
}

func TestIngestDocuments_HappyPath(t *testing.T) {
	srv := singleEmbedServer(4)
	defer srv.Close()
	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "my-collection", mock.Anything).Return(nil)
	act := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingAPIKey:   "test-key",
			EmbeddingBaseURL:  srv.URL + "/v1",
			EmbeddingModel:    "text-embedding-3-small",
			ContentField:      "text",
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "my-collection",
		"documents": []interface{}{
			map[string]interface{}{"id": "doc1", "text": "the quick brown fox"},
		},
	}}
	done, err := act.Eval(ctx)
	require.NoError(t, err)
	assert.True(t, done)
	out := getOutput(ctx.outputs)
	assert.True(t, out.Success)
	assert.Equal(t, 1, out.IngestedCount)
	assert.Equal(t, 4, out.Dimensions)
	assert.Empty(t, out.Error)
	mc.AssertExpectations(t)
}

func TestIngestDocuments_BatchDocuments(t *testing.T) {
	srv := batchEmbedServer(4)
	defer srv.Close()
	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "batch-col", mock.Anything).Return(nil)
	act := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingBaseURL:  srv.URL + "/v1",
			EmbeddingModel:    "text-embedding-3-small",
			ContentField:      "text",
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "batch-col",
		"documents": []interface{}{
			map[string]interface{}{"id": "a", "text": "first doc"},
			map[string]interface{}{"id": "b", "text": "second doc"},
			map[string]interface{}{"id": "c", "text": "third doc"},
		},
	}}
	done, err := act.Eval(ctx)
	require.NoError(t, err)
	assert.True(t, done)
	out := getOutput(ctx.outputs)
	assert.True(t, out.Success)
	assert.Equal(t, 3, out.IngestedCount)
	mc.AssertExpectations(t)
}

func TestIngestDocuments_AutoGeneratesIDs(t *testing.T) {
	srv := singleEmbedServer(4)
	defer srv.Close()
	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "col", mock.Anything).Return(nil)
	act := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingBaseURL:  srv.URL + "/v1",
			EmbeddingModel:    "text-embedding-3-small",
			ContentField:      "text",
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents":      []interface{}{map[string]interface{}{"text": "auto id document"}},
	}}
	done, err := act.Eval(ctx)
	require.NoError(t, err)
	assert.True(t, done)
	out := getOutput(ctx.outputs)
	assert.True(t, out.Success)
	assert.Equal(t, 1, out.IngestedCount)
	assert.Empty(t, out.Error)
}

func TestIngestDocuments_DefaultCollection(t *testing.T) {
	srv := singleEmbedServer(4)
	defer srv.Close()
	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "default-col", mock.Anything).Return(nil)
	act := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingBaseURL:  srv.URL + "/v1",
			EmbeddingModel:    "text-embedding-3-small",
			ContentField:      "text",
			DefaultCollection: "default-col",
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "",
		"documents":      []interface{}{map[string]interface{}{"text": "doc"}},
	}}
	done, err := act.Eval(ctx)
	require.NoError(t, err)
	assert.True(t, done)
	out := getOutput(ctx.outputs)
	assert.True(t, out.Success, "expected success but got error: %s", out.Error)
	mc.AssertExpectations(t)
}

func TestIngestDocuments_CustomContentField(t *testing.T) {
	srv := singleEmbedServer(4)
	defer srv.Close()
	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "col", mock.Anything).Return(nil)
	act := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingBaseURL:  srv.URL + "/v1",
			EmbeddingModel:    "text-embedding-3-small",
			ContentField:      "body",
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents":      []interface{}{map[string]interface{}{"id": "x1", "body": "document body text"}},
	}}
	done, err := act.Eval(ctx)
	require.NoError(t, err)
	assert.True(t, done)
	out := getOutput(ctx.outputs)
	assert.True(t, out.Success, "expected success but got error: %s", out.Error)
	mc.AssertExpectations(t)
}

func TestIngestDocuments_EmbeddingFails(t *testing.T) {
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"service unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer errSrv.Close()
	mc := &mockclient.VectorDBClient{}
	act := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingBaseURL:  errSrv.URL + "/v1",
			EmbeddingModel:    "text-embedding-3-small",
			ContentField:      "text",
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents":      []interface{}{map[string]interface{}{"text": "some text"}},
	}}
	done, err := act.Eval(ctx)
	assert.False(t, done)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "embedding")
	assert.Contains(t, err.Error(), "503")
}

func TestIngestDocuments_VectorDBFails(t *testing.T) {
	srv := singleEmbedServer(4)
	defer srv.Close()
	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "col", mock.Anything).Return(fmt.Errorf("connection refused"))
	act := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingBaseURL:  srv.URL + "/v1",
			EmbeddingModel:    "text-embedding-3-small",
			ContentField:      "text",
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents":      []interface{}{map[string]interface{}{"text": "some text"}},
	}}
	done, err := act.Eval(ctx)
	assert.False(t, done)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upsert")
	mc.AssertExpectations(t)
}

func TestIngestDocuments_MissingCollection(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	act := &Activity{
		conn:     newTestConn(mc),
		settings: &Settings{EmbeddingProvider: "OpenAI", EmbeddingModel: "text-embedding-3-small", ContentField: "text"},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "",
		"documents":      []interface{}{map[string]interface{}{"text": "some text"}},
	}}
	_, err := act.Eval(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collectionName is required")
}

func TestIngestDocuments_MissingTextField(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	act := &Activity{
		conn:     newTestConn(mc),
		settings: &Settings{EmbeddingProvider: "OpenAI", EmbeddingModel: "text-embedding-3-small", ContentField: "text"},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents":      []interface{}{map[string]interface{}{"id": "d1", "content": "wrong field"}},
	}}
	_, err := act.Eval(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `field "text" is required`)
}

func TestIngestDocuments_EmptyDocuments(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	act := &Activity{
		conn:     newTestConn(mc),
		settings: &Settings{EmbeddingProvider: "OpenAI", EmbeddingModel: "text-embedding-3-small", ContentField: "text"},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents":      []interface{}{},
	}}
	_, err := act.Eval(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one document or file is required")
}

// ---------------------------------------------------------------------------
// Pure function unit tests — parseDocuments
// ---------------------------------------------------------------------------

func TestParseDocuments_HappyPath(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{"id": "doc1", "text": "hello world", "metadata": map[string]interface{}{"source": "web"}},
		map[string]interface{}{"text": "second doc"},
	}
	docs, err := parseDocuments(raw, "text")
	require.NoError(t, err)
	require.Len(t, docs, 2)
	assert.Equal(t, "doc1", docs[0].ID)
	assert.Equal(t, "hello world", docs[0].Text)
	assert.Equal(t, "web", docs[0].Metadata["source"])
	assert.Equal(t, "", docs[1].ID) // no id provided
	assert.Equal(t, "second doc", docs[1].Text)
}

func TestParseDocuments_Empty(t *testing.T) {
	docs, err := parseDocuments([]interface{}{}, "text")
	require.NoError(t, err)
	assert.Nil(t, docs)
}

func TestParseDocuments_MissingTextField(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{"id": "doc1"},
	}
	_, err := parseDocuments(raw, "text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `field "text" is required`)
}

func TestParseDocuments_DefaultContentField(t *testing.T) {
	// empty contentField should default to "text"
	raw := []interface{}{
		map[string]interface{}{"text": "default field content"},
	}
	docs, err := parseDocuments(raw, "")
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, "default field content", docs[0].Text)
}

func TestParseDocuments_CustomContentField(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{"body": "custom field content"},
	}
	docs, err := parseDocuments(raw, "body")
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, "custom field content", docs[0].Text)
}

func TestParseDocuments_MetadataPassthrough(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{
			"text":     "with metadata",
			"metadata": map[string]interface{}{"category": "news", "priority": float64(1)},
		},
	}
	docs, err := parseDocuments(raw, "text")
	require.NoError(t, err)
	assert.Equal(t, "news", docs[0].Metadata["category"])
	assert.Equal(t, float64(1), docs[0].Metadata["priority"])
}
