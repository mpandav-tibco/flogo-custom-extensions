// Package sse provides a Flogo trigger that starts an HTTP server for
// Server-Sent Events (SSE) streaming.  Clients connect to the configured
// path and receive real-time events pushed from the SSE Send activity.
// The trigger also maintains a global registry so the SSE Send activity can
// locate the running server instance at flow execution time.
package sse

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/trigger"
)

var triggerMd = trigger.NewMetadata(&Settings{}, &HandlerSettings{}, &Output{})

func init() {
	_ = trigger.Register(&Trigger{}, &Factory{})
}

// ── Shared types ─────────────────────────────────────────────────────────────

// SSEEventData is the data-transfer object exchanged between the SSE trigger
// and the SSE Send activity via the shared registry.
type SSEEventData struct {
	ID    string
	Event string
	Data  string
	Retry int
}

// ConnectionInfo describes an active SSE client connection.
type ConnectionInfo struct {
	ID       string
	Topic    string
	IsActive bool
}

// SSEServerInterface is the interface the SSE Send activity uses to dispatch events.
type SSEServerInterface interface {
	BroadcastEvent(event *SSEEventData) error
	BroadcastEventToTopic(topic string, event *SSEEventData) error
	SendEventToConnection(connectionID string, event *SSEEventData) error
	GetActiveConnections() []ConnectionInfo
}

// ── Global registry ───────────────────────────────────────────────────────────

var (
	registry   = make(map[string]SSEServerInterface)
	registryMu sync.RWMutex
)

// RegisterSSEServer registers an SSE server under the given name.
func RegisterSSEServer(name string, srv SSEServerInterface) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = srv
}

// GetSSEServer retrieves a registered SSE server by name.
func GetSSEServer(name string) (SSEServerInterface, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	srv, ok := registry[name]
	return srv, ok
}

// UnregisterSSEServer removes a server from the registry.
func UnregisterSSEServer(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, name)
}

// ListRegisteredServers returns the names of all currently registered servers.
func ListRegisteredServers() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// ── Internal connection ───────────────────────────────────────────────────────

type connection struct {
	id        string
	topic     string
	ch        chan *SSEEventData
	done      chan struct{}
	closeOnce sync.Once
}

func (c *connection) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

// ── Flogo trigger ─────────────────────────────────────────────────────────────

// Trigger is the SSE Flogo trigger.
type Trigger struct {
	settings *Settings
	handlers []trigger.Handler
	logger   log.Logger
	server   *http.Server
	mu       sync.RWMutex
	conns    map[string]*connection
	stopOnce sync.Once
	done     chan struct{}
}

// Factory creates Trigger instances.
type Factory struct{}

// Metadata returns the trigger factory metadata.
func (*Factory) Metadata() *trigger.Metadata { return triggerMd }

// New creates a new, uninitialised Trigger from the design-time config.
func (*Factory) New(config *trigger.Config) (trigger.Trigger, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(config.Settings, s, true); err != nil {
		return nil, fmt.Errorf("sse-trigger: failed to map settings: %w", err)
	}
	if s.Port == 0 {
		s.Port = 9998
	}
	if s.Path == "" {
		s.Path = "/events"
	}
	if s.HeartbeatInterval == 0 {
		s.HeartbeatInterval = 30
	}
	return &Trigger{
		settings: s,
		conns:    make(map[string]*connection),
		done:     make(chan struct{}),
	}, nil
}

// Metadata returns the trigger metadata.
func (t *Trigger) Metadata() *trigger.Metadata { return triggerMd }

// Initialize wires up the handlers from the design-time config.
func (t *Trigger) Initialize(ctx trigger.InitContext) error {
	t.logger = ctx.Logger()
	t.handlers = ctx.GetHandlers()
	return nil
}

// Start launches the HTTP/SSE server and registers the trigger in the global registry.
func (t *Trigger) Start() error {
	mux := http.NewServeMux()
	handler := http.HandlerFunc(t.handleSSE)
	if t.settings.CORSEnabled {
		mux.Handle(t.settings.Path, t.withCORS(handler))
	} else {
		mux.Handle(t.settings.Path, handler)
	}

	t.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", t.settings.Port),
		Handler: mux,
	}

	ln, err := net.Listen("tcp", t.server.Addr)
	if err != nil {
		return fmt.Errorf("sse-trigger: cannot listen on %s: %w", t.server.Addr, err)
	}

	// Register under "default" so the SSE Send activity can find it without
	// knowing the trigger instance name.
	RegisterSSEServer("default", t)

	go func() {
		t.logger.Infof("SSE trigger listening on port %d path %s", t.settings.Port, t.settings.Path)
		if err := t.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			t.logger.Errorf("SSE server error: %v", err)
		}
	}()

	go t.runHeartbeat()
	return nil
}

