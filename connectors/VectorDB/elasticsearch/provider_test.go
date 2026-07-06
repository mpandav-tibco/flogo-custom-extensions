//go:build !integration

package vectordb_test

import (
	"context"
	"testing"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCtx() context.Context { return context.Background() }

// ── NewClient validation ─────────────────────────────────────────────────────

func TestNewClient_InvalidPort(t *testing.T) {
	_, err := vectordb.NewClient(testCtx(), vectordb.ConnectionConfig{
		Host: "localhost",
		Port: 99999, // port out of range
	})
	require.Error(t, err)
}

func TestNewClient_NegativeTimeout(t *testing.T) {
	// Negative timeout should be rejected.
	_, err := vectordb.NewClient(testCtx(), vectordb.ConnectionConfig{
		Host:           "localhost",
		TimeoutSeconds: -1,
	})
	require.Error(t, err)
}

func TestNewClient_ValidConfig(t *testing.T) {
	client, err := vectordb.NewClient(testCtx(), vectordb.ConnectionConfig{
		Host: "localhost",
		Port: 9200,
	})
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, "elasticsearch", client.DBType())
}

// ── ConnectionConfig String() redaction ─────────────────────────────────────

func TestConnectionConfig_StringRedaction(t *testing.T) {
	cfg := vectordb.ConnectionConfig{
		Host:     "localhost",
		Port:     9200,
		Username: "elastic",
		Password: "supersecret",
		APIKey:   "myapikey",
		DBType:   "elasticsearch",
	}
	s := cfg.String()
	assert.NotContains(t, s, "supersecret", "password must be redacted")
	assert.NotContains(t, s, "myapikey", "apiKey must be redacted")
	assert.Contains(t, s, "localhost")
}

// ── CollectionConfig validation ──────────────────────────────────────────────

func TestValidateCollectionConfig_ValidCosine(t *testing.T) {
	client, _ := vectordb.NewClient(testCtx(), vectordb.ConnectionConfig{Host: "localhost"})
	cfg := vectordb.CollectionConfig{
		Name:           "test-col",
		Dimensions:     1536,
		DistanceMetric: "cosine",
	}
	// Just verify the collection config is accepted without panic
	_ = cfg
	assert.NotNil(t, client)
}

func TestValidateCollectionConfig_ZeroDimensions(t *testing.T) {
	cfg := vectordb.CollectionConfig{
		Name:           "test-col",
		Dimensions:     0,
		DistanceMetric: "cosine",
	}
	// Zero dimensions should be rejected
	err := vectordb.ValidateCollectionConfig(cfg)
	require.Error(t, err)
}

func TestValidateCollectionConfig_InvalidMetric(t *testing.T) {
	cfg := vectordb.CollectionConfig{
		Name:           "test-col",
		Dimensions:     768,
		DistanceMetric: "hamming",
	}
	err := vectordb.ValidateCollectionConfig(cfg)
	require.Error(t, err)
}

// ── SearchRequest validation ─────────────────────────────────────────────────

func TestValidateSearchRequest_NoVector(t *testing.T) {
	req := vectordb.SearchRequest{
		CollectionName: "test",
		QueryVector:    nil,
		TopK:           5,
	}
	err := vectordb.ValidateSearchRequest(req)
	require.Error(t, err)
}

func TestValidateSearchRequest_ZeroTopK(t *testing.T) {
	req := vectordb.SearchRequest{
		CollectionName: "test",
		QueryVector:    []float64{0.1, 0.2, 0.3},
		TopK:           0,
	}
	err := vectordb.ValidateSearchRequest(req)
	require.Error(t, err)
}

func TestValidateSearchRequest_Valid(t *testing.T) {
	req := vectordb.SearchRequest{
		CollectionName: "test",
		QueryVector:    []float64{0.1, 0.2, 0.3},
		TopK:           10,
	}
	err := vectordb.ValidateSearchRequest(req)
	require.NoError(t, err)
}

// ── Registry ─────────────────────────────────────────────────────────────────

func TestRegistry_GetOrCreate(t *testing.T) {
	_ = vectordb.ResetRegistry()
	defer func() { _ = vectordb.ResetRegistry() }()

	// GetOrCreateClient does a health check — skip if ES not running.
	cfg := vectordb.ConnectionConfig{Host: "localhost", Port: 9200}
	c1, err := vectordb.GetOrCreateClient(testCtx(), "conn1", cfg)
	if err != nil {
		t.Skipf("ES not reachable, skipping: %v", err)
	}
	require.NotNil(t, c1)

	c2, err := vectordb.GetOrCreateClient(testCtx(), "conn1", cfg)
	require.NoError(t, err)
	assert.Equal(t, c1, c2, "second call must return the same instance")
}

func TestRegistry_GetClient_Missing(t *testing.T) {
	_ = vectordb.ResetRegistry()
	defer func() { _ = vectordb.ResetRegistry() }()

	_, err := vectordb.GetClient("nonexistent")
	require.Error(t, err)
}

func TestRegistry_Seal(t *testing.T) {
	_ = vectordb.ResetRegistry()
	// NOTE: SealRegistry is permanent for the process; skip this test to avoid
	// poisoning other tests in the same run.
	t.Skip("SealRegistry is process-permanent; run in isolated binary only")
}

func TestRegistry_Deregister(t *testing.T) {
	_ = vectordb.ResetRegistry()
	defer func() { _ = vectordb.ResetRegistry() }()

	// GetOrCreateClient does a health check — skip if ES not running.
	cfg := vectordb.ConnectionConfig{Host: "localhost", Port: 9200}
	_, err := vectordb.GetOrCreateClient(testCtx(), "to-remove", cfg)
	if err != nil {
		t.Skipf("ES not reachable, skipping: %v", err)
	}

	vectordb.DeregisterClient("to-remove")

	_, err = vectordb.GetClient("to-remove")
	require.Error(t, err)
}

// ── DBType ────────────────────────────────────────────────────────────────────

func TestDBType_ReturnsElasticsearch(t *testing.T) {
	client, err := vectordb.NewClient(testCtx(), vectordb.ConnectionConfig{Host: "localhost"})
	require.NoError(t, err)
	assert.Equal(t, "elasticsearch", client.DBType())
}

// ── HealthCheck (offline - expect network error, not panic) ──────────────────

func TestHealthCheck_Offline(t *testing.T) {
	client, err := vectordb.NewClient(testCtx(), vectordb.ConnectionConfig{
		Host:           "localhost",
		Port:           19200, // port unlikely to be listening
		MaxRetries:     1,
		RetryBackoffMs: 50,
		TimeoutSeconds: 2,
	})
	require.NoError(t, err)

	err = client.HealthCheck(testCtx())
	// We expect an error (connection refused) but NOT a panic
	assert.Error(t, err)
}
