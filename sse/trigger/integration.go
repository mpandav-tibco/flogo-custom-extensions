package sse

import (
	sharedsse "github.com/milindpandav/flogo-extensions/sse"
)

// Global registry for SSE servers - now uses shared registry

// RegisterSSEServerGlobal registers an SSE server with the shared global registry
func RegisterSSEServerGlobal(name string, server sharedsse.SSEServerInterface) {
	sharedsse.RegisterSSEServer(name, server)
}

// GetSSEServerGlobal retrieves an SSE server from the shared global registry
func GetSSEServerGlobal(name string) (sharedsse.SSEServerInterface, bool) {
	return sharedsse.GetSSEServer(name)
}

// UnregisterSSEServerGlobal removes an SSE server from the shared global registry
func UnregisterSSEServerGlobal(name string) {
	sharedsse.UnregisterSSEServer(name)
}

// SSEServerAdapter adapts our SSE server to the interface expected by SSE Send activity
type SSEServerAdapter struct {
	server *SSEServer
}

// BroadcastEvent sends event to all clients
func (a *SSEServerAdapter) BroadcastEvent(event *sharedsse.SSEEventData) error {
	sseEvent := &SSEEvent{
		ID:    event.ID,
		Event: event.Event,
		Data:  event.Data,
		Retry: event.Retry,
	}
	return a.server.BroadcastEvent(sseEvent)
}

// BroadcastEventToTopic sends event to topic subscribers
func (a *SSEServerAdapter) BroadcastEventToTopic(topic string, event *sharedsse.SSEEventData) error {
	sseEvent := &SSEEvent{
		ID:    event.ID,
		Event: event.Event,
		Data:  event.Data,
		Retry: event.Retry,
	}
	return a.server.BroadcastEventToTopic(topic, sseEvent)
}

// SendEventToConnection sends event to specific connection
func (a *SSEServerAdapter) SendEventToConnection(connectionID string, event *sharedsse.SSEEventData) error {
	sseEvent := &SSEEvent{
		ID:    event.ID,
		Event: event.Event,
		Data:  event.Data,
		Retry: event.Retry,
	}
	return a.server.SendEventToConnection(connectionID, sseEvent)
}

// GetActiveConnections returns active connection info
func (a *SSEServerAdapter) GetActiveConnections() []sharedsse.ConnectionInfo {
	connections := a.server.GetActiveConnections()
	result := make([]sharedsse.ConnectionInfo, len(connections))

	for i, conn := range connections {
		result[i] = sharedsse.ConnectionInfo{
			ID:       conn.ID,
			Topic:    conn.Topic,
			IsActive: conn.IsActive,
		}
	}

	return result
}

// SetupActivityIntegration configures the activity to use this trigger's registry
func SetupActivityIntegration() {
	// Integration is now handled through the shared registry
	// No additional setup needed
}
