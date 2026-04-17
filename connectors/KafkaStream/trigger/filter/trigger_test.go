package filter

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newTrigger(s *Settings) *Trigger {
	return &Trigger{settings: s}
}

func eval(t *testing.T, trig *Trigger, hs *HandlerSettings, msg map[string]interface{}) (bool, string, string) {
	t.Helper()
	h := &handler{hs: hs}
	// Mirror the Initialize() logic: parse and cache predicates so that tests
	// using the eval() shortcut exercise the same startup-time validation.
	if hs.Predicates != "" {
		preds, err := hs.ParsedPredicates()
		if err != nil {
			return false, "", fmt.Sprintf("invalid predicates JSON: %v", err)
		}
		h.parsedPreds = preds
	}
	return trig.evaluate(msg, h)
}

// ─── validateSettings ────────────────────────────────────────────────────────

func TestValidateSettings_Valid(t *testing.T) {
	require.NoError(t, validateSettings(&Settings{Topic: "t", ConsumerGroup: "g"}))
}

func TestValidateSettings_MissingTopic(t *testing.T) {
	err := validateSettings(&Settings{ConsumerGroup: "g"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "topic")
}

func TestValidateSettings_MissingConsumerGroup(t *testing.T) {
	err := validateSettings(&Settings{Topic: "t"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "consumerGroup")
}

// ─── evalSingle ──────────────────────────────────────────────────────────────

func TestEvalSingle_Eq_Numeric_Pass(t *testing.T) {
	ok, _, errMsg := evalSingle(map[string]interface{}{"status": 200.0}, "status", "eq", "200", false, nil)
	assert.True(t, ok)
	assert.Empty(t, errMsg)
}

func TestEvalSingle_Eq_Numeric_Fail(t *testing.T) {
	ok, reason, errMsg := evalSingle(map[string]interface{}{"status": 404.0}, "status", "eq", "200", false, nil)
	assert.False(t, ok)
	assert.NotEmpty(t, reason)
	assert.Empty(t, errMsg)
}

func TestEvalSingle_Neq_Pass(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"code": 200.0}, "code", "neq", "404", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Neq_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"code": 404.0}, "code", "neq", "404", false, nil)
	assert.False(t, ok)
}

func TestEvalSingle_Gt_Pass(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"temp": 35.0}, "temp", "gt", "30", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Gt_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"temp": 25.0}, "temp", "gt", "30", false, nil)
	assert.False(t, ok)
}

func TestEvalSingle_Gte_PassEqual(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"v": 30.0}, "v", "gte", "30", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Lt_Pass(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"v": 5.0}, "v", "lt", "10", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Lte_PassEqual(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"v": 10.0}, "v", "lte", "10", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Contains_Pass(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"msg": "hello world"}, "msg", "contains", "world", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Contains_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"msg": "hello world"}, "msg", "contains", "missing", false, nil)
	assert.False(t, ok)
}

func TestEvalSingle_StartsWith_Pass(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"path": "/api/v1"}, "path", "startsWith", "/api", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_EndsWith_Pass(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"file": "report.csv"}, "file", "endsWith", ".csv", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Regex_Pass(t *testing.T) {
	ok, _, errMsg := evalSingle(map[string]interface{}{"id": "USR-12345"}, "id", "regex", `^USR-\d+$`, false, nil)
	assert.True(t, ok)
	assert.Empty(t, errMsg)
}

func TestEvalSingle_Regex_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"id": "SVC-abc"}, "id", "regex", `^USR-\d+$`, false, nil)
	assert.False(t, ok)
}

func TestEvalSingle_Regex_Invalid(t *testing.T) {
	ok, _, errMsg := evalSingle(map[string]interface{}{"id": "x"}, "id", "regex", `[invalid`, false, nil)
	assert.False(t, ok)
	assert.NotEmpty(t, errMsg)
}

func TestEvalSingle_MissingField_Block(t *testing.T) {
	ok, reason, errMsg := evalSingle(map[string]interface{}{}, "status", "eq", "200", false, nil)
	assert.False(t, ok)
	assert.Contains(t, reason, "not found")
	assert.Empty(t, errMsg)
}

func TestEvalSingle_MissingField_PassThrough(t *testing.T) {
	ok, _, errMsg := evalSingle(map[string]interface{}{}, "status", "eq", "200", true, nil)
	assert.True(t, ok)
	assert.Empty(t, errMsg)
}

func TestEvalSingle_EmptyField_Error(t *testing.T) {
	ok, _, errMsg := evalSingle(map[string]interface{}{"v": 1}, "", "eq", "1", false, nil)
	assert.False(t, ok)
	assert.NotEmpty(t, errMsg)
}

