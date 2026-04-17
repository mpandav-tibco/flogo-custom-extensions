// Package join — store_redis.go
// redisStore uses Redis as the single authoritative backing store.
//
// Why Redis fixes all three limitations:
//  1. Cross-instance sharing: every Flogo instance hits the same Redis keys.
//     A contribution from instance A is immediately visible to instance B.
//  2. Restart recovery: Redis keys survive process restarts (within their TTL).
//     Contributions already in Redis are available to the new process without
//     any explicit Save/Load — state is always there.
//  3. Rebalance handoff: contributions stored in Redis are automatically
//     available to whichever instance takes over a partition after a rebalance.
//     No Setup/Cleanup action is needed — Redis is always the truth.
//
// Atomicity:
//
//	Contribute uses a Lua script that atomically reads the current entry,
//	appends the new contribution, checks for completeness, and writes back —
//	all in a single Redis round-trip.  This prevents split-brain between
//	concurrent instances.
//
// Timeout sweep:
//
//	SweepExpired uses SCAN to find matching keys and a second Lua script to
//	atomically mark-and-return expired entries.  Only the instance that wins
//	the atomic expire fires the timeout handler — no duplicate timeout events.
//
// Key format:  join:<consumerGroup>:<joinKey>
// TTL:         joinWindowMs × 3  (Redis auto-deletes long-stale entries)
package join

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/project-flogo/core/support/log"
	"github.com/redis/go-redis/v9"
)

// contribute is the Lua script that atomically records a topic contribution
// and returns all contributions when the join is complete.
//
// Returns:
//
//	nil / redis.Nil  → contribution recorded, join still incomplete
//	"CLOSED"         → entry was already completed/timed-out; caller starts fresh
//	JSON string      → all contributions merged; join is now complete
var contributeScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if raw == false then
    local entry = {contributions={}, createdAt=ARGV[3], closed=false}
    entry.contributions[ARGV[1]] = cjson.decode(ARGV[2])
    redis.call('SET', KEYS[1], cjson.encode(entry), 'PX', ARGV[4])
    return false
end
local entry = cjson.decode(raw)
if entry.closed then return 'CLOSED' end
entry.contributions[ARGV[1]] = cjson.decode(ARGV[2])
local count = 0
for _ in pairs(entry.contributions) do count = count + 1 end
if count >= tonumber(ARGV[5]) then
    entry.closed = true
    redis.call('SET', KEYS[1], cjson.encode(entry), 'PX', ARGV[4])
    return cjson.encode(entry.contributions)
end
redis.call('SET', KEYS[1], cjson.encode(entry), 'PX', ARGV[4])
return false
`)

// expireScript atomically marks an entry as timed-out if its age exceeds the
// window, returning the partial contributions so the caller can fire the
// timeout handler.
//
// Returns:
//
//	false        → not expired yet (or already gone)
//	"CLOSED"     → already completed/timed-out by another instance
//	JSON string  → partial contributions; entry is now marked closed
var expireScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if raw == false then return false end
local entry = cjson.decode(raw)
if entry.closed then return 'CLOSED' end
local ageMs = tonumber(ARGV[1]) - tonumber(entry.createdAt)
if ageMs < tonumber(ARGV[2]) then return false end
entry.closed = true
redis.call('SET', KEYS[1], cjson.encode(entry), 'PX', ARGV[3])
return cjson.encode(entry)
`)

// redisStore implements JoinStore using Redis as the single authoritative store.
type redisStore struct {
	client        *redis.Client
	consumerGroup string // used as key namespace prefix
	totalTopics   int
	ttlMs         int64 // = joinWindowMs * 3
}

// RedisOptions holds the connection parameters for the Redis store.
type RedisOptions struct {
	Addr     string // host:port, e.g. "localhost:6379"
	Password string // empty = no auth
	DB       int    // 0 = default DB
}

