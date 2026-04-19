package split

import (
	"fmt"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// newTestTrigger builds a Trigger with default settings for unit testing.
// It does NOT need a live Kafka connection — it is only used to call
// routeMessage() and evaluateHandler(), which are Kafka-free.
func newTestTrigger(routingMode string) *Trigger {
	return &Trigger{
		settings: &Settings{
			Topic:         "test-topic",
			ConsumerGroup: "test-cg",
			RoutingMode:   routingMode,
		},
	}
}

// addMatched appends a matched-type handler with the given HandlerSettings to
// the trigger's matchedHandlers slice, mirroring what Initialize() does.
// Pre-compiles regex if the operator is "regex".
func addMatched(trig *Trigger, hs *HandlerSettings) *handler {
	et := EventTypeMatched
	h := &handler{hs: hs, eventType: et}
	if hs.Predicates != "" {
		preds, _ := hs.ParsedPredicates()
		h.parsedPreds = preds
		h.multiRegex = make(map[int]*regexp.Regexp)
		for i, p := range preds {
			if p.Operator == "regex" {
				h.multiRegex[i], _ = regexp.Compile(p.Value)
			}
		}
	} else if hs.Operator == "regex" && hs.Field != "" {
		h.singleRegex, _ = regexp.Compile(hs.Value)
	}
	trig.matchedHandlers = append(trig.matchedHandlers, h)
	return h
}

func addUnmatched(trig *Trigger, hs *HandlerSettings) *handler {
	h := &handler{hs: hs, eventType: EventTypeUnmatched}
	trig.unmatchedHandlers = append(trig.unmatchedHandlers, h)
	return h
}

func addEvalError(trig *Trigger, hs *HandlerSettings) *handler {
	h := &handler{hs: hs, eventType: EventTypeEvalError}
	trig.evalErrorHandlers = append(trig.evalErrorHandlers, h)
	return h
}

func addTap(trig *Trigger, hs *HandlerSettings) *handler {
	h := &handler{hs: hs, eventType: EventTypeAll}
	trig.allHandlers = append(trig.allHandlers, h)
	return h
}

// ─── validateSettings ────────────────────────────────────────────────────────

func TestValidateSettings_Valid(t *testing.T) {
	require.NoError(t, validateSettings(&Settings{Topic: "t", ConsumerGroup: "g"}))
}

func TestValidateSettings_ValidWithRoutingModes(t *testing.T) {
	require.NoError(t, validateSettings(&Settings{Topic: "t", ConsumerGroup: "g", RoutingMode: RoutingModeFirstMatch}))
	require.NoError(t, validateSettings(&Settings{Topic: "t", ConsumerGroup: "g", RoutingMode: RoutingModeAllMatch}))
}

func TestValidateSettings_MissingTopic(t *testing.T) {
	err := validateSettings(&Settings{ConsumerGroup: "g"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "topic")
}

func TestValidateSettings_BlankTopic(t *testing.T) {
	err := validateSettings(&Settings{Topic: "  ", ConsumerGroup: "g"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "topic")
}

func TestValidateSettings_MissingConsumerGroup(t *testing.T) {
	err := validateSettings(&Settings{Topic: "t"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "consumerGroup")
}

func TestValidateSettings_InvalidRoutingMode(t *testing.T) {
	err := validateSettings(&Settings{Topic: "t", ConsumerGroup: "g", RoutingMode: "bad-mode"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "routingMode")
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

func TestEvalSingle_Eq_String_Pass(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"region": "us-east"}, "region", "eq", "us-east", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Eq_String_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"region": "eu-west"}, "region", "eq", "us-east", false, nil)
	assert.False(t, ok)
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

func TestEvalSingle_Gt_Equal_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"temp": 30.0}, "temp", "gt", "30", false, nil)
	assert.False(t, ok)
}

func TestEvalSingle_Gte_PassEqual(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"v": 30.0}, "v", "gte", "30", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Gte_PassAbove(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"v": 31.0}, "v", "gte", "30", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Gte_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"v": 29.0}, "v", "gte", "30", false, nil)
	assert.False(t, ok)
}

func TestEvalSingle_Lt_Pass(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"v": 5.0}, "v", "lt", "10", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Lt_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"v": 15.0}, "v", "lt", "10", false, nil)
	assert.False(t, ok)
}

func TestEvalSingle_Lte_PassEqual(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"v": 10.0}, "v", "lte", "10", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_Lte_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"v": 11.0}, "v", "lte", "10", false, nil)
	assert.False(t, ok)
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

func TestEvalSingle_StartsWith_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"path": "/other/v1"}, "path", "startsWith", "/api", false, nil)
	assert.False(t, ok)
}

