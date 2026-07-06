//go:build integration

package vectordb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPgvector_Integration runs the full provider suite against a live pgvector instance.
// Start the test database with: docker compose -f docker-compose.pgvector.yml up -d
// Run with: go test -v -count=1 -tags=integration ./...
func TestPgvector_Integration(t *testing.T) {
	cfg := ConnectionConfig{
		DBType:         "pgvector",
		Host:           "localhost",
		Port:           5433,
		Username:       "testuser",
		Password:       "testpassword",
		DBName:         "vectordb_test",
		SSLMode:        "disable",
		TimeoutSeconds: 30,
		MaxRetries:     3,
		RetryBackoffMs: 500,
	}
	runProviderSuite(t, cfg)
}

// runProviderSuite executes the full VectorDBClient integration test suite.
func runProviderSuite(t *testing.T, cfg ConnectionConfig) {
	t.Helper()
	ctx := context.Background()

	client, err := NewClient(ctx, cfg)
	require.NoError(t, err, "NewClient should succeed")
	defer client.Close()

	// Health check
	t.Run("HealthCheck", func(t *testing.T) {
		err := client.HealthCheck(ctx)
		require.NoError(t, err)
	})

	// DBType
	t.Run("DBType", func(t *testing.T) {
		assert.Equal(t, "pgvector", client.DBType())
	})

	const testCollection = "test_suite_pgvector"
	const dims = 4

	// Cleanup before and after.
	_ = client.DeleteCollection(ctx, testCollection)
	t.Cleanup(func() { _ = client.DeleteCollection(ctx, testCollection) })

	// CreateCollection
	t.Run("CreateCollection", func(t *testing.T) {
		err := client.CreateCollection(ctx, CollectionConfig{
			Name:           testCollection,
			Dimensions:     dims,
			DistanceMetric: "cosine",
		})
		require.NoError(t, err)

		// Idempotent second call should not error.
		err = client.CreateCollection(ctx, CollectionConfig{
			Name:           testCollection,
			Dimensions:     dims,
			DistanceMetric: "cosine",
		})
		require.NoError(t, err)
	})

	// CollectionExists
	t.Run("CollectionExists", func(t *testing.T) {
		exists, err := client.CollectionExists(ctx, testCollection)
		require.NoError(t, err)
		assert.True(t, exists)

		exists, err = client.CollectionExists(ctx, "nonexistent_table_xyz")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// ListCollections
	t.Run("ListCollections", func(t *testing.T) {
		cols, err := client.ListCollections(ctx)
		require.NoError(t, err)
		assert.Contains(t, cols, testCollection)
	})

	// UpsertDocuments
	docs := []Document{
		{ID: "doc1", Vector: []float64{1.0, 0.0, 0.0, 0.0}, Content: "first document about cats", Payload: map[string]interface{}{"category": "animals", "year": 2021}},
		{ID: "doc2", Vector: []float64{0.0, 1.0, 0.0, 0.0}, Content: "second document about dogs", Payload: map[string]interface{}{"category": "animals", "year": 2022}},
		{ID: "doc3", Vector: []float64{0.0, 0.0, 1.0, 0.0}, Content: "third document about science", Payload: map[string]interface{}{"category": "science", "year": 2023}},
		{ID: "doc4", Vector: []float64{0.0, 0.0, 0.0, 1.0}, Content: "fourth document about technology", Payload: map[string]interface{}{"category": "tech", "year": 2024}},
	}

	t.Run("UpsertDocuments", func(t *testing.T) {
		err := client.UpsertDocuments(ctx, testCollection, docs)
		require.NoError(t, err)

		// Upsert again to test ON CONFLICT DO UPDATE.
		err = client.UpsertDocuments(ctx, testCollection, []Document{
			{ID: "doc1", Vector: []float64{1.0, 0.0, 0.0, 0.0}, Content: "first document updated", Payload: map[string]interface{}{"category": "animals", "year": 2021}},
		})
		require.NoError(t, err)
	})

	// Give a brief pause for indexes to be ready.
	time.Sleep(100 * time.Millisecond)

	// GetDocument
	t.Run("GetDocument", func(t *testing.T) {
		doc, err := client.GetDocument(ctx, testCollection, "doc2")
		require.NoError(t, err)
		require.NotNil(t, doc)
		assert.Equal(t, "doc2", doc.ID)
		assert.Equal(t, "second document about dogs", doc.Content)
	})

	t.Run("GetDocument_NotFound", func(t *testing.T) {
		_, err := client.GetDocument(ctx, testCollection, "nonexistent")
		require.Error(t, err)
		var vdbErr *VDBError
		require.ErrorAs(t, err, &vdbErr)
		assert.Equal(t, ErrCodeDocumentNotFound, vdbErr.Code)
	})

	// CountDocuments
	t.Run("CountDocuments", func(t *testing.T) {
		count, err := client.CountDocuments(ctx, testCollection, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(4), count)
	})

	t.Run("CountDocuments_WithFilter", func(t *testing.T) {
		count, err := client.CountDocuments(ctx, testCollection, map[string]interface{}{
			"category": map[string]interface{}{"$eq": "animals"},
		})
		require.NoError(t, err)
		assert.Equal(t, int64(2), count)
	})

	// VectorSearch
	t.Run("VectorSearch", func(t *testing.T) {
		results, err := client.VectorSearch(ctx, SearchRequest{
			CollectionName: testCollection,
			QueryVector:    []float64{1.0, 0.0, 0.0, 0.0},
			TopK:           3,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
		// doc1 should be the top result (identical vector).
		assert.Equal(t, "doc1", results[0].ID)
	})

	t.Run("VectorSearch_WithFilter", func(t *testing.T) {
		results, err := client.VectorSearch(ctx, SearchRequest{
			CollectionName: testCollection,
			QueryVector:    []float64{1.0, 0.0, 0.0, 0.0},
			TopK:           10,
			Filters: map[string]interface{}{
				"category": map[string]interface{}{"$eq": "science"},
			},
		})
		require.NoError(t, err)
		for _, r := range results {
			assert.Equal(t, "doc3", r.ID)
		}
	})

	// HybridSearch
	t.Run("HybridSearch", func(t *testing.T) {
		results, err := client.HybridSearch(ctx, HybridSearchRequest{
			CollectionName: testCollection,
			QueryText:      "cats animals",
			QueryVector:    []float64{1.0, 0.0, 0.0, 0.0},
			TopK:           4,
			Alpha:          0.5,
		})
		require.NoError(t, err)
		// Results should not error; top results depend on scoring but should include doc1/doc2.
		assert.NotNil(t, results)
	})

	// ScrollDocuments
	t.Run("ScrollDocuments", func(t *testing.T) {
		result, err := client.ScrollDocuments(ctx, ScrollRequest{
			CollectionName: testCollection,
			Limit:          2,
		})
		require.NoError(t, err)
		assert.Len(t, result.Documents, 2)
		assert.NotEmpty(t, result.NextOffset)

		// Second page.
		result2, err := client.ScrollDocuments(ctx, ScrollRequest{
			CollectionName: testCollection,
			Limit:          2,
			Offset:         result.NextOffset,
		})
		require.NoError(t, err)
		assert.Len(t, result2.Documents, 2)
		// Third page should be empty.
		result3, err := client.ScrollDocuments(ctx, ScrollRequest{
			CollectionName: testCollection,
			Limit:          2,
			Offset:         result2.NextOffset,
		})
		require.NoError(t, err)
		assert.Empty(t, result3.Documents)
		assert.Empty(t, result3.NextOffset)
	})

	// DeleteDocuments
	t.Run("DeleteDocuments", func(t *testing.T) {
		err := client.DeleteDocuments(ctx, testCollection, []string{"doc4"})
		require.NoError(t, err)

		count, err := client.CountDocuments(ctx, testCollection, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(3), count)
	})

	// DeleteByFilter
	t.Run("DeleteByFilter", func(t *testing.T) {
		deleted, err := client.DeleteByFilter(ctx, testCollection, map[string]interface{}{
			"category": map[string]interface{}{"$eq": "science"},
		})
		require.NoError(t, err)
		assert.Equal(t, int64(1), deleted)

		count, err := client.CountDocuments(ctx, testCollection, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(2), count)
	})

	// DeleteCollection
	t.Run("DeleteCollection", func(t *testing.T) {
		err := client.DeleteCollection(ctx, testCollection)
		require.NoError(t, err)

		exists, err := client.CollectionExists(ctx, testCollection)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// Multiple collection lifecycle test
	t.Run("MultipleCollections", func(t *testing.T) {
		names := []string{"col_a_pgv", "col_b_pgv"}
		for _, name := range names {
			err := client.CreateCollection(ctx, CollectionConfig{
				Name:           name,
				Dimensions:     2,
				DistanceMetric: "euclidean",
			})
			require.NoError(t, err, fmt.Sprintf("create %s", name))
		}
		t.Cleanup(func() {
			for _, name := range names {
				_ = client.DeleteCollection(ctx, name)
			}
		})

		cols, err := client.ListCollections(ctx)
		require.NoError(t, err)
		for _, name := range names {
			assert.Contains(t, cols, name)
		}
	})
}