func newRedisStore(opts RedisOptions, consumerGroup string, totalTopics int, joinWindowMs int64) (*redisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
	})
	// Verify connectivity.
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("kafka-stream/join-trigger[redis-store]: ping failed addr=%q: %w", opts.Addr, err)
	}
	return &redisStore{
		client:        client,
		consumerGroup: consumerGroup,
		totalTopics:   totalTopics,
		ttlMs:         joinWindowMs * 3,
	}, nil
}

func (s *redisStore) key(joinKey string) string {
	return fmt.Sprintf("join:%s:%s", s.consumerGroup, joinKey)
}

// Contribute implements JoinStore using a Lua script for atomic read-modify-write.
// If the existing entry is already CLOSED (completed or timed out by another
// instance), the stale entry is deleted and the script is retried up to
// maxContributeRetries times — avoiding the unbounded recursion that a naive
// recursive retry would introduce under heavy contention.
func (s *redisStore) Contribute(joinKey, topic string, payload map[string]interface{}, now time.Time) (map[string]map[string]interface{}, bool, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, false, fmt.Errorf("kafka-stream/join-trigger[redis-store]: marshal payload: %w", err)
	}

	key := s.key(joinKey)
	const maxContributeRetries = 3
	ctx := context.Background()

	for attempt := 0; attempt < maxContributeRetries; attempt++ {
		result, scriptErr := contributeScript.Run(
			ctx, s.client, []string{key},
			topic,
			string(payloadJSON),
			now.UnixMilli(),
			s.ttlMs,
			s.totalTopics,
		).Text()

		if scriptErr == redis.Nil || result == "" {
			// Incomplete — contribution recorded; join still waiting for other topics.
			return nil, false, nil
		}
		if scriptErr != nil {
			return nil, false, fmt.Errorf("kafka-stream/join-trigger[redis-store]: contribute script: %w", scriptErr)
		}
		if result == "CLOSED" {
			// Entry already completed/timed-out by another instance.
			// Delete the stale closed key and retry so this contribution opens a
			// fresh window (same semantics as memoryStore's closed-entry handling).
			_ = s.client.Del(ctx, key).Err()
			continue
		}

		// Complete — result is JSON of all contributions.
		var allContribs map[string]map[string]interface{}
		if err := json.Unmarshal([]byte(result), &allContribs); err != nil {
			return nil, false, fmt.Errorf("kafka-stream/join-trigger[redis-store]: decode join result: %w", err)
		}
		// Remove the completed entry from Redis immediately.
		_ = s.client.Del(ctx, key).Err()
		return allContribs, true, nil
	}

	// Exhausted retries under extreme contention — treat as incomplete so the
	// offset is committed and the message is not redelivered indefinitely.
	return nil, false, nil
}

