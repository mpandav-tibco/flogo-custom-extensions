// Package join — store_file.go
// fileStore wraps memoryStore and adds JSON snapshot persistence.
// Graceful-restart recovery: Stop() saves, Start() restores.
// Rebalance-aware handoff (single-instance): Cleanup() saves, Setup() restores.
package join

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/project-flogo/core/support/log"
)

// fileStore extends memoryStore with JSON snapshot persistence.
// It satisfies JoinStore and delegates all store operations to the embedded
// memoryStore.  Save() and Load() are the only methods that differ.
//
// Snapshot lifecycle:
//   - Stop()                    → Save()  (graceful shutdown)
//   - Start()                   → Load()  (process startup)
//   - consumerGroupHandler.Cleanup() → Save()  (before partition reassignment)
//   - consumerGroupHandler.Setup()   → Load()  (after partition reassignment)
//
// For multi-instance deployments the snapshot file must reside on a shared
// filesystem (e.g. NFS, EFS, Azure Files) that all instances can write/read.
// For single-instance deployments a local path is sufficient.
type fileStore struct {
	*memoryStore
	path string
}

func newFileStore(path string, totalTopics int) *fileStore {
	return &fileStore{memoryStore: newMemoryStore(totalTopics), path: path}
}

// Save writes a JSON snapshot of all in-flight (non-closed) join entries to
// PersistPath using an atomic write-then-rename to prevent partial files.
func (s *fileStore) Save(logger log.Logger) error {
	snap := s.Snapshot()
	logger.Debugf("kafka-stream/join-trigger[file-store]: saving snapshot — path=%q in-flight=%d", s.path, len(snap))

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("kafka-stream/join-trigger[file-store]: create snapshot dir: %w", err)
	}

	// Write to a temp file in the same directory so Rename is atomic on POSIX.
	f, err := os.CreateTemp(filepath.Dir(s.path), ".join-snap-*.tmp")
	if err != nil {
		return fmt.Errorf("kafka-stream/join-trigger[file-store]: create temp file: %w", err)
	}
	tmpPath := f.Name()

	if err := json.NewEncoder(f).Encode(snap); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("kafka-stream/join-trigger[file-store]: encode snapshot: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("kafka-stream/join-trigger[file-store]: close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("kafka-stream/join-trigger[file-store]: commit snapshot: %w", err)
	}
	logger.Infof("kafka-stream/join-trigger[file-store]: snapshot saved — path=%q entries=%d", s.path, len(snap))
	return nil
}

// Load reads the snapshot file and restores entries into the in-memory store.
// A missing file is treated as a clean start and is not an error.
// All restored entries are valid join windows — the timeout sweep will evict
// any that have already exceeded joinWindowMs since their createdAt.
func (s *fileStore) Load(logger log.Logger) error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		logger.Debugf("kafka-stream/join-trigger[file-store]: no snapshot at %q — starting fresh", s.path)
		return nil
	}
	if err != nil {
		return fmt.Errorf("kafka-stream/join-trigger[file-store]: read snapshot: %w", err)
	}

	var entries map[string]*persistedEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("kafka-stream/join-trigger[file-store]: decode snapshot: %w", err)
	}

	s.Restore(entries)
	logger.Infof("kafka-stream/join-trigger[file-store]: snapshot restored — path=%q entries=%d", s.path, len(entries))
	return nil
}
