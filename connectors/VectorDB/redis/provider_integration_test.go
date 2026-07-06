//go:build integration

package vectordb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedis_Integration runs against a real Redis Stack instance.
// Start with: docker compose -f docker-compose.redis.yml up -d
// Run with: go test -count=1 -tags=integration ./...
func TestRedis_Integration(t *testing.T) {
	cfg := ConnectionConfig{
		DBType:         "redis",
		Host:           "localhost",
		Port:           16379,
		TimeoutSeconds: 30,
	}
	if err := validateConnectionConfig(&cfg); err != nil {
		t.Fatalf("validateConnectionConfig: %v", err)
	}

	ctx := context.Background()
	client, err := newRedisClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	// Health check
	require.NoError(t, client.HealthCheck(ctx))

	const indexName = "integration-test-idx"
	const dims = 4

	// Cleanup from previous run
	_ = client.DeleteCollection(ctx, indexName)

	// CreateCollection
	t.Run("CreateCollection", func(t *testing.T) {
		err := client.CreateCollection(ctx, CollectionConfig{
			Name:           indexName,
			Dimensions:     dims,
			DistanceMetric: "cosine",
		})
		require.NoError(t, err)
	})

	// CollectionExists
	t.Run("CollectionExists", func(t *testing.T) {
		exists, err := client.CollectionExists(ctx, indexName)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	// ListCollections
	t.Run("ListCollections", func(t *testing.T) {
		names, err := client.ListCollections(ctx)
		require.NoError(t, err)
		found := false
		for _, n := range names {
			if n == indexName {
				found = true
				break
			}
		}
		assert.True(t, found, "index should appear in ListCollections")
	})

	// UpsertDocuments
	t.Run("UpsertDocuments", func(t *testing.T) {
		docs := []Document{
			{
				ID:      "int-doc1",
				Content: "the quick brown fox",
				Vector:  []float64{0.1, 0.2, 0.3, 0.4},
				Payload: map[string]interface{}{"category": "animal", "score": 9.5},
			},
			{
				ID:      "int-doc2",
				Content: "lazy dog sleeping",
				Vector:  []float64{0.5, 0.6, 0.7, 0.8},
				Payload: map[string]interface{}{"category": "animal", "score": 7.0},
			},
		}
		err := client.UpsertDocuments(ctx, indexName, docs)
		require.NoError(t, err)
	})

	// GetDocument
	t.Run("GetDocument", func(t *testing.T) {
		doc, err := client.GetDocument(ctx, indexName, "int-doc1")
		require.NoError(t, err)
		require.NotNil(t, doc)
		assert.Equal(t, "int-doc1", doc.ID)
		assert.Equal(t, "the quick brown fox", doc.Content)
	})

	// GetDocument not found
	t.Run("GetDocument_NotFound", func(t *testing.T) {
		doc, err := client.GetDocument(ctx, indexName, "no-such-doc")
		assert.Nil(t, doc)
		require.Error(t, err)
	})

	// CountDocuments
	t.Run("CountDocuments", func(t *testing.T) {
		count, err := client.CountDocuments(ctx, indexName, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(2), count)
	})

	// VectorSearch
	t.Run("VectorSearch", func(t *testing.T) {
		results, err := client.VectorSearch(ctx, SearchRequest{
			CollectionName: indexName,
			QueryVector:    []float64{0.1, 0.2, 0.3, 0.4},
			TopK:           2,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 1, "should return at least one result")
	})

	// ScrollDocuments
	t.Run("ScrollDocuments", func(t *testing.T) {
		result, err := client.ScrollDocuments(ctx, ScrollRequest{
			CollectionName: indexName,
			Limit:          10,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(result.Documents), 1)
	})

	// DeleteDocuments
	t.Run("DeleteDocuments", func(t *testing.T) {
		err := client.DeleteDocuments(ctx, indexName, []string{"int-doc1"})
		require.NoError(t, err)

		// Verify deleted
		doc, err := client.GetDocument(ctx, indexName, "int-doc1")
		assert.Nil(t, doc)
		require.Error(t, err)
	})

	// DeleteCollection
	t.Run("DeleteCollection", func(t *testing.T) {
		err := client.DeleteCollection(ctx, indexName)
		require.NoError(t, err)

		exists, err := client.CollectionExists(ctx, indexName)
		require.NoError(t, err)
		assert.False(t, exists)
	})
}
