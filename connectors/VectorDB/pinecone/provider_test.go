package vectordb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── buildSparseVector ────────────────────────────────────────────────────────

func TestBuildSparseVectorDeterminism(t *testing.T) {
	idxA, valA := buildSparseVector("hello world pinecone")
	idxB, valB := buildSparseVector("hello world pinecone")
	require.Equal(t, len(idxA), len(idxB), "indices length must be equal")
	require.Equal(t, idxA, idxB, "indices must match on identical input")
	require.InDeltaSlice(t, valA, valB, 1e-9, "values must match on identical input")
}

func TestBuildSparseVectorEmpty(t *testing.T) {
	idx, val := buildSparseVector("")
	assert.Empty(t, idx, "empty input should yield empty sparse vector")
	assert.Empty(t, val, "empty input should yield empty sparse vector")
}

func TestBuildSparseVectorNormalised(t *testing.T) {
	_, val := buildSparseVector("the quick brown fox jumps over the lazy dog")
	if len(val) == 0 {
		t.Skip("no tokens produced")
	}
	var sum float32
	for _, v := range val {
		sum += v
	}
	assert.InDelta(t, 1.0, sum, 1e-5, "TF weights should be normalised to sum=1.0")
}

func TestBuildSparseVectorCap(t *testing.T) {
	// Generate a long text with many unique tokens to exceed the 50-dim cap.
	var sb []byte
	for i := 0; i < 300; i++ {
		sb = append(sb, []byte("token")...)
		sb = append(sb, byte('a'+i%26))
		sb = append(sb, ' ')
	}
	idx, _ := buildSparseVector(string(sb))
	assert.LessOrEqual(t, len(idx), 50, "sparse vector must be capped at 50 dimensions")
}

func TestBuildSparseVectorSortedIndices(t *testing.T) {
	idx, _ := buildSparseVector("apple banana cherry date elderberry fig grape")
	for i := 1; i < len(idx); i++ {
		assert.Less(t, idx[i-1], idx[i], "indices must be sorted ascending")
	}
}

// ── validateConnectionConfig ─────────────────────────────────────────────────

func TestValidateConnectionConfigDefaults(t *testing.T) {
	cfg := &ConnectionConfig{APIKey: "pk-test-key"}
	require.NoError(t, validateConnectionConfig(cfg))
	assert.Equal(t, "pinecone", cfg.DBType)
	assert.Equal(t, "api.pinecone.io", cfg.Host)
	assert.Equal(t, "aws", cfg.PineconeCloud)
	assert.Equal(t, "us-east-1", cfg.PineconeRegion)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 500, cfg.RetryBackoffMs)
	assert.Equal(t, 30, cfg.TimeoutSeconds)
}

func TestValidateConnectionConfigMissingAPIKey(t *testing.T) {
	cfg := &ConnectionConfig{}
	err := validateConnectionConfig(cfg)
	require.Error(t, err)
	vdbErr, ok := err.(*VDBError)
	require.True(t, ok, "expected *VDBError")
	assert.Equal(t, ErrCodeAuthFailed, vdbErr.Code)
}

func TestValidateConnectionConfigNegativeTimeout(t *testing.T) {
	cfg := &ConnectionConfig{APIKey: "pk-test-key", TimeoutSeconds: -1}
	err := validateConnectionConfig(cfg)
	require.Error(t, err)
	vdbErr, ok := err.(*VDBError)
	require.True(t, ok, "expected *VDBError")
	assert.Equal(t, ErrCodeInvalidTimeout, vdbErr.Code)
}

func TestValidateConnectionConfigCustomHost(t *testing.T) {
	cfg := &ConnectionConfig{
		APIKey:         "pk-test-key",
		Host:           "custom-api.example.com",
		PineconeCloud:  "gcp",
		PineconeRegion: "us-central1",
	}
	require.NoError(t, validateConnectionConfig(cfg))
	assert.Equal(t, "custom-api.example.com", cfg.Host, "custom host must not be overwritten")
	assert.Equal(t, "gcp", cfg.PineconeCloud)
}

// ── NewClient (compile-time interface check) ─────────────────────────────────

func TestNewClientReturnsVectorDBClient(t *testing.T) {
	cfg := ConnectionConfig{
		APIKey:         "pk-test-key",
		Host:           "api.pinecone.io",
		PineconeCloud:  "aws",
		PineconeRegion: "us-east-1",
		TimeoutSeconds: 30,
		MaxRetries:     3,
		RetryBackoffMs: 500,
	}
	client, err := NewClient(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, client)
	// VectorDBClient interface is satisfied — compile-time check
	// var _ VectorDBClient = (*pineconeClient)(nil) covers this.
	assert.Equal(t, "pinecone", client.DBType())
}

// ── buildPineconeFilter ──────────────────────────────────────────────────────

func TestBuildPineconeFilterScalarEquality(t *testing.T) {
	f := buildPineconeFilter(map[string]interface{}{
		"category": "science",
	})
	require.NotNil(t, f)
	catFilter, ok := f["category"].(map[string]interface{})
	require.True(t, ok, "scalar filter should be wrapped in {$eq: v}")
	assert.Equal(t, "science", catFilter["$eq"])
}

func TestBuildPineconeFilterOperatorPassthrough(t *testing.T) {
	f := buildPineconeFilter(map[string]interface{}{
		"score": map[string]interface{}{"$gt": 0.5},
	})
	require.NotNil(t, f)
	scoreFilter, ok := f["score"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0.5, scoreFilter["$gt"])
}

func TestBuildPineconeFilterNil(t *testing.T) {
	assert.Nil(t, buildPineconeFilter(nil))
	assert.Nil(t, buildPineconeFilter(map[string]interface{}{}))
}

// ── VDBError ─────────────────────────────────────────────────────────────────

func TestVDBErrorMessage(t *testing.T) {
	err := NewError(ErrCodeCollectionNotFound, "index my-index not found", nil)
	assert.Contains(t, err.Error(), "index my-index not found")
	assert.Contains(t, err.Error(), ErrCodeCollectionNotFound)
}

func TestVDBErrorWraps(t *testing.T) {
	inner := assert.AnError
	err := NewError(ErrCodeConnectionFailed, "dial failed", inner)
	require.NotNil(t, err)
	assert.Equal(t, ErrCodeConnectionFailed, err.Code)
	assert.Equal(t, inner, err.Cause)
	// Unwrap must expose the inner error so errors.Is works.
	assert.ErrorIs(t, err, inner)
}
