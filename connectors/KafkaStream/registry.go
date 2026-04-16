package kafkastream

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/milindpandav/flogo-extensions/kafkastream/window"
)

// Global window store registry — one entry per named window instance.
// Mirrors the SSE server registry pattern used in this workspace.
var (
	windowRegistry  = make(map[string]window.WindowStore)
	pendingRestores = make(map[string]window.PersistedWindowState)
	registryMutex   sync.RWMutex
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
	// Apply any state that was restored from disk before this keyed store existed.
	if pending, hasPending := pendingRestores[cfg.Name]; hasPending {
		newStore.LoadState(pending)
		delete(pendingRestores, cfg.Name)
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

// SweepIdleFor is the scoped variant of SweepIdle — it only considers windows
// whose name exactly matches prefix (unkeyed window) or starts with prefix+":"
// (keyed sub-windows). This prevents a trigger's idle sweep goroutine from
// consuming idle results that belong to a different aggregate trigger instance
// running in the same process.
func SweepIdleFor(prefix string) []*window.WindowResult {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	var results []*window.WindowResult
	for name, store := range windowRegistry {
		if name != prefix && !strings.HasPrefix(name, prefix+":") {
			continue
		}
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

// ---------------------------------------------------------------------------
// State persistence
// ---------------------------------------------------------------------------

// persistedRegistry is the gob-encoded envelope written to disk.
type persistedRegistry struct {
	States []window.PersistedWindowState
}

// SaveStateTo serialises all registered window stores to a gob file at path.
// Creates parent directories if needed. Atomic write (temp file + rename).
func SaveStateTo(path string) error {
	registryMutex.RLock()
	states := make([]window.PersistedWindowState, 0, len(windowRegistry))
	for _, store := range windowRegistry {
		states = append(states, store.SaveState())
	}
	registryMutex.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("kafka-stream/persist: cannot create directory: %w", err)
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(persistedRegistry{States: states}); err != nil {
		return fmt.Errorf("kafka-stream/persist: encode error: %w", err)
	}

	// Atomic write: write to .tmp, rename to target.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("kafka-stream/persist: write error: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("kafka-stream/persist: rename error: %w", err)
	}
	return nil
}

// RestoreStateFrom reads a gob file written by SaveStateTo and calls LoadState
// on any matching registered window store (matched by name). States for stores
// that do not yet exist (keyed sub-windows created lazily) are held in
// pendingRestores and applied the moment that store is first created.
func RestoreStateFrom(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // first run — nothing to restore
	}
	if err != nil {
		return fmt.Errorf("kafka-stream/persist: read error: %w", err)
	}

	var reg persistedRegistry
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&reg); err != nil {
		return fmt.Errorf("kafka-stream/persist: decode error: %w", err)
	}

	registryMutex.Lock()
	defer registryMutex.Unlock()
	for _, state := range reg.States {
		if store, exists := windowRegistry[state.Name]; exists {
			// Store already registered (base window) — apply immediately.
			store.LoadState(state)
		} else {
			// Keyed sub-window not yet created — park state for lazy apply.
			pendingRestores[state.Name] = state
		}
	}
	return nil
}

// persistCounter is incremented on every window Add. Used by the aggregate
// activity to trigger periodic snapshots without a lock.
var persistCounter atomic.Int64

// IncrPersistCounter bumps the global message counter and returns the new value.
func IncrPersistCounter() int64 {
	return persistCounter.Add(1)
}
