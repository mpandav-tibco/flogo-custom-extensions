package countDocuments

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
	if f.inputs == nil { return nil }; return f.inputs[name]
}
func (f *fakeActivityContext) SetOutput(name string, value interface{}) error {
	if f.outputs == nil { f.outputs = make(map[string]interface{}) }
	f.outputs[name] = value; return nil
}
func (f *fakeActivityContext) GetInputObject(obj data.StructValue) error { return obj.FromMap(f.inputs) }
func (f *fakeActivityContext) SetOutputObject(obj data.StructValue) error {
	if f.outputs == nil { f.outputs = make(map[string]interface{}) }
	for k, v := range obj.ToMap() { f.outputs[k] = v }; return nil
}
func (f *fakeActivityContext) GetSharedTempData() map[string]interface{} { return nil }
func (f *fakeActivityContext) Logger() log.Logger                        { return log.RootLogger() }
func (f *fakeActivityContext) GetTracingContext() trace.TracingContext    { return nil }
func (f *fakeActivityContext) GoContext() context.Context                { return context.Background() }

func newTestConn(client vectordb.VectorDBClient) *vectordbconnector.VectorDBConnection {
	s := &vectordbconnector.Settings{DBType: "qdrant", Host: "localhost", TimeoutSeconds: 5}
	return vectordbconnector.NewConnectionForTest("test-conn", client, s)
}

func TestCountDocuments_All(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("CountDocuments", mock.Anything, "col", map[string]interface{}(nil)).
		Return(int64(42), nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"collectionName": "col"}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok); assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, int64(42), ctx.outputs["count"])
}

func TestCountDocuments_WithFilter(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("CountDocuments", mock.Anything, "col",
		map[string]interface{}{"status": "published"}).
		Return(int64(10), nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"filters":        map[string]interface{}{"status": "published"},
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok); assert.NoError(t, err)
	assert.Equal(t, int64(10), ctx.outputs["count"])
}

func TestCountDocuments_MissingCollection(t *testing.T) {
	a := &Activity{conn: newTestConn(&mockclient.VectorDBClient{}), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
}

func TestCountDocuments_ClientError(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("CountDocuments", mock.Anything, "col", mock.Anything).
		Return(int64(0), fmt.Errorf("count failed"))

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"collectionName": "col"}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok); assert.NoError(t, err)
	assert.Equal(t, false, ctx.outputs["success"])
}
