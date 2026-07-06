//go:build integration

package vectordb_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	intTestHost       = "localhost"
	intTestPort       = 9200 // docker-compose maps host:9200 → container:9200
	intTestCollection = "flogo-integration-test"
	intTestDimensions = 4
)

func intCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func newIntClient(t *testing.T) vectordb.VectorDBClient {
	t.Helper()
	ctx := context.Background()
	client, err := vectordb.NewClient(ctx, vectordb.ConnectionConfig{
		Host:           intTestHost,
		Port:           intTestPort,
		DBType:         "elasticsearch",
		TimeoutSeconds: 10,
		MaxRetries:     2,
		RetryBackoffMs: 200,
	})
	if err != nil {
		t.Skipf("Elasticsearch not reachable at %s:%d — start the Docker container first: %v", intTestHost, intTestPort, err)
	}
	return client
}

func TestIntegration_HealthCheck(t *testing.T) {
	client := newIntClient(t)
	err := client.HealthCheck(intCtx(t))
	require.NoError(t, err)
}

func TestIntegration_CreateAndDeleteCollection(t *testing.T) {
	client := newIntClient(t)
	ctx := intCtx(t)

	// Cleanup before
	_ = client.DeleteCollection(ctx, intTestCollection)

	err := client.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           intTestCollection,
		Dimensions:     intTestDimensions,
		DistanceMetric: "cosine",
	})
	require.NoError(t, err)

	exists, err := client.CollectionExists(ctx, intTestCollection)
	require.NoError(t, err)
	assert.True(t, exists)

	// Second create returns ErrCodeCollectionExists (ES does not support IF NOT EXISTS)
	err = client.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           intTestCollection,
		Dimensions:     intTestDimensions,
		DistanceMetric: "cosine",
	})
	require.Error(t, err, "second create on existing index must return ErrCodeCollectionExists")

	err = client.DeleteCollection(ctx, intTestCollection)
	require.NoError(t, err)

	exists, err = client.CollectionExists(ctx, intTestCollection)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestIntegration_UpsertAndGetDocument(t *testing.T) {
	client := newIntClient(t)
	ctx := intCtx(t)

	_ = client.DeleteCollection(ctx, intTestCollection)
	require.NoError(t, client.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           intTestCollection,
		Dimensions:     intTestDimensions,
		DistanceMetric: "cosine",
	}))
	defer client.DeleteCollection(context.Background(), intTestCollection)

	docs := []vectordb.Document{
		{ID: "doc-1", Content: "Hello world", Vector: []float64{0.1, 0.2, 0.3, 0.4}, Payload: map[string]interface{}{"tag": "test"}},
		{ID: "doc-2", Content: "Goodbye world", Vector: []float64{0.4, 0.3, 0.2, 0.1}, Payload: map[string]interface{}{"tag": "test"}},
	}
	require.NoError(t, client.UpsertDocuments(ctx, intTestCollection, docs))

	// Give ES time to index
	time.Sleep(1 * time.Second)

	got, err := client.GetDocument(ctx, intTestCollection, "doc-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "doc-1", got.ID)
	assert.Equal(t, "Hello world", got.Content)
}

