package sse

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/project-flogo/core/support/log"
)

// SSEServerConfig represents the SSE server configuration
type SSEServerConfig struct {
	Port              int
	Path              string
	MaxConnections    int
	EnableCORS        bool
	CORSOrigins       string
	KeepAliveInterval time.Duration
	EnableEventStore  bool
	EventStoreSize    int
	EventTTL          time.Duration
	Logger            log.Logger
}

// SSEServer represents the SSE HTTP server
type SSEServer struct {
	config             *SSEServerConfig
	logger             log.Logger
	server             *http.Server
	connections        sync.Map
	eventStore         *EventStore
	connectionCallback func(*SSEConnection)
	metrics            *ServerMetrics
	ctx                context.Context
	cancel             context.CancelFunc
}

// ServerMetrics represents server-level metrics
type ServerMetrics struct {
	mutex            sync.RWMutex
	connectionsCount int64
	eventsCount      int64
	bytesTransferred int64
}

// NewSSEServer creates a new SSE server
func NewSSEServer(config *SSEServerConfig) (*SSEServer, error) {
	if config == nil {
		return nil, fmt.Errorf("server configuration cannot be nil")
	}

	// Enterprise validation checks before server creation
	if err := ValidatePortRange(config.Port); err != nil {
		return nil, fmt.Errorf("server creation failed: %v", err)
	}

	if err := ValidateSSEPath(config.Path); err != nil {
		return nil, fmt.Errorf("server creation failed: %v", err)
	}

	if err := ValidateMaxConnections(config.MaxConnections); err != nil {
		return nil, fmt.Errorf("server creation failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	server := &SSEServer{
		config:  config,
		logger:  config.Logger,
		ctx:     ctx,
		cancel:  cancel,
		metrics: &ServerMetrics{},
	}

	if config.EnableEventStore {
		server.eventStore = NewEventStore(config.EventStoreSize, config.EventTTL, config.Logger)
	}

	return server, nil
}

// Start starts the SSE server
func (s *SSEServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register SSE endpoint
	mux.HandleFunc(s.config.Path, s.handleSSEConnection)

	// Register health check endpoint
	mux.HandleFunc(s.config.Path+"/health", s.handleHealthCheck)

	// Register metrics endpoint (if enabled)
	mux.HandleFunc(s.config.Path+"/metrics", s.handleMetrics)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.config.Port),
		Handler: mux,
	}

	// Start keep-alive routine
	go s.keepAliveRoutine()

	// Start server in goroutine
	go func() {
		s.logger.Infof("Starting SSE server on :%d%s", s.config.Port, s.config.Path)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("SSE server error: %v", err)
		}
	}()

	// Wait for context cancellation
	go func() {
		<-ctx.Done()
		s.Stop()
	}()

	return nil
}

// Stop stops the SSE server
func (s *SSEServer) Stop() error {
	s.logger.Info("Stopping SSE server")

	if s.cancel != nil {
		s.cancel()
	}

	// Close all connections
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*SSEConnection); ok {
			conn.Close()
		}
		return true
	})

	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(ctx)
	}

	return nil
}

// SetConnectionCallback sets the callback for new connections
func (s *SSEServer) SetConnectionCallback(callback func(*SSEConnection)) {
	s.connectionCallback = callback
}

// handleSSEConnection handles incoming SSE connection requests
func (s *SSEServer) handleSSEConnection(w http.ResponseWriter, r *http.Request) {
	// Check if this is a valid SSE request
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check connection limit
	currentConnections := s.getConnectionCount()
	if currentConnections >= int64(s.config.MaxConnections) {
		s.logger.Warnf("Connection limit exceeded: %d/%d connections, rejecting new connection from %s", 
			currentConnections, s.config.MaxConnections, r.RemoteAddr)
		http.Error(w, "Connection limit exceeded", http.StatusServiceUnavailable)
		return
	}
	s.logger.Debugf("Accepting new connection: %d/%d connections used", currentConnections+1, s.config.MaxConnections)

	// Enable CORS if configured
	if s.config.EnableCORS {
		requestOrigin := r.Header.Get("Origin")
		s.logger.Debugf("CORS enabled, processing origin: %s", requestOrigin)
		s.setCORSHeaders(w, r)
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Check if the connection supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Create SSE connection
	conn := NewSSEConnection(w, r, flusher, s.logger)
	s.logger.Infof("New SSE connection established: id=%s, ip=%s, lastEventID=%s", 
		conn.ID, conn.ClientIP, conn.LastEventID)

	// Store connection
	s.connections.Store(conn.ID, conn)
	s.incrementConnectionCount()
	s.logger.Debugf("Connection stored, total active connections: %d", s.getConnectionCount())

	// Set close callback
	conn.SetCloseCallback(func(connID string) {
		s.connections.Delete(connID)
		s.decrementConnectionCount()
		s.logger.Debugf("Connection %s closed, remaining connections: %d", connID, s.getConnectionCount())
	})

	// Call connection callback if set
	if s.connectionCallback != nil {
		s.connectionCallback(conn)
	}

	// Handle event replay if requested
	if s.config.EnableEventStore && conn.LastEventID != "" {
		s.logger.Debugf("Replaying events for connection %s from lastEventID: %s", conn.ID, conn.LastEventID)
		s.replayEvents(conn)
	}

	// Keep connection alive until context is cancelled or client disconnects
	<-conn.Done()
}

// handleHealthCheck handles health check requests
func (s *SSEServer) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Simple JSON response showing server health
	fmt.Fprintf(w, `{"status":"healthy","activeConnections":%d}`, s.getConnectionCount())
}

