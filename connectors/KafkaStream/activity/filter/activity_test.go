package filter

import (
	"testing"

	"github.com/project-flogo/core/support/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFilterAct(t *testing.T, s *Settings) *Activity {
	t.Helper()
	act, err := New(test.NewActivityInitContext(s, nil))
	require.NoError(t, err)
	return act.(*Activity)
}

func runFilter(t *testing.T, act *Activity, input *Input) *Output {
	t.Helper()
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(input))
	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
	out := &Output{}
	require.NoError(t, tc.GetOutputObject(out))
	return out
}

// ---- eq / neq (numeric) -----------------------------------------------------

func TestFilter_Eq_Numeric_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "status", Message: map[string]interface{}{"status": 200.0}, Operator: "eq", Value: "200"})
	assert.True(t, out.Passed)
	assert.Empty(t, out.Reason)
}

func TestFilter_Eq_Numeric_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "status", Message: map[string]interface{}{"status": 404.0}, Operator: "eq", Value: "200"})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
	require.NotNil(t, out.Message, "message is always returned so the caller can route or log it")
	assert.Equal(t, 404.0, out.Message["status"])
}

func TestFilter_Neq_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "code", Message: map[string]interface{}{"code": 200.0}, Operator: "neq", Value: "404"})
	assert.True(t, out.Passed)
}

// ---- gt / gte / lt / lte ----------------------------------------------------

func TestFilter_Gt_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "temp", Message: map[string]interface{}{"temp": 35.0}, Operator: "gt", Value: "30"})
	assert.True(t, out.Passed)
}

func TestFilter_Gt_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "temp", Message: map[string]interface{}{"temp": 30.0}, Operator: "gt", Value: "30"})
	assert.False(t, out.Passed)
}

func TestFilter_Gte_Pass_Equal(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "temp", Message: map[string]interface{}{"temp": 30.0}, Operator: "gte", Value: "30"})
	assert.True(t, out.Passed, "gte must pass when equal")
}

func TestFilter_Lt_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "latency", Message: map[string]interface{}{"latency": 50.0}, Operator: "lt", Value: "100"})
	assert.True(t, out.Passed)
}

func TestFilter_Lte_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "latency", Message: map[string]interface{}{"latency": 101.0}, Operator: "lte", Value: "100"})
	assert.False(t, out.Passed)
}

// ---- string operators -------------------------------------------------------

func TestFilter_Contains_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "msg", Message: map[string]interface{}{"msg": "parse error occurred"}, Operator: "contains", Value: "error"})
	assert.True(t, out.Passed)
}

func TestFilter_Contains_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "msg", Message: map[string]interface{}{"msg": "all good"}, Operator: "contains", Value: "error"})
	assert.False(t, out.Passed)
}

func TestFilter_StartsWith_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "topic", Message: map[string]interface{}{"topic": "sensor.temperature"}, Operator: "startsWith", Value: "sensor."})
	assert.True(t, out.Passed)
}

func TestFilter_EndsWith_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "file", Message: map[string]interface{}{"file": "payload.json"}, Operator: "endsWith", Value: ".json"})
	assert.True(t, out.Passed)
}

func TestFilter_EndsWith_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "file", Message: map[string]interface{}{"file": "payload.xml"}, Operator: "endsWith", Value: ".json"})
	assert.False(t, out.Passed)
}

// ---- regex ------------------------------------------------------------------

func TestFilter_Regex_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "device_id", Message: map[string]interface{}{"device_id": "sensor-42"}, Operator: "regex", Value: `^sensor-[0-9]+$`})
	assert.True(t, out.Passed)
}

func TestFilter_Regex_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "device_id", Message: map[string]interface{}{"device_id": "actuator-42"}, Operator: "regex", Value: `^sensor-[0-9]+$`})
	assert.False(t, out.Passed)
}

func TestFilter_InvalidRegex_SetsErrorMessage(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "f", Message: map[string]interface{}{"f": "val"}, Operator: "regex", Value: "[invalid"})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.ErrorMessage)
}

