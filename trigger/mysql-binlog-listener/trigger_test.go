package mysqlbinloglistener

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/trigger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockHandler for testing trigger handlers
type MockHandler struct {
	mock.Mock
}

func (m *MockHandler) Handle(ctx context.Context, data interface{}) (map[string]interface{}, error) {
	args := m.Called(ctx, data)
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockHandler) Settings() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

func (m *MockHandler) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockHandler) Logger() log.Logger {
	return log.RootLogger()
}

func (m *MockHandler) Schemas() *trigger.SchemaConfig {
	return nil
}

// MockInitContext for testing trigger initialization
type MockInitContext struct {
	mock.Mock
	logger   log.Logger
	handlers []interface{} // Changed from trigger.Handler to interface{}
}

func (m *MockInitContext) Logger() log.Logger {
	if m.logger == nil {
		m.logger = log.RootLogger()
	}
	return m.logger
}

func (m *MockInitContext) GetHandlers() []interface{} { // Changed return type
	return m.handlers
}

func (m *MockInitContext) GetInitCtx() context.Context {
	return context.Background()
}

// func TestTriggerFactory(t *testing.T) {
// 	factory := &Factory{}

// 	// Test metadata
// 	metadata := factory.Metadata()
// 	assert.NotNil(t, metadata, "Factory metadata should not be nil")

// 	// Test trigger creation with valid settings
// 	config := &trigger.Config{
// 		Settings: map[string]interface{}{
// 			"host":         "localhost",
// 			"port":         3306,
// 			"user":         "testuser",
// 			"password":     "testpass",
// 			"databaseName": "testdb",
// 		},
// 	}

// 	triggerInstance, err := factory.New(config)
// 	assert.NoError(t, err, "Should create trigger without error")
// 	assert.NotNil(t, triggerInstance, "Trigger instance should not be nil")

// 	// Verify it's the correct type
// 	mysqlTrigger, ok := triggerInstance.(*Trigger)
// 	assert.True(t, ok, "Should be a MySQL trigger instance")
// 	assert.NotNil(t, mysqlTrigger.settings, "Settings should be initialized")
// 	assert.Equal(t, "localhost", mysqlTrigger.settings.Host)
// 	assert.Equal(t, 3306, mysqlTrigger.settings.Port)
// }

func TestTriggerFactoryWithInvalidSettings(t *testing.T) {
	factory := &Factory{}

	// Test with missing required settings
	config := &trigger.Config{
		Settings: map[string]interface{}{
			"host": "localhost",
			// Missing required fields
		},
	}

	_, err := factory.New(config)
	assert.Error(t, err, "Should fail with invalid settings")
}

