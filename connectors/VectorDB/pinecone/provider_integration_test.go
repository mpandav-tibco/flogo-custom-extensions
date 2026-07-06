//go:build integration

package vectordb

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPinecone_Local_Integration runs against Pinecone Local (Docker).
//
// Start the container before running:
//
//	docker run -d --name vectordb-pinecone-local \
//	  -e PORT=5080 -e PINECONE_HOST=localhost \
//	  -p 5080-5090:5080-5090 --platform linux/amd64 \
//	  ghcr.io/pinecone-io/pinecone-local:latest
//
// Then:
//
//	go test -tags=integration -v -timeout 3m -run TestPinecone_Local ./...
func TestPinecone_Local_Integration(t *testing.T) {
	cfg := ConnectionConfig{
		Host:           "localhost:5080",
		Scheme:         "http",
		APIKey:         "localkey", // local emulator ignores API key
		PineconeCloud:  "aws",
		PineconeRegion: "us-east-1",
		TimeoutSeconds: 30,
	}
	ctx := context.Background()
	client, err := NewClient(ctx, cfg)
	if err != nil {
		t.Skipf("Pinecone Local not reachable at localhost:5080 — start the Docker container first: %v", err)
	}
	defer client.Close()

	const indexName = "flogo-local-test"
	const dims = 8

	// Cleanup in case previous run left state.
	_ = client.DeleteCollection(ctx, indexName)

	t.Run("CreateCollection", func(t *testing.T) {
		err := client.CreateCollection(ctx, CollectionConfig{
			Name:           indexName,
			Dimensions:     dims,
			DistanceMetric: "cosine",
		})
		require.NoError(t, err)
	})

	t.Run("CollectionExists", func(t *testing.T) {
		exists, err := client.CollectionExists(ctx, indexName)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("ListCollections", func(t *testing.T) {
		cols, err := client.ListCollections(ctx)
		require.NoError(t, err)
		assert.Contains(t, cols, indexName)
	})

	docs := []Document{
		{
			ID:      "local-1",
			Vector:  []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
			Content: "machine learning algorithms",
			Payload: map[string]interface{}{"category": "ml"},
		},
		{
			ID:      "local-2",
			Vector:  []float64{0.8, 0.7, 0.6, 0.5, 0.4, 0.3, 0.2, 0.1},
			Content: "neural network training",
			Payload: map[string]interface{}{"category": "dl"},
		},
		{
			ID:      "local-3",
			Vector:  []float64{0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5},
			Content: "data science methods",
			Payload: map[string]interface{}{"category": "ds"},
		},
	}

	t.Run("UpsertDocuments", func(t *testing.T) {
		err := client.UpsertDocuments(ctx, indexName, docs)
		require.NoError(t, err)
		// Give the local emulator a moment to index
		time.Sleep(500 * time.Millisecond)
	})

	t.Run("GetDocument", func(t *testing.T) {
		doc, err := client.GetDocument(ctx, indexName, "local-1")
		require.NoError(t, err)
		require.NotNil(t, doc)
		assert.Equal(t, "local-1", doc.ID)
	})

	t.Run("GetDocument_NotFound", func(t *testing.T) {
		doc, err := client.GetDocument(ctx, indexName, "does-not-exist")
		require.Error(t, err)
		assert.Nil(t, doc)
	})

	t.Run("VectorSearch", func(t *testing.T) {
		results, err := client.VectorSearch(ctx, SearchRequest{
			CollectionName: indexName,
			QueryVector:    []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
			TopK:           2,
		})
		require.NoError(t, err)
		require.NotEmpty(t, results, "VectorSearch must return results")
		assert.Equal(t, "local-1", results[0].ID, "closest vector should be local-1")
	})

	t.Run("HybridSearch", func(t *testing.T) {
		// Local emulator: hybrid uses the same query endpoint with sparse vectors.
		results, err := client.HybridSearch(ctx, HybridSearchRequest{
			CollectionName: indexName,
			QueryText:      "machine learning",
			QueryVector:    []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
			TopK:           2,
			Alpha:          0.5,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
	})

	t.Run("CountDocuments", func(t *testing.T) {
		count, err := client.CountDocuments(ctx, indexName, nil)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(3))
	})

	t.Run("ScrollDocuments", func(t *testing.T) {
		result, err := client.ScrollDocuments(ctx, ScrollRequest{
			CollectionName: indexName,
			Limit:          10,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, result.Documents)
	})

	t.Run("DeleteDocuments", func(t *testing.T) {
		err := client.DeleteDocuments(ctx, indexName, []string{"local-3"})
		require.NoError(t, err)
		time.Sleep(200 * time.Millisecond)
		count, err := client.CountDocuments(ctx, indexName, nil)
		require.NoError(t, err)
		assert.LessOrEqual(t, count, int64(2))
	})

	t.Run("DeleteCollection", func(t *testing.T) {
		err := client.DeleteCollection(ctx, indexName)
		require.NoError(t, err)
		exists, err := client.CollectionExists(ctx, indexName)
		require.NoError(t, err)
		assert.False(t, exists)
	})
}

// TestPinecone_Cloud_Integration runs against the real Pinecone cloud API.
// Requires PINECONE_API_KEY environment variable.
//
//	PINECONE_API_KEY=pk-... go test -tags=integration -v -timeout 10m -run TestPinecone_Cloud ./...
func TestPinecone_Cloud_Integration(t *testing.T) {
	apiKey := os.Getenv("PINECONE_API_KEY")
	if apiKey == "" {
		t.Skip("PINECONE_API_KEY not set — skipping cloud integration tests")
	}
	cloud := os.Getenv("PINECONE_CLOUD")
	if cloud == "" {
		cloud = "aws"
	}
	region := os.Getenv("PINECONE_REGION")
	if region == "" {
		region = "us-east-1"
	}

	cfg := ConnectionConfig{
		APIKey:         apiKey,
		PineconeCloud:  cloud,
		PineconeRegion: region,
		TimeoutSeconds: 60,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client, err := NewClient(ctx, cfg)
	require.NoError(t, err)
	defer client.Close()

	indexName := os.Getenv("PINECONE_INDEX_NAME")
	if indexName == "" {
		indexName = fmt.Sprintf("flogo-cloud-test-%d", time.Now().Unix())
	}

	_ = client.DeleteCollection(ctx, indexName)
	time.Sleep(2 * time.Second)

	t.Run("CreateAndUpsertAndSearch", func(t *testing.T) {
		err := client.CreateCollection(ctx, CollectionConfig{
			Name:           indexName,
			Dimensions:     8,
			DistanceMetric: "cosine",
		})
		require.NoError(t, err)

		err = client.UpsertDocuments(ctx, indexName, []Document{
			{ID: "c1", Vector: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8}, Content: "hello"},
			{ID: "c2", Vector: []float64{0.8, 0.7, 0.6, 0.5, 0.4, 0.3, 0.2, 0.1}, Content: "world"},
		})
		require.NoError(t, err)

		time.Sleep(5 * time.Second) // eventual consistency

		results, err := client.VectorSearch(ctx, SearchRequest{
			CollectionName: indexName,
			QueryVector:    []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
			TopK:           2,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
	})

	t.Cleanup(func() {
		_ = client.DeleteCollection(ctx, indexName)
	})
}