func TestEvalSingle_EndsWith_Pass(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"file": "report.csv"}, "file", "endsWith", ".csv", false, nil)
	assert.True(t, ok)
}

func TestEvalSingle_EndsWith_Fail(t *testing.T) {
	ok, _, _ := evalSingle(map[string]interface{}{"file": "report.pdf"}, "file", "endsWith", ".csv", false, nil)
	assert.False(t, ok)
}

func TestEvalSingle_Regex_Pass(t *testing.T) {
	compiled := regexp.MustCompile(`^USR-\d+$`)
	ok, _, errMsg := evalSingle(map[string]interface{}{"id": "USR-12345"}, "id", "regex", `^USR-\d+$`, false, compiled)
	assert.True(t, ok)
	assert.Empty(t, errMsg)
}

func TestEvalSingle_Regex_Fail(t *testing.T) {
	// compiled regex provided — field value does not match the pattern.
	compiled := regexp.MustCompile(`^USR-\d+$`)
	ok, reason, errMsg := evalSingle(map[string]interface{}{"id": "SVC-abc"}, "id", "regex", `^USR-\d+$`, false, compiled)
	assert.False(t, ok)
	assert.NotEmpty(t, reason)  // "SVC-abc" does not match regex ...
	assert.Empty(t, errMsg)     // not an eval error — just a non-match
}

func TestEvalSingle_Regex_Invalid(t *testing.T) {
	ok, _, errMsg := evalSingle(map[string]interface{}{"id": "x"}, "id", "regex", `[invalid`, false, nil)
	assert.False(t, ok)
	assert.NotEmpty(t, errMsg)
}

func TestEvalSingle_Regex_PrecompilledUsed(t *testing.T) {
	compiled := regexp.MustCompile(`^ORD-\d+$`)
	ok, _, _ := evalSingle(map[string]interface{}{"id": "ORD-999"}, "id", "regex", `^ORD-\d+$`, false, compiled)
	assert.True(t, ok)
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

func TestEvalSingle_EmptyOperator_Error(t *testing.T) {
	ok, _, errMsg := evalSingle(map[string]interface{}{"v": 1}, "v", "", "1", false, nil)
	assert.False(t, ok)
	assert.NotEmpty(t, errMsg)
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
	ok, _, _ := evalMulti(map[string]interface{}{"status": 200.0}, preds, "or", false, nil)
	assert.True(t, ok)
}

func TestEvalMulti_OR_SecondPasses(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "status", Operator: "eq", Value: "201"},
	}
	ok, _, _ := evalMulti(map[string]interface{}{"status": 201.0}, preds, "or", false, nil)
	assert.True(t, ok)
}

func TestEvalMulti_OR_NonePasses(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "status", Operator: "eq", Value: "201"},
	}
	ok, reason, _ := evalMulti(map[string]interface{}{"status": 500.0}, preds, "or", false, nil)
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

func TestEvalMulti_MissingField_AND_Block(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "missing", Operator: "eq", Value: "x"},
	}
	ok, _, _ := evalMulti(map[string]interface{}{"status": 200.0}, preds, "and", false, nil)
	assert.False(t, ok)
}

func TestEvalMulti_MissingField_AND_PassThrough(t *testing.T) {
	preds := []Predicate{
		{Field: "status", Operator: "eq", Value: "200"},
		{Field: "missing", Operator: "eq", Value: "x"},
	}
	ok, _, _ := evalMulti(map[string]interface{}{"status": 200.0}, preds, "and", true, nil)
	assert.True(t, ok)
}

func TestEvalMulti_MissingField_OR_PassThrough(t *testing.T) {
	// OR + passThroughOnMissing: a missing field is now "no opinion" — it is
	// skipped, NOT treated as an automatic pass.  If every predicate refers to
	// a missing field the OR evaluates to false (no predicate actually passed).
	preds := []Predicate{
		{Field: "missing", Operator: "eq", Value: "x"},
	}
	ok, _, _ := evalMulti(map[string]interface{}{}, preds, "or", true, nil)
	assert.False(t, ok) // was true (bug) — missing field must not auto-pass OR
}

func TestEvalMulti_MissingField_OR_PassThrough_PresentPredicateStillMatches(t *testing.T) {
	// OR + passThroughOnMissing: missing field is skipped; a present predicate
	// that passes makes the overall OR true.
	preds := []Predicate{
		{Field: "missing", Operator: "eq", Value: "x"},
		{Field: "status", Operator: "eq", Value: "ok"},
	}
	ok, _, _ := evalMulti(map[string]interface{}{"status": "ok"}, preds, "or", true, nil)
	assert.True(t, ok)
}

