// Package join — store.go
// Defines the JoinStore interface and the default in-memory implementation.
// Additional backends (file, Redis) each live in their own source files.
package join

import (
	"sync"
	"time"

	"github.com/project-flogo/core/support/log"
)

// Store type selectors (used in Settings.StoreType).
const (
	// StoreTypeMemory is the default: process-local sync.Map.
	// No cross-instance sharing; state is lost on process restart.
	StoreTypeMemory = "memory"

	// StoreTypeFile persists in-flight join state to a JSON snapshot file on
	// graceful shutdown and before each consumer rebalance. On startup and after
	// rebalance the snapshot is restored. Enables single-process restart recovery.
	// Requires Settings.PersistPath to be set.
	StoreTypeFile = "file"

	// StoreTypeRedis uses Redis as the authoritative backing store.
	// All Contribute and SweepExpired operations hit Redis, making the state
	// visible to every Flogo instance in the same consumer group and automatically
	// surviving restarts and partition rebalances without extra configuration.
	// Requires Settings.RedisAddr.
	StoreTypeRedis = "redis"
)

// JoinStore is the backing store abstraction used by the join trigger.
//
// Memory store  — fast, single-process; default.
// File store    — adds graceful-restart snapshot to disk; no new dependencies.
// Redis store   — full cross-instance sharing + rebalance handoff via Redis.
//
// All implementations are safe for concurrent use.
type JoinStore interface {
	// Contribute atomically records topic's payload for joinKey.
	// Returns (allContributions, true, nil) when every configured topic has
	// contributed within the window — the entry is simultaneously removed.
	// Returns (nil, false, nil) when the join is still incomplete.
	// If the existing entry for joinKey was already closed (completed or timed
	// out by the sweep), Contribute starts a fresh join window and records this
	// payload as the first contribution in that new window.
	Contribute(joinKey, topic string, payload map[string]interface{}, now time.Time) (allContribs map[string]map[string]interface{}, complete bool, err error)

	// SweepExpired walks all in-flight entries and calls onExpired for each
	// whose age (now − createdAt) exceeds deadline. Expired entries are removed
	// from the store before onExpired is invoked.
	SweepExpired(now time.Time, deadline time.Duration, onExpired func(key string, partial *persistedEntry))

	// Snapshot returns all non-closed in-flight entries in serialisable form.
	// Used for disk/Redis persistence and the rebalance-handoff path.
	Snapshot() map[string]*persistedEntry

	// Restore loads previously persisted entries into the store.
	// Called before consumer goroutines start (startup or post-rebalance).
	Restore(entries map[string]*persistedEntry)

	// Save persists in-flight state to the durable backing medium.
	// Called on graceful shutdown and in consumerGroupHandler.Cleanup().
	// No-op for memoryStore.
	Save(logger log.Logger) error

	// Load restores in-flight state from the durable backing medium.
	// Called on startup and in consumerGroupHandler.Setup().
	// No-op for memoryStore.
	Load(logger log.Logger) error

	// Close releases resources held by the store (e.g. Redis connection pool).
	Close() error

	// rawStore unconditionally stores entry under key.
	// Used by tests and the rebalance-handoff path.
	rawStore(key string, entry *joinEntry)

	// rawLoad retrieves the entry for key. Used by tests.
	rawLoad(key string) (*joinEntry, bool)

	// rawDelete removes the entry for key. Used by tests and cleanup paths.
	rawDelete(key string)
}

// persistedEntry is the serialisable, lock-free representation of a joinEntry.
// It is used for disk snapshots and Redis serialisation.
type persistedEntry struct {
	Contributions map[string]map[string]interface{} `json:"contributions"`
	CreatedAt     time.Time                         `json:"createdAt"`
}

// toJoinEntry converts a persistedEntry back to a live, unlocked joinEntry.
func (pe *persistedEntry) toJoinEntry() *joinEntry {
	contrib := make(map[string]map[string]interface{}, len(pe.Contributions))
	for t, p := range pe.Contributions {
		contrib[t] = p
	}
	return &joinEntry{contributions: contrib, createdAt: pe.CreatedAt}
}

