package vectordb

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "vectordb-connector")

var (
	clientRegistry = make(map[string]VectorDBClient)
	registryMu     sync.RWMutex
	// registrySealed is set by SealRegistry to prevent ResetRegistry from
	// accidentally clearing production state. Once sealed, cannot be unsealed.
	registrySealed atomic.Bool
)

// GetOrCreateClient returns an existing registered client by name, or creates and
// registers a new one using the provided ConnectionConfig. Thread-safe.
// If a client already exists under the given name the config is ignored (reuse wins).
// The context controls cancellation of the health-check retry loop; pass
// context.Background() when no deadline is required.
func GetOrCreateClient(ctx context.Context, name string, cfg ConnectionConfig) (VectorDBClient, error) {
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

	c, err := NewClient(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("vectordb: failed to create client %q: %w", name, err)
	}

	// Verify connectivity before registering.  We use a short probe timeout
	// (5 s) and retry up to 3 times with brief back-off so the connector is
	// resilient to brief startup delays (e.g. the DB container starting after
	// the app), while still failing fast for genuine misconfigurations.
	const (
		healthCheckTimeout  = 5 * time.Second
		healthCheckMaxTries = 3
		healthCheckBackoff  = 2 * time.Second
	)
	var hErr error
	for attempt := 1; attempt <= healthCheckMaxTries; attempt++ {
		hCtx, hCancel := context.WithTimeout(ctx, healthCheckTimeout)
		hErr = c.HealthCheck(hCtx)
		hCancel()
		if hErr == nil {
			break
		}
		if attempt < healthCheckMaxTries {
			logger.Warnf("VectorDB health check attempt %d/%d failed for %q: %v; retrying in %s",
				attempt, healthCheckMaxTries, name, hErr, healthCheckBackoff)
			select {
			case <-time.After(healthCheckBackoff):
			case <-ctx.Done():
				_ = c.Close()
				return nil, fmt.Errorf("vectordb: context cancelled during health check for client %q: %w", name, ctx.Err())
			}
		}
	}
	if hErr != nil {
		_ = c.Close()
		return nil, fmt.Errorf("vectordb: health check failed for client %q after %d attempts: %w",
			name, healthCheckMaxTries, hErr)
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

// ResetRegistry closes all registered clients and clears the registry.
// Intended for use in tests only — do not call in production code.
// Returns an error if the registry has been sealed by SealRegistry.
func ResetRegistry() error {
	if registrySealed.Load() {
		return fmt.Errorf("vectordb: ResetRegistry called on a sealed registry; " +
			"this is a programming error (SealRegistry was called in production code)")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	for name, c := range clientRegistry {
		if err := c.Close(); err != nil {
			logger.Warnf("VectorDB ResetRegistry: close error for ref=%s: %v", name, err)
		}
	}
	clientRegistry = make(map[string]VectorDBClient)
	return nil
}

// SealRegistry prevents future calls to ResetRegistry from succeeding.
// Call this once at application startup (after all connections are registered)
// to guard against accidental registry wipes in long-running processes.
// Sealing is permanent for the lifetime of the process.
func SealRegistry() {
	registrySealed.Store(true)
	logger.Infof("VectorDB registry sealed — ResetRegistry is disabled for this process lifetime")
}