func TestEvalMulti_MixedRegexAndEq_AND_Pass(t *testing.T) {
	preds := []Predicate{
		{Field: "id", Operator: "regex", Value: `^ORD-\d+$`},
		{Field: "status", Operator: "eq", Value: "pending"},
	}
	compiled := regexp.MustCompile(`^ORD-\d+$`)
	compiledMap := map[int]*regexp.Regexp{0: compiled}
	ok, _, errMsg := evalMulti(
		map[string]interface{}{"id": "ORD-42", "status": "pending"},
		preds, "and", false, compiledMap,
	)
	assert.True(t, ok)
	assert.Empty(t, errMsg)
}

// TestEvalMulti_Regex_NilCompiled_Error tests that evalMulti returns a hard error
// when the compiled regex map is nil (handler not properly initialised via Initialize).
// Invalid-regex detection now happens at Initialize() time, not per-message.
func TestEvalMulti_Regex_NilCompiled_Error(t *testing.T) {
	preds := []Predicate{{Field: "id", Operator: "regex", Value: `^ok$`}}
	ok, _, errMsg := evalMulti(map[string]interface{}{"id": "ok"}, preds, "and", false, nil)
	assert.False(t, ok)
	assert.Contains(t, errMsg, "not pre-compiled")
}

// ─── evaluateHandler ─────────────────────────────────────────────────────────

func TestEvaluateHandler_SinglePredicate_Pass(t *testing.T) {
	trig := newTestTrigger("")
	hs := &HandlerSettings{Field: "status", Operator: "eq", Value: "ok"}
	h := &handler{hs: hs, eventType: EventTypeMatched}
	passed, _, evalErr := trig.evaluateHandler(map[string]interface{}{"status": "ok"}, h)
	assert.True(t, passed)
	assert.Empty(t, evalErr)
}

func TestEvaluateHandler_SinglePredicate_Fail(t *testing.T) {
	trig := newTestTrigger("")
	hs := &HandlerSettings{Field: "status", Operator: "eq", Value: "ok"}
	h := &handler{hs: hs, eventType: EventTypeMatched}
	passed, reason, _ := trig.evaluateHandler(map[string]interface{}{"status": "error"}, h)
	assert.False(t, passed)
	assert.NotEmpty(t, reason)
}

func TestEvaluateHandler_MultiPredicate_Pass(t *testing.T) {
	trig := newTestTrigger("")
	preds := `[{"field":"type","operator":"eq","value":"order"},{"field":"amount","operator":"gt","value":"100"}]`
	hs := &HandlerSettings{Predicates: preds, PredicateMode: "and"}
	ps, _ := hs.ParsedPredicates()
	h := &handler{hs: hs, eventType: EventTypeMatched, parsedPreds: ps, multiRegex: map[int]*regexp.Regexp{}}
	passed, _, errMsg := trig.evaluateHandler(map[string]interface{}{"type": "order", "amount": 150.0}, h)
	assert.True(t, passed)
	assert.Empty(t, errMsg)
}

func TestEvaluateHandler_MultiPredicate_Fail(t *testing.T) {
	trig := newTestTrigger("")
	preds := `[{"field":"type","operator":"eq","value":"order"},{"field":"amount","operator":"gt","value":"100"}]`
	hs := &HandlerSettings{Predicates: preds, PredicateMode: "and"}
	ps, _ := hs.ParsedPredicates()
	h := &handler{hs: hs, eventType: EventTypeMatched, parsedPreds: ps, multiRegex: map[int]*regexp.Regexp{}}
	passed, _, _ := trig.evaluateHandler(map[string]interface{}{"type": "order", "amount": 50.0}, h)
	assert.False(t, passed)
}

func TestEvaluateHandler_NoPredicate_AlwaysPass(t *testing.T) {
	// A handler with no Field and no Predicates always passes.
	trig := newTestTrigger("")
	h := &handler{hs: &HandlerSettings{}, eventType: EventTypeMatched}
	passed, _, evalErr := trig.evaluateHandler(map[string]interface{}{"x": 1}, h)
	assert.True(t, passed)
	assert.Empty(t, evalErr)
}

func TestEvaluateHandler_InvalidOperator_EvalError(t *testing.T) {
	trig := newTestTrigger("")
	hs := &HandlerSettings{Field: "v", Operator: "UNKNOWN", Value: "1"}
	h := &handler{hs: hs, eventType: EventTypeMatched}
	passed, _, evalErr := trig.evaluateHandler(map[string]interface{}{"v": 1}, h)
	assert.False(t, passed)
	assert.NotEmpty(t, evalErr)
}

