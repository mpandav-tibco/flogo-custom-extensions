package vectorSearch

import (
	"context"
	"fmt"
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

func TestVectorSearch_HappyPath(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	results := []vectordb.SearchResult{{ID: "1", Score: 0.95, Payload: map[string]interface{}{"text": "hello"}}}
	mc.On("VectorSearch", mock.Anything, mock.MatchedBy(func(r vectordb.SearchRequest) bool {
		return r.CollectionName == "col" && r.TopK == 5
	})).Return(results, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryVector":    []interface{}{0.1, 0.2, 0.3},
		"topK":           5,
		"scoreThreshold": 0.0,
		"withVectors":    false,
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, 1, ctx.outputs["totalCount"])
}

func TestVectorSearch_EmptyVector(t *testing.T) {
	a := &Activity{conn: newTestConn(&mockclient.VectorDBClient{}), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryVector":    []interface{}{},
	}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "queryVector must not be empty")
}

func TestVectorSearch_MissingCollection(t *testing.T) {
	a := &Activity{conn: newTestConn(&mockclient.VectorDBClient{}), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "",
		"queryVector":    []interface{}{0.1},
	}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
}

func TestVectorSearch_DefaultTopKFromSettings(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("VectorSearch", mock.Anything, mock.MatchedBy(func(r vectordb.SearchRequest) bool {
		return r.TopK == 3
	})).Return([]vectordb.SearchResult{}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{DefaultTopK: 3}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryVector":    []interface{}{0.1},
		"topK":           0, // should use default
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	mc.AssertExpectations(t)
}

func TestVectorSearch_ClientError(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("VectorSearch", mock.Anything, mock.Anything).
		Return([]vectordb.SearchResult{}, fmt.Errorf("timeout"))

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryVector":    []interface{}{0.1},
		"topK":           5,
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, false, ctx.outputs["success"])
	assert.Contains(t, ctx.outputs["error"].(string), "timeout")
}

func TestVectorSearch_FallbackTopK(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("VectorSearch", mock.Anything, mock.MatchedBy(func(r vectordb.SearchRequest) bool {
		return r.TopK == 10 // hardcoded fallback
	})).Return([]vectordb.SearchResult{}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{DefaultTopK: 0}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryVector":    []interface{}{0.1},
		"topK":           0,
	}}
	a.Eval(ctx) //nolint
	mc.AssertExpectations(t)
}

func TestVectorSearch_SkipPayload_Forwarded(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("VectorSearch", mock.Anything, mock.MatchedBy(func(r vectordb.SearchRequest) bool {
		return r.SkipPayload == true
	})).Return([]vectordb.SearchResult{{ID: "1", Score: 0.9}}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryVector":    []interface{}{0.1},
		"topK":           3,
		"skipPayload":    true,
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	mc.AssertExpectations(t)
}
