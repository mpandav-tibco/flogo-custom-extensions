package mysqlbinloglistener

import (
	"testing"

	"github.com/project-flogo/core/data/metadata"
	"github.com/stretchr/testify/assert"
)

// TestJSONConfigurationMapping tests that JSON float64 values can be mapped to int fields
func TestJSONConfigurationMapping(t *testing.T) {
	// Simulate configuration data that comes from JSON parsing
	// JSON numbers are always parsed as float64 in Go
	configData := map[string]interface{}{
		"serverID":   float64(1001), // This is how JSON numbers come in
		"binlogFile": "",
		"binlogPos":  float64(4), // This is how JSON numbers come in
		"tables":     []string{},
		"eventTypes": "ALL",
	}

	// Test the metadata mapping that was failing before
	handlerSettings := &HandlerSettings{}
	err := metadata.MapToStruct(configData, handlerSettings, true)

	// This should not fail with the type conversion fix
	assert.NoError(t, err, "Configuration mapping should succeed with float64 to int conversion")
	assert.Equal(t, 1001, handlerSettings.ServerID, "ServerID should be mapped correctly")
	assert.Equal(t, 4, handlerSettings.BinlogPos, "BinlogPos should be mapped correctly")
	assert.Equal(t, "ALL", handlerSettings.EventTypes)

	// Test validation
	err = handlerSettings.Validate()
	assert.NoError(t, err, "Validation should pass with valid settings")
}

// TestBasicCompilation tests that basic types can be instantiated
func TestBasicCompilation(t *testing.T) {
	factory := &Factory{}
	assert.NotNil(t, factory, "Factory should not be nil")

	settings := &Settings{
		Host:         "localhost",
		Port:         3306,
		User:         "testuser",
		Password:     "testpass",
		DatabaseName: "testdb",
	}
	assert.NotNil(t, settings, "Settings should not be nil")
	assert.Equal(t, "localhost", settings.Host)
}

// TestMetadataBasic tests basic metadata functionality
func TestMetadataBasic(t *testing.T) {
	factory := &Factory{}
	metadata := factory.Metadata()
	assert.NotNil(t, metadata, "Metadata should not be nil")
}

// TestBasicSettingsValidation tests basic settings validation
func TestBasicSettingsValidation(t *testing.T) {
	settings := &Settings{}

	// Test validation with empty settings
	err := settings.Validate()
	assert.Error(t, err, "Should error with empty settings")

	// Test validation with valid settings
	settings.Host = "localhost"
	settings.Port = 3306
	settings.User = "testuser"
	settings.Password = "testpass"
	settings.DatabaseName = "testdb"

	err = settings.Validate()
	assert.NoError(t, err, "Should not error with valid settings")
}
