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

func runFilter(t *testing.T, act *Activity, message map[string]interface{}) *Output {
	t.Helper()
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&Input{Message: message}))
	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
	out := &Output{}
	require.NoError(t, tc.GetOutputObject(out))
	return out
}

// ---- eq / neq (numeric) -----------------------------------------------------

func TestFilter_Eq_Numeric_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "status", Operator: "eq", Value: "200"})
	out := runFilter(t, act, map[string]interface{}{"status": 200.0})
	assert.True(t, out.Passed)
	assert.Empty(t, out.Reason)
}

func TestFilter_Eq_Numeric_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "status", Operator: "eq", Value: "200"})
	out := runFilter(t, act, map[string]interface{}{"status": 404.0})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
	assert.Nil(t, out.Message, "message must be nil when filtered out")
}

func TestFilter_Neq_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "code", Operator: "neq", Value: "404"})
	out := runFilter(t, act, map[string]interface{}{"code": 200.0})
	assert.True(t, out.Passed)
}

// ---- gt / gte / lt / lte ----------------------------------------------------

func TestFilter_Gt_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "temp", Operator: "gt", Value: "30"})
	out := runFilter(t, act, map[string]interface{}{"temp": 35.0})
	assert.True(t, out.Passed)
}

func TestFilter_Gt_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "temp", Operator: "gt", Value: "30"})
	out := runFilter(t, act, map[string]interface{}{"temp": 30.0})
	assert.False(t, out.Passed)
}

func TestFilter_Gte_Pass_Equal(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "temp", Operator: "gte", Value: "30"})
	out := runFilter(t, act, map[string]interface{}{"temp": 30.0})
	assert.True(t, out.Passed, "gte must pass when equal")
}

func TestFilter_Lt_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "latency", Operator: "lt", Value: "100"})
	out := runFilter(t, act, map[string]interface{}{"latency": 50.0})
	assert.True(t, out.Passed)
}

func TestFilter_Lte_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "latency", Operator: "lte", Value: "100"})
	out := runFilter(t, act, map[string]interface{}{"latency": 101.0})
	assert.False(t, out.Passed)
}

// ---- string operators -------------------------------------------------------

func TestFilter_Contains_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "msg", Operator: "contains", Value: "error"})
	out := runFilter(t, act, map[string]interface{}{"msg": "parse error occurred"})
	assert.True(t, out.Passed)
}

func TestFilter_Contains_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "msg", Operator: "contains", Value: "error"})
	out := runFilter(t, act, map[string]interface{}{"msg": "all good"})
	assert.False(t, out.Passed)
}

func TestFilter_StartsWith_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "topic", Operator: "startsWith", Value: "sensor."})
	out := runFilter(t, act, map[string]interface{}{"topic": "sensor.temperature"})
	assert.True(t, out.Passed)
}

func TestFilter_EndsWith_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "file", Operator: "endsWith", Value: ".json"})
	out := runFilter(t, act, map[string]interface{}{"file": "payload.json"})
	assert.True(t, out.Passed)
}

func TestFilter_EndsWith_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "file", Operator: "endsWith", Value: ".json"})
	out := runFilter(t, act, map[string]interface{}{"file": "payload.xml"})
	assert.False(t, out.Passed)
}

// ---- regex ------------------------------------------------------------------

func TestFilter_Regex_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "device_id", Operator: "regex", Value: `^sensor-[0-9]+$`})
	out := runFilter(t, act, map[string]interface{}{"device_id": "sensor-42"})
	assert.True(t, out.Passed)
}

func TestFilter_Regex_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "device_id", Operator: "regex", Value: `^sensor-[0-9]+$`})
	out := runFilter(t, act, map[string]interface{}{"device_id": "actuator-42"})
	assert.False(t, out.Passed)
}

func TestNew_InvalidRegex_ReturnsError(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		Field: "f", Operator: "regex", Value: `[invalid`,
	}, nil))
	assert.Error(t, err)
}

// ---- eq (string fallback) ---------------------------------------------------

func TestFilter_Eq_String_Pass(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "env", Operator: "eq", Value: "production"})
	out := runFilter(t, act, map[string]interface{}{"env": "production"})
	assert.True(t, out.Passed)
}

func TestFilter_Eq_String_Fail(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "env", Operator: "eq", Value: "production"})
	out := runFilter(t, act, map[string]interface{}{"env": "staging"})
	assert.False(t, out.Passed)
}

// ---- missing field handling -------------------------------------------------

func TestFilter_MissingField_DefaultFail(t *testing.T) {
	act := newFilterAct(t, &Settings{
		Field: "temp", Operator: "gt", Value: "30",
		PassThroughOnMissing: false,
	})
	out := runFilter(t, act, map[string]interface{}{"pressure": 101.3})
	assert.False(t, out.Passed)
}

func TestFilter_MissingField_PassThrough(t *testing.T) {
	act := newFilterAct(t, &Settings{
		Field: "temp", Operator: "gt", Value: "30",
		PassThroughOnMissing: true,
	})
	out := runFilter(t, act, map[string]interface{}{"pressure": 101.3})
	assert.True(t, out.Passed, "passThroughOnMissing must allow message through")
}

