//go:build !integration

package vectordb

import (
	"context"
	"encoding/binary"
	"math"
	"net"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient starts a miniredis server and returns a redisClient connected to it.
func newTestClient(t *testing.T) (*redisClient, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err, "failed to start miniredis")
	t.Cleanup(mr.Close)

	// Parse port from miniredis address using net.SplitHostPort
	_, portStr, splitErr := net.SplitHostPort(mr.Addr())
	require.NoError(t, splitErr, "failed to parse miniredis address")
	port, convErr := strconv.Atoi(portStr)
	require.NoError(t, convErr, "failed to parse port")

	cfg := ConnectionConfig{
		Host: "127.0.0.1",
		Port: port,
	}
	if err := validateConnectionConfig(&cfg); err != nil {
		t.Fatalf("validateConnectionConfig: %v", err)
	}

	c, err := newRedisClient(cfg)
	require.NoError(t, err)

	rc := c.(*redisClient)
	return rc, mr
}

func TestEncodeDecodeVector(t *testing.T) {
	original := []float64{0.1, 0.2, 0.3, -0.5, 1.0, 0.0}
	encoded := encodeVector(original)
	assert.Equal(t, len(original)*4, len(encoded), "encoded length should be 4 bytes per float")

	decoded := decodeVector(encoded)
	require.Equal(t, len(original), len(decoded))

	for i := range original {
		// float64 -> float32 -> float64 has limited precision
		assert.InDelta(t, original[i], decoded[i], 1e-6, "decoded[%d] mismatch", i)
	}
}

func TestEncodeVectorRoundtrip(t *testing.T) {
	// Test with known values
	v := []float64{1.0, 0.0, -1.0}
	b := encodeVector(v)

	// Verify byte layout (little-endian float32)
	f0 := math.Float32frombits(binary.LittleEndian.Uint32(b[0:]))
	assert.InDelta(t, 1.0, float64(f0), 1e-6)

	decoded := decodeVector(b)
	assert.InDelta(t, -1.0, decoded[2], 1e-6)
}

func TestDBType(t *testing.T) {
	rc, _ := newTestClient(t)
	assert.Equal(t, "redis", rc.DBType())
}

func TestValidateConnectionConfigDefaults(t *testing.T) {
	cfg := &ConnectionConfig{Host: "myhost"}
	err := validateConnectionConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, "redis", cfg.DBType)
	assert.Equal(t, 6379, cfg.Port)
	assert.Equal(t, 0, cfg.RedisDB)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 500, cfg.RetryBackoffMs)
	assert.Equal(t, 30, cfg.TimeoutSeconds)
}

func TestValidateConnectionConfigHostDefault(t *testing.T) {
	cfg := &ConnectionConfig{}
	err := validateConnectionConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, "localhost", cfg.Host)
}

func TestValidateConnectionConfigInvalidRedisDB(t *testing.T) {
	cfg := &ConnectionConfig{Host: "localhost", RedisDB: 16}
	err := validateConnectionConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redisDB")
}

func TestValidateConnectionConfigNegativeRedisDB(t *testing.T) {
	cfg := &ConnectionConfig{Host: "localhost", RedisDB: -1}
	err := validateConnectionConfig(cfg)
	require.Error(t, err)
}

