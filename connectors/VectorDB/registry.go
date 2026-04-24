package vectordb

import (
	"fmt"
	"sync"

	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "vectordb-connector")

var (
	clientRegistry = make(map[string]VectorDBClient)
	registryMu     sync.RWMutex
)

// GetOrCreateClient returns an existing registered client by name, or creates and
// registers a new one using the provided ConnectionConfig. Thread-safe.
// If a client already exists under the given name the config is ignored (reuse wins).
func GetOrCreateClient(name string, cfg ConnectionConfig) (VectorDBClient, error) {
	// Fast path: already registered
	registryMu.RLock()
	if c, ok := clientRegistry[name]; ok {
		registryMu.RUnlock()
		return c, nil
	}
	registryMu.RUnlock()

	// Slow path: create + register (double-checked locking)
	registryMu.Lock()
	defer registryMu.Unlock()

	if c, ok := clientRegistry[name]; ok {
		return c, nil
	}

	c, err := NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("vectordb: failed to create client %q: %w", name, err)
	}

	clientRegistry[name] = c
	logger.Infof("VectorDB client registered: ref=%s provider=%s host=%s:%d", name, cfg.DBType, cfg.Host, cfg.Port)
	return c, nil
}

// GetClient retrieves a registered client by name. Returns an error if not found.
func GetClient(name string) (VectorDBClient, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	c, ok := clientRegistry[name]
	if !ok {
		return nil, newError(ErrCodeClientNotFound,
			fmt.Sprintf("no VectorDB client registered with connectionRef %q", name), nil)
	}
	return c, nil
}

// DeregisterClient removes a client from the registry and calls Close() on it.
func DeregisterClient(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if c, ok := clientRegistry[name]; ok {
		if err := c.Close(); err != nil {
			logger.Warnf("VectorDB client close error during deregister: ref=%s err=%v", name, err)
		}
		delete(clientRegistry, name)
		logger.Infof("VectorDB client deregistered: ref=%s", name)
	}
}