// ---- passed message is propagated -------------------------------------------

func TestFilter_PassedMessage_IsInOutput(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "val", Operator: "gt", Value: "0"})
	msg := map[string]interface{}{"val": 5.0, "name": "sensor-1"}
	out := runFilter(t, act, msg)
	assert.True(t, out.Passed)
	require.NotNil(t, out.Message)
	assert.Equal(t, 5.0, out.Message["val"])
	assert.Equal(t, "sensor-1", out.Message["name"])
}

// ---- invalid operator -------------------------------------------------------

func TestNew_InvalidOperator(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		Field: "f", Operator: "between", Value: "10",
	}, nil))
	assert.Error(t, err)
}

// ─── Enterprise: multi-predicate AND ─────────────────────────────────────────

func TestFilter_MultiPredicate_AND_AllPass(t *testing.T) {
	act := newFilterAct(t, &Settings{
		PredicatesJSON: `[{"field":"price","operator":"gt","value":"100"},{"field":"region","operator":"eq","value":"US"}]`,
		PredicateMode:  "and",
	})
	out := runFilter(t, act, map[string]interface{}{"price": 150.0, "region": "US"})
	assert.True(t, out.Passed)
	assert.Empty(t, out.ErrorMessage)
}

func TestFilter_MultiPredicate_AND_OneFails(t *testing.T) {
	act := newFilterAct(t, &Settings{
		PredicatesJSON: `[{"field":"price","operator":"gt","value":"100"},{"field":"region","operator":"eq","value":"US"}]`,
		PredicateMode:  "and",
	})
	out := runFilter(t, act, map[string]interface{}{"price": 50.0, "region": "US"})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
}

// ─── Enterprise: multi-predicate OR ──────────────────────────────────────────

func TestFilter_MultiPredicate_OR_OnePass(t *testing.T) {
	act := newFilterAct(t, &Settings{
		PredicatesJSON: `[{"field":"status","operator":"eq","value":"200"},{"field":"status","operator":"eq","value":"201"}]`,
		PredicateMode:  "or",
	})
	out := runFilter(t, act, map[string]interface{}{"status": 201.0})
	assert.True(t, out.Passed)
}

func TestFilter_MultiPredicate_OR_AllFail(t *testing.T) {
	act := newFilterAct(t, &Settings{
		PredicatesJSON: `[{"field":"status","operator":"eq","value":"200"},{"field":"status","operator":"eq","value":"201"}]`,
		PredicateMode:  "or",
	})
	out := runFilter(t, act, map[string]interface{}{"status": 404.0})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
}

// ─── Enterprise: multi-predicate with regex ───────────────────────────────────

func TestFilter_MultiPredicate_WithRegex(t *testing.T) {
	act := newFilterAct(t, &Settings{
		PredicatesJSON: `[{"field":"device","operator":"regex","value":"^sensor-[0-9]+$"},{"field":"value","operator":"gt","value":"0"}]`,
	})
	out := runFilter(t, act, map[string]interface{}{"device": "sensor-42", "value": 5.0})
	assert.True(t, out.Passed)
	out2 := runFilter(t, act, map[string]interface{}{"device": "actuator-1", "value": 5.0})
	assert.False(t, out2.Passed)
}

// ─── Enterprise: invalid predicates JSON ─────────────────────────────────────

func TestNew_MultiPredicate_InvalidJSON(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		PredicatesJSON: `not valid json`,
	}, nil))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "predicates JSON")
}

// ─── Enterprise: ErrorMessage output ─────────────────────────────────────────

func TestFilter_ErrorMessage_Empty_OnNormalFilter(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "v", Operator: "gt", Value: "0"})
	out := runFilter(t, act, map[string]interface{}{"v": 5.0})
	assert.True(t, out.Passed)
	assert.Empty(t, out.ErrorMessage, "no error on normal filter pass")
}

func TestFilter_MissingField_ErrorMessage_Empty(t *testing.T) {
	// Missing field with default fail does NOT set ErrorMessage (not an eval error)
	act := newFilterAct(t, &Settings{Field: "v", Operator: "gt", Value: "0"})
	out := runFilter(t, act, map[string]interface{}{"other": 5.0})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
	assert.Empty(t, out.ErrorMessage)
}

// ─── Negative tests ───────────────────────────────────────────────────────────

// TestFilter_Neq_Fail_WhenEqual verifies that neq returns false when the field
// value exactly equals the configured comparison value.
func TestFilter_Neq_Fail_WhenEqual(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "code", Operator: "neq", Value: "200"})
	out := runFilter(t, act, map[string]interface{}{"code": 200.0})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
}

// TestFilter_Gt_Boundary_EqualFails verifies that gt is strictly greater-than
// and must fail when the field value equals the threshold.
func TestFilter_Gt_Boundary_EqualFails(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "val", Operator: "gt", Value: "10"})
	out := runFilter(t, act, map[string]interface{}{"val": 10.0})
	assert.False(t, out.Passed, "gt must be strictly greater-than; equal value must fail")
}