// ---- eq (string fallback) ---------------------------------------------------

func TestFilter_Eq_String_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "env", Message: map[string]interface{}{"env": "production"}, Operator: "eq", Value: "production"})
	assert.True(t, out.Passed)
}

func TestFilter_Eq_String_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "env", Message: map[string]interface{}{"env": "staging"}, Operator: "eq", Value: "production"})
	assert.False(t, out.Passed)
}

// ---- missing field handling -------------------------------------------------

func TestFilter_MissingField_DefaultFail(t *testing.T) {
	act := newFilterAct(t, &Settings{PassThroughOnMissing: false})
	out := runFilter(t, act, &Input{Field: "temp", Message: map[string]interface{}{"pressure": 101.3}, Operator: "gt", Value: "30"})
	assert.False(t, out.Passed)
}

func TestFilter_MissingField_PassThrough(t *testing.T) {
	act := newFilterAct(t, &Settings{PassThroughOnMissing: true})
	out := runFilter(t, act, &Input{Field: "temp", Message: map[string]interface{}{"pressure": 101.3}, Operator: "gt", Value: "30"})
	assert.True(t, out.Passed, "passThroughOnMissing must allow message through")
}

// ---- passed message is propagated -------------------------------------------

func TestFilter_PassedMessage_IsInOutput(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	msg := map[string]interface{}{"val": 5.0, "name": "sensor-1"}
	out := runFilter(t, act, &Input{Field: "val", Message: msg, Operator: "gt", Value: "0"})
	assert.True(t, out.Passed)
	require.NotNil(t, out.Message)
	assert.Equal(t, 5.0, out.Message["val"])
	assert.Equal(t, "sensor-1", out.Message["name"])
}

// ---- invalid operator -------------------------------------------------------

func TestFilter_InvalidOperator_SetsErrorMessage(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "f", Message: map[string]interface{}{"f": 10.0}, Operator: "between", Value: "10"})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.ErrorMessage)
}

// ─── Enterprise: multi-predicate AND ─────────────────────────────────────────

func TestFilter_MultiPredicate_AND_AllPass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{
		Message:        map[string]interface{}{"price": 150.0, "region": "US"},
		PredicatesJSON: `[{"field":"price","operator":"gt","value":"100"},{"field":"region","operator":"eq","value":"US"}]`,
		PredicateMode:  "and",
	})
	assert.True(t, out.Passed)
	assert.Empty(t, out.ErrorMessage)
}

func TestFilter_MultiPredicate_AND_OneFails(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{
		Message:        map[string]interface{}{"price": 50.0, "region": "US"},
		PredicatesJSON: `[{"field":"price","operator":"gt","value":"100"},{"field":"region","operator":"eq","value":"US"}]`,
		PredicateMode:  "and",
	})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
}

// ─── Enterprise: multi-predicate OR ──────────────────────────────────────────

func TestFilter_MultiPredicate_OR_OnePass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{
		Message:        map[string]interface{}{"status": 201.0},
		PredicatesJSON: `[{"field":"status","operator":"eq","value":"200"},{"field":"status","operator":"eq","value":"201"}]`,
		PredicateMode:  "or",
	})
	assert.True(t, out.Passed)
}

func TestFilter_MultiPredicate_OR_AllFail(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{
		Message:        map[string]interface{}{"status": 404.0},
		PredicatesJSON: `[{"field":"status","operator":"eq","value":"200"},{"field":"status","operator":"eq","value":"201"}]`,
		PredicateMode:  "or",
	})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
}

// ─── Enterprise: multi-predicate with regex ───────────────────────────────────

