package join

import (
	"fmt"
	"testing"
	"time"

	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// newJoinTrigger builds a Trigger ready for processPayload unit-tests.
// No Kafka client is initialised — topics must be set manually.
func newJoinTrigger(s *Settings) *Trigger {
	t := &Trigger{
		settings: s,
		logger:   log.RootLogger(),
		topics:   s.TopicList(),
	}
	t.store = newMemoryStore(len(t.topics), int(s.MaxKeys))
	return t
}

// ─── validateSettings ────────────────────────────────────────────────────────

func TestValidateSettings_Valid(t *testing.T) {
	s := &Settings{
		Topics:        "orders,payments",
		ConsumerGroup: "test-cg",
		JoinKeyField:  "order_id",
		JoinWindowMs:  5000,
	}
	require.NoError(t, validateSettings(s))
}

func TestValidateSettings_ThreeTopics(t *testing.T) {
	s := &Settings{
		Topics:        "a,b,c",
		ConsumerGroup: "cg",
		JoinKeyField:  "id",
		JoinWindowMs:  1000,
	}
	require.NoError(t, validateSettings(s))
}

func TestValidateSettings_SingleTopic_Rejected(t *testing.T) {
	s := &Settings{
		Topics:        "orders",
		ConsumerGroup: "cg",
		JoinKeyField:  "id",
		JoinWindowMs:  1000,
	}
	assert.ErrorContains(t, validateSettings(s), "at least 2")
}

func TestValidateSettings_EmptyTopics(t *testing.T) {
	s := &Settings{ConsumerGroup: "cg", JoinKeyField: "id", JoinWindowMs: 1000}
	assert.ErrorContains(t, validateSettings(s), "topics")
}

func TestValidateSettings_DuplicateTopic(t *testing.T) {
	s := &Settings{
		Topics:        "orders,orders",
		ConsumerGroup: "cg",
		JoinKeyField:  "id",
		JoinWindowMs:  1000,
	}
	assert.ErrorContains(t, validateSettings(s), "duplicate")
}

func TestValidateSettings_MissingConsumerGroup(t *testing.T) {
	s := &Settings{Topics: "a,b", JoinKeyField: "id", JoinWindowMs: 1000}
	assert.ErrorContains(t, validateSettings(s), "consumerGroup")
}

func TestValidateSettings_MissingJoinKeyField(t *testing.T) {
	s := &Settings{Topics: "a,b", ConsumerGroup: "cg", JoinWindowMs: 1000}
	assert.ErrorContains(t, validateSettings(s), "joinKeyField")
}

func TestValidateSettings_ZeroJoinWindow(t *testing.T) {
	s := &Settings{Topics: "a,b", ConsumerGroup: "cg", JoinKeyField: "id", JoinWindowMs: 0}
	assert.ErrorContains(t, validateSettings(s), "joinWindowMs")
}

// ─── sanitizeGroupSuffix ─────────────────────────────────────────────────────

func TestSanitizeGroupSuffix_Clean(t *testing.T) {
	assert.Equal(t, "my-topic_1.data", sanitizeGroupSuffix("my-topic_1.data"))
}

func TestSanitizeGroupSuffix_InvalidChars(t *testing.T) {
	assert.Equal(t, "orders-v2--test", sanitizeGroupSuffix("orders/v2::test"))
}

// ─── Settings.TopicList ───────────────────────────────────────────────────────

func TestTopicList_TwoTopics(t *testing.T) {
	s := &Settings{Topics: "orders, payments"}
	got := s.TopicList()
	require.Equal(t, []string{"orders", "payments"}, got)
}

func TestTopicList_TrailingComma(t *testing.T) {
	s := &Settings{Topics: "a,b,"}
	got := s.TopicList()
	require.Equal(t, []string{"a", "b"}, got)
}

// ─── processPayload — two-topic join ─────────────────────────────────────────

func TestProcessPayload_TwoTopics_JoinComplete(t *testing.T) {
	trig := newJoinTrigger(&Settings{
		Topics:        "orders,payments",
		ConsumerGroup: "cg",
		JoinKeyField:  "order_id",
		JoinWindowMs:  10000,
	})

	// First side — orders arrives first; join is not yet complete.
	ordersMsg := map[string]interface{}{"order_id": "O1", "amount": 99.9}
	out, et, err := trig.processPayload("orders", ordersMsg)
	require.NoError(t, err)
	assert.Empty(t, et, "join should not complete after only one topic")
	assert.Nil(t, out)

	// Second side — payments arrives; join should complete.
	paymentsMsg := map[string]interface{}{"order_id": "O1", "status": "paid"}
	out, et, err = trig.processPayload("payments", paymentsMsg)
	require.NoError(t, err)
	require.Equal(t, EventTypeJoined, et)
	require.NotNil(t, out)

	assert.Equal(t, EventTypeJoined, out.EventType)
	assert.Equal(t, "O1", out.JoinResult.JoinKey)
	assert.Equal(t, []string{"orders", "payments"}, out.JoinResult.Topics)
	assert.NotZero(t, out.JoinResult.JoinedAt)

	// Verify merged messages carry the correct payloads.
	require.Contains(t, out.JoinResult.Messages, "orders")
	require.Contains(t, out.JoinResult.Messages, "payments")
	ordersMerged := out.JoinResult.Messages["orders"].(map[string]interface{})
	assert.Equal(t, 99.9, ordersMerged["amount"])
	paymentsMerged := out.JoinResult.Messages["payments"].(map[string]interface{})
	assert.Equal(t, "paid", paymentsMerged["status"])
}

func TestProcessPayload_TwoTopics_ReverseOrder(t *testing.T) {
	trig := newJoinTrigger(&Settings{
		Topics:        "orders,payments",
		ConsumerGroup: "cg",
		JoinKeyField:  "order_id",
		JoinWindowMs:  10000,
	})

	// payments arrives first this time.
	out, et, err := trig.processPayload("payments", map[string]interface{}{"order_id": "O2", "status": "authorised"})
	require.NoError(t, err)
	assert.Empty(t, et)
	assert.Nil(t, out)

	out, et, err = trig.processPayload("orders", map[string]interface{}{"order_id": "O2", "amount": 50.0})
	require.NoError(t, err)
	require.Equal(t, EventTypeJoined, et)
	require.NotNil(t, out)
	assert.Equal(t, "O2", out.JoinResult.JoinKey)
}

func TestProcessPayload_ThreeTopics_JoinComplete(t *testing.T) {
	trig := newJoinTrigger(&Settings{
		Topics:        "a,b,c",
		ConsumerGroup: "cg",
		JoinKeyField:  "id",
		JoinWindowMs:  10000,
	})

	out, et, err := trig.processPayload("a", map[string]interface{}{"id": "X", "va": 1})
	require.NoError(t, err)
	assert.Empty(t, et)

	out, et, err = trig.processPayload("b", map[string]interface{}{"id": "X", "vb": 2})
	require.NoError(t, err)
	assert.Empty(t, et)
	assert.Nil(t, out)

	out, et, err = trig.processPayload("c", map[string]interface{}{"id": "X", "vc": 3})
	require.NoError(t, err)
	require.Equal(t, EventTypeJoined, et)
	require.NotNil(t, out)
	assert.Equal(t, "X", out.JoinResult.JoinKey)
	assert.Len(t, out.JoinResult.Messages, 3)
}

func TestProcessPayload_TwoKeys_Independent(t *testing.T) {
	trig := newJoinTrigger(&Settings{
		Topics:        "orders,payments",
		ConsumerGroup: "cg",
		JoinKeyField:  "order_id",
		JoinWindowMs:  10000,
	})

	// Key K1 — orders side.
	_, _, _ = trig.processPayload("orders", map[string]interface{}{"order_id": "K1", "x": 1})
	// Key K2 — both sides arrive; K1 should still be pending.
	_, _, _ = trig.processPayload("orders", map[string]interface{}{"order_id": "K2", "x": 2})
	out, et, err := trig.processPayload("payments", map[string]interface{}{"order_id": "K2", "y": 20})
	require.NoError(t, err)
	require.Equal(t, EventTypeJoined, et)
	assert.Equal(t, "K2", out.JoinResult.JoinKey)

	// K1 is still in the store (pending orders side only — not yet joined).
	_, loaded := trig.store.rawLoad("K1")
	assert.True(t, loaded, "K1 join entry should still be pending")
}

func TestProcessPayload_MissingJoinKey_Error(t *testing.T) {
	trig := newJoinTrigger(&Settings{
		Topics:        "orders,payments",
		ConsumerGroup: "cg",
		JoinKeyField:  "order_id",
		JoinWindowMs:  10000,
	})
	_, _, err := trig.processPayload("orders", map[string]interface{}{"amount": 10})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "order_id")
}