// ─────────────────────────────────────────────────────────────────────────────
// memoryStore — process-local sync.Map (StoreTypeMemory, default)
// ─────────────────────────────────────────────────────────────────────────────

type memoryStore struct {
	m           sync.Map
	totalTopics int
}

func newMemoryStore(totalTopics int) *memoryStore {
	return &memoryStore{totalTopics: totalTopics}
}

// Contribute implements JoinStore.
func (s *memoryStore) Contribute(joinKey, topic string, payload map[string]interface{}, now time.Time) (map[string]map[string]interface{}, bool, error) {
	actual, _ := s.m.LoadOrStore(joinKey, &joinEntry{
		contributions: make(map[string]map[string]interface{}),
		createdAt:     now,
	})
	entry := actual.(*joinEntry)

	entry.mu.Lock()
	if entry.closed {
		// Entry was already completed or timed out — open a fresh window.
		// Two goroutines may race here; both Store identical fresh entries and
		// only one contribution proceeds (last-write-wins for the Store call is
		// acceptable — no messages are lost, at most one is re-processed).
		entry.mu.Unlock()
		fresh := &joinEntry{
			contributions: make(map[string]map[string]interface{}),
			createdAt:     now,
		}
		s.m.Store(joinKey, fresh)
		entry = fresh
		entry.mu.Lock()
	}

	entry.contributions[topic] = payload
	complete := len(entry.contributions) == s.totalTopics
	if complete {
		entry.closed = true
	}

	var allContribs map[string]map[string]interface{}
	if complete {
		allContribs = make(map[string]map[string]interface{}, len(entry.contributions))
		for t, p := range entry.contributions {
			allContribs[t] = p
		}
	}
	entry.mu.Unlock()

	if complete {
		s.m.Delete(joinKey)
	}
	return allContribs, complete, nil
}

// SweepExpired implements JoinStore.
func (s *memoryStore) SweepExpired(now time.Time, deadline time.Duration, onExpired func(string, *persistedEntry)) {
	s.m.Range(func(rawKey, rawVal interface{}) bool {
		entry := rawVal.(*joinEntry)
		entry.mu.Lock()
		if entry.closed || now.Sub(entry.createdAt) < deadline {
			entry.mu.Unlock()
			return true
		}
		entry.closed = true
		partial := make(map[string]map[string]interface{}, len(entry.contributions))
		for t, p := range entry.contributions {
			partial[t] = p
		}
		createdAt := entry.createdAt
		entry.mu.Unlock()

		key := rawKey.(string)
		s.m.Delete(key)
		onExpired(key, &persistedEntry{Contributions: partial, CreatedAt: createdAt})
		return true
	})
}

// Snapshot implements JoinStore.
func (s *memoryStore) Snapshot() map[string]*persistedEntry {
	out := make(map[string]*persistedEntry)
	s.m.Range(func(k, v interface{}) bool {
		e := v.(*joinEntry)
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.closed {
			return true
		}
		contrib := make(map[string]map[string]interface{}, len(e.contributions))
		for t, p := range e.contributions {
			cp := make(map[string]interface{}, len(p))
			for pk, pv := range p {
				cp[pk] = pv
			}
			contrib[t] = cp
		}
		out[k.(string)] = &persistedEntry{Contributions: contrib, CreatedAt: e.createdAt}
		return true
	})
	return out
}

// Restore implements JoinStore.
func (s *memoryStore) Restore(entries map[string]*persistedEntry) {
	for key, pe := range entries {
		s.m.Store(key, pe.toJoinEntry())
	}
}

// Save/Load/Close are no-ops for the in-memory store.
func (s *memoryStore) Save(log.Logger) error { return nil }
func (s *memoryStore) Load(log.Logger) error { return nil }
func (s *memoryStore) Close() error          { return nil }

func (s *memoryStore) rawStore(key string, entry *joinEntry) { s.m.Store(key, entry) }
func (s *memoryStore) rawDelete(key string)                  { s.m.Delete(key) }
func (s *memoryStore) rawLoad(key string) (*joinEntry, bool) {
	v, ok := s.m.Load(key)
	if !ok {
		return nil, false
	}
	return v.(*joinEntry), true
}