func TestFilter_MultiPredicate_WithRegex(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{
		Message:        map[string]interface{}{"device": "sensor-42", "value": 5.0},
		PredicatesJSON: `[{"field":"device","operator":"regex","value":"^sensor-[0-9]+$"},{"field":"value","operator":"gt","value":"0"}]`,
	})
	assert.True(t, out.Passed)
	out2 := runFilter(t, act, &Input{
		Message:        map[string]interface{}{"device": "actuator-1", "value": 5.0},
		PredicatesJSON: `[{"field":"device","operator":"regex","value":"^sensor-[0-9]+$"},{"field":"value","operator":"gt","value":"0"}]`,
	})
	assert.False(t, out2.Passed)
}

// ─── Enterprise: invalid predicates JSON ─────────────────────────────────────

func TestFilter_MultiPredicate_InvalidJSON_SetsErrorMessage(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{
		Message:        map[string]interface{}{"a": 1.0},
		PredicatesJSON: `not valid json`,
	})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.ErrorMessage)
	assert.Contains(t, out.ErrorMessage, "predicates JSON")
}

// ─── Enterprise: ErrorMessage output ─────────────────────────────────────────

func TestFilter_ErrorMessage_Empty_OnNormalFilter(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "v", Message: map[string]interface{}{"v": 5.0}, Operator: "gt", Value: "0"})
	assert.True(t, out.Passed)
	assert.Empty(t, out.ErrorMessage, "no error on normal filter pass")
}

func TestFilter_MissingField_ErrorMessage_Empty(t *testing.T) {
	// Missing field with default fail does NOT set ErrorMessage (not an eval error)
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "v", Message: map[string]interface{}{"other": 5.0}, Operator: "gt", Value: "0"})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
	assert.Empty(t, out.ErrorMessage)
}

// ─── Negative tests ───────────────────────────────────────────────────────────

// TestFilter_Neq_Fail_WhenEqual verifies that neq returns false when the field
// value exactly equals the configured comparison value.
func TestFilter_Neq_Fail_WhenEqual(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "code", Message: map[string]interface{}{"code": 200.0}, Operator: "neq", Value: "200"})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
}

// TestFilter_Gt_Boundary_EqualFails verifies that gt is strictly greater-than
// and must fail when the field value equals the threshold.
func TestFilter_Gt_Boundary_EqualFails(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "val", Message: map[string]interface{}{"val": 10.0}, Operator: "gt", Value: "10"})
	assert.False(t, out.Passed, "gt must be strictly greater-than; equal value must fail")
}

// TestFilter_Lte_Boundary_EqualPasses verifies that lte includes the boundary
// value (i.e. passes when field == threshold).
func TestFilter_Lte_Boundary_EqualPasses(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "val", Message: map[string]interface{}{"val": 10.0}, Operator: "lte", Value: "10"})
	assert.True(t, out.Passed, "lte must pass when field equals the threshold")
}

// TestFilter_NumericOp_NonNumericField_SetsErrorMessage verifies that when a
// numeric operator (gt/lt/…) is used against a non-numeric field value the
// activity sets ErrorMessage (DLQ path) rather than Reason.
func TestFilter_NumericOp_NonNumericField_SetsErrorMessage(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "val", Message: map[string]interface{}{"val": "abc"}, Operator: "gt", Value: "30"})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.ErrorMessage, "parse failure must be reported in errorMessage")
	assert.Empty(t, out.Reason, "reason must not be set for parse errors")
}

// TestFilter_MultiPredicate_DefaultMode_IsAnd verifies that when predicateMode
// is not set the default mode is AND — all predicates must pass.
func TestFilter_MultiPredicate_DefaultMode_IsAnd(t *testing.T) {
	const preds = `[{"field":"a","operator":"gt","value":"0"},{"field":"b","operator":"gt","value":"0"}]`
	act := newFilterAct(t, &Settings{})
	// Both pass → overall pass.
	out := runFilter(t, act, &Input{Message: map[string]interface{}{"a": 1.0, "b": 1.0}, PredicatesJSON: preds})
	assert.True(t, out.Passed)
	// One fails → AND mode means overall failure.
	out2 := runFilter(t, act, &Input{Message: map[string]interface{}{"a": 1.0, "b": -1.0}, PredicatesJSON: preds})
	assert.False(t, out2.Passed, "AND mode: one failing predicate must reject the message")
}