// ─── routeMessage: first-match ────────────────────────────────────────────────

func TestRouteMessage_FirstMatch_OnlyFirstHandlerFires(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	hA := addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order", Priority: 0})
	hB := addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "payment", Priority: 1})

	d := trig.routeMessage(map[string]interface{}{"type": "order"})
	require.Len(t, d.matched, 1)
	assert.Same(t, hA, d.matched[0])
	_ = hB // not matched
	assert.Empty(t, d.unmatched)
	assert.Empty(t, d.evalErrors)
}

func TestRouteMessage_FirstMatch_SecondHandlerFires_WhenFirstFails(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	hA := addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order", Priority: 0})
	hB := addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "payment", Priority: 1})

	d := trig.routeMessage(map[string]interface{}{"type": "payment"})
	require.Len(t, d.matched, 1)
	assert.Same(t, hB, d.matched[0])
	_ = hA
}

func TestRouteMessage_FirstMatch_NoHandlerMatches_EmptyMatchedSlice(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order", Priority: 0})

	d := trig.routeMessage(map[string]interface{}{"type": "unknown"})
	assert.Empty(t, d.matched)
}

func TestRouteMessage_FirstMatch_PriorityOrdering_LowerFirst(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	// Both handlers match status >= 100, but hB has lower priority.
	hB := addMatched(trig, &HandlerSettings{Field: "status", Operator: "gte", Value: "100", Priority: 10})
	hA := addMatched(trig, &HandlerSettings{Field: "status", Operator: "gte", Value: "100", Priority: 0})
	// After sortSliceStable, hA (priority=0) should be evaluated first.
	// But addMatched appends hB then hA so the pre-sort order is [hB, hA].
	// Sort by priority: [hA(0), hB(10)].
	sortMatchedHandlers(trig)

	d := trig.routeMessage(map[string]interface{}{"status": 200.0})
	require.Len(t, d.matched, 1)
	assert.Same(t, hA, d.matched[0])
	_ = hB
}

func TestRouteMessage_FirstMatch_DefaultEmptyRoutingMode(t *testing.T) {
	// Empty RoutingMode should default to first-match behaviour.
	trig := newTestTrigger("")
	hA := addMatched(trig, &HandlerSettings{Field: "x", Operator: "eq", Value: "1", Priority: 0})
	hB := addMatched(trig, &HandlerSettings{Field: "x", Operator: "eq", Value: "1", Priority: 1})

	d := trig.routeMessage(map[string]interface{}{"x": "1"})
	require.Len(t, d.matched, 1)
	assert.Same(t, hA, d.matched[0])
	_ = hB
}

func TestRouteMessage_FirstMatch_ThreeHandlers_MiddleMatches(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order", Priority: 0})
	hB := addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "payment", Priority: 1})
	addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "refund", Priority: 2})

	d := trig.routeMessage(map[string]interface{}{"type": "payment"})
	require.Len(t, d.matched, 1)
	assert.Same(t, hB, d.matched[0])
}

// ─── routeMessage: all-match ──────────────────────────────────────────────────

func TestRouteMessage_AllMatch_BothHandlersFire(t *testing.T) {
	trig := newTestTrigger(RoutingModeAllMatch)
	hA := addMatched(trig, &HandlerSettings{Field: "priority", Operator: "gte", Value: "5"})
	hB := addMatched(trig, &HandlerSettings{Field: "region", Operator: "eq", Value: "us-east"})

	d := trig.routeMessage(map[string]interface{}{"priority": 7.0, "region": "us-east"})
	require.Len(t, d.matched, 2)
	assert.Same(t, hA, d.matched[0])
	assert.Same(t, hB, d.matched[1])
}

func TestRouteMessage_AllMatch_OnlyOneMatches(t *testing.T) {
	trig := newTestTrigger(RoutingModeAllMatch)
	hA := addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order"})
	hB := addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "payment"})

	d := trig.routeMessage(map[string]interface{}{"type": "order"})
	require.Len(t, d.matched, 1)
	assert.Same(t, hA, d.matched[0])
	_ = hB
}

func TestRouteMessage_AllMatch_NoneMatch_EmptyMatchedSlice(t *testing.T) {
	trig := newTestTrigger(RoutingModeAllMatch)
	addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order"})
	addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "payment"})

	d := trig.routeMessage(map[string]interface{}{"type": "refund"})
	assert.Empty(t, d.matched)
}