// SweepExpired scans Redis for expired join entries and fires onExpired for each.
// The atomic expire Lua script guarantees only one instance fires the timeout
// handler for a given join key — preventing duplicate timeout events.
func (s *redisStore) SweepExpired(now time.Time, deadline time.Duration, onExpired func(string, *persistedEntry)) {
	ctx := context.Background()
	pattern := fmt.Sprintf("join:%s:*", s.consumerGroup)
	prefixLen := len(fmt.Sprintf("join:%s:", s.consumerGroup))

	var cursor uint64
	for {
		keys, nextCursor, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			break
		}
		for _, redisKey := range keys {
			result, err := expireScript.Run(ctx, s.client, []string{redisKey},
				now.UnixMilli(),
				deadline.Milliseconds(),
				s.ttlMs,
			).Text()
			if err != nil || result == "false" || result == "" || result == "CLOSED" {
				continue
			}
			// result is JSON of the entry including createdAt and contributions.
			var raw struct {
				Contributions map[string]map[string]interface{} `json:"contributions"`
				CreatedAt     int64                             `json:"createdAt"`
			}
			if err := json.Unmarshal([]byte(result), &raw); err != nil {
				continue
			}
			joinKey := redisKey[prefixLen:]
			createdAt := time.UnixMilli(raw.CreatedAt)
			onExpired(joinKey, &persistedEntry{
				Contributions: raw.Contributions,
				CreatedAt:     createdAt,
			})
			// Clean up from Redis after timeout handler is dispatched.
			_ = s.client.Del(ctx, redisKey).Err()
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
}

// Snapshot returns all non-closed in-flight entries by scanning Redis.
func (s *redisStore) Snapshot() map[string]*persistedEntry {
	ctx := context.Background()
	out := make(map[string]*persistedEntry)
	pattern := fmt.Sprintf("join:%s:*", s.consumerGroup)
	prefixLen := len(fmt.Sprintf("join:%s:", s.consumerGroup))

	var cursor uint64
	for {
		keys, nextCursor, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			break
		}
		for _, redisKey := range keys {
			raw, err := s.client.Get(ctx, redisKey).Bytes()
			if err != nil {
				continue
			}
			var entry struct {
				Contributions map[string]map[string]interface{} `json:"contributions"`
				CreatedAt     int64                             `json:"createdAt"`
				Closed        bool                              `json:"closed"`
			}
			if err := json.Unmarshal(raw, &entry); err != nil || entry.Closed {
				continue
			}
			joinKey := redisKey[prefixLen:]
			out[joinKey] = &persistedEntry{
				Contributions: entry.Contributions,
				CreatedAt:     time.UnixMilli(entry.CreatedAt),
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return out
}

// Restore loads entries into Redis. Used when switching from a file/memory store
// to Redis, or when seeding Redis from a disk snapshot.
func (s *redisStore) Restore(entries map[string]*persistedEntry) {
	ctx := context.Background()
	for joinKey, pe := range entries {
		entry := map[string]interface{}{
			"contributions": pe.Contributions,
			"createdAt":     pe.CreatedAt.UnixMilli(),
			"closed":        false,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		_ = s.client.Set(ctx, s.key(joinKey), data, time.Duration(s.ttlMs)*time.Millisecond).Err()
	}
}

// Save and Load are no-ops for the Redis store: state lives in Redis persistently.
// The rebalance handoff and restart recovery are automatic — no action needed.
func (s *redisStore) Save(logger log.Logger) error {
	logger.Debugf("kafka-stream/join-trigger[redis-store]: Save() called — no-op (state is in Redis)")
	return nil
}

func (s *redisStore) Load(logger log.Logger) error {
	logger.Debugf("kafka-stream/join-trigger[redis-store]: Load() called — no-op (state is in Redis)")
	return nil
}

func (s *redisStore) Close() error {
	return s.client.Close()
}

// rawStore, rawLoad, rawDelete operate directly on Redis (used in tests + handoff).
func (s *redisStore) rawStore(key string, entry *joinEntry) {
	entry.mu.Lock()
	contrib := make(map[string]map[string]interface{}, len(entry.contributions))
	for t, p := range entry.contributions {
		contrib[t] = p
	}
	closed := entry.closed
	createdAt := entry.createdAt
	entry.mu.Unlock()

	raw := map[string]interface{}{
		"contributions": contrib,
		"createdAt":     createdAt.UnixMilli(),
		"closed":        closed,
	}
	data, _ := json.Marshal(raw)
	_ = s.client.Set(context.Background(), s.key(key), data, time.Duration(s.ttlMs)*time.Millisecond).Err()
}

func (s *redisStore) rawDelete(key string) {
	_ = s.client.Del(context.Background(), s.key(key)).Err()
}

func (s *redisStore) rawLoad(key string) (*joinEntry, bool) {
	data, err := s.client.Get(context.Background(), s.key(key)).Bytes()
	if err != nil {
		return nil, false
	}
	var raw struct {
		Contributions map[string]map[string]interface{} `json:"contributions"`
		CreatedAt     int64                             `json:"createdAt"`
		Closed        bool                              `json:"closed"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false
	}
	e := &joinEntry{
		contributions: raw.Contributions,
		createdAt:     time.UnixMilli(raw.CreatedAt),
		closed:        raw.Closed,
	}
	return e, true
}

