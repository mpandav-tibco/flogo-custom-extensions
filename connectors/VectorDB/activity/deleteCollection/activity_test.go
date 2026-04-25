package deleteCollection

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

func TestDeleteCollection_HappyPath(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("DeleteCollection", mock.Anything, "drop-me").Return(nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"collectionName": "drop-me"}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok); assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	mc.AssertExpectations(t)
}

func TestDeleteCollection_MissingName(t *testing.T) {
	a := &Activity{conn: newTestConn(&mockclient.VectorDBClient{}), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"collectionName": ""}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
}

func TestDeleteCollection_ClientError(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("DeleteCollection", mock.Anything, "col").
		Return(fmt.Errorf("not found"))

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"collectionName": "col"}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok); assert.NoError(t, err)
	assert.Equal(t, false, ctx.outputs["success"])
}