func TestRouteMessage_AllMatch_AllThreeMatch(t *testing.T) {
	trig := newTestTrigger(RoutingModeAllMatch)
	hA := addMatched(trig, &HandlerSettings{Field: "v", Operator: "gt", Value: "0"})
	hB := addMatched(trig, &HandlerSettings{Field: "v", Operator: "lt", Value: "100"})
	hC := addMatched(trig, &HandlerSettings{Field: "v", Operator: "neq", Value: "50"})

	d := trig.routeMessage(map[string]interface{}{"v": 42.0})
	require.Len(t, d.matched, 3)
	assert.Same(t, hA, d.matched[0])
	assert.Same(t, hB, d.matched[1])
	assert.Same(t, hC, d.matched[2])
}

// ─── routeMessage: unmatched ──────────────────────────────────────────────────

func TestRouteMessage_NoMatch_UnmatchedHandlerFires(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order"})
	hU := addUnmatched(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"type": "unknown"})
	assert.Empty(t, d.matched)
	require.Len(t, d.unmatched, 1)
	assert.Same(t, hU, d.unmatched[0])
}

func TestRouteMessage_WithMatch_UnmatchedHandlerDoesNotFire(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order"})
	addUnmatched(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"type": "order"})
	assert.Len(t, d.matched, 1)
	assert.Empty(t, d.unmatched)
}

func TestRouteMessage_NoHandlers_UnmatchedEmpty_NoMatchedFires(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	// No handlers registered at all.
	d := trig.routeMessage(map[string]interface{}{"type": "order"})
	assert.Empty(t, d.matched)
	assert.Empty(t, d.unmatched)
	assert.Empty(t, d.evalErrors)
	assert.Empty(t, d.tap)
}

func TestRouteMessage_MultipleUnmatchedHandlers_AllFire(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order"})
	hU1 := addUnmatched(trig, &HandlerSettings{})
	hU2 := addUnmatched(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"type": "unknown"})
	assert.Empty(t, d.matched)
	require.Len(t, d.unmatched, 2)
	assert.Same(t, hU1, d.unmatched[0])
	assert.Same(t, hU2, d.unmatched[1])
}

// ─── routeMessage: evalError ──────────────────────────────────────────────────

func TestRouteMessage_EvalError_EvalErrorHandlerFires(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	// Handler with an unsupported operator causes an eval error.
	addMatched(trig, &HandlerSettings{Field: "v", Operator: "UNKNOWN", Value: "1"})
	hE := addEvalError(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"v": 1})
	assert.Empty(t, d.matched)
	require.Len(t, d.evalErrors, 1)
	assert.Same(t, hE, d.evalErrors[0])
	assert.NotEmpty(t, d.evalErrReason)
}

func TestRouteMessage_EvalError_NoEvalErrorHandler_NoFire(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "v", Operator: "UNKNOWN", Value: "1"})
	// No evalError handler registered.

	d := trig.routeMessage(map[string]interface{}{"v": 1})
	assert.Empty(t, d.matched)
	assert.Empty(t, d.evalErrors)
	assert.NotEmpty(t, d.evalErrReason)
}

func TestRouteMessage_EvalError_ContinuesToNextHandler(t *testing.T) {
	// When a handler has an eval error, the router should skip it and continue
	// to the next handler, which may still produce a match.
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "v", Operator: "UNKNOWN", Value: "1", Priority: 0})
	hB := addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order", Priority: 1})
	addEvalError(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"v": 1, "type": "order"})
	// hB should still match even though hA had an eval error.
	require.Len(t, d.matched, 1)
	assert.Same(t, hB, d.matched[0])
	// evalError handler should also fire because there was an eval error.
	require.Len(t, d.evalErrors, 1)
}

func TestRouteMessage_EvalError_EvalErrReason_Populated(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "v", Operator: "", Value: "1"}) // empty operator = eval error
	addEvalError(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"v": 1})
	assert.NotEmpty(t, d.evalErrReason)
}

// ─── routeMessage: tap (all) handlers ────────────────────────────────────────

func TestRouteMessage_TapHandler_AlwaysFires_WhenMatch(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order"})
	hT := addTap(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"type": "order"})
	assert.Len(t, d.matched, 1)
	require.Len(t, d.tap, 1)
	assert.Same(t, hT, d.tap[0])
}

func TestRouteMessage_TapHandler_AlwaysFires_WhenNoMatch(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "type", Operator: "eq", Value: "order"})
	hT := addTap(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"type": "unknown"})
	assert.Empty(t, d.matched)
	require.Len(t, d.tap, 1)
	assert.Same(t, hT, d.tap[0])
}

func TestRouteMessage_TapHandler_AlwaysFires_WhenEvalError(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "v", Operator: "UNKNOWN", Value: "x"})
	hT := addTap(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"v": 1})
	require.Len(t, d.tap, 1)
	assert.Same(t, hT, d.tap[0])
}