func TestProcessPayload_EmptyJoinKey_Error(t *testing.T) {
	trig := newJoinTrigger(&Settings{
		Topics:        "orders,payments",
		ConsumerGroup: "cg",
		JoinKeyField:  "order_id",
		JoinWindowMs:  10000,
	})
	_, _, err := trig.processPayload("orders", map[string]interface{}{"order_id": "  "})
	assert.Error(t, err)
}

func TestProcessPayload_DuplicateContribution_LastWriteWins(t *testing.T) {
	trig := newJoinTrigger(&Settings{
		Topics:        "orders,payments",
		ConsumerGroup: "cg",
		JoinKeyField:  "id",
		JoinWindowMs:  10000,
	})

	// orders sends the same key twice; second value should win.
	_, _, _ = trig.processPayload("orders", map[string]interface{}{"id": "D1", "v": "first"})
	_, _, _ = trig.processPayload("orders", map[string]interface{}{"id": "D1", "v": "second"})
	// payments completes the join.
	out, et, err := trig.processPayload("payments", map[string]interface{}{"id": "D1", "p": "ok"})
	require.NoError(t, err)
	require.Equal(t, EventTypeJoined, et)
	ordersMerged := out.JoinResult.Messages["orders"].(map[string]interface{})
	assert.Equal(t, "second", ordersMerged["v"])
}

