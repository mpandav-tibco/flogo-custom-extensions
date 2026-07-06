//go:build integration

// Integration tests for Azure AI Search VectorDB connector.
//
// Prerequisites:
//   - Set AZURE_SEARCH_ENDPOINT to your Azure AI Search service URL
//     (e.g. https://myservice.search.windows.net)
//   - Set AZURE_SEARCH_API_KEY to an admin API key for the service
//
// No Docker container is available for Azure AI Search — this is a cloud-only
// service. These tests make live API calls.
//
// Run with:
//
//	AZURE_SEARCH_ENDPOINT=https://... AZURE_SEARCH_API_KEY=... \
//	  go test -v -tags integration ./...

package vectordb_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-azureaisearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func integrationConfig(t *testing.T) vectordb.ConnectionConfig {
	t.Helper()
	endpoint := os.Getenv("AZURE_SEARCH_ENDPOINT")
	apiKey := os.Getenv("AZURE_SEARCH_API_KEY")
	if endpoint == "" || apiKey == "" {
		t.Skip("AZURE_SEARCH_ENDPOINT and AZURE_SEARCH_API_KEY must be set for integration tests")
	}
	return vectordb.ConnectionConfig{
		DBType:         "azureaisearch",
		Endpoint:       endpoint,
		APIKey:         apiKey,
		APIVersion:     "2024-05-01-Preview",
		TimeoutSeconds: 30,
		MaxRetries:     3,
		RetryBackoffMs: 500,
	}
}

func integrationClient(t *testing.T) vectordb.VectorDBClient {
	t.Helper()
	cfg := integrationConfig(t)
	name := fmt.Sprintf("integration-test-%d", time.Now().UnixMilli())
	client, err := vectordb.GetOrCreateClient(context.Background(), name, cfg)
	require.NoError(t, err)
	return client
}

func TestIntegration_HealthCheck(t *testing.T) {
	c := integrationClient(t)
	err := c.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestIntegration_CRUD(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	indexName := fmt.Sprintf("flogo-test-%d", time.Now().UnixMilli())
	const dims = 4

	// CreateCollection
	err := c.CreateCollection(ctx, vectordb.CollectionConfig{
		Name:           indexName,
		Dimensions:     dims,
		DistanceMetric: "cosine",
	})
	require.NoError(t, err)
	t.Logf("Created index: %s", indexName)

	defer func() {
		_ = c.DeleteCollection(ctx, indexName)
		t.Logf("Deleted index: %s", indexName)
	}()

	// CollectionExists
	exists, err := c.CollectionExists(ctx, indexName)
	require.NoError(t, err)
	assert.True(t, exists)

	// Allow index to become active
	time.Sleep(3 * time.Second)

	// UpsertDocuments
	docs := []vectordb.Document{
		{ID: "d1", Vector: []float64{0.1, 0.2, 0.3, 0.4}, Content: "hello world", Payload: map[string]interface{}{"category": "test"}},
		{ID: "d2", Vector: []float64{0.5, 0.6, 0.7, 0.8}, Content: "foo bar", Payload: map[string]interface{}{"category": "test2"}},
	}
	err = c.UpsertDocuments(ctx, indexName, docs)
	require.NoError(t, err)

	// Allow indexing to complete
	time.Sleep(3 * time.Second)

	// GetDocument
	doc, err := c.GetDocument(ctx, indexName, "d1")
	require.NoError(t, err)
	assert.Equal(t, "d1", doc.ID)
	assert.Equal(t, "hello world", doc.Content)

	// CountDocuments
	count, err := c.CountDocuments(ctx, indexName, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// VectorSearch
	results, err := c.VectorSearch(ctx, vectordb.SearchRequest{
		CollectionName: indexName,
		QueryVector:    []float64{0.1, 0.2, 0.3, 0.4},
		TopK:           2,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	assert.Equal(t, "d1", results[0].ID)

	// ScrollDocuments
	scrollResult, err := c.ScrollDocuments(ctx, vectordb.ScrollRequest{
		CollectionName: indexName,
		Limit:          10,
	})
	require.NoError(t, err)
	assert.Len(t, scrollResult.Documents, 2)

	// DeleteDocuments
	err = c.DeleteDocuments(ctx, indexName, []string{"d2"})
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	count2, err := c.CountDocuments(ctx, indexName, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count2)

	// ListCollections
	names, err := c.ListCollections(ctx)
	require.NoError(t, err)
	assert.Contains(t, names, indexName)
}
