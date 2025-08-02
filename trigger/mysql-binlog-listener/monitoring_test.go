package mysqlbinloglistener

import (
	"testing"

	"github.com/project-flogo/core/trigger"
	"github.com/stretchr/testify/assert"
)

func TestHealthMonitoringConfiguration(t *testing.T) {
	factory := &Factory{}
	config := &trigger.Config{
		Settings: map[string]interface{}{
			"host":                "localhost",
			"port":                3306,
			"user":                "test",
			"password":            "test",
			"databaseName":        "test",
			"healthCheckInterval": "30s",
		},
	}

	triggerInstance, err := factory.New(config)
	assert.NoError(t, err)

	mysqlTrigger := triggerInstance.(*Trigger)
	assert.Equal(t, "30s", mysqlTrigger.settings.HealthCheckInterval)
}

func TestHeartbeatConfiguration(t *testing.T) {
	factory := &Factory{}
	config := &trigger.Config{
		Settings: map[string]interface{}{
			"host":              "localhost",
			"port":              3306,
			"user":              "test",
			"password":          "test",
			"databaseName":      "test",
			"enableHeartbeat":   true,
			"heartbeatInterval": "120s",
		},
	}

	triggerInstance, err := factory.New(config)
	assert.NoError(t, err)

	mysqlTrigger := triggerInstance.(*Trigger)
	assert.True(t, mysqlTrigger.settings.EnableHeartbeat)
	assert.Equal(t, "120s", mysqlTrigger.settings.HeartbeatInterval)
}

func TestMonitoringDefaults(t *testing.T) {
	factory := &Factory{}
	config := &trigger.Config{
		Settings: map[string]interface{}{
			"host":         "localhost",
			"port":         3306,
			"user":         "test",
			"password":     "test",
			"databaseName": "test",
		},
	}

	triggerInstance, err := factory.New(config)
	assert.NoError(t, err)

	mysqlTrigger := triggerInstance.(*Trigger)

	assert.Equal(t, "60s", mysqlTrigger.settings.HealthCheckInterval)
	assert.False(t, mysqlTrigger.settings.EnableHeartbeat)
	assert.Equal(t, "30s", mysqlTrigger.settings.HeartbeatInterval)
}

func TestMemoryTracking(t *testing.T) {
	// Test memory tracking functionality
	memStats := getMemoryStats()

	// Verify required memory statistics are present
	assert.Contains(t, memStats, "alloc_mb")
	assert.Contains(t, memStats, "total_alloc_mb")
	assert.Contains(t, memStats, "sys_mb")
	assert.Contains(t, memStats, "gc_runs")
	assert.Contains(t, memStats, "goroutines")

	// Verify values are reasonable (non-negative) and proper types
	allocMB, ok := memStats["alloc_mb"].(uint64)
	assert.True(t, ok, "alloc_mb should be uint64")
	assert.GreaterOrEqual(t, allocMB, uint64(0))

	totalAllocMB, ok := memStats["total_alloc_mb"].(uint64)
	assert.True(t, ok, "total_alloc_mb should be uint64")
	assert.GreaterOrEqual(t, totalAllocMB, uint64(0))

	sysMB, ok := memStats["sys_mb"].(uint64)
	assert.True(t, ok, "sys_mb should be uint64")
	assert.GreaterOrEqual(t, sysMB, uint64(0))

	gcRuns, ok := memStats["gc_runs"].(uint64)
	assert.True(t, ok, "gc_runs should be uint64")
	assert.GreaterOrEqual(t, gcRuns, uint64(0))

	goroutines, ok := memStats["goroutines"].(int)
	assert.True(t, ok, "goroutines should be int")
	assert.Greater(t, goroutines, 0) // Should have at least 1 goroutine
}

func TestMemoryConverter(t *testing.T) {
	// Test bytes to megabytes conversion
	tests := []struct {
		bytes    uint64
		expected uint64
	}{
		{0, 0},
		{1024 * 1024, 1},           // 1 MB
		{1024 * 1024 * 10, 10},     // 10 MB
		{1024 * 1024 * 1024, 1024}, // 1 GB = 1024 MB
	}

	for _, tt := range tests {
		result := bToMb(tt.bytes)
		assert.Equal(t, tt.expected, result, "Conversion of %d bytes should equal %d MB", tt.bytes, tt.expected)
	}
}