func TestRouteMessage_MultipleTapHandlers_AllFire(t *testing.T) {
	trig := newTestTrigger(RoutingModeFirstMatch)
	hT1 := addTap(trig, &HandlerSettings{})
	hT2 := addTap(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"x": 1})
	require.Len(t, d.tap, 2)
	assert.Same(t, hT1, d.tap[0])
	assert.Same(t, hT2, d.tap[1])
}

// ─── routeMessage: complex scenarios ─────────────────────────────────────────

func TestRouteMessage_ContentBasedRouting_OrderProcessing(t *testing.T) {
	// Simulate a real use case: route orders by status.
	// priority=0: status=="pending"  → new-order branch
	// priority=1: status=="shipped"  → fulfillment branch
	// priority=2: status=="returned" → returns branch
	// unmatched: catch-all DLQ
	trig := newTestTrigger(RoutingModeFirstMatch)
	hPending := addMatched(trig, &HandlerSettings{Field: "status", Operator: "eq", Value: "pending", Priority: 0})
	hShipped := addMatched(trig, &HandlerSettings{Field: "status", Operator: "eq", Value: "shipped", Priority: 1})
	hReturned := addMatched(trig, &HandlerSettings{Field: "status", Operator: "eq", Value: "returned", Priority: 2})
	hDLQ := addUnmatched(trig, &HandlerSettings{})

	tests := []struct {
		status      string
		wantHandler *handler
		wantDLQ     bool
	}{
		{"pending", hPending, false},
		{"shipped", hShipped, false},
		{"returned", hReturned, false},
		{"cancelled", nil, true},
		{"unknown", nil, true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("status=%s", tc.status), func(t *testing.T) {
			d := trig.routeMessage(map[string]interface{}{"status": tc.status})
			if tc.wantDLQ {
				assert.Empty(t, d.matched)
				require.Len(t, d.unmatched, 1)
				assert.Same(t, hDLQ, d.unmatched[0])
			} else {
				require.Len(t, d.matched, 1)
				assert.Same(t, tc.wantHandler, d.matched[0])
				assert.Empty(t, d.unmatched)
			}
		})
	}
}

func TestRouteMessage_FanOut_SensorData_AllMatch(t *testing.T) {
	// Sensor data fan-out: route high-temp AND high-humidity to separate monitors.
	trig := newTestTrigger(RoutingModeAllMatch)
	hTemp := addMatched(trig, &HandlerSettings{Field: "temperature", Operator: "gt", Value: "80"})
	hHumidity := addMatched(trig, &HandlerSettings{Field: "humidity", Operator: "gt", Value: "90"})

	// Both conditions met — both handlers fire.
	d := trig.routeMessage(map[string]interface{}{"temperature": 85.0, "humidity": 95.0})
	require.Len(t, d.matched, 2)
	assert.Same(t, hTemp, d.matched[0])
	assert.Same(t, hHumidity, d.matched[1])

	// Only temperature — only hTemp fires.
	d2 := trig.routeMessage(map[string]interface{}{"temperature": 85.0, "humidity": 70.0})
	require.Len(t, d2.matched, 1)
	assert.Same(t, hTemp, d2.matched[0])
}

func TestRouteMessage_MultiPredicate_AND_ContentRouting(t *testing.T) {
	// Route high-value US orders to premium branch.
	trig := newTestTrigger(RoutingModeFirstMatch)
	premiumPreds := `[{"field":"amount","operator":"gt","value":"500"},{"field":"region","operator":"eq","value":"us-east"}]`
	hPremium := addMatched(trig, &HandlerSettings{Predicates: premiumPreds, PredicateMode: "and", Priority: 0})
	hStandard := addMatched(trig, &HandlerSettings{Field: "amount", Operator: "gt", Value: "0", Priority: 1})

	// Premium order.
	d := trig.routeMessage(map[string]interface{}{"amount": 1000.0, "region": "us-east"})
	require.Len(t, d.matched, 1)
	assert.Same(t, hPremium, d.matched[0])

	// Standard order (not us-east) — falls through to standard handler.
	d2 := trig.routeMessage(map[string]interface{}{"amount": 1000.0, "region": "eu-west"})
	require.Len(t, d2.matched, 1)
	assert.Same(t, hStandard, d2.matched[0])
}