func TestEvalSingle_UnsupportedOperator_Error(t *testing.T) {
	ok, _, errMsg := evalSingle(map[string]interface{}{"v": 1}, "v", "UNKNOWN", "1", false, nil)
	assert.False(t, ok)
	assert.NotEmpty(t, errMsg)
}

func TestEvalSingle_Eq_String_Pass(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"region": "us-east"}, "region", "eq", "us-east", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Eq_String_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"region": "eu-west"}, "region", "eq", "us-east", false, nil)
	assert.False(t, ok)
}

// ─── evalMulti ───────────────────────────────────────────────────────────────

func TestEvalMulti_AND_AllPass(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "region", Operator: "eq", Value: "us-east"},
	}
	ok, _, errMsg := evalMulti(
		map[string]interface{}{"status": 200.0, "region": "us-east"},
		preds, "and", false, nil,
	)
	assert.True(t, ok)
	assert.Empty(t, errMsg)
}

func TestEvalMulti_AND_FirstFails(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "region", Operator: "eq", Value: "us-east"},
	}
	ok, reason, _ := evalMulti(
		map[string]interface{}{"status": 404.0, "region": "us-east"},
		preds, "and", false, nil,
	)
	assert.False(t, ok)
	assert.NotEmpty(t, reason)
}

func TestEvalMulti_AND_SecondFails(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "region", Operator: "eq", Value: "us-east"},
	}
	ok, _, _ := evalMulti(
		map[string]interface{}{"status": 200.0, "region": "eu-west"},
		preds, "and", false, nil,
	)
	assert.False(t, ok)
}

func TestEvalMulti_OR_FirstPasses(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "status", Operator: "eq", Value: "201"},
	}
	ok, _, _ := evalMulti(
		map[string]interface{}{"status": 200.0},
		preds, "or", false, nil,
	)
	assert.True(t, ok)
}

func TestEvalMulti_OR_SecondPasses(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "status", Operator: "eq", Value: "201"},
	}
	ok, _, _ := evalMulti(
		map[string]interface{}{"status": 201.0},
		preds, "or", false, nil,
	)
	assert.True(t, ok)
}

func TestEvalMulti_OR_NonePasses(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "status", Operator: "eq", Value: "201"},
	}
	ok, reason, _ := evalMulti(
		map[string]interface{}{"status": 500.0},
		preds, "or", false, nil,
	)
	assert.False(t, ok)
	assert.NotEmpty(t, reason)
}

func TestEvalMulti_EmptyPredicates_Pass(t *testing.T) {
	ok, _, _ := evalMulti(map[string]interface{}{}, []Predicate{}, "and", false, nil)
	assert.True(t, ok)
}

func TestEvalMulti_UnsupportedOperator_Error(t *testing.T) {
	preds := []Predicate{{Field: "v", Operator: "UNKNOWN", Value: "1"}}
	ok, _, errMsg := evalMulti(map[string]interface{}{"v": 1}, preds, "and", false, nil)
	assert.False(t, ok)
	assert.NotEmpty(t, errMsg)
}

func TestEvalMulti_MissingField_AND_PassThrough(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "missing", Operator: "eq", Value: "x"},
	}
	ok, _, _ := evalMulti(
		map[string]interface{}{"status": 200.0},
		preds, "and", true, nil, // passThroughOnMissing=true
	)
	assert.True(t, ok)
}

func TestEvalMulti_MissingField_AND_Block(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "missing", Operator: "eq", Value: "x"},
	}
	ok, _, _ := evalMulti(
		map[string]interface{}{"status": 200.0},
		preds, "and", false, nil,
	)
	assert.False(t, ok)
}

// ─── Trigger.evaluate (integration of defaults) ───────────────────────────────

func TestTriggerEvaluate_HandlerOperatorOverridesTriggerDefault(t *testing.T) {
	trig := newTrigger(&Settings{Operator: "eq", PredicateMode: "and"})
	// Handler overrides to "gt"
	hs := &HandlerSettings{Field: "temp", Operator: "gt", Value: "30"}
	ok, _, errMsg := eval(t, trig, hs, map[string]interface{}{"temp": 35.0})
	assert.True(t, ok)
	assert.Empty(t, errMsg)
}

func TestTriggerEvaluate_TriggerDefaultOperatorUsed(t *testing.T) {
	trig := newTrigger(&Settings{Operator: "eq"})
	hs := &HandlerSettings{Field: "status", Value: "200"} // operator empty → uses trigger default "eq"
	ok, _, _ := eval(t, trig, hs, map[string]interface{}{"status": 200.0})
	assert.True(t, ok)
}

