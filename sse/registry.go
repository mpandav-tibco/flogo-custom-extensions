package sse

import (
	"sync"
)

// SSEEventData represents an SSE event for sending
type SSEEventData struct {
	ID    string `json:"id,omitempty"`
	Event string `json:"event,omitempty"`
	Data  string `json:"data"`
	Retry int    `json:"retry,omitempty"`
}

// ConnectionInfo represents connection information
type ConnectionInfo struct {
	ID       string
	Topic    string
	IsActive bool
}

// SSEServerInterface defines the interface that SSE servers must implement
type SSEServerInterface interface {
	BroadcastEvent(event *SSEEventData) error
	BroadcastEventToTopic(topic string, event *SSEEventData) error
	SendEventToConnection(connectionID string, event *SSEEventData) error
	GetActiveConnections() []ConnectionInfo
}

// Global registry for SSE servers
var (
	serverRegistry = make(map[string]SSEServerInterface)
	registryMutex  sync.RWMutex
)

// RegisterSSEServer registers an SSE server with the global registry
func RegisterSSEServer(name string, server SSEServerInterface) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	serverRegistry[name] = server
}

// GetSSEServer retrieves an SSE server from the global registry
func GetSSEServer(name string) (SSEServerInterface, bool) {
	registryMutex.RLock()
	defer registryMutex.RUnlock()
	server, exists := serverRegistry[name]
	return server, exists
}

// UnregisterSSEServer removes an SSE server from the global registry
func UnregisterSSEServer(name string) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	delete(serverRegistry, name)
}

// ListRegisteredServers returns a list of all registered server names
func ListRegisteredServers() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	names := make([]string, 0, len(serverRegistry))
	for name := range serverRegistry {
		names = append(names, name)
	}
	return names
}