func TestRouteMessage_MultiPredicate_OR_RouteToNotifications(t *testing.T) {
	// Notify on either critical severity OR error level.
	trig := newTestTrigger(RoutingModeFirstMatch)
	orPreds := `[{"field":"severity","operator":"eq","value":"critical"},{"field":"level","operator":"eq","value":"error"}]`
	hNotify := addMatched(trig, &HandlerSettings{Predicates: orPreds, PredicateMode: "or", Priority: 0})
	hU := addUnmatched(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"severity": "critical", "level": "info"})
	require.Len(t, d.matched, 1)
	assert.Same(t, hNotify, d.matched[0])
	_ = hU

	d2 := trig.routeMessage(map[string]interface{}{"severity": "low", "level": "error"})
	require.Len(t, d2.matched, 1)
	assert.Same(t, hNotify, d2.matched[0])

	d3 := trig.routeMessage(map[string]interface{}{"severity": "low", "level": "info"})
	assert.Empty(t, d3.matched)
	require.Len(t, d3.unmatched, 1)
}

func TestRouteMessage_RegexRouting_URLPath(t *testing.T) {
	// Route API requests by URL path pattern.
	trig := newTestTrigger(RoutingModeFirstMatch)
	hAPI := addMatched(trig, &HandlerSettings{Field: "path", Operator: "regex", Value: `^/api/v[12]/`, Priority: 0})
	hStatic := addMatched(trig, &HandlerSettings{Field: "path", Operator: "startsWith", Value: "/static/", Priority: 1})
	hDLQ := addUnmatched(trig, &HandlerSettings{})

	tests := []struct {
		path    string
		handler *handler
		dlq     bool
	}{
		{"/api/v1/users", hAPI, false},
		{"/api/v2/orders", hAPI, false},
		{"/static/css/main.css", hStatic, false},
		{"/unknown", nil, true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			d := trig.routeMessage(map[string]interface{}{"path": tc.path})
			if tc.dlq {
				assert.Empty(t, d.matched)
				require.Len(t, d.unmatched, 1)
				assert.Same(t, hDLQ, d.unmatched[0])
			} else {
				require.Len(t, d.matched, 1)
				assert.Same(t, tc.handler, d.matched[0])
			}
		})
	}
}

func TestRouteMessage_DefaultBranch_NoPredicate_LastPriority(t *testing.T) {
	// A matched handler with no predicates always passes.  Place it last
	// as the "default" branch in first-match mode.
	trig := newTestTrigger(RoutingModeFirstMatch)
	hCritical := addMatched(trig, &HandlerSettings{Field: "priority", Operator: "eq", Value: "critical", Priority: 0})
	hDefault := addMatched(trig, &HandlerSettings{Priority: 99}) // no predicates → always pass
	sortMatchedHandlers(trig)

	// Critical priority → hCritical fires.
	d := trig.routeMessage(map[string]interface{}{"priority": "critical"})
	require.Len(t, d.matched, 1)
	assert.Same(t, hCritical, d.matched[0])

	// Anything else → hDefault fires (always-pass).
	d2 := trig.routeMessage(map[string]interface{}{"priority": "low"})
	require.Len(t, d2.matched, 1)
	assert.Same(t, hDefault, d2.matched[0])
}

func TestRouteMessage_AllMatch_WithUnmatchedAndTap(t *testing.T) {
	// all-match: some handlers match, unmatched does NOT fire, tap ALWAYS fires.
	trig := newTestTrigger(RoutingModeAllMatch)
	hA := addMatched(trig, &HandlerSettings{Field: "x", Operator: "gt", Value: "0"})
	hU := addUnmatched(trig, &HandlerSettings{})
	hT := addTap(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"x": 5.0})
	require.Len(t, d.matched, 1)
	assert.Same(t, hA, d.matched[0])
	assert.Empty(t, d.unmatched) // has a match → no unmatched
	require.Len(t, d.tap, 1)
	assert.Same(t, hT, d.tap[0])
	_ = hU
}

func TestRouteMessage_EvalError_MatchedAndUnmatchedAndEvalError_AllFire(t *testing.T) {
	// Scenario: first handler errors, second handler matches,
	// evalError handler fires, unmatched does NOT fire, tap fires.
	trig := newTestTrigger(RoutingModeFirstMatch)
	addMatched(trig, &HandlerSettings{Field: "v", Operator: "UNKNOWN", Value: "x", Priority: 0})
	hB := addMatched(trig, &HandlerSettings{Field: "ok", Operator: "eq", Value: "yes", Priority: 1})
	hE := addEvalError(trig, &HandlerSettings{})
	hU := addUnmatched(trig, &HandlerSettings{})
	hT := addTap(trig, &HandlerSettings{})

	d := trig.routeMessage(map[string]interface{}{"v": 1, "ok": "yes"})
	// hB matches (first handler errored, second matched).
	require.Len(t, d.matched, 1)
	assert.Same(t, hB, d.matched[0])
	// evalError fires because hA had an error.
	require.Len(t, d.evalErrors, 1)
	assert.Same(t, hE, d.evalErrors[0])
	// unmatched does NOT fire (there is a match).
	assert.Empty(t, d.unmatched)
	// tap always fires.
	require.Len(t, d.tap, 1)
	assert.Same(t, hT, d.tap[0])
	_ = hU
}

