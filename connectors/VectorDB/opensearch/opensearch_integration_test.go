//go:build integration

package vectordb_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	integrationHost       = "localhost"
	integrationPort       = 19201
	integrationCollection = "test-opensearch-integration"
	integrationDimensions = 8
)

func newIntegrationClient(t *testing.T) vectordb.VectorDBClient {
	t.Helper()
	cfg := vectordb.ConnectionConfig{
		Host:           integrationHost,
		Port:           integrationPort,
		TimeoutSeconds: 30,
		MaxRetries:     3,
		RetryBackoffMs: 500,
	}
	client, err := vectordb.NewClient(context.Background(), cfg)
	require.NoError(t, err)

	// Skip if not reachable
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.HealthCheck(ctx); err != nil {
		t.Skipf("OpenSearch not reachable at %s:%d — skipping integration tests: %v", integrationHost, integrationPort, err)
	}
	return client
}

func sampleDocs() []vectordb.Document {
	return []vectordb.Document{
		{
			ID:      "doc-001",
			Content: "The quick brown fox jumps over the lazy dog",
			Vector:  []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
			Payload: map[string]interface{}{"category": "animals", "year": 2023},
		},
		{
			ID:      "doc-002",
			Content: "Machine learning models require large datasets for training",
			Vector:  []float64{0.8, 0.7, 0.6, 0.5, 0.4, 0.3, 0.2, 0.1},
			Payload: map[string]interface{}{"category": "technology", "year": 2024},
		},
		{
			ID:      "doc-003",
			Content: "Natural language processing transforms text into vectors",
			Vector:  []float64{0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5},
			Payload: map[string]interface{}{"category": "technology", "year": 2024},
		},
	}
}

