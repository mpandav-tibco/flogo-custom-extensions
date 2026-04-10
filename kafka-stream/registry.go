package kafkastream

import (
	"fmt"
	"sync"

	"github.com/milindpandav/flogo-extensions/kafka-stream/window"
)

// Global window store registry — one entry per named window instance.
// Mirrors the SSE server registry pattern used in this workspace.
var (
	windowRegistry = make(map[string]window.WindowStore)
	registryMutex  sync.RWMutex
)

// GetOrCreateWindowStore retrieves an existing window store by name, or creates and
// registers a new one from the provided config. Thread-safe via double-checked locking.
// When MaxKeys > 0 it enforces a cardinality limit on the total registry size.
func GetOrCreateWindowStore(cfg window.WindowConfig) (window.WindowStore, error) {
	// Fast path: read lock
	registryMutex.RLock()
	store, exists := windowRegistry[cfg.Name]
	registryMutex.RUnlock()
	if exists {
		return store, nil
	}

	// Slow path: write lock + double-check
	registryMutex.Lock()
	defer registryMutex.Unlock()
	if store, exists = windowRegistry[cfg.Name]; exists {
		return store, nil
	}

	// Cardinality enforcement
	if cfg.MaxKeys > 0 && int64(len(windowRegistry)) >= cfg.MaxKeys {
		return nil, fmt.Errorf("kafka-stream: max keyed windows (%d) reached — cannot create %q", cfg.MaxKeys, cfg.Name)
	}

	newStore, err := newWindowStore(cfg)
	if err != nil {
		return nil, err
	}
	windowRegistry[cfg.Name] = newStore
	return newStore, nil
}

// GetWindowStore retrieves a registered window store by name.
// Returns (store, true) if found, (nil, false) if not.
func GetWindowStore(name string) (window.WindowStore, bool) {
	registryMutex.RLock()
	defer registryMutex.RUnlock()
	store, exists := windowRegistry[name]
	return store, exists
}

// UnregisterWindowStore removes a window store from the registry.
func UnregisterWindowStore(name string) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	delete(windowRegistry, name)
}

// ListSnapshots returns an observability snapshot for every registered window.
// Safe to call from health endpoints without taking a write lock.
func ListSnapshots() []window.WindowSnapshot {
	registryMutex.RLock()
	defer registryMutex.RUnlock()
	snaps := make([]window.WindowSnapshot, 0, len(windowRegistry))
	for _, store := range windowRegistry {
		snaps = append(snaps, store.Snapshot())
	}
	return snaps
}

// SweepIdle checks every registered window for idle timeout and returns any
// partial results for windows that have been closed due to inactivity.
// The caller is responsible for processing returned results (e.g. forwarding to
// downstream activities). Idle-closed stores are removed from the registry.
func SweepIdle() []*window.WindowResult {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	var results []*window.WindowResult
	for name, store := range windowRegistry {
		if result, closed := store.CheckIdle(); closed {
			results = append(results, result)
			delete(windowRegistry, name)
		}
	}
	return results
}

// newWindowStore is the internal factory for WindowStore implementations.
func newWindowStore(cfg window.WindowConfig) (window.WindowStore, error) {
	switch cfg.Type {
	case window.WindowTumblingTime:
		return window.NewTumblingTimeWindow(cfg), nil
	case window.WindowTumblingCount:
		return window.NewTumblingCountWindow(cfg), nil
	case window.WindowSlidingTime:
		return window.NewSlidingTimeWindow(cfg), nil
	case window.WindowSlidingCount:
		return window.NewSlidingCountWindow(cfg), nil
	default:
		return nil, fmt.Errorf("kafka-stream: unknown window type %q", cfg.Type)
	}
}