// Stop gracefully shuts down the HTTP server and removes the registry entry.
func (t *Trigger) Stop() error {
	t.stopOnce.Do(func() {
		close(t.done)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = t.server.Shutdown(ctx)
		UnregisterSSEServer("default")
		t.logger.Info("SSE trigger stopped")
	})
	return nil
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

func (t *Trigger) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Last-Event-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (t *Trigger) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	connID := fmt.Sprintf("conn_%d", time.Now().UnixNano())
	topic := r.URL.Query().Get("topic")
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = r.URL.Query().Get("lastEventId")
	}

	conn := &connection{
		id:    connID,
		topic: topic,
		ch:    make(chan *SSEEventData, 64),
		done:  make(chan struct{}),
	}

	t.mu.Lock()
	t.conns[connID] = conn
	t.mu.Unlock()

	defer func() {
		conn.close()
		t.mu.Lock()
		delete(t.conns, connID)
		t.mu.Unlock()
	}()

	// Fire the Flogo flow handlers for this new connection.
	go t.fireHandlers(r, connID, topic, lastEventID)

	bw := bufio.NewWriter(w)
	for {
		select {
		case evt, ok := <-conn.ch:
			if !ok {
				return
			}
			writeSSEEvent(bw, evt)
			bw.Flush()
			flusher.Flush()
		case <-conn.done:
			return
		case <-r.Context().Done():
			return
		case <-t.done:
			return
		}
	}
}

func writeSSEEvent(w *bufio.Writer, evt *SSEEventData) {
	if evt.ID != "" {
		fmt.Fprintf(w, "id: %s\n", evt.ID)
	}
	if evt.Event != "" && evt.Event != "message" {
		fmt.Fprintf(w, "event: %s\n", evt.Event)
	}
	if evt.Retry > 0 {
		fmt.Fprintf(w, "retry: %d\n", evt.Retry)
	}
	fmt.Fprintf(w, "data: %s\n\n", evt.Data)
}

func (t *Trigger) fireHandlers(r *http.Request, connID, topic, lastEventID string) {
	headers := make(map[string]interface{})
	for k, vs := range r.Header {
		if len(vs) == 1 {
			headers[k] = vs[0]
		} else {
			headers[k] = vs
		}
	}

	qp := make(map[string]interface{})
	for k, vs := range r.URL.Query() {
		if len(vs) == 1 {
			qp[k] = vs[0]
		} else {
			qp[k] = vs
		}
	}

	out := &Output{
		ConnectionID: connID,
		ClientIP:     r.RemoteAddr,
		UserAgent:    r.UserAgent(),
		Headers:      headers,
		QueryParams:  qp,
		Topic:        topic,
		LastEventID:  lastEventID,
		Timestamp:    time.Now().Format(time.RFC3339),
	}

	for _, h := range t.handlers {
		if _, err := h.Handle(context.Background(), out.ToMap()); err != nil {
			t.logger.Errorf("SSE handler error for connection %s: %v", connID, err)
		}
	}
}

func (t *Trigger) runHeartbeat() {
	if t.settings.HeartbeatInterval <= 0 {
		return
	}
	ticker := time.NewTicker(time.Duration(t.settings.HeartbeatInterval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = t.BroadcastEvent(&SSEEventData{Event: "heartbeat", Data: "ping"})
		case <-t.done:
			return
		}
	}
}

// ── SSEServerInterface implementation ────────────────────────────────────────

// BroadcastEvent sends an event to all active connections.
func (t *Trigger) BroadcastEvent(event *SSEEventData) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, conn := range t.conns {
		select {
		case conn.ch <- event:
		default:
			// Drop for slow consumers rather than blocking the caller.
		}
	}
	return nil
}

// BroadcastEventToTopic sends an event to connections subscribed to a topic
// (or to all connections that have no topic filter).
func (t *Trigger) BroadcastEventToTopic(topic string, event *SSEEventData) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, conn := range t.conns {
		if conn.topic == topic || conn.topic == "" {
			select {
			case conn.ch <- event:
			default:
			}
		}
	}
	return nil
}

// SendEventToConnection sends an event to a specific connection by ID.
func (t *Trigger) SendEventToConnection(connectionID string, event *SSEEventData) error {
	t.mu.RLock()
	conn, ok := t.conns[connectionID]
	t.mu.RUnlock()
	if !ok {
		return fmt.Errorf("sse-trigger: connection %q not found", connectionID)
	}
	select {
	case conn.ch <- event:
		return nil
	case <-conn.done:
		return fmt.Errorf("sse-trigger: connection %q is closed", connectionID)
	default:
		return fmt.Errorf("sse-trigger: send buffer full for connection %q", connectionID)
	}
}

// GetActiveConnections returns a snapshot of all active connections.
func (t *Trigger) GetActiveConnections() []ConnectionInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	infos := make([]ConnectionInfo, 0, len(t.conns))
	for _, conn := range t.conns {
		infos = append(infos, ConnectionInfo{
			ID:       conn.id,
			Topic:    conn.topic,
			IsActive: true,
		})
	}
	return infos
}