func TestHealthCheck(t *testing.T) {
	rc, _ := newTestClient(t)
	err := rc.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestUpsertAndGetDocument(t *testing.T) {
	rc, mr := newTestClient(t)
	ctx := context.Background()

	// miniredis supports HSET/HGETALL (no FT commands needed for upsert/get)
	indexName := "testidx"
	docs := []Document{
		{
			ID:      "doc1",
			Content: "hello world",
			Vector:  []float64{0.1, 0.2, 0.3},
			Payload: map[string]interface{}{"category": "test"},
		},
	}

	err := rc.UpsertDocuments(ctx, indexName, docs)
	require.NoError(t, err, "UpsertDocuments should succeed with miniredis")

	// Verify the key exists in miniredis
	key := docKey(indexName, "doc1")
	assert.True(t, mr.Exists(key), "hash key should exist in miniredis")

	// GetDocument reads back the hash
	got, err := rc.GetDocument(ctx, indexName, "doc1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "doc1", got.ID)
	assert.Equal(t, "hello world", got.Content)
	require.NotNil(t, got.Payload)
	assert.Equal(t, "test", got.Payload["category"])
}

func TestGetDocument_NotFound(t *testing.T) {
	rc, _ := newTestClient(t)
	ctx := context.Background()

	doc, err := rc.GetDocument(ctx, "nonexistent", "no-such-id")
	assert.Nil(t, doc)
	require.Error(t, err)
	vdbErr, ok := err.(*VDBError)
	require.True(t, ok, "error should be VDBError")
	assert.Equal(t, ErrCodeDocumentNotFound, vdbErr.Code)
}

func TestDeleteDocuments(t *testing.T) {
	rc, mr := newTestClient(t)
	ctx := context.Background()

	indexName := "delidx"
	docs := []Document{
		{ID: "d1", Content: "one", Vector: []float64{0.1, 0.2}},
		{ID: "d2", Content: "two", Vector: []float64{0.3, 0.4}},
	}
	require.NoError(t, rc.UpsertDocuments(ctx, indexName, docs))

	key1 := docKey(indexName, "d1")
	key2 := docKey(indexName, "d2")
	assert.True(t, mr.Exists(key1))
	assert.True(t, mr.Exists(key2))

	err := rc.DeleteDocuments(ctx, indexName, []string{"d1"})
	require.NoError(t, err)

	assert.False(t, mr.Exists(key1), "d1 should be deleted")
	assert.True(t, mr.Exists(key2), "d2 should still exist")
}

func TestDeleteDocuments_Empty(t *testing.T) {
	rc, _ := newTestClient(t)
	ctx := context.Background()
	// Deleting empty list should be a no-op
	err := rc.DeleteDocuments(ctx, "someidx", []string{})
	assert.NoError(t, err)
}

func TestCollectionExistsListCollections_MiniredisSkip(t *testing.T) {
	t.Skip("miniredis: FT commands (FT.INFO, FT._LIST) not supported")
}

func TestVectorSearch_MiniredisSkip(t *testing.T) {
	t.Skip("miniredis: FT.SEARCH with KNN vector syntax not supported")
}

func TestHybridSearch_MiniredisSkip(t *testing.T) {
	t.Skip("miniredis: FT.SEARCH hybrid not supported")
}

func TestCreateCollection_MiniredisSkip(t *testing.T) {
	t.Skip("miniredis: FT.CREATE not supported")
}

func TestBuildRedisFilter_Empty(t *testing.T) {
	q := buildRedisFilter(nil)
	assert.Equal(t, "*", q)

	q2 := buildRedisFilter(map[string]interface{}{})
	assert.Equal(t, "*", q2)
}

func TestBuildRedisFilter_Single(t *testing.T) {
	q := buildRedisFilter(map[string]interface{}{"category": "science"})
	assert.Contains(t, q, `"category":"science"`)
	assert.Contains(t, q, "@metadata:")
}

func TestBuildRedisFilter_Multiple(t *testing.T) {
	q := buildRedisFilter(map[string]interface{}{
		"category": "science",
		"year":     2024.0,
	})
	assert.Contains(t, q, "@metadata:")
	assert.Contains(t, q, "category")
	assert.Contains(t, q, "year")
}

func TestUpsertDocuments_Validation(t *testing.T) {
	rc, _ := newTestClient(t)
	ctx := context.Background()

	// Empty docs
	err := rc.UpsertDocuments(ctx, "idx", []Document{})
	require.Error(t, err)

	// Empty ID
	err = rc.UpsertDocuments(ctx, "idx", []Document{{Vector: []float64{0.1}}})
	require.Error(t, err)

	// Empty vector
	err = rc.UpsertDocuments(ctx, "idx", []Document{{ID: "d1", Vector: nil}})
	require.Error(t, err)
}

func TestValidateCollectionConfig(t *testing.T) {
	// Valid
	err := validateCollectionConfig(CollectionConfig{Name: "idx", Dimensions: 128, DistanceMetric: "cosine"})
	assert.NoError(t, err)

	// Empty name
	err = validateCollectionConfig(CollectionConfig{Dimensions: 128})
	require.Error(t, err)

	// Zero dimensions
	err = validateCollectionConfig(CollectionConfig{Name: "idx", Dimensions: 0})
	require.Error(t, err)

	// Invalid metric
	err = validateCollectionConfig(CollectionConfig{Name: "idx", Dimensions: 128, DistanceMetric: "invalid"})
	require.Error(t, err)
}

func TestRedisMetric(t *testing.T) {
	assert.Equal(t, "COSINE", redisMetric("cosine"))
	assert.Equal(t, "IP", redisMetric("dot"))
	assert.Equal(t, "L2", redisMetric("euclidean"))
	assert.Equal(t, "COSINE", redisMetric("unknown"))
}