// handleMetrics handles metrics requests
func (s *SSEServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	s.metrics.mutex.RLock()
	defer s.metrics.mutex.RUnlock()

	fmt.Fprintf(w, `{"connections":%d,"events":%d,"bytes":%d}`,
		s.metrics.connectionsCount,
		s.metrics.eventsCount,
		s.metrics.bytesTransferred)
}

// setCORSHeaders sets CORS headers
func (s *SSEServer) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origins := s.config.CORSOrigins
	requestOrigin := r.Header.Get("Origin")
	
	if origins == "*" {
		s.logger.Debugf("CORS: allowing all origins (*) for request from %s", requestOrigin)
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		// Parse origins and check if request origin is allowed
		allowedOrigins := strings.Split(origins, ",")
		originAllowed := false
		for _, origin := range allowedOrigins {
			if strings.TrimSpace(origin) == requestOrigin {
				s.logger.Debugf("CORS: origin %s allowed", requestOrigin)
				w.Header().Set("Access-Control-Allow-Origin", requestOrigin)
				originAllowed = true
				break
			}
		}
		if !originAllowed && requestOrigin != "" {
			s.logger.Warnf("CORS: origin %s not in allowed list: %s", requestOrigin, origins)
		}
	}

	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")
	w.Header().Set("Access-Control-Allow-Methods", "GET")
}

// keepAliveRoutine sends periodic keep-alive messages
func (s *SSEServer) keepAliveRoutine() {
	ticker := time.NewTicker(s.config.KeepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.sendKeepAlive()
		}
	}
}

// sendKeepAlive sends keep-alive message to all connections
func (s *SSEServer) sendKeepAlive() {
	activeConnections := s.getConnectionCount()
	if activeConnections == 0 {
		return // No connections to send keep-alive to
	}

	s.logger.Debugf("Sending keep-alive to %d active connections", activeConnections)
	event := &SSEEvent{
		Event: "keep-alive",
		Data:  "ping",
	}

	sentCount := 0
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*SSEConnection); ok {
			if err := conn.SendEvent(event); err == nil {
				sentCount++
			}
		}
		return true
	})
	
	if sentCount < int(activeConnections) {
		s.logger.Debugf("Keep-alive sent to %d/%d connections (some may have disconnected)", sentCount, activeConnections)
	}
}

// BroadcastEvent broadcasts an event to all connections
func (s *SSEServer) BroadcastEvent(event *SSEEvent) error {
	activeConnections := s.getConnectionCount()
	s.logger.Debugf("Broadcasting event (id=%s, type=%s) to %d connections", 
		event.ID, event.Event, activeConnections)

	// Store event if event store is enabled
	if s.eventStore != nil {
		s.eventStore.AddEvent(event)
	}

	// Broadcast to all connections
	sentCount := 0
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*SSEConnection); ok {
			if err := conn.SendEvent(event); err != nil {
				s.logger.Errorf("Failed to send event to connection %s: %v", conn.ID, err)
			} else {
				sentCount++
			}
		}
		return true
	})

	s.logger.Debugf("Broadcast complete: sent to %d/%d connections", sentCount, activeConnections)
	s.incrementEventCount()
	return nil
}

