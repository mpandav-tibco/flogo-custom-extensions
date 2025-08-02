package mysqlbinloglistener

import (
	"context"
	"testing"
	"time"

	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
)

// TestUpdateEventHandling verifies that UPDATE events only generate one output event
// instead of two (one for "before" row and one for "after" row)
func TestUpdateEventHandling(t *testing.T) {
	logger := log.RootLogger()

	// Create a mock event handler to count events
	eventCount := 0
	mockHandler := &MockEventHandler{
		handleFunc: func(ctx context.Context, event *BinlogEvent) error {
			eventCount++
			assert.Equal(t, "UPDATE", event.Type)
			// The data should be the "after" row data (not "before")
			assert.NotNil(t, event.Data)
			return nil
		},
	}

	listener := NewMySQLBinlogListener(&Settings{
		Host:         "localhost",
		Port:         3306,
		User:         "test",
		Password:     "test",
		DatabaseName: "testdb",
	}, logger)

	// Create a mock UPDATE binlog event with two rows (before and after)
	// This simulates what MySQL binlog actually provides for UPDATE operations
	updateEvent := &replication.BinlogEvent{
		Header: &replication.EventHeader{
			Timestamp: uint32(time.Now().Unix()),
			EventType: replication.UPDATE_ROWS_EVENTv2,
			ServerID:  1,
			LogPos:    1000,
		},
		Event: &replication.RowsEvent{
			Table: &replication.TableMapEvent{
				TableID: 1,
				Schema:  []byte("testdb"),
				Table:   []byte("users"),
			},
			// For UPDATE events, rows come in pairs: [before_row, after_row]
			Rows: [][]interface{}{
				// "Before" row (old values)
				{1, "john_old", "john.old@example.com"},
				// "After" row (new values) - this is what we want to capture
				{1, "john_new", "john.new@example.com"},
			},
		},
	}

	handlerSettings := &HandlerSettings{
		ServerID:      1001,
		EventTypes:    "UPDATE",
		IncludeSchema: false,
	}

	ctx := context.Background()

	// Process the UPDATE event
	err := listener.processBinlogEvent(updateEvent, nil, map[string]bool{"UPDATE": true}, handlerSettings, mockHandler, ctx)
	assert.NoError(t, err)

	// Verify that only ONE event was generated (not two)
	assert.Equal(t, 1, eventCount, "UPDATE operation should generate exactly one event, got %d", eventCount)
}

// MockEventHandler for testing
type MockEventHandler struct {
	handleFunc func(ctx context.Context, event *BinlogEvent) error
}

func (m *MockEventHandler) HandleEvent(ctx context.Context, event *BinlogEvent) error {
	if m.handleFunc != nil {
		return m.handleFunc(ctx, event)
	}
	return nil
}