// ─── ParsedPredicates ────────────────────────────────────────────────────────

func TestParsedPredicates_ValidJSON(t *testing.T) {
	hs := &HandlerSettings{Predicates: `[{"field":"f","operator":"eq","value":"v"}]`}
	preds, err := hs.ParsedPredicates()
	require.NoError(t, err)
	require.Len(t, preds, 1)
	assert.Equal(t, "f", preds[0].Field)
	assert.Equal(t, "eq", preds[0].Operator)
	assert.Equal(t, "v", preds[0].Value)
}

func TestParsedPredicates_EmptyString_ReturnsNil(t *testing.T) {
	hs := &HandlerSettings{}
	preds, err := hs.ParsedPredicates()
	require.NoError(t, err)
	assert.Nil(t, preds)
}

func TestParsedPredicates_InvalidJSON_Error(t *testing.T) {
	hs := &HandlerSettings{Predicates: `{bad json`}
	_, err := hs.ParsedPredicates()
	require.Error(t, err)
}

// ─── Output.ToMap / FromMap round-trip ───────────────────────────────────────

func TestOutput_ToMap_FromMap_RoundTrip(t *testing.T) {
	orig := &Output{
		Message:            map[string]interface{}{"key": "value", "num": float64(42)},
		Topic:              "my-topic",
		Partition:          3,
		Offset:             1024,
		Key:                "msg-key",
		MatchedHandlerName: "branch-A",
		RoutingMode:        RoutingModeFirstMatch,
		EvalError:          false,
		EvalErrorReason:    "",
	}
	m := orig.ToMap()

	restored := &Output{}
	require.NoError(t, restored.FromMap(m))

	assert.Equal(t, orig.Topic, restored.Topic)
	assert.Equal(t, orig.Partition, restored.Partition)
	assert.Equal(t, orig.Offset, restored.Offset)
	assert.Equal(t, orig.Key, restored.Key)
	assert.Equal(t, orig.MatchedHandlerName, restored.MatchedHandlerName)
	assert.Equal(t, orig.RoutingMode, restored.RoutingMode)
	assert.Equal(t, orig.EvalError, restored.EvalError)
	assert.Equal(t, orig.EvalErrorReason, restored.EvalErrorReason)
}

func TestOutput_ToMap_FromMap_EvalError_RoundTrip(t *testing.T) {
	orig := &Output{
		EvalError:       true,
		EvalErrorReason: "unsupported operator",
		RoutingMode:     RoutingModeAllMatch,
	}
	m := orig.ToMap()
	restored := &Output{}
	require.NoError(t, restored.FromMap(m))
	assert.True(t, restored.EvalError)
	assert.Equal(t, "unsupported operator", restored.EvalErrorReason)
}

// ─── resolveBalanceStrategy ──────────────────────────────────────────────────

func TestResolveBalanceStrategy_RoundRobin(t *testing.T) {
	s := resolveBalanceStrategy("roundrobin")
	assert.NotNil(t, s)
}

func TestResolveBalanceStrategy_Sticky(t *testing.T) {
	s := resolveBalanceStrategy("sticky")
	assert.NotNil(t, s)
}

func TestResolveBalanceStrategy_Range(t *testing.T) {
	s := resolveBalanceStrategy("range")
	assert.NotNil(t, s)
}

func TestResolveBalanceStrategy_Empty_DefaultRoundRobin(t *testing.T) {
	s := resolveBalanceStrategy("")
	assert.NotNil(t, s)
}

func TestResolveBalanceStrategy_Unknown_DefaultRoundRobin(t *testing.T) {
	s := resolveBalanceStrategy("unknown")
	assert.NotNil(t, s)
}

// ─── sortMatchedHandlers helper (only used in tests) ─────────────────────────

// sortMatchedHandlers mirrors the sort.SliceStable call in Initialize()
// so test setup can produce a deterministic priority order without a live broker.
func sortMatchedHandlers(trig *Trigger) {
	sort.SliceStable(trig.matchedHandlers, func(i, j int) bool {
		return trig.matchedHandlers[i].hs.Priority < trig.matchedHandlers[j].hs.Priority
	})
}