func TestIntegration_VectorSearch(t *testing.T) {
	client := newIntClient(t)
	ctx := intCtx(t)

	_ = client.DeleteCollection(ctx, intTestCollection)
	require.NoError(t, client.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           intTestCollection,
		Dimensions:     intTestDimensions,
		DistanceMetric: "cosine",
	}))
	defer client.DeleteCollection(context.Background(), intTestCollection)

	docs := []vectordb.Document{
		{ID: "v1", Content: "apple", Vector: []float64{1.0, 0.0, 0.0, 0.0}},
		{ID: "v2", Content: "banana", Vector: []float64{0.0, 1.0, 0.0, 0.0}},
		{ID: "v3", Content: "cherry", Vector: []float64{0.0, 0.0, 1.0, 0.0}},
	}
	require.NoError(t, client.UpsertDocuments(ctx, intTestCollection, docs))
	time.Sleep(1 * time.Second)

	results, err := client.VectorSearch(ctx, vectordb.SearchRequest{
		CollectionName: intTestCollection,
		QueryVector:    []float64{1.0, 0.0, 0.0, 0.0},
		TopK:           2,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	assert.Equal(t, "v1", results[0].ID, "v1 should be closest to query")
}

func TestIntegration_HybridSearch(t *testing.T) {
	client := newIntClient(t)
	ctx := intCtx(t)

	_ = client.DeleteCollection(ctx, intTestCollection)
	require.NoError(t, client.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           intTestCollection,
		Dimensions:     intTestDimensions,
		DistanceMetric: "cosine",
	}))
	defer client.DeleteCollection(context.Background(), intTestCollection)

	docs := []vectordb.Document{
		{ID: "h1", Content: "machine learning embeddings", Vector: []float64{0.9, 0.1, 0.0, 0.0}},
		{ID: "h2", Content: "database indexing", Vector: []float64{0.0, 0.0, 0.9, 0.1}},
	}
	require.NoError(t, client.UpsertDocuments(ctx, intTestCollection, docs))
	time.Sleep(1 * time.Second)

	results, err := client.HybridSearch(ctx, vectordb.HybridSearchRequest{
		CollectionName: intTestCollection,
		QueryText:      "machine learning",
		QueryVector:    []float64{0.9, 0.1, 0.0, 0.0},
		TopK:           2,
		Alpha:          0.5,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestIntegration_DeleteDocuments(t *testing.T) {
	client := newIntClient(t)
	ctx := intCtx(t)

	_ = client.DeleteCollection(ctx, intTestCollection)
	require.NoError(t, client.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           intTestCollection,
		Dimensions:     intTestDimensions,
		DistanceMetric: "cosine",
	}))
	defer client.DeleteCollection(context.Background(), intTestCollection)

	docs := []vectordb.Document{
		{ID: "del-1", Content: "to delete", Vector: []float64{0.1, 0.2, 0.3, 0.4}},
		{ID: "del-2", Content: "to keep", Vector: []float64{0.4, 0.3, 0.2, 0.1}},
	}
	require.NoError(t, client.UpsertDocuments(ctx, intTestCollection, docs))
	time.Sleep(500 * time.Millisecond)

	require.NoError(t, client.DeleteDocuments(ctx, intTestCollection, []string{"del-1"}))
	time.Sleep(500 * time.Millisecond)

	_, err := client.GetDocument(ctx, intTestCollection, "del-1")
	require.Error(t, err, "del-1 should be gone")

	kept, err := client.GetDocument(ctx, intTestCollection, "del-2")
	require.NoError(t, err)
	assert.NotNil(t, kept)
}

func TestIntegration_CountDocuments(t *testing.T) {
	client := newIntClient(t)
	ctx := intCtx(t)

	_ = client.DeleteCollection(ctx, intTestCollection)
	require.NoError(t, client.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           intTestCollection,
		Dimensions:     intTestDimensions,
		DistanceMetric: "cosine",
	}))
	defer client.DeleteCollection(context.Background(), intTestCollection)

	docs := make([]vectordb.Document, 5)
	for i := range docs {
		docs[i] = vectordb.Document{
			ID:      fmt.Sprintf("cnt-%d", i),
			Content: fmt.Sprintf("doc %d", i),
			Vector:  []float64{float64(i) * 0.1, 0.1, 0.1, 0.1},
		}
	}
	require.NoError(t, client.UpsertDocuments(ctx, intTestCollection, docs))
	time.Sleep(1 * time.Second)

	count, err := client.CountDocuments(ctx, intTestCollection, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 5, count)
}

func TestIntegration_ListCollections(t *testing.T) {
	client := newIntClient(t)
	ctx := intCtx(t)

	_ = client.DeleteCollection(ctx, intTestCollection)
	require.NoError(t, client.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           intTestCollection,
		Dimensions:     intTestDimensions,
		DistanceMetric: "cosine",
	}))
	defer client.DeleteCollection(context.Background(), intTestCollection)

	names, err := client.ListCollections(ctx)
	require.NoError(t, err)
	assert.Contains(t, names, intTestCollection)
}

func TestIntegration_ScrollDocuments(t *testing.T) {
	client := newIntClient(t)
	ctx := intCtx(t)

	_ = client.DeleteCollection(ctx, intTestCollection)
	require.NoError(t, client.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           intTestCollection,
		Dimensions:     intTestDimensions,
		DistanceMetric: "cosine",
	}))
	defer client.DeleteCollection(context.Background(), intTestCollection)

	docs := make([]vectordb.Document, 10)
	for i := range docs {
		docs[i] = vectordb.Document{
			ID:      fmt.Sprintf("scroll-%d", i),
			Content: fmt.Sprintf("document %d", i),
			Vector:  []float64{float64(i) * 0.05, 0.1, 0.1, 0.1},
		}
	}
	require.NoError(t, client.UpsertDocuments(ctx, intTestCollection, docs))
	time.Sleep(1 * time.Second)

	result, err := client.ScrollDocuments(ctx, vectordb.ScrollRequest{
		CollectionName: intTestCollection,
		Limit:          5,
	})
	require.NoError(t, err)
	assert.Len(t, result.Documents, 5)
}
