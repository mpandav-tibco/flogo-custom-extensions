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

// SSEConnection represents an individual SSE connection
type SSEConnection struct {
	ID            string
	ClientIP      string
	UserAgent     string
	Headers       map[string]interface{}
	QueryParams   map[string]interface{}
	Topic         string
	LastEventID   string
	ConnectedAt   time.Time
	writer        http.ResponseWriter
	flusher       http.Flusher
	logger        log.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	mutex         sync.RWMutex
	active        bool
	closeCallback func(string)
}

// NewSSEConnection creates a new SSE connection
func NewSSEConnection(w http.ResponseWriter, r *http.Request, flusher http.Flusher, logger log.Logger) *SSEConnection {
	ctx, cancel := context.WithCancel(context.Background())

	// Generate unique connection ID
	connID := generateConnectionID()

	// Extract client information
	clientIP := getClientIP(r)
	userAgent := r.UserAgent()

	// Convert headers to map
	headers := make(map[string]interface{})
	for key, values := range r.Header {
		if len(values) == 1 {
			headers[key] = values[0]
		} else {
			headers[key] = values
		}
	}

	// Parse query parameters
	queryParams := make(map[string]interface{})
	for key, values := range r.URL.Query() {
		if len(values) == 1 {
			queryParams[key] = values[0]
		} else {
			queryParams[key] = values
		}
	}

	// Extract topic from query parameters
	topic := r.URL.Query().Get("topic")

	// Extract last event ID for replay
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = r.URL.Query().Get("lastEventId")
	}

	conn := &SSEConnection{
		ID:          connID,
		ClientIP:    clientIP,
		UserAgent:   userAgent,
		Headers:     headers,
		QueryParams: queryParams,
		Topic:       topic,
		LastEventID: lastEventID,
		ConnectedAt: time.Now(),
		writer:      w,
		flusher:     flusher,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		active:      true,
	}

	// Start connection monitoring
	go conn.monitor()

	return conn
}

// SendEvent sends an SSE event to the client
func (c *SSEConnection) SendEvent(event *SSEEvent) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.active {
		return fmt.Errorf("connection is closed")
	}

	// Format SSE event
	var eventStr strings.Builder

	if event.ID != "" {
		eventStr.WriteString(fmt.Sprintf("id: %s\n", event.ID))
	}

	if event.Event != "" {
		eventStr.WriteString(fmt.Sprintf("event: %s\n", event.Event))
	}

	if event.Retry > 0 {
		eventStr.WriteString(fmt.Sprintf("retry: %d\n", event.Retry))
	}

	// Handle multi-line data
	dataLines := strings.Split(event.Data, "\n")
	for _, line := range dataLines {
		eventStr.WriteString(fmt.Sprintf("data: %s\n", line))
	}

	eventStr.WriteString("\n") // End of event

	// Send event
	if _, err := c.writer.Write([]byte(eventStr.String())); err != nil {
		c.logger.Errorf("Failed to write event to connection %s: %v", c.ID, err)
		c.close()
		return err
	}

	// Flush to ensure immediate delivery
	c.flusher.Flush()

	return nil
}

// Close closes the connection
func (c *SSEConnection) Close() {
	c.close()
}

// close internal method to close the connection
func (c *SSEConnection) close() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.active {
		return
	}

	c.active = false
	c.cancel()

	if c.closeCallback != nil {
		c.closeCallback(c.ID)
	}
}

// IsActive returns whether the connection is active
func (c *SSEConnection) IsActive() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.active
}

// Done returns a channel that's closed when the connection is closed
func (c *SSEConnection) Done() <-chan struct{} {
	return c.ctx.Done()
}

// SetCloseCallback sets the callback function called when connection closes
func (c *SSEConnection) SetCloseCallback(callback func(string)) {
	c.closeCallback = callback
}

// monitor monitors the connection for client disconnection
func (c *SSEConnection) monitor() {
	// Send initial connection event
	welcomeEvent := &SSEEvent{
		ID:    generateEventID(),
		Event: "connected",
		Data:  fmt.Sprintf(`{"connectionId":"%s","timestamp":"%s"}`, c.ID, c.ConnectedAt.Format(time.RFC3339)),
	}

	if err := c.SendEvent(welcomeEvent); err != nil {
		c.logger.Errorf("Failed to send welcome event: %v", err)
		c.close()
		return
	}

	// Monitor for context cancellation or connection errors
	<-c.ctx.Done()
	c.close()
}

// Helper functions

// generateConnectionID generates a unique connection ID
func generateConnectionID() string {
	return fmt.Sprintf("conn_%d_%d", time.Now().UnixNano(), getRandomInt())
}

// generateEventID generates a unique event ID
func generateEventID() string {
	return fmt.Sprintf("evt_%d_%d", time.Now().UnixNano(), getRandomInt())
}

// getClientIP extracts the client IP address from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// Take the first IP in the list
		ips := strings.Split(forwarded, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}

	return ip
}

// getRandomInt returns a random integer (simplified implementation)
func getRandomInt() int64 {
	return time.Now().UnixNano() % 1000000
}