// ─── missingTopics ───────────────────────────────────────────────────────────

func TestMissingTopics_OneContributed(t *testing.T) {
	trig := newJoinTrigger(&Settings{
		Topics:        "orders,payments,shipments",
		ConsumerGroup: "cg",
		JoinKeyField:  "id",
		JoinWindowMs:  1000,
	})
	contributions := map[string]map[string]interface{}{"orders": nil}
	missing := trig.missingTopics(contributions)
	assert.ElementsMatch(t, []string{"payments", "shipments"}, missing)
}

func TestMissingTopics_AllContributed(t *testing.T) {
	trig := newJoinTrigger(&Settings{
		Topics:        "a,b",
		ConsumerGroup: "cg",
		JoinKeyField:  "id",
		JoinWindowMs:  1000,
	})
	contributions := map[string]map[string]interface{}{"a": nil, "b": nil}
	missing := trig.missingTopics(contributions)
	assert.Empty(t, missing)
}

// ─── Entry closed-flag prevents double-fire after timeout ────────────────────

func TestProcessPayload_ClosedEntry_StartsNewWindow(t *testing.T) {
	trig := newJoinTrigger(&Settings{
		Topics:        "orders,payments",
		ConsumerGroup: "cg",
		JoinKeyField:  "id",
		JoinWindowMs:  10000,
	})

	// Simulate a timed-out entry already in the store.
	timedOut := &joinEntry{
		contributions: make(map[string]map[string]interface{}),
		createdAt:     time.Now().Add(-30 * time.Second),
		closed:        true,
	}
	trig.store.rawStore("Z1", timedOut)

	// A new contribution for the same key should open a fresh window.
	out, et, err := trig.processPayload("orders", map[string]interface{}{"id": "Z1", "v": 1})
	require.NoError(t, err)
	assert.Empty(t, et) // fresh window, not yet complete
	assert.Nil(t, out)

	// Complete the join on the fresh window.
	out, et, err = trig.processPayload("payments", map[string]interface{}{"id": "Z1", "p": "ok"})
	require.NoError(t, err)
	require.Equal(t, EventTypeJoined, et)
	require.NotNil(t, out)
	assert.Equal(t, "Z1", out.JoinResult.JoinKey)
}

// ─── Output ToMap / FromMap round-trip ───────────────────────────────────────

func TestOutput_ToMap_JoinedRoundTrip(t *testing.T) {
	orig := &Output{
		JoinResult: JoinedMessage{
			Messages: map[string]interface{}{"orders": map[string]interface{}{"id": "O1"}},
			JoinKey:  "O1",
			Topics:   []string{"orders", "payments"},
			JoinedAt: 1700000000000,
		},
		EventType: EventTypeJoined,
	}
	m := orig.ToMap()
	restored := &Output{}
	require.NoError(t, restored.FromMap(m))
	assert.Equal(t, EventTypeJoined, restored.EventType)
	assert.Equal(t, "O1", restored.JoinResult.JoinKey)
}

func TestOutput_ToMap_TimeoutRoundTrip(t *testing.T) {
	orig := &Output{
		TimeoutResult: TimeoutResult{
			PartialMessages: map[string]interface{}{"orders": nil},
			JoinKey:         "T1",
			MissingTopics:   []string{"payments"},
			CreatedAt:       1700000001000,
		},
		EventType: EventTypeTimeout,
	}
	m := orig.ToMap()
	restored := &Output{}
	require.NoError(t, restored.FromMap(m))
	assert.Equal(t, EventTypeTimeout, restored.EventType)
	assert.Equal(t, "T1", restored.TimeoutResult.JoinKey)
}

