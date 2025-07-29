package sse

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/trigger"
)

// Trigger metadata
var triggerMd = trigger.NewMetadata(&Settings{}, &HandlerSettings{}, &Output{})

func init() {
	_ = trigger.Register(&Trigger{}, &Factory{})
}

// Trigger represents the SSE trigger
type Trigger struct {
	settings *Settings
	logger   log.Logger
	server   *SSEServer
	handlers []trigger.Handler
	ctx      context.Context
	cancel   context.CancelFunc
	mutex    sync.RWMutex
	metrics  *Metrics
	name     string // Trigger name for registration
}

// Factory is the trigger factory
type Factory struct{}

// Metadata returns the trigger metadata
func (*Factory) Metadata() *trigger.Metadata {
	return triggerMd
}

// New creates a new trigger instance
func (*Factory) New(config *trigger.Config) (trigger.Trigger, error) {
	s := &Settings{}
	err := metadata.MapToStruct(config.Settings, s, true)
	if err != nil {
		return nil, fmt.Errorf("failed to map trigger settings: %v", err)
	}

	// Validate settings using our enterprise validation framework
	if validationErrors := ValidateSettings(config.Settings); len(validationErrors) > 0 {
		var errorMessage string
		for i, validationError := range validationErrors {
			if i > 0 {
				errorMessage += "; "
			}
			errorMessage += validationError.Error()
		}
		return nil, fmt.Errorf("trigger validation failed: %s", errorMessage)
	}

	return &Trigger{
		settings: s,
		metrics:  &Metrics{},
		name:     config.Id, // Use config ID as trigger name
	}, nil
}

// Initialize initializes the trigger
func (t *Trigger) Initialize(ctx trigger.InitContext) error {
	t.logger = ctx.Logger()
	t.ctx, t.cancel = context.WithCancel(context.Background())

	// Validate settings
	if err := t.validateSettings(); err != nil {
		return fmt.Errorf("validation failed: %v", err)
	}

	// Store handlers
	for _, handler := range ctx.GetHandlers() {
		t.handlers = append(t.handlers, handler)
	}

	// Create SSE server
	serverConfig := &SSEServerConfig{
		Port:              t.settings.Port,
		Path:              t.settings.Path,
		MaxConnections:    t.settings.MaxConnections,
		EnableCORS:        t.settings.EnableCORS,
		CORSOrigins:       t.settings.CORSOrigins,
		KeepAliveInterval: time.Duration(t.settings.KeepAliveInterval) * time.Second,
		EnableEventStore:  t.settings.EnableEventStore,
		EventStoreSize:    t.settings.EventStoreSize,
		EventTTL:          time.Duration(t.settings.EventTTL) * time.Second,
		Logger:            t.logger,
	}

	server, err := NewSSEServer(serverConfig)
	if err != nil {
		return fmt.Errorf("failed to create SSE server: %v", err)
	}
	t.server = server

	// Set connection callback
	t.server.SetConnectionCallback(t.handleNewConnection)

	t.logger.Infof("SSE Trigger initialized on port %d, path %s", t.settings.Port, t.settings.Path)
	return nil
}

// Start starts the trigger
func (t *Trigger) Start() error {
	t.logger.Info("Starting SSE Trigger")

	// Start the SSE server
	if err := t.server.Start(t.ctx); err != nil {
		return fmt.Errorf("failed to start SSE server: %v", err)
	}

	// Register the server with the global registry for SSE Send activity integration
	adapter := &SSEServerAdapter{server: t.server}
	RegisterSSEServerGlobal(t.name, adapter)

	// Also register with "default" name for backward compatibility
	if t.name != "default" {
		RegisterSSEServerGlobal("default", adapter)
	}

	t.logger.Infof("SSE Trigger started successfully on :%d%s and registered as '%s'", t.settings.Port, t.settings.Path, t.name)
	return nil
}

// Stop stops the trigger
func (t *Trigger) Stop() error {
	t.logger.Info("Stopping SSE Trigger")

	// Unregister from the global registry
	UnregisterSSEServerGlobal(t.name)
	if t.name != "default" {
		UnregisterSSEServerGlobal("default")
	}

	if t.cancel != nil {
		t.cancel()
	}

	if t.server != nil {
		if err := t.server.Stop(); err != nil {
			t.logger.Errorf("Error stopping SSE server: %v", err)
			return err
		}
	}

	t.logger.Info("SSE Trigger stopped")
	return nil
}