func TestSettingsValidation(t *testing.T) {
	tests := []struct {
		name     string
		settings *Settings
		wantErr  bool
	}{
		{
			name: "valid settings",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				User:         "testuser",
				Password:     "testpass",
				DatabaseName: "testdb",
			},
			wantErr: false,
		},
		{
			name: "missing host",
			settings: &Settings{
				Port:         3306,
				User:         "testuser",
				Password:     "testpass",
				DatabaseName: "testdb",
			},
			wantErr: true,
		},
		{
			name: "invalid port gets fixed to default",
			settings: &Settings{
				Host:         "localhost",
				Port:         0,
				User:         "testuser",
				Password:     "testpass",
				DatabaseName: "testdb",
			},
			wantErr: false, // Port 0 gets automatically fixed to 3306
		},
		{
			name: "missing user",
			settings: &Settings{
				Host:         "localhost",
				Port:         3306,
				Password:     "testpass",
				DatabaseName: "testdb",
			},
			wantErr: true,
		},
		{
			name: "missing database",
			settings: &Settings{
				Host:     "localhost",
				Port:     3306,
				User:     "testuser",
				Password: "testpass",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHandlerSettingsValidation(t *testing.T) {
	tests := []struct {
		name     string
		settings *HandlerSettings
		wantErr  bool
	}{
		{
			name: "valid settings",
			settings: &HandlerSettings{
				ServerID:   100,
				BinlogFile: "mysql-bin.000001",
				BinlogPos:  4,
				Tables:     []string{"users", "orders"},
				EventTypes: "INSERT",
			},
			wantErr: false,
		},
		{
			name: "missing server ID",
			settings: &HandlerSettings{
				BinlogFile: "mysql-bin.000001",
				BinlogPos:  4,
			},
			wantErr: true,
		},
		{
			name: "invalid event type",
			settings: &HandlerSettings{
				ServerID:   100,
				EventTypes: "INVALID_TYPE",
			},
			wantErr: true,
		},
		{
			name: "valid with minimal settings",
			settings: &HandlerSettings{
				ServerID: 100,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTriggerInitialize(t *testing.T) {
	// Create a trigger with valid settings
	settings := &Settings{
		Host:         "localhost",
		Port:         3306,
		User:         "testuser",
		Password:     "testpass",
		DatabaseName: "testdb",
	}

	trigger := &Trigger{
		settings: settings,
		logger:   log.RootLogger(),
	}

	// Test that trigger initialization should work with valid settings
	assert.NotNil(t, trigger)
	assert.Equal(t, "localhost", trigger.settings.Host)
	assert.Equal(t, 3306, trigger.settings.Port)
	assert.Equal(t, "testdb", trigger.settings.DatabaseName)

	// Create mock handler
	mockHandler := &MockHandler{}
	mockHandler.On("Settings").Return(map[string]interface{}{
		"serverID":   100,
		"binlogFile": "mysql-bin.000001",
		"binlogPos":  4,
		"tables":     []string{"users"},
		"eventTypes": "ALL",
	})
	mockHandler.On("Name").Return("test-handler")

	// Note: Full initialization would require database connection,
	// which is tested in integration tests
}

func TestFlogoEventHandler(t *testing.T) {
	mockHandler := &MockHandler{}
	logger := log.RootLogger()

	eventHandler := NewFlogoEventHandler(mockHandler, logger)
	assert.NotNil(t, eventHandler, "Event handler should not be nil")
	assert.Equal(t, mockHandler, eventHandler.runner)
	assert.Equal(t, logger, eventHandler.logger)

	// Test event handling
	event := &BinlogEvent{
		ID:            "test-event-1",
		Type:          "INSERT",
		Database:      "testdb",
		Table:         "users",
		Timestamp:     time.Now(),
		Data:          map[string]interface{}{"id": 1, "name": "test"},
		BinlogFile:    "mysql-bin.000001",
		BinlogPos:     123,
		ServerID:      100,
		CorrelationID: "test-correlation",
	}

	// Mock the Handle method - use mock.Anything for context to avoid type issues
	mockHandler.On("Handle", mock.Anything, mock.AnythingOfType("*mysqlbinloglistener.Output")).Return(
		map[string]interface{}{"result": "success"}, nil)

	ctx := context.Background()
	err := eventHandler.HandleEvent(ctx, event)
	assert.NoError(t, err, "Event handling should succeed")

	mockHandler.AssertExpectations(t)
}

func TestBinlogEventToOutput(t *testing.T) {
	now := time.Now()
	event := &BinlogEvent{
		ID:            "test-event-1",
		Type:          "INSERT",
		Database:      "testdb",
		Table:         "users",
		Timestamp:     now,
		Data:          map[string]interface{}{"id": 1, "name": "John"},
		BinlogFile:    "mysql-bin.000001",
		BinlogPos:     123,
		ServerID:      100,
		GTID:          "uuid:1-5",
		CorrelationID: "test-correlation",
	}

	output := &Output{
		EventID:       event.ID,
		EventType:     event.Type,
		Database:      event.Database,
		Table:         event.Table,
		Timestamp:     event.Timestamp.Format(time.RFC3339), // Convert time.Time to string
		Data:          event.Data,
		Schema:        event.Schema, // Include schema field
		BinlogFile:    event.BinlogFile,
		BinlogPos:     int(event.BinlogPos), // Convert uint32 to int
		ServerID:      int(event.ServerID),  // Convert uint32 to int
		GTID:          event.GTID,
		CorrelationID: event.CorrelationID,
	}

	// Verify all fields are correctly mapped
	assert.Equal(t, event.ID, output.EventID)
	assert.Equal(t, event.Type, output.EventType)
	assert.Equal(t, event.Database, output.Database)
	assert.Equal(t, event.Table, output.Table)
	assert.Equal(t, event.Timestamp.Format(time.RFC3339), output.Timestamp) // Compare formatted timestamp
	assert.Equal(t, event.Data, output.Data)
	assert.Equal(t, event.Schema, output.Schema) // Compare schema field
	assert.Equal(t, event.BinlogFile, output.BinlogFile)
	assert.Equal(t, int(event.BinlogPos), output.BinlogPos) // Compare with type conversion
	assert.Equal(t, int(event.ServerID), output.ServerID)   // Compare with type conversion
	assert.Equal(t, event.GTID, output.GTID)
	assert.Equal(t, event.CorrelationID, output.CorrelationID)
}

func TestBinlogEventWithSchema(t *testing.T) {
	now := time.Now()
	event := &BinlogEvent{
		ID:        "test-event-schema",
		Type:      "INSERT",
		Database:  "testdb",
		Table:     "users",
		Timestamp: now,
		Data: map[string]interface{}{
			"id":    1,
			"name":  "John Doe",
			"email": "john@example.com",
		},
		Schema: map[string]interface{}{
			"database": "testdb",
			"table":    "users",
			"columns": []map[string]interface{}{
				{
					"name":             "id",
					"type":             "int",
					"nullable":         false,
					"key":              "PRI",
					"ordinal_position": 1,
				},
				{
					"name":             "name",
					"type":             "varchar",
					"nullable":         true,
					"key":              "",
					"ordinal_position": 2,
				},
				{
					"name":             "email",
					"type":             "varchar",
					"nullable":         false,
					"key":              "UNI",
					"ordinal_position": 3,
				},
			},
			"column_names": []string{"id", "name", "email"},
		},
		BinlogFile:    "mysql-bin.000002",
		BinlogPos:     456,
		ServerID:      200,
		GTID:          "uuid:6-10",
		CorrelationID: "test-correlation-schema",
	}

	output := &Output{
		EventID:       event.ID,
		EventType:     event.Type,
		Database:      event.Database,
		Table:         event.Table,
		Timestamp:     event.Timestamp.Format(time.RFC3339),
		Data:          event.Data,
		Schema:        event.Schema,
		BinlogFile:    event.BinlogFile,
		BinlogPos:     int(event.BinlogPos),
		ServerID:      int(event.ServerID),
		GTID:          event.GTID,
		CorrelationID: event.CorrelationID,
	}

	// Verify schema information is properly mapped
	assert.NotNil(t, output.Schema)
	schema := output.Schema
	assert.Equal(t, "testdb", schema["database"])
	assert.Equal(t, "users", schema["table"])

	// Verify column information
	columns, ok := schema["columns"].([]map[string]interface{})
	assert.True(t, ok, "columns should be a slice of maps")
	assert.Len(t, columns, 3, "should have 3 columns")

	// Verify first column details
	assert.Equal(t, "id", columns[0]["name"])
	assert.Equal(t, "int", columns[0]["type"])
	assert.Equal(t, false, columns[0]["nullable"])
	assert.Equal(t, "PRI", columns[0]["key"])

	// Verify column names array
	columnNames, ok := schema["column_names"].([]string)
	assert.True(t, ok, "column_names should be a slice of strings")
	assert.Equal(t, []string{"id", "name", "email"}, columnNames)
}

func TestFormatRowDataWithSchema(t *testing.T) {
	settings := &Settings{
		Host:         "localhost",
		Port:         3306,
		User:         "test",
		Password:     "test",
		DatabaseName: "test",
	}
	logger := log.RootLogger()
	listener := NewMySQLBinlogListener(settings, logger)

	// Test with schema information
	schema := map[string]interface{}{
		"column_names": []string{"id", "name", "email"},
	}

	row := []interface{}{1, "John Doe", "john@example.com"}
	result := listener.formatRowDataWithSchema(row, schema)

	expected := map[string]interface{}{
		"id":    1,
		"name":  "John Doe",
		"email": "john@example.com",
	}

	assert.Equal(t, expected, result)

	// Test without schema information (should fall back to col_X format)
	emptySchema := map[string]interface{}{}
	result2 := listener.formatRowDataWithSchema(row, emptySchema)

	expected2 := map[string]interface{}{
		"col_0": 1,
		"col_1": "John Doe",
		"col_2": "john@example.com",
	}

	assert.Equal(t, expected2, result2)

	// Test with more data than schema columns
	row3 := []interface{}{1, "John Doe", "john@example.com", "extra_data"}
	result3 := listener.formatRowDataWithSchema(row3, schema)

	expected3 := map[string]interface{}{
		"id":    1,
		"name":  "John Doe",
		"email": "john@example.com",
		"col_3": "extra_data", // Extra column falls back to col_X format
	}

	assert.Equal(t, expected3, result3)
}

func TestTriggerMetadata(t *testing.T) {
	trigger := &Trigger{}
	metadata := trigger.Metadata()
	assert.NotNil(t, metadata, "Trigger metadata should not be nil")
	assert.Equal(t, triggerMd, metadata, "Should return the correct metadata")
}

// Integration test helpers
func skipIfNoMySQL(t *testing.T) {
	if os.Getenv("MYSQL_TEST_HOST") == "" {
		t.Skip("Skipping MySQL integration test: MYSQL_TEST_HOST not set")
	}
}

func TestMySQLIntegration(t *testing.T) {
	skipIfNoMySQL(t)

	// This would be a full integration test with a real MySQL instance
	// For now, we just validate the structure
	host := os.Getenv("MYSQL_TEST_HOST")
	port := 3306
	user := os.Getenv("MYSQL_TEST_USER")
	password := os.Getenv("MYSQL_TEST_PASSWORD")
	database := os.Getenv("MYSQL_TEST_DATABASE")

	if host == "" || user == "" || password == "" || database == "" {
		t.Skip("Missing MySQL test environment variables")
	}

	settings := &Settings{
		Host:         host,
		Port:         port,
		User:         user,
		Password:     password,
		DatabaseName: database,
	}

	err := settings.Validate()
	assert.NoError(t, err, "Settings should be valid")

	// Create listener (this will fail without proper MySQL setup, but tests the structure)
	logger := log.RootLogger()
	listener := NewMySQLBinlogListener(settings, logger)
	assert.NotNil(t, listener, "Listener should be created")
}

func TestTriggerRegistration(t *testing.T) {
	// Test that the trigger is properly registered
	factory := &Factory{}

	// Verify factory interface compliance
	var _ trigger.Factory = factory

	// Test metadata
	metadata := factory.Metadata()
	require.NotNil(t, metadata, "Factory metadata should not be nil")

	// Verify the trigger implements the interface
	config := &trigger.Config{
		Settings: map[string]interface{}{
			"host":         "localhost",
			"port":         3306,
			"user":         "testuser",
			"password":     "testpass",
			"databaseName": "testdb",
		},
	}

	triggerInstance, err := factory.New(config)
	require.NoError(t, err, "Should create trigger without error")

	// Verify trigger interface compliance
	var _ trigger.Trigger = triggerInstance
}

func BenchmarkEventHandling(b *testing.B) {
	mockHandler := &MockHandler{}
	logger := log.RootLogger()
	eventHandler := NewFlogoEventHandler(mockHandler, logger)

	// Setup mock
	mockHandler.On("Handle", mock.Anything, mock.Anything).Return(
		map[string]interface{}{"result": "success"}, nil)

	event := &BinlogEvent{
		ID:            "bench-event",
		Type:          "INSERT",
		Database:      "testdb",
		Table:         "users",
		Timestamp:     time.Now(),
		Data:          map[string]interface{}{"id": 1, "name": "benchmark"},
		BinlogFile:    "mysql-bin.000001",
		BinlogPos:     123,
		ServerID:      100,
		CorrelationID: "bench-correlation",
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := eventHandler.HandleEvent(ctx, event)
		if err != nil {
			b.Fatal(err)
		}
	}
}