// ─── Store cardinality limit ─────────────────────────────────────────────────

func TestMemoryStore_CardinalityLimit_Enforced(t *testing.T) {
	store := newMemoryStore(2, 2) // 2 topics, maxKeys=2
	now := time.Now()

	// First two keys succeed
	_, complete1, err1 := store.Contribute("key-1", "orders", map[string]interface{}{"v": 1}, now)
	assert.NoError(t, err1)
	assert.False(t, complete1)

	_, complete2, err2 := store.Contribute("key-2", "orders", map[string]interface{}{"v": 2}, now)
	assert.NoError(t, err2)
	assert.False(t, complete2)

	// Third key exceeds limit
	_, _, err3 := store.Contribute("key-3", "orders", map[string]interface{}{"v": 3}, now)
	assert.Error(t, err3)
	assert.Contains(t, err3.Error(), "cardinality limit")
}

func TestMemoryStore_CardinalityLimit_FreedAfterComplete(t *testing.T) {
	store := newMemoryStore(2, 1) // 2 topics, maxKeys=1
	now := time.Now()

	// First key succeeds
	_, _, err1 := store.Contribute("key-1", "orders", map[string]interface{}{"v": 1}, now)
	assert.NoError(t, err1)

	// Complete key-1 by contributing from second topic
	_, complete, err2 := store.Contribute("key-1", "payments", map[string]interface{}{"v": 2}, now)
	assert.NoError(t, err2)
	assert.True(t, complete)

	// Now a new key should be accepted (slot freed)
	_, _, err3 := store.Contribute("key-2", "orders", map[string]interface{}{"v": 3}, now)
	assert.NoError(t, err3)
}

func TestMemoryStore_CardinalityLimit_Zero_Unlimited(t *testing.T) {
	store := newMemoryStore(2, 0) // maxKeys=0 → unlimited
	now := time.Now()

	for i := 0; i < 100; i++ {
		_, _, err := store.Contribute(fmt.Sprintf("key-%d", i), "orders", map[string]interface{}{"v": i}, now)
		assert.NoError(t, err)
	}
}

// ─── Restore — keyCount synchronisation (M2 fix) ─────────────────────────────

func TestMemoryStore_Restore_UpdatesKeyCount(t *testing.T) {
	store := newMemoryStore(2, 10)
	now := time.Now()

	entries := map[string]*persistedEntry{
		"key-1": {Contributions: map[string]map[string]interface{}{"orders": {"v": 1}}, CreatedAt: now},
		"key-2": {Contributions: map[string]map[string]interface{}{"orders": {"v": 2}}, CreatedAt: now},
		"key-3": {Contributions: map[string]map[string]interface{}{"orders": {"v": 3}}, CreatedAt: now},
	}
	store.Restore(entries)

	// After restore, keyCount must reflect the number of restored entries.
	assert.Equal(t, int64(3), store.keyCount.Load(), "keyCount should equal number of restored entries")
}