func TestOpenSearch_Integration(t *testing.T) {
	client := newIntegrationClient(t)
	ctx := context.Background()

	// Clean up before test
	_ = client.DeleteCollection(ctx, integrationCollection)

	t.Run("CreateCollection", func(t *testing.T) {
		err := client.CreateCollection(ctx, vectordb.CollectionConfig{
			Name:           integrationCollection,
			Dimensions:     integrationDimensions,
			DistanceMetric: "cosine",
		})
		require.NoError(t, err)
	})

	t.Run("CollectionExists", func(t *testing.T) {
		exists, err := client.CollectionExists(ctx, integrationCollection)
		require.NoError(t, err)
		assert.True(t, exists, "collection should exist after creation")

		notExists, err := client.CollectionExists(ctx, "non-existent-collection-xyz")
		require.NoError(t, err)
		assert.False(t, notExists, "non-existent collection should return false")
	})

	t.Run("ListCollections", func(t *testing.T) {
		names, err := client.ListCollections(ctx)
		require.NoError(t, err)
		assert.Contains(t, names, integrationCollection)
	})

	t.Run("UpsertDocuments", func(t *testing.T) {
		docs := sampleDocs()
		err := client.UpsertDocuments(ctx, integrationCollection, docs)
		require.NoError(t, err)

		// Wait for indexing
		time.Sleep(1 * time.Second)
	})

	t.Run("GetDocument", func(t *testing.T) {
		doc, err := client.GetDocument(ctx, integrationCollection, "doc-001")
		require.NoError(t, err)
		require.NotNil(t, doc)
		assert.Equal(t, "doc-001", doc.ID)
		assert.Equal(t, "The quick brown fox jumps over the lazy dog", doc.Content)
	})

	t.Run("GetDocument_NotFound", func(t *testing.T) {
		doc, err := client.GetDocument(ctx, integrationCollection, "non-existent-id")
		require.Error(t, err) // VectorDBClient returns ErrCodeDocumentNotFound for missing docs
		assert.Nil(t, doc)
	})

	t.Run("VectorSearch", func(t *testing.T) {
		results, err := client.VectorSearch(ctx, vectordb.SearchRequest{
			CollectionName: integrationCollection,
			QueryVector:    []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
			TopK:           3,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
		t.Logf("VectorSearch returned %d results", len(results))
		for _, r := range results {
			t.Logf("  id=%s score=%.4f", r.ID, r.Score)
		}
	})

	t.Run("HybridSearch", func(t *testing.T) {
		results, err := client.HybridSearch(ctx, vectordb.HybridSearchRequest{
			CollectionName: integrationCollection,
			QueryText:      "machine learning technology",
			QueryVector:    []float64{0.8, 0.7, 0.6, 0.5, 0.4, 0.3, 0.2, 0.1},
			TopK:           3,
			Alpha:          0.5,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
		t.Logf("HybridSearch returned %d results", len(results))
	})

	t.Run("CountDocuments", func(t *testing.T) {
		count, err := client.CountDocuments(ctx, integrationCollection, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(3), count)

		countFiltered, err := client.CountDocuments(ctx, integrationCollection, map[string]interface{}{
			"category": "technology",
		})
		require.NoError(t, err)
		assert.Equal(t, int64(2), countFiltered)
	})

	t.Run("ScrollDocuments", func(t *testing.T) {
		result, err := client.ScrollDocuments(ctx, vectordb.ScrollRequest{
			CollectionName: integrationCollection,
			Limit:          2,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Len(t, result.Documents, 2)
		assert.NotEmpty(t, result.NextOffset, "should have next page")

		// Get second page
		result2, err := client.ScrollDocuments(ctx, vectordb.ScrollRequest{
			CollectionName: integrationCollection,
			Limit:          2,
			Offset:         result.NextOffset,
		})
		require.NoError(t, err)
		assert.Len(t, result2.Documents, 1)
		assert.Empty(t, result2.NextOffset, "last page should have empty next offset")
	})

	t.Run("DeleteDocuments", func(t *testing.T) {
		err := client.DeleteDocuments(ctx, integrationCollection, []string{"doc-001"})
		require.NoError(t, err)

		// OpenSearch default refresh interval is 1s; wait 2s to be safe
		time.Sleep(2 * time.Second)

		count, err := client.CountDocuments(ctx, integrationCollection, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(2), count)
	})

	t.Run("DeleteByFilter", func(t *testing.T) {
		deleted, err := client.DeleteByFilter(ctx, integrationCollection, map[string]interface{}{
			"category": "technology",
		})
		require.NoError(t, err)
		t.Logf("DeleteByFilter deleted %d documents", deleted)

		// delete_by_query is asynchronous by default; wait for refresh
		time.Sleep(2 * time.Second)

		count, err := client.CountDocuments(ctx, integrationCollection, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(0), count)
	})

	t.Run("DeleteCollection", func(t *testing.T) {
		err := client.DeleteCollection(ctx, integrationCollection)
		require.NoError(t, err)

		exists, err := client.CollectionExists(ctx, integrationCollection)
		require.NoError(t, err)
		assert.False(t, exists)
	})
}

func TestOpenSearch_DBType(t *testing.T) {
	client := newIntegrationClient(t)
	assert.Equal(t, "opensearch", client.DBType())
}

func TestOpenSearch_FilterBuilder(t *testing.T) {
	// Test filter building through CountDocuments with various filter types
	client := newIntegrationClient(t)
	ctx := context.Background()

	collection := fmt.Sprintf("test-filter-%d", time.Now().UnixNano())
	err := client.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           collection,
		Dimensions:     integrationDimensions,
		DistanceMetric: "cosine",
	})
	require.NoError(t, err)
	defer func() { _ = client.DeleteCollection(ctx, collection) }()

	docs := []vectordb.Document{
		{ID: "f1", Vector: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
			Payload: map[string]interface{}{"score": 10, "active": true}},
		{ID: "f2", Vector: []float64{0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9},
			Payload: map[string]interface{}{"score": 20, "active": false}},
		{ID: "f3", Vector: []float64{0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
			Payload: map[string]interface{}{"score": 30, "active": true}},
	}
	err = client.UpsertDocuments(ctx, collection, docs)
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	// Test $gte operator
	count, err := client.CountDocuments(ctx, collection, map[string]interface{}{
		"score": map[string]interface{}{"$gte": 20},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "scores >= 20")
}