func TestTriggerEvaluate_MultiPredicateFromHandler(t *testing.T) {
	trig := newTrigger(&Settings{PredicateMode: "and"})
	hs := &HandlerSettings{
		Predicates:    `[{"field":"status","operator":"eq","value":"200"},{"field":"region","operator":"eq","value":"us-east"}]`,
		PredicateMode: "and",
	}
	ok, _, errMsg := eval(t, trig, hs, map[string]interface{}{"status": 200.0, "region": "us-east"})
	assert.True(t, ok)
	assert.Empty(t, errMsg)
}

func TestTriggerEvaluate_InvalidPredicatesJSON_Error(t *testing.T) {
	trig := newTrigger(&Settings{})
	hs := &HandlerSettings{Predicates: `not json`}
	ok, _, errMsg := eval(t, trig, hs, map[string]interface{}{"v": 1})
	assert.False(t, ok)
	assert.NotEmpty(t, errMsg)
}

// ─── dedupStore ───────────────────────────────────────────────────────────────

func TestDedupStore_FirstSeenNotDuplicate(t *testing.T) {
	ds := newDedupStore(10*time.Minute, 1000)
	assert.False(t, ds.isDuplicate("event-1"))
}

func TestDedupStore_SecondSeenIsDuplicate(t *testing.T) {
	ds := newDedupStore(10*time.Minute, 1000)
	ds.isDuplicate("event-1") // first call — registers
	assert.True(t, ds.isDuplicate("event-1"))
}

func TestDedupStore_DifferentIDsNotDuplicate(t *testing.T) {
	ds := newDedupStore(10*time.Minute, 1000)
	ds.isDuplicate("event-1")
	assert.False(t, ds.isDuplicate("event-2"))
}

func TestDedupStore_ExpiredEntryNotDuplicate(t *testing.T) {
	// Use a 1ms window so entries expire immediately.
	ds := newDedupStore(time.Millisecond, 1000)
	ds.isDuplicate("event-1")
	time.Sleep(10 * time.Millisecond)
	assert.False(t, ds.isDuplicate("event-1"), "entry should have expired")
}

func TestDedupStore_MaxEntriesEvictsOldest(t *testing.T) {
	ds := newDedupStore(10*time.Minute, 2) // cap at 2 entries
	ds.isDuplicate("a")
	ds.isDuplicate("b")
	// Adding "c" must evict one of the two existing entries.
	ds.isDuplicate("c")
	// The store should still have exactly 2 entries (one was evicted).
	ds.mu.Lock()
	defer ds.mu.Unlock()
	assert.LessOrEqual(t, len(ds.seen), 2)
}

// ─── HandlerSettings.ParsedPredicates ─────────────────────────────────────────

func TestHandlerSettings_ParsedPredicates_Valid(t *testing.T) {
	hs := &HandlerSettings{Predicates: `[{"field":"f","operator":"eq","value":"v"}]`}
	preds, err := hs.ParsedPredicates()
	require.NoError(t, err)
	require.Len(t, preds, 1)
	assert.Equal(t, "f", preds[0].Field)
	assert.Equal(t, "eq", preds[0].Operator)
	assert.Equal(t, "v", preds[0].Value)
}

func TestHandlerSettings_ParsedPredicates_Empty(t *testing.T) {
	hs := &HandlerSettings{}
	preds, err := hs.ParsedPredicates()
	require.NoError(t, err)
	assert.Nil(t, preds)
}

func TestHandlerSettings_ParsedPredicates_Invalid(t *testing.T) {
	hs := &HandlerSettings{Predicates: `{bad}`}
	_, err := hs.ParsedPredicates()
	require.Error(t, err)
}

// ─── Output ToMap / FromMap round-trip ───────────────────────────────────────

func TestOutput_RoundTrip(t *testing.T) {
	orig := &Output{
		Message:   map[string]interface{}{"k": "v"},
		Topic:     "test-topic",
		Partition: 2,
		Offset:    42,
		Key:       "msg-key",
	}
	restored := &Output{}
	require.NoError(t, restored.FromMap(orig.ToMap()))
	assert.Equal(t, orig.Topic, restored.Topic)
	assert.Equal(t, orig.Partition, restored.Partition)
	assert.Equal(t, orig.Offset, restored.Offset)
	assert.Equal(t, orig.Key, restored.Key)
}

// ─── evalPredicate (string fallback path) ────────────────────────────────────

func TestEvalPredicate_Eq_StringFallback(t *testing.T) {
	ok, _, _ := evalPredicate("active", "state", "eq", "active", nil)
	assert.True(t, ok)
}

func TestEvalPredicate_Neq_StringFallback(t *testing.T) {
	ok, _, _ := evalPredicate("inactive", "state", "neq", "active", nil)
	assert.True(t, ok)
}

func TestEvalPredicate_NumericGtBeyondStringPath(t *testing.T) {
	ok, _, errMsg := evalPredicate("not-a-number", "val", "gt", "5", nil)
	assert.False(t, ok)
	assert.NotEmpty(t, errMsg)
}
