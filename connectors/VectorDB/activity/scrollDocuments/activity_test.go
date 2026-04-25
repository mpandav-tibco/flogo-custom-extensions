package scrollDocuments

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

func TestScrollDocuments_FirstPage(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("ScrollDocuments", mock.Anything, mock.MatchedBy(func(r vectordb.ScrollRequest) bool {
		return r.CollectionName == "col" && r.Limit == 50 && r.Offset == ""
	})).Return(&vectordb.ScrollResult{
		Documents:  []vectordb.Document{{ID: "1"}, {ID: "2"}},
		NextOffset: "cursor-2",
		Total:      100,
	}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"limit":          50,
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok); assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, "cursor-2", ctx.outputs["nextOffset"])
	assert.Equal(t, int64(100), ctx.outputs["total"])
}

func TestScrollDocuments_DefaultLimit(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("ScrollDocuments", mock.Anything, mock.MatchedBy(func(r vectordb.ScrollRequest) bool {
		return r.Limit == 100
	})).Return(&vectordb.ScrollResult{}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"limit":          0, // triggers default of 100
	}}
	a.Eval(ctx) //nolint
	mc.AssertExpectations(t)
}

func TestScrollDocuments_MissingCollection(t *testing.T) {
	a := &Activity{conn: newTestConn(&mockclient.VectorDBClient{}), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{"limit": 10}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
}

func TestScrollDocuments_ClientError(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("ScrollDocuments", mock.Anything, mock.Anything).
		Return((*vectordb.ScrollResult)(nil), fmt.Errorf("scroll failed"))

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col", "limit": 10,
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok); assert.NoError(t, err)
	assert.Equal(t, false, ctx.outputs["success"])
}
