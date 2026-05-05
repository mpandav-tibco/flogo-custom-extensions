package hybridSearch

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

func TestHybridSearch_TextOnly(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("HybridSearch", mock.Anything, mock.MatchedBy(func(r vectordb.HybridSearchRequest) bool {
		// alpha=0.5 is the descriptor default; user must pass it explicitly for balanced blend.
		// alpha=0.0 is valid (sparse-only) and must NOT be overridden by the activity.
		return r.QueryText == "machine learning" && r.Alpha == 0.5
	})).Return([]vectordb.SearchResult{{ID: "1", Score: 0.9}}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryText":      "machine learning",
		"topK":           5,
		"alpha":          0.5, // Flogo sends the descriptor default; explicit here for clarity
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, true, ctx.outputs["success"])
	assert.Equal(t, 1, ctx.outputs["totalCount"])
}

func TestHybridSearch_DenseOnly(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("HybridSearch", mock.Anything, mock.Anything).
		Return([]vectordb.SearchResult{}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryVector":    []interface{}{0.1, 0.2},
		"topK":           10,
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
}

func TestHybridSearch_NeitherTextNorVector(t *testing.T) {
	a := &Activity{conn: newTestConn(&mockclient.VectorDBClient{}), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
	}}
	_, err := a.Eval(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of")
}

func TestHybridSearch_CustomAlpha(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("HybridSearch", mock.Anything, mock.MatchedBy(func(r vectordb.HybridSearchRequest) bool {
		return r.Alpha == 0.7
	})).Return([]vectordb.SearchResult{}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryText":      "foo",
		"alpha":          0.7,
	}}
	a.Eval(ctx) //nolint
	mc.AssertExpectations(t)
}

func TestHybridSearch_SparseOnly_AlphaZero(t *testing.T) {
	// alpha=0.0 means pure BM25/keyword search and must NOT be overridden to 0.5.
	mc := &mockclient.VectorDBClient{}
	mc.On("HybridSearch", mock.Anything, mock.MatchedBy(func(r vectordb.HybridSearchRequest) bool {
		return r.Alpha == 0.0
	})).Return([]vectordb.SearchResult{}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryText":      "keyword search",
		"alpha":          0.0,
		"topK":           5,
	}}
	a.Eval(ctx) //nolint
	mc.AssertExpectations(t)
}

func TestHybridSearch_ClientError(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("HybridSearch", mock.Anything, mock.Anything).
		Return([]vectordb.SearchResult{}, fmt.Errorf("upstream error"))

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryText":      "foo",
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	assert.Equal(t, false, ctx.outputs["success"])
}

func TestHybridSearch_SkipPayload_Forwarded(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("HybridSearch", mock.Anything, mock.MatchedBy(func(r vectordb.HybridSearchRequest) bool {
		return r.SkipPayload == true
	})).Return([]vectordb.SearchResult{{ID: "1", Score: 0.9}}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"queryText":      "find me",
		"queryVector":    []interface{}{0.1},
		"topK":           3,
		"skipPayload":    true,
	}}
	ok, err := a.Eval(ctx)
	assert.True(t, ok)
	assert.NoError(t, err)
	mc.AssertExpectations(t)
}

func TestHybridSearch_AlphaOutOfRange(t *testing.T) {
	a := &Activity{conn: newTestConn(&mockclient.VectorDBClient{}), settings: &Settings{}}
	for _, alpha := range []float64{-0.1, 1.1, 2.0, -1.0} {
		ctx := &fakeActivityContext{inputs: map[string]interface{}{
			"collectionName": "col",
			"queryText":      "test",
			"alpha":          alpha,
		}}
		_, err := a.Eval(ctx)
		assert.Error(t, err, "alpha %.2f should be rejected", alpha)
		assert.Contains(t, err.Error(), "out of range [0, 1]")
		// Must be a typed VDBError with the correct code
		var vdbErr *vectordb.VDBError
		assert.ErrorAs(t, err, &vdbErr, "alpha error must be a VDBError")
		assert.Equal(t, vectordb.ErrCodeInvalidAlpha, vdbErr.Code)
	}
}

func TestHybridSearch_AlphaBoundaryValid(t *testing.T) {
	mc := &mockclient.VectorDBClient{}
	mc.On("HybridSearch", mock.Anything, mock.Anything).
		Return([]vectordb.SearchResult{}, nil)

	a := &Activity{conn: newTestConn(mc), settings: &Settings{}}
	for _, alpha := range []float64{0.0, 1.0} {
		ctx := &fakeActivityContext{inputs: map[string]interface{}{
			"collectionName": "col",
			"queryText":      "test",
			"topK":           3,
			"alpha":          alpha,
		}}
		ok, err := a.Eval(ctx)
		assert.True(t, ok, "alpha %.1f should be accepted", alpha)
		assert.NoError(t, err)
	}
}
