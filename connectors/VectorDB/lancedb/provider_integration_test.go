//go:build integration

package vectordb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLanceDB_Integration(t *testing.T) {
	host := "localhost"
	if h := os.Getenv("LANCEDB_HOST"); h != "" {
		host = h
	}
	port := 18181
	cfg := ConnectionConfig{
		Host:           host,
		Port:           port,
		Scheme:         "http",
		TimeoutSeconds: 30,
		MaxRetries:     3,
		RetryBackoffMs: 500,
	}
	client, err := NewClient(context.Background(), cfg)
	if err != nil {
		t.Skipf("LanceDB not reachable at %s:%d: %v", host, port, err)
	}

	// Verify health
	if hErr := client.HealthCheck(context.Background()); hErr != nil {
		t.Skipf("LanceDB health check failed at %s:%d: %v", host, port, hErr)
	}
	defer client.Close()

	const testCollection = "lancedb_integration_test"
	ctx := context.Background()

	// Cleanup before test
	_ = client.DeleteCollection(ctx, testCollection)

	t.Run("CreateCollection", func(t *testing.T) {
		err := client.CreateCollection(ctx, CollectionConfig{
			Name:           testCollection,
			Dimensions:     8,
			DistanceMetric: "cosine",
		})
		require.NoError(t, err)
	})

	t.Run("CollectionExists", func(t *testing.T) {
		exists, err := client.CollectionExists(ctx, testCollection)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("CollectionExists_Missing", func(t *testing.T) {
		exists, err := client.CollectionExists(ctx, "does_not_exist_xyz")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("ListCollections", func(t *testing.T) {
		names, err := client.ListCollections(ctx)
		require.NoError(t, err)
		assert.Contains(t, names, testCollection)
	})

	docs := []Document{
		{ID: "d1", Content: "the quick brown fox", Vector: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8}, Payload: map[string]interface{}{"category": "animals"}},
		{ID: "d2", Content: "machine learning introduction", Vector: []float64{0.9, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7}, Payload: map[string]interface{}{"category": "tech"}},
		{ID: "d3", Content: "vector databases overview", Vector: []float64{0.5, 0.5, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6}, Payload: map[string]interface{}{"category": "tech"}},
	}

	t.Run("UpsertDocuments", func(t *testing.T) {
		err := client.UpsertDocuments(ctx, testCollection, docs)
		require.NoError(t, err)
		time.Sleep(500 * time.Millisecond) // allow LanceDB to index
	})

	t.Run("GetDocument", func(t *testing.T) {
		doc, err := client.GetDocument(ctx, testCollection, "d1")
		require.NoError(t, err)
		require.NotNil(t, doc)
		assert.Equal(t, "d1", doc.ID)
	})

	t.Run("GetDocument_NotFound", func(t *testing.T) {
		_, err := client.GetDocument(ctx, testCollection, "missing_xyz")
		require.Error(t, err)
		vdbErr, ok := err.(*VDBError)
		require.True(t, ok)
		assert.Equal(t, ErrCodeDocumentNotFound, vdbErr.Code)
	})

	t.Run("VectorSearch", func(t *testing.T) {
		results, err := client.VectorSearch(ctx, SearchRequest{
			CollectionName: testCollection,
			QueryVector:    []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
			TopK:           3,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
	})

	t.Run("HybridSearch", func(t *testing.T) {
		results, err := client.HybridSearch(ctx, HybridSearchRequest{
			CollectionName: testCollection,
			QueryText:      "machine learning",
			QueryVector:    []float64{0.9, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7},
			TopK:           3,
			Alpha:          0.5,
		})
		require.NoError(t, err)
		// hybrid may fall back to vector-only if FTS not indexed; just check no error
		_ = results
	})

	t.Run("CountDocuments", func(t *testing.T) {
		count, err := client.CountDocuments(ctx, testCollection, nil)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(3))
	})

	t.Run("ScrollDocuments", func(t *testing.T) {
		result, err := client.ScrollDocuments(ctx, ScrollRequest{
			CollectionName: testCollection,
			Limit:          10,
		})
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotEmpty(t, result.Documents)
	})

	t.Run("DeleteDocuments", func(t *testing.T) {
		err := client.DeleteDocuments(ctx, testCollection, []string{"d3"})
		require.NoError(t, err)
	})

	t.Run("DeleteByFilter", func(t *testing.T) {
		count, err := client.DeleteByFilter(ctx, testCollection, map[string]interface{}{"category": "animals"})
		require.NoError(t, err)
		// LanceDB returns -1 for count
		assert.Equal(t, int64(-1), count)
	})

	t.Run("DeleteCollection", func(t *testing.T) {
		err := client.DeleteCollection(ctx, testCollection)
		require.NoError(t, err)
	})
}
