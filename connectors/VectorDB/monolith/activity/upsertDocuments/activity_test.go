package upsertDocuments

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

// ── fake activity context ──────────────────────────────────────────────────

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
func (f *fakeActivityContext) GetTracingContext() trace.TracingContext    { return nil }
func (f *fakeActivityContext) GoContext() context.Context                { return context.Background() }

func newTestConn(client vectordb.VectorDBClient) *vectordbconnector.VectorDBConnection {
	s := &vectordbconnector.Settings{DBType: "qdrant", Host: "localhost", TimeoutSeconds: 5}
	return vectordbconnector.NewConnectionForTest("test-conn", client, s)
}

// ── tests ──────────────────────────────────────────────────────────────────

func TestUpsertDocuments_HappyPath(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "my-col", mock.Anything).Return(nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "my-col",
		"documents": []interface{}{map[string]interface{}{
			"id": "1", "content": "hello", "vector": []interface{}{0.1, 0.2},
		}},
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, 1, ctx.outputs["upsertedCount"])
	mc.AssertExpectations(t)
}

func TestUpsertDocuments_MissingCollectionName(t *testing.T) {
	a := &Activity{conn: newTestConn(&mockclient.VectorDBClient{}), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "",
		"documents": []interface{}{map[string]interface{}{
			"id": "1", "content": "x", "vector": []interface{}{0.1},
		}},
	}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "collectionName is required")
}

func TestUpsertDocuments_DefaultCollectionFromSettings(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "default-col", mock.Anything).Return(nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{DefaultCollection: "default-col"}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "",
		"documents": []interface{}{map[string]interface{}{
			"id": "2", "content": "world", "vector": []interface{}{0.3},
		}},
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	mc.AssertExpectations(t)
}

func TestUpsertDocuments_EmptyDocuments(t *testing.T) {
	a := &Activity{conn: newTestConn(&mockclient.VectorDBClient{}), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents":      []interface{}{},
	}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
}

func TestUpsertDocuments_ClientError(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "col", mock.Anything).
		Return(fmt.Errorf("connection refused"))
	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents": []interface{}{map[string]interface{}{
			"id": "1", "content": "x", "vector": []interface{}{0.1},
		}},
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, false, ctx.outputs["success"])
	assert.Contains(t, ctx.outputs["error"].(string), "connection refused")
}

func TestUpsertDocuments_InvalidVectorType(t *testing.T) {
	a := &Activity{conn: newTestConn(&mockclient.VectorDBClient{}), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents": []interface{}{map[string]interface{}{
			"id": "1", "vector": "not-a-vector",
		}},
	}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
}

func TestUpsertDocuments_MultipleDocuments(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "col", mock.Anything).Return(nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents": []interface{}{
			map[string]interface{}{"id": "1", "content": "a", "vector": []interface{}{0.1}},
			map[string]interface{}{"id": "2", "content": "b", "vector": []interface{}{0.2}},
			map[string]interface{}{"id": "3", "content": "c", "vector": []interface{}{0.3}},
		},
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, 3, ctx.outputs["upsertedCount"])
}