// TestFilter_MultiPredicate_MissingField_AND_Fails verifies that in AND mode a
// missing required field (PassThroughOnMissing=false) causes the filter to fail.
func TestFilter_MultiPredicate_MissingField_AND_Fails(t *testing.T) {
	const preds = `[{"field":"a","operator":"gt","value":"0"},{"field":"b","operator":"gt","value":"0"}]`
	act := newFilterAct(t, &Settings{PassThroughOnMissing: false})
	out := runFilter(t, act, &Input{Message: map[string]interface{}{"a": 1.0}, PredicatesJSON: preds})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
}

// TestFilter_MultiPredicate_MissingField_AND_PassThrough_Continues verifies that
// in AND mode with PassThroughOnMissing=true an absent field is treated as a
// non-blocking pass and evaluation continues with the remaining predicates.
func TestFilter_MultiPredicate_MissingField_AND_PassThrough_Continues(t *testing.T) {
	const preds = `[{"field":"absent","operator":"gt","value":"0"},{"field":"b","operator":"gt","value":"0"}]`
	act := newFilterAct(t, &Settings{PassThroughOnMissing: true})
	// "absent" is missing → skipped (pass-through); "b" passes → overall pass.
	out := runFilter(t, act, &Input{Message: map[string]interface{}{"b": 1.0}, PredicatesJSON: preds})
	assert.True(t, out.Passed, "missing field with PassThroughOnMissing=true must not block AND chain")
}

// TestFilter_NoFieldAndNoPredicates_SetsErrorMessage verifies that when neither
// field nor predicates are provided the activity sets ErrorMessage.
func TestFilter_NoFieldAndNoPredicates_SetsErrorMessage(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Message: map[string]interface{}{"a": 1.0}})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.ErrorMessage)
}

// TestFilter_MultiPredicate_InvalidOperatorInChain_SetsErrorMessage verifies
// that an unsupported operator inside the predicates JSON array sets ErrorMessage.
func TestFilter_MultiPredicate_InvalidOperatorInChain_SetsErrorMessage(t *testing.T) {
	const preds = `[{"field":"a","operator":"gt","value":"0"},{"field":"b","operator":"between","value":"1,10"}]`
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Message: map[string]interface{}{"a": 1.0, "b": 5.0}, PredicatesJSON: preds})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.ErrorMessage)
	assert.Contains(t, out.ErrorMessage, "unsupported operator")
}

// TestFilter_FailedMessage_IsPresent verifies that when a message is filtered
// out (passed=false) the Message output field still carries the original
// payload so downstream activities (e.g. DLQ, logging) can use it.
func TestFilter_FailedMessage_IsPresent(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "v", Message: map[string]interface{}{"v": 5.0}, Operator: "gt", Value: "100"})
	assert.False(t, out.Passed)
	require.NotNil(t, out.Message, "message must be present even when filtered out")
	assert.Equal(t, 5.0, out.Message["v"])
}

// TestFilter_Reason_EmptyOnPass verifies that the Reason output is always empty
// when the message passes the filter — there is nothing to explain.
func TestFilter_Reason_EmptyOnPass(t *testing.T) {
	act := newFilterAct(t, &Settings{})
	out := runFilter(t, act, &Input{Field: "v", Message: map[string]interface{}{"v": 10.0}, Operator: "gt", Value: "0"})
	assert.True(t, out.Passed)
	assert.Empty(t, out.Reason, "reason must be empty on a passing message")
}

// ─── Settings defaults / input override ──────────────────────────────────────

// TestFilter_Settings_Operator_UsedWhenInputEmpty verifies that when the
// operator input is not set, the default from settings is applied.
func TestFilter_Settings_Operator_UsedWhenInputEmpty(t *testing.T) {
	act := newFilterAct(t, &Settings{Operator: "gt"})
	// No Operator in Input — should fall back to settings default "gt".
	out := runFilter(t, act, &Input{Field: "temp", Message: map[string]interface{}{"temp": 35.0}, Value: "30"})
	assert.True(t, out.Passed)
}