func TestMemoryStore_Restore_KeyCount_EnforcedAfterRestore(t *testing.T) {
	store := newMemoryStore(2, 2) // maxKeys=2
	now := time.Now()

	entries := map[string]*persistedEntry{
		"key-1": {Contributions: map[string]map[string]interface{}{"orders": {"v": 1}}, CreatedAt: now},
		"key-2": {Contributions: map[string]map[string]interface{}{"orders": {"v": 2}}, CreatedAt: now},
	}
	store.Restore(entries)

	// Store is now at capacity (2/2). A third key should be rejected.
	_, _, err := store.Contribute("key-3", "orders", map[string]interface{}{"v": 3}, now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cardinality limit")
}

func TestMemoryStore_Restore_Idempotent(t *testing.T) {
	store := newMemoryStore(2, 10)
	now := time.Now()

	entries := map[string]*persistedEntry{
		"key-1": {Contributions: map[string]map[string]interface{}{"orders": {"v": 1}}, CreatedAt: now},
	}
	// Restore twice — duplicate keys should not double-count.
	store.Restore(entries)
	store.Restore(entries)

	assert.Equal(t, int64(1), store.keyCount.Load(), "idempotent restore should not double-count keyCount")
}

func TestMemoryStore_Restore_EmptyEntries(t *testing.T) {
	store := newMemoryStore(2, 5)
	store.Restore(map[string]*persistedEntry{})
	assert.Equal(t, int64(0), store.keyCount.Load(), "empty restore should leave keyCount at 0")
}

func TestMemoryStore_Restore_ThenComplete_FreesSlot(t *testing.T) {
	store := newMemoryStore(2, 1) // maxKeys=1
	now := time.Now()

	entries := map[string]*persistedEntry{
		"key-1": {Contributions: map[string]map[string]interface{}{"orders": {"v": 1}}, CreatedAt: now},
	}
	store.Restore(entries)

	// At capacity (1/1). Complete key-1 by adding second topic.
	_, complete, err := store.Contribute("key-1", "payments", map[string]interface{}{"v": 2}, now)
	require.NoError(t, err)
	assert.True(t, complete, "key-1 should complete with both topics")

	// Now slot is freed — a new key should be accepted.
	_, _, err2 := store.Contribute("key-2", "orders", map[string]interface{}{"v": 3}, now)
	assert.NoError(t, err2, "new key should be accepted after slot freed")
}

// ─── SweepExpired — timeout path ─────────────────────────────────────────────

// TestMemoryStore_SweepExpired_Fires verifies that expired entries are swept
// and the onExpired callback receives the partial contributions.
func TestMemoryStore_SweepExpired_Fires(t *testing.T) {
	store := newMemoryStore(2, 0)
	old := time.Now().Add(-2 * time.Second)

	store.rawStore("stale-key", &joinEntry{
		contributions: map[string]map[string]interface{}{
			"orders": {"id": "x"},
		},
		createdAt: old,
	})

	var swept []string
	store.SweepExpired(time.Now(), 1*time.Second, func(key string, pe *persistedEntry) {
		swept = append(swept, key)
	})
	assert.Contains(t, swept, "stale-key", "stale-key should be swept after deadline")

	// Entry should be deleted from the store.
	_, loaded := store.rawLoad("stale-key")
	assert.False(t, loaded, "stale-key should be removed after sweep")
}

// TestMemoryStore_SweepExpired_FreshEntryNotSwept verifies that a fresh entry
// (age < deadline) is not swept.
func TestMemoryStore_SweepExpired_FreshEntryNotSwept(t *testing.T) {
	store := newMemoryStore(2, 0)
	store.rawStore("fresh-key", &joinEntry{
		contributions: map[string]map[string]interface{}{"orders": {"id": "y"}},
		createdAt:     time.Now(),
	})

	var swept []string
	store.SweepExpired(time.Now(), 5*time.Second, func(key string, _ *persistedEntry) {
		swept = append(swept, key)
	})
	assert.NotContains(t, swept, "fresh-key")
}

// TestProcessPayload_JoinedAt_Reasonable verifies JoinedAt is a plausible Unix-ms value.
func TestProcessPayload_JoinedAt_Reasonable(t *testing.T) {
	s := &Settings{
		Topics:        "orders,payments",
		ConsumerGroup: "test-cg",
		JoinKeyField:  "id",
		JoinWindowMs:  5000,
	}
	trig := newJoinTrigger(s)
	before := time.Now().UnixMilli()
	trig.processPayload("orders", map[string]interface{}{"id": "k1", "data": "o"})
	out, et, err := trig.processPayload("payments", map[string]interface{}{"id": "k1", "data": "p"})
	after := time.Now().UnixMilli()
	require.NoError(t, err)
	require.Equal(t, EventTypeJoined, et)
	require.NotNil(t, out)
	assert.GreaterOrEqual(t, out.JoinResult.JoinedAt, before)
	assert.LessOrEqual(t, out.JoinResult.JoinedAt, after)
}

// TestOutput_FromMap_MissingJoinResult verifies that a map with no joinResult
// key does not error (optional field).
func TestOutput_FromMap_MissingJoinResult(t *testing.T) {
	m := map[string]interface{}{
		"eventType": EventTypeTimeout,
	}
	var o Output
	err := o.FromMap(m)
	require.NoError(t, err)
	assert.Equal(t, EventTypeTimeout, o.EventType)
}

// TestTopicList_SpacesAreStripped verifies leading/trailing spaces in topic list are handled.
func TestTopicList_SpacesAreStripped(t *testing.T) {
	s := &Settings{Topics: " orders , payments "}
	list := s.TopicList()
	require.Len(t, list, 2)
	assert.Equal(t, "orders", list[0])
	assert.Equal(t, "payments", list[1])
}

// TestSanitizeGroupSuffix_AllSpecialChars verifies that a string of all
// non-alphanumeric characters collapses to "-".
func TestSanitizeGroupSuffix_AllSpecialChars(t *testing.T) {
	result := sanitizeGroupSuffix("!@#$%")
	// All chars are non-word chars → replaced by '-'; leading/trailing '-' may be trimmed.
	assert.NotContains(t, result, "!", "special chars should be replaced")
}