// BroadcastEventToTopic broadcasts an event to connections subscribed to a topic
func (s *SSEServer) BroadcastEventToTopic(topic string, event *SSEEvent) error {
	s.logger.Debugf("Broadcasting event (id=%s, type=%s) to topic: %s", 
		event.ID, event.Event, topic)

	if s.eventStore != nil {
		s.eventStore.AddEvent(event)
	}

	sentCount := 0
	totalConnections := 0
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*SSEConnection); ok {
			totalConnections++
			if conn.Topic == topic || conn.Topic == "" {
				if err := conn.SendEvent(event); err != nil {
					s.logger.Errorf("Failed to send event to connection %s: %v", conn.ID, err)
				} else {
					sentCount++
				}
			}
		}
		return true
	})

	s.logger.Debugf("Topic broadcast complete: sent to %d matching connections out of %d total", 
		sentCount, totalConnections)
	s.incrementEventCount()
	return nil
}

// SendEventToConnection sends an event to a specific connection
func (s *SSEServer) SendEventToConnection(connectionID string, event *SSEEvent) error {
	s.logger.Debugf("Sending event (id=%s, type=%s) to specific connection: %s", 
		event.ID, event.Event, connectionID)

	if conn, ok := s.connections.Load(connectionID); ok {
		if sseConn, ok := conn.(*SSEConnection); ok {
			if err := sseConn.SendEvent(event); err != nil {
				s.logger.Errorf("Failed to send event to connection %s: %v", connectionID, err)
				return err
			}
			s.logger.Debugf("Event successfully sent to connection %s", connectionID)
			return nil
		}
	}
	
	// List available connections for debugging
	availableConnections := make([]string, 0)
	s.connections.Range(func(key, value interface{}) bool {
		if connID, ok := key.(string); ok {
			availableConnections = append(availableConnections, connID)
		}
		return true
	})
	
	s.logger.Warnf("Connection %s not found. Available connections: %v", connectionID, availableConnections)
	return fmt.Errorf("connection not found: %s", connectionID)
}

// GetActiveConnections returns information about active connections
func (s *SSEServer) GetActiveConnections() []*ConnectionInfo {
	var connections []*ConnectionInfo

	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*SSEConnection); ok {
			connections = append(connections, &ConnectionInfo{
				ID:          conn.ID,
				ClientIP:    conn.ClientIP,
				UserAgent:   conn.UserAgent,
				Topic:       conn.Topic,
				LastEventID: conn.LastEventID,
				ConnectedAt: conn.ConnectedAt.Format(time.RFC3339),
				IsActive:    conn.IsActive(),
			})
		}
		return true
	})

	return connections
}

// CloseConnection closes a specific connection
func (s *SSEServer) CloseConnection(connectionID string) error {
	if conn, ok := s.connections.Load(connectionID); ok {
		if sseConn, ok := conn.(*SSEConnection); ok {
			sseConn.Close()
			return nil
		}
	}
	return fmt.Errorf("connection not found: %s", connectionID)
}

// replayEvents replays stored events to a connection
func (s *SSEServer) replayEvents(conn *SSEConnection) {
	if s.eventStore == nil {
		s.logger.Debugf("Event store not enabled, skipping replay for connection %s", conn.ID)
		return
	}

	events := s.eventStore.GetEventsSince(conn.LastEventID)
	s.logger.Debugf("Replaying %d events to connection %s", len(events), conn.ID)
	
	replayedCount := 0
	for _, event := range events {
		if err := conn.SendEvent(event); err != nil {
			s.logger.Errorf("Failed to replay event to connection %s: %v", conn.ID, err)
			break
		}
		replayedCount++
	}
	
	if replayedCount > 0 {
		s.logger.Infof("Successfully replayed %d events to connection %s", replayedCount, conn.ID)
	}
}

// Helper methods for metrics
func (s *SSEServer) getConnectionCount() int64 {
	s.metrics.mutex.RLock()
	defer s.metrics.mutex.RUnlock()
	return s.metrics.connectionsCount
}

func (s *SSEServer) incrementConnectionCount() {
	s.metrics.mutex.Lock()
	defer s.metrics.mutex.Unlock()
	s.metrics.connectionsCount++
}

func (s *SSEServer) decrementConnectionCount() {
	s.metrics.mutex.Lock()
	defer s.metrics.mutex.Unlock()
	s.metrics.connectionsCount--
}

func (s *SSEServer) incrementEventCount() {
	s.metrics.mutex.Lock()
	defer s.metrics.mutex.Unlock()
	s.metrics.eventsCount++
}
