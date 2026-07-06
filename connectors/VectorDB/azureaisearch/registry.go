package vectordb

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "azureaisearch-connector")

var (
	clientRegistry = make(map[string]VectorDBClient)
	registryMu     sync.RWMutex
	registrySealed atomic.Bool
)

func GetOrCreateClient(ctx context.Context, name string, cfg ConnectionConfig) (VectorDBClient, error) {
	registryMu.RLock()
	if c, ok := clientRegistry[name]; ok {
		registryMu.RUnlock()
		return c, nil
	}
	registryMu.RUnlock()

	registryMu.Lock()
	defer registryMu.Unlock()

	if c, ok := clientRegistry[name]; ok {
		return c, nil
	}

	c, err := NewClient(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("vectordb: failed to create client %q: %w", name, err)
	}

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
			logger.Warnf("Azure AI Search health check attempt %d/%d failed for %q: %v; retrying in %s",
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
	logger.Infof("VectorDB client registered: ref=%s provider=%s endpoint=%s", name, cfg.DBType, cfg.Endpoint)
	return c, nil
}

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

func ResetRegistry() error {
	if registrySealed.Load() {
		return fmt.Errorf("vectordb: ResetRegistry called on a sealed registry")
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

func SealRegistry() {
	registrySealed.Store(true)
	logger.Infof("VectorDB registry sealed — ResetRegistry is disabled for this process lifetime")
}