// TestFilter_Input_Operator_OverridesSettings verifies that a non-empty
// operator in the input takes precedence over the settings default.
func TestFilter_Input_Operator_OverridesSettings(t *testing.T) {
	act := newFilterAct(t, &Settings{Operator: "gt"}) // default gt
	// Input overrides to "lt" — 35 lt 30 is false.
	out := runFilter(t, act, &Input{Field: "temp", Message: map[string]interface{}{"temp": 35.0}, Operator: "lt", Value: "30"})
	assert.False(t, out.Passed, "input operator 'lt' must override settings default 'gt'")
}

// TestFilter_Settings_PredicateMode_UsedWhenInputEmpty verifies that when the
// predicateMode input is not set, the default from settings is applied.
func TestFilter_Settings_PredicateMode_UsedWhenInputEmpty(t *testing.T) {
	act := newFilterAct(t, &Settings{PredicateMode: "or"})
	// No PredicateMode in Input — should fall back to settings default "or".
	// Only second predicate passes → overall true because mode is "or".
	const preds = `[{"field":"a","operator":"gt","value":"100"},{"field":"b","operator":"gt","value":"0"}]`
	out := runFilter(t, act, &Input{Message: map[string]interface{}{"a": 1.0, "b": 5.0}, PredicatesJSON: preds})
	assert.True(t, out.Passed, "settings predicateMode 'or' must be used when input predicateMode is empty")
}

// TestFilter_Input_PredicateMode_OverridesSettings verifies that a non-empty
// predicateMode in the input takes precedence over the settings default.
func TestFilter_Input_PredicateMode_OverridesSettings(t *testing.T) {
	act := newFilterAct(t, &Settings{PredicateMode: "or"}) // default or
	// Input overrides to "and" — first predicate fails → overall false.
	const preds = `[{"field":"a","operator":"gt","value":"100"},{"field":"b","operator":"gt","value":"0"}]`
	out := runFilter(t, act, &Input{
		Message:        map[string]interface{}{"a": 1.0, "b": 5.0},
		PredicatesJSON: preds,
		PredicateMode:  "and",
	})
	assert.False(t, out.Passed, "input predicateMode 'and' must override settings default 'or'")
}

// ─── Deduplication ───────────────────────────────────────────────────────────

func TestFilter_Dedup_FirstMessage_Passes(t *testing.T) {
	act := newFilterAct(t, &Settings{
		Operator:    "gt",
		EnableDedup: true,
		DedupWindow: "1m",
	})
	msg := map[string]interface{}{"event_id": "abc-001", "temp": 25.0}
	out := runFilter(t, act, &Input{Field: "temp", Operator: "gt", Value: "20", Message: msg, DedupField: "abc-001"})
	assert.True(t, out.Passed, "first occurrence should pass predicate and dedup")
}

func TestFilter_Dedup_SecondMessage_IsBlocked(t *testing.T) {
	act := newFilterAct(t, &Settings{
		Operator:    "gt",
		EnableDedup: true,
		DedupWindow: "1m",
	})
	msg := map[string]interface{}{"event_id": "abc-002", "temp": 25.0}
	input := &Input{Field: "temp", Operator: "gt", Value: "20", Message: msg, DedupField: "abc-002"}
	out1 := runFilter(t, act, input)
	assert.True(t, out1.Passed, "first occurrence should pass")
	out2 := runFilter(t, act, input)
	assert.False(t, out2.Passed, "duplicate should be blocked")
	assert.Equal(t, "duplicate", out2.Reason)
}