// validateSettings validates the trigger settings
func (t *Trigger) validateSettings() error {
	if t.settings.Port <= 0 || t.settings.Port > 65535 {
		return fmt.Errorf("invalid port: %d", t.settings.Port)
	}

	if t.settings.Path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	if t.settings.MaxConnections <= 0 {
		t.settings.MaxConnections = 1000
	}

	if t.settings.KeepAliveInterval <= 0 {
		t.settings.KeepAliveInterval = 30
	}

	if t.settings.EventStoreSize <= 0 {
		t.settings.EventStoreSize = 100
	}

	if t.settings.EventTTL <= 0 {
		t.settings.EventTTL = 3600
	}

	return nil
}

// handleNewConnection handles new SSE connections
func (t *Trigger) handleNewConnection(conn *SSEConnection) {
	t.mutex.Lock()
	t.metrics.ActiveConnections++
	t.metrics.TotalConnections++
	t.mutex.Unlock()

	t.logger.Debugf("New SSE connection: %s from %s", conn.ID, conn.ClientIP)

	// Create output data
	output := &Output{
		ConnectionID: conn.ID,
		ClientIP:     conn.ClientIP,
		UserAgent:    conn.UserAgent,
		Headers:      conn.Headers,
		QueryParams:  conn.QueryParams,
		Topic:        conn.Topic,
		LastEventID:  conn.LastEventID,
		Timestamp:    time.Now().Format(time.RFC3339),
	}

	// Trigger handlers for each registered handler
	for _, handler := range t.handlers {
		// Get handler settings
		handlerSettings := &HandlerSettings{}
		if err := metadata.MapToStruct(handler.Settings(), handlerSettings, true); err != nil {
			t.logger.Errorf("Failed to map handler settings: %v", err)
			continue
		}

		// Check if topic matches (if specified)
		if handlerSettings.Topic != "" && handlerSettings.Topic != conn.Topic {
			continue
		}

		// Execute handler asynchronously
		go func(h trigger.Handler, out *Output) {
			defer func() {
				if r := recover(); r != nil {
					t.logger.Errorf("Handler panic: %v", r)
				}
			}()

			// Execute the handler directly with the data
			_, err := h.Handle(context.Background(), out.ToMap())
			if err != nil {
				t.logger.Errorf("Handler execution error: %v", err)
			}
		}(handler, output)
	}

	// Set up connection close callback
	conn.SetCloseCallback(func(connID string) {
		t.mutex.Lock()
		t.metrics.ActiveConnections--
		t.mutex.Unlock()
		t.logger.Debugf("SSE connection closed: %s", connID)
	})
}

// GetMetrics returns current trigger metrics
func (t *Trigger) GetMetrics() *Metrics {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	return &Metrics{
		ActiveConnections: t.metrics.ActiveConnections,
		TotalConnections:  t.metrics.TotalConnections,
		EventsSent:        t.metrics.EventsSent,
		EventsBuffered:    t.metrics.EventsBuffered,
		BytesSent:         t.metrics.BytesSent,
		ErrorCount:        t.metrics.ErrorCount,
	}
}

// SendEvent sends an event to all connected clients
func (t *Trigger) SendEvent(event *SSEEvent) error {
	if t.server == nil {
		return fmt.Errorf("server not initialized")
	}

	return t.server.BroadcastEvent(event)
}

// SendEventToTopic sends an event to clients subscribed to a specific topic
func (t *Trigger) SendEventToTopic(topic string, event *SSEEvent) error {
	if t.server == nil {
		return fmt.Errorf("server not initialized")
	}

	return t.server.BroadcastEventToTopic(topic, event)
}

// SendEventToConnection sends an event to a specific connection
func (t *Trigger) SendEventToConnection(connectionID string, event *SSEEvent) error {
	if t.server == nil {
		return fmt.Errorf("server not initialized")
	}

	return t.server.SendEventToConnection(connectionID, event)
}

// GetActiveConnections returns information about active connections
func (t *Trigger) GetActiveConnections() []*ConnectionInfo {
	if t.server == nil {
		return nil
	}

	return t.server.GetActiveConnections()
}

// CloseConnection closes a specific connection
func (t *Trigger) CloseConnection(connectionID string) error {
	if t.server == nil {
		return fmt.Errorf("server not initialized")
	}

	return t.server.CloseConnection(connectionID)
}

// HealthCheck returns the health status of the trigger
func (t *Trigger) HealthCheck() map[string]interface{} {
	status := map[string]interface{}{
		"status": "healthy",
		"port":   t.settings.Port,
		"path":   t.settings.Path,
	}

	if t.server != nil {
		metrics := t.GetMetrics()
		status["activeConnections"] = metrics.ActiveConnections
		status["totalConnections"] = metrics.TotalConnections
		status["eventsSent"] = metrics.EventsSent
	}

	return status
}