// TestFilter_Lte_Boundary_EqualPasses verifies that lte includes the boundary
// value (i.e. passes when field == threshold).
func TestFilter_Lte_Boundary_EqualPasses(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "val", Operator: "lte", Value: "10"})
	out := runFilter(t, act, map[string]interface{}{"val": 10.0})
	assert.True(t, out.Passed, "lte must pass when field equals the threshold")
}

// TestFilter_NumericOp_NonNumericField_SetsErrorMessage verifies that when a
// numeric operator (gt/lt/…) is used against a non-numeric field value the
// activity sets ErrorMessage (DLQ path) rather than Reason.
func TestFilter_NumericOp_NonNumericField_SetsErrorMessage(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "val", Operator: "gt", Value: "30"})
	out := runFilter(t, act, map[string]interface{}{"val": "abc"})
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.ErrorMessage, "parse failure must be reported in errorMessage")
	assert.Empty(t, out.Reason, "reason must not be set for parse errors")
}

// TestFilter_MultiPredicate_DefaultMode_IsAnd verifies that when predicateMode
// is not set the default mode is AND — all predicates must pass.
func TestFilter_MultiPredicate_DefaultMode_IsAnd(t *testing.T) {
	const preds = `[{"field":"a","operator":"gt","value":"0"},{"field":"b","operator":"gt","value":"0"}]`
	act := newFilterAct(t, &Settings{PredicatesJSON: preds})
	// Both pass → overall pass.
	out := runFilter(t, act, map[string]interface{}{"a": 1.0, "b": 1.0})
	assert.True(t, out.Passed)
	// One fails → AND mode means overall failure.
	out2 := runFilter(t, act, map[string]interface{}{"a": 1.0, "b": -1.0})
	assert.False(t, out2.Passed, "AND mode: one failing predicate must reject the message")
}

// TestFilter_MultiPredicate_MissingField_AND_Fails verifies that in AND mode a
// missing required field (PassThroughOnMissing=false) causes the filter to fail.
func TestFilter_MultiPredicate_MissingField_AND_Fails(t *testing.T) {
	const preds = `[{"field":"a","operator":"gt","value":"0"},{"field":"b","operator":"gt","value":"0"}]`
	act := newFilterAct(t, &Settings{
		PredicatesJSON:      preds,
		PassThroughOnMissing: false,
	})
	out := runFilter(t, act, map[string]interface{}{"a": 1.0}) // "b" absent
	assert.False(t, out.Passed)
	assert.NotEmpty(t, out.Reason)
}

// TestFilter_MultiPredicate_MissingField_AND_PassThrough_Continues verifies that
// in AND mode with PassThroughOnMissing=true an absent field is treated as a
// non-blocking pass and evaluation continues with the remaining predicates.
func TestFilter_MultiPredicate_MissingField_AND_PassThrough_Continues(t *testing.T) {
	const preds = `[{"field":"absent","operator":"gt","value":"0"},{"field":"b","operator":"gt","value":"0"}]`
	act := newFilterAct(t, &Settings{
		PredicatesJSON:      preds,
		PassThroughOnMissing: true,
	})
	// "absent" is missing → skipped (pass-through); "b" passes → overall pass.
	out := runFilter(t, act, map[string]interface{}{"b": 1.0})
	assert.True(t, out.Passed, "missing field with PassThroughOnMissing=true must not block AND chain")
}

// TestNew_NoFieldAndNoPredicates_ReturnsError verifies that New returns an error
// when neither a single-predicate (field+operator) nor a predicates list is configured.
func TestNew_NoFieldAndNoPredicates_ReturnsError(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		Field: "", Operator: "", Value: "", PredicatesJSON: "",
	}, nil))
	require.Error(t, err)
}

// TestNew_MultiPredicate_InvalidOperatorInChain_ReturnsError verifies that an
// unsupported operator inside the predicates JSON array is caught at New() time.
func TestNew_MultiPredicate_InvalidOperatorInChain_ReturnsError(t *testing.T) {
	// "between" is not a supported operator.
	const preds = `[{"field":"a","operator":"gt","value":"0"},{"field":"b","operator":"between","value":"1,10"}]`
	_, err := New(test.NewActivityInitContext(&Settings{PredicatesJSON: preds}, nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported operator")
}

// TestFilter_FailedMessage_IsNil verifies that when a message is filtered out
// (passed=false) the Message output field is nil — never leaks the raw payload.
func TestFilter_FailedMessage_IsNil(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "v", Operator: "gt", Value: "100"})
	out := runFilter(t, act, map[string]interface{}{"v": 5.0})
	assert.False(t, out.Passed)
	assert.Nil(t, out.Message, "message must be nil when the event is filtered out")
}

// TestFilter_Reason_EmptyOnPass verifies that the Reason output is always empty
// when the message passes the filter — there is nothing to explain.
func TestFilter_Reason_EmptyOnPass(t *testing.T) {
	act := newFilterAct(t, &Settings{Field: "v", Operator: "gt", Value: "0"})
	out := runFilter(t, act, map[string]interface{}{"v": 10.0})
	assert.True(t, out.Passed)
	assert.Empty(t, out.Reason, "reason must be empty on a passing message")
}