func TestFilter_Dedup_DifferentIDs_BothPass(t *testing.T) {
	act := newFilterAct(t, &Settings{
		Operator:    "gt",
		EnableDedup: true,
		DedupWindow: "1m",
	})
	msg1 := map[string]interface{}{"event_id": "id-A", "temp": 25.0}
	msg2 := map[string]interface{}{"event_id": "id-B", "temp": 25.0}
	out1 := runFilter(t, act, &Input{Field: "temp", Operator: "gt", Value: "20", Message: msg1, DedupField: "id-A"})
	out2 := runFilter(t, act, &Input{Field: "temp", Operator: "gt", Value: "20", Message: msg2, DedupField: "id-B"})
	assert.True(t, out1.Passed)
	assert.True(t, out2.Passed)
}

func TestFilter_Dedup_Disabled_WhenFieldEmpty(t *testing.T) {
	// EnableDedup is false (default) — deduplication off, same message twice both pass predicate.
	act := newFilterAct(t, &Settings{Operator: "gt"})
	msg := map[string]interface{}{"event_id": "abc-003", "temp": 25.0}
	input := &Input{Field: "temp", Operator: "gt", Value: "20", Message: msg}
	out1 := runFilter(t, act, input)
	out2 := runFilter(t, act, input)
	assert.True(t, out1.Passed)
	assert.True(t, out2.Passed)
}

func TestFilter_Dedup_MaxEntries_Enforced(t *testing.T) {
	act := newFilterAct(t, &Settings{
		Operator:        "gt",
		EnableDedup:     true,
		DedupWindow:     "1h",
		DedupMaxEntries: 3,
	})
	// Fill to capacity.
	for i := 0; i < 3; i++ {
		id := "id-" + string(rune('A'+i))
		msg := map[string]interface{}{"event_id": id, "temp": 25.0}
		runFilter(t, act, &Input{Field: "temp", Operator: "gt", Value: "20", Message: msg, DedupField: id})
	}
	// Store should have max 3 entries.
	assert.LessOrEqual(t, act.dedup.size(), 3)
	// A 4th distinct ID should still be accepted (oldest evicted to make room).
	msg4 := map[string]interface{}{"event_id": "id-NEW", "temp": 25.0}
	out := runFilter(t, act, &Input{Field: "temp", Operator: "gt", Value: "20", Message: msg4, DedupField: "id-NEW"})
	assert.True(t, out.Passed, "new ID should be accepted even when at capacity")
}

// ─── Rate Limiting ────────────────────────────────────────────────────────────

func TestFilter_RateLimit_Drop_AllowsUnderLimit(t *testing.T) {
	act := newFilterAct(t, &Settings{
		Operator:       "gt",
		RateLimitRPS:   1000, // very high — should pass
		RateLimitBurst: 100,
		RateLimitMode:  "drop",
	})
	msg := map[string]interface{}{"temp": 25.0}
	out := runFilter(t, act, &Input{Field: "temp", Operator: "gt", Value: "20", Message: msg})
	assert.True(t, out.Passed)
}

func TestFilter_RateLimit_Drop_BlocksOverLimit(t *testing.T) {
	act := newFilterAct(t, &Settings{
		Operator:       "gt",
		RateLimitRPS:   0.001, // effectively 0 — burst=1, immediately exhausted
		RateLimitBurst: 1,
		RateLimitMode:  "drop",
	})
	msg := map[string]interface{}{"temp": 25.0}
	input := &Input{Field: "temp", Operator: "gt", Value: "20", Message: msg}
	// First consumes the single burst token.
	runFilter(t, act, input)
	// Second should be rate-limited.
	out := runFilter(t, act, input)
	assert.False(t, out.Passed)
	assert.Equal(t, "rate_limited", out.Reason)
}

func TestFilter_RateLimit_Disabled_WhenRPSZero(t *testing.T) {
	act := newFilterAct(t, &Settings{Operator: "gt"}) // RateLimitRPS = 0
	assert.Nil(t, act.limiter, "limiter must be nil when RateLimitRPS is 0")
	msg := map[string]interface{}{"temp": 25.0}
	out := runFilter(t, act, &Input{Field: "temp", Operator: "gt", Value: "20", Message: msg})
	assert.True(t, out.Passed)
}
