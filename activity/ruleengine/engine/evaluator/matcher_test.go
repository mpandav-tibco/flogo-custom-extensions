package evaluator

import (
	"encoding/json"
	"testing"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/model"
)

// ─── mock document ────────────────────────────────────────────────────────────
// mapDoc satisfies parser.Document using a plain Go map — no real parser needed.

type mapDoc struct {
	root map[string]interface{}
}

func (d *mapDoc) Root() interface{} { return d.root }

func (d *mapDoc) ResolveScope(path string) ([]interface{}, error) {
	if path == "" {
		return []interface{}{d.root}, nil
	}
	val, ok := d.root[path]
	if !ok {
		return nil, nil
	}
	if arr, ok := val.([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{val}, nil
}

func (d *mapDoc) ResolvePath(obj interface{}, path string) (interface{}, bool) {
	if path == "" {
		return obj, true
	}
	m, ok := obj.(map[string]interface{})
	if !ok {
		return nil, false
	}
	val, found := m[path]
	return val, found
}

func doc(pairs ...interface{}) *mapDoc {
	m := make(map[string]interface{})
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i].(string)] = pairs[i+1]
	}
	return &mapDoc{root: m}
}

func cond(typ, path string, extras ...interface{}) model.Condition {
	c := model.Condition{Type: typ, Path: path}
	for i := 0; i+1 < len(extras); i += 2 {
		switch extras[i].(string) {
		case "value":
			c.Value = extras[i+1]
		case "substring":
			c.Substring = extras[i+1].(string)
		case "substrings":
			c.Substrings = extras[i+1].([]string)
		case "pattern":
			c.Pattern = extras[i+1].(string)
		case "flags":
			c.Flags = extras[i+1].([]string)
		case "conditions":
			c.Conditions = extras[i+1].([]model.Condition)
		}
	}
	return c
}

// ─── missing ──────────────────────────────────────────────────────────────────

func TestMatcher_Missing_FieldAbsent(t *testing.T) {
	d := doc()
	r, err := EvaluateCondition(cond("missing", "key"), d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected match for absent field: err=%v matched=%v", err, r.Matched)
	}
}

func TestMatcher_Missing_FieldPresent(t *testing.T) {
	d := doc("key", "value")
	r, _ := EvaluateCondition(cond("missing", "key"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match when field is present")
	}
}

func TestMatcher_Missing_NilValue(t *testing.T) {
	d := doc("key", nil)
	r, _ := EvaluateCondition(cond("missing", "key"), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for nil value (treated as missing)")
	}
}

func TestMatcher_Missing_EmptyString(t *testing.T) {
	d := doc("key", "")
	r, _ := EvaluateCondition(cond("missing", "key"), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for empty string (treated as missing)")
	}
}

func TestMatcher_Missing_EmptyArray(t *testing.T) {
	d := doc("key", []interface{}{})
	r, _ := EvaluateCondition(cond("missing", "key"), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for empty array (treated as missing)")
	}
}

func TestMatcher_Missing_NonEmptyArray(t *testing.T) {
	d := doc("key", []interface{}{"a", "b"})
	r, _ := EvaluateCondition(cond("missing", "key"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for non-empty array")
	}
}

// ─── exists ───────────────────────────────────────────────────────────────────

func TestMatcher_Exists_FieldPresent(t *testing.T) {
	d := doc("key", "value")
	r, err := EvaluateCondition(cond("exists", "key"), d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected match for existing field: err=%v", err)
	}
}

func TestMatcher_Exists_FieldAbsent(t *testing.T) {
	d := doc()
	r, _ := EvaluateCondition(cond("exists", "key"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for absent field")
	}
}

func TestMatcher_Exists_NilValue(t *testing.T) {
	d := doc("key", nil)
	r, _ := EvaluateCondition(cond("exists", "key"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for nil value")
	}
}

func TestMatcher_Exists_EmptyString(t *testing.T) {
	d := doc("key", "")
	r, _ := EvaluateCondition(cond("exists", "key"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for empty string (not considered to exist)")
	}
}

func TestMatcher_Exists_NonEmptyArray(t *testing.T) {
	d := doc("key", []interface{}{"item"})
	r, _ := EvaluateCondition(cond("exists", "key"), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for non-empty array")
	}
}

func TestMatcher_Exists_EmptyArray(t *testing.T) {
	d := doc("key", []interface{}{})
	r, _ := EvaluateCondition(cond("exists", "key"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for empty array")
	}
}

// ─── equals ───────────────────────────────────────────────────────────────────

func TestMatcher_Equals_StringMatch(t *testing.T) {
	d := doc("status", "active")
	r, _ := EvaluateCondition(cond("equals", "status", "value", "active"), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for equal strings")
	}
}

func TestMatcher_Equals_StringMismatch(t *testing.T) {
	d := doc("status", "inactive")
	r, _ := EvaluateCondition(cond("equals", "status", "value", "active"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for unequal strings")
	}
}

func TestMatcher_Equals_CaseSensitive(t *testing.T) {
	d := doc("status", "Active")
	r, _ := EvaluateCondition(cond("equals", "status", "value", "active"), d, d.root)
	if r.Matched {
		t.Fatal("equals should be case-sensitive")
	}
}

func TestMatcher_Equals_NumberMatch(t *testing.T) {
	d := doc("timeout", 30)
	r, _ := EvaluateCondition(cond("equals", "timeout", "value", 30), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for equal numbers")
	}
}

func TestMatcher_Equals_NumberViaStringComparison(t *testing.T) {
	// JSON unmarshals numbers as float64; rule value might be int
	d := doc("timeout", float64(30))
	r, _ := EvaluateCondition(cond("equals", "timeout", "value", 30), d, d.root)
	if !r.Matched {
		t.Fatal("expected match via string comparison fallback (30 == 30)")
	}
}

func TestMatcher_Equals_FieldAbsent(t *testing.T) {
	d := doc()
	r, _ := EvaluateCondition(cond("equals", "missing", "value", "x"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match when field absent")
	}
}

func TestMatcher_Equals_BoolTrue(t *testing.T) {
	d := doc("enabled", true)
	r, _ := EvaluateCondition(cond("equals", "enabled", "value", true), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for boolean true")
	}
}

// ─── not_equals ───────────────────────────────────────────────────────────────

func TestMatcher_NotEquals_Mismatch_Matches(t *testing.T) {
	d := doc("status", "inactive")
	r, _ := EvaluateCondition(cond("not_equals", "status", "value", "active"), d, d.root)
	if !r.Matched {
		t.Fatal("expected match when values differ")
	}
}

func TestMatcher_NotEquals_SameValue_NoMatch(t *testing.T) {
	d := doc("status", "active")
	r, _ := EvaluateCondition(cond("not_equals", "status", "value", "active"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match when values are equal")
	}
}

func TestMatcher_NotEquals_AbsentField_Matches(t *testing.T) {
	// Absent field → equals returns false → not_equals returns true
	d := doc()
	r, _ := EvaluateCondition(cond("not_equals", "missing", "value", "x"), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for absent field (not_equals semantics)")
	}
}

// ─── contains ─────────────────────────────────────────────────────────────────

func TestMatcher_Contains_Found(t *testing.T) {
	d := doc("image", "nginx:latest")
	r, _ := EvaluateCondition(cond("contains", "image", "substring", ":latest"), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for contained substring")
	}
}

func TestMatcher_Contains_NotFound(t *testing.T) {
	d := doc("image", "nginx:1.25")
	r, _ := EvaluateCondition(cond("contains", "image", "substring", ":latest"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match when substring not present")
	}
}

func TestMatcher_Contains_AbsentField(t *testing.T) {
	d := doc()
	r, _ := EvaluateCondition(cond("contains", "image", "substring", "x"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for absent field")
	}
}

func TestMatcher_Contains_EmptySubstring(t *testing.T) {
	d := doc("key", "value")
	r, _ := EvaluateCondition(cond("contains", "key", "substring", ""), d, d.root)
	if !r.Matched {
		t.Fatal("empty substring is always contained")
	}
}

// ─── not_contains ─────────────────────────────────────────────────────────────

func TestMatcher_NotContains_SubstringAbsent(t *testing.T) {
	d := doc("image", "nginx:1.25")
	r, _ := EvaluateCondition(cond("not_contains", "image", "substring", ":latest"), d, d.root)
	if !r.Matched {
		t.Fatal("expected match when substring not present")
	}
}

func TestMatcher_NotContains_SubstringPresent(t *testing.T) {
	d := doc("image", "nginx:latest")
	r, _ := EvaluateCondition(cond("not_contains", "image", "substring", ":latest"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match when substring is present")
	}
}

func TestMatcher_NotContains_AbsentField_Matches(t *testing.T) {
	// Absent field → contains returns false → not_contains returns true
	d := doc()
	r, _ := EvaluateCondition(cond("not_contains", "image", "substring", ":latest"), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for absent field (not_contains semantics)")
	}
}

// ─── contains_any ─────────────────────────────────────────────────────────────

func TestMatcher_ContainsAny_OneMatches(t *testing.T) {
	d := doc("ref", "github.com/tibco/rest")
	r, _ := EvaluateCondition(cond("contains_any", "ref",
		"substrings", []string{"tibco/log", "tibco/rest", "tibco/mapper"}), d, d.root)
	if !r.Matched {
		t.Fatal("expected match when one substring matches")
	}
}

func TestMatcher_ContainsAny_NoneMatch(t *testing.T) {
	d := doc("ref", "github.com/project-flogo/core")
	r, _ := EvaluateCondition(cond("contains_any", "ref",
		"substrings", []string{"tibco/log", "tibco/rest"}), d, d.root)
	if r.Matched {
		t.Fatal("expected no match when no substring matches")
	}
}

func TestMatcher_ContainsAny_EmptySubstrings(t *testing.T) {
	d := doc("ref", "anything")
	r, _ := EvaluateCondition(cond("contains_any", "ref",
		"substrings", []string{}), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for empty substrings list")
	}
}

func TestMatcher_ContainsAny_AbsentField(t *testing.T) {
	d := doc()
	r, _ := EvaluateCondition(cond("contains_any", "ref",
		"substrings", []string{"x"}), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for absent field")
	}
}

// ─── regex ────────────────────────────────────────────────────────────────────

func TestMatcher_Regex_Matches(t *testing.T) {
	d := doc("image", "nginx:latest")
	r, err := EvaluateCondition(cond("regex", "image", "pattern", ":latest$"), d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected regex match: err=%v", err)
	}
}

func TestMatcher_Regex_NoMatch(t *testing.T) {
	d := doc("image", "nginx:1.25.0")
	r, _ := EvaluateCondition(cond("regex", "image", "pattern", ":latest$"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match")
	}
}

func TestMatcher_Regex_CaseInsensitiveFlag(t *testing.T) {
	d := doc("status", "ERROR")
	r, err := EvaluateCondition(cond("regex", "status",
		"pattern", "error",
		"flags", []string{"i"}), d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected case-insensitive match: err=%v", err)
	}
}

func TestMatcher_Regex_InvalidPattern_Error(t *testing.T) {
	d := doc("key", "value")
	_, err := EvaluateCondition(cond("regex", "key", "pattern", "[invalid"), d, d.root)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestMatcher_Regex_EmptyPath_MatchesScope(t *testing.T) {
	d := doc()
	// When path is empty, regex applies to stringify(scope) — the whole map
	r, err := EvaluateCondition(cond("regex", "", "pattern", "map\\["), d, d.root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Result depends on map stringification — just verify no crash
	_ = r.Matched
}

func TestMatcher_Regex_AbsentField(t *testing.T) {
	d := doc()
	r, _ := EvaluateCondition(cond("regex", "missing", "pattern", ".+"), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for absent field")
	}
}

// ─── greater_than ─────────────────────────────────────────────────────────────

func TestMatcher_GreaterThan_NumberAbove(t *testing.T) {
	d := doc("timeout", float64(60))
	r, _ := EvaluateCondition(cond("greater_than", "timeout", "value", 30), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for 60 > 30")
	}
}

func TestMatcher_GreaterThan_Equal_NoMatch(t *testing.T) {
	d := doc("timeout", float64(30))
	r, _ := EvaluateCondition(cond("greater_than", "timeout", "value", 30), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for equal values (strict greater)")
	}
}

func TestMatcher_GreaterThan_NumberBelow(t *testing.T) {
	d := doc("timeout", float64(10))
	r, _ := EvaluateCondition(cond("greater_than", "timeout", "value", 30), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for 10 > 30")
	}
}

func TestMatcher_GreaterThan_StringNumber(t *testing.T) {
	d := doc("port", "8080")
	r, _ := EvaluateCondition(cond("greater_than", "port", "value", 1024), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for string '8080' > 1024")
	}
}

func TestMatcher_GreaterThan_AbsentField(t *testing.T) {
	d := doc()
	r, _ := EvaluateCondition(cond("greater_than", "missing", "value", 0), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for absent field")
	}
}

func TestMatcher_GreaterThan_NonNumericString_Error(t *testing.T) {
	d := doc("key", "notanumber")
	_, err := EvaluateCondition(cond("greater_than", "key", "value", 0), d, d.root)
	if err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

// ─── less_than ────────────────────────────────────────────────────────────────

func TestMatcher_LessThan_NumberBelow(t *testing.T) {
	d := doc("timeout", float64(5))
	r, _ := EvaluateCondition(cond("less_than", "timeout", "value", 30), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for 5 < 30")
	}
}

func TestMatcher_LessThan_Equal_NoMatch(t *testing.T) {
	d := doc("timeout", float64(30))
	r, _ := EvaluateCondition(cond("less_than", "timeout", "value", 30), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for equal values (strict less)")
	}
}

func TestMatcher_LessThan_ZeroValue(t *testing.T) {
	d := doc("timeout", float64(0))
	r, _ := EvaluateCondition(cond("less_than", "timeout", "value", 1), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for 0 < 1")
	}
}

// ─── count_exceeds ────────────────────────────────────────────────────────────

func TestMatcher_CountExceeds_LargeArray(t *testing.T) {
	d := doc("items", []interface{}{"a", "b", "c", "d"})
	r, _ := EvaluateCondition(cond("count_exceeds", "items", "value", 3), d, d.root)
	if !r.Matched {
		t.Fatal("expected match for array of 4 > 3")
	}
}

func TestMatcher_CountExceeds_ExactCount_NoMatch(t *testing.T) {
	d := doc("items", []interface{}{"a", "b", "c"})
	r, _ := EvaluateCondition(cond("count_exceeds", "items", "value", 3), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for exact count (strict greater)")
	}
}

func TestMatcher_CountExceeds_SmallArray(t *testing.T) {
	d := doc("items", []interface{}{"a"})
	r, _ := EvaluateCondition(cond("count_exceeds", "items", "value", 3), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for small array")
	}
}

func TestMatcher_CountExceeds_EmptyArray(t *testing.T) {
	d := doc("items", []interface{}{})
	r, _ := EvaluateCondition(cond("count_exceeds", "items", "value", 0), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for empty array with threshold 0 (strict greater)")
	}
}

func TestMatcher_CountExceeds_NonArray_NoMatch(t *testing.T) {
	d := doc("items", "not an array")
	r, _ := EvaluateCondition(cond("count_exceeds", "items", "value", 0), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for non-array value")
	}
}

func TestMatcher_CountExceeds_AbsentField(t *testing.T) {
	d := doc()
	r, _ := EvaluateCondition(cond("count_exceeds", "missing", "value", 0), d, d.root)
	if r.Matched {
		t.Fatal("expected no match for absent field")
	}
}

// ─── any_of (composite) ───────────────────────────────────────────────────────

func TestMatcher_AnyOf_FirstMatches(t *testing.T) {
	d := doc("image", "nginx:latest")
	c := model.Condition{
		Type: "any_of",
		Conditions: []model.Condition{
			{Type: "regex", Path: "image", Pattern: ":latest$"},
			{Type: "not_contains", Path: "image", Substring: ":"},
		},
	}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected any_of match when first condition matches: err=%v", err)
	}
}

func TestMatcher_AnyOf_LastMatches(t *testing.T) {
	d := doc("image", "busybox")
	c := model.Condition{
		Type: "any_of",
		Conditions: []model.Condition{
			{Type: "regex", Path: "image", Pattern: ":latest$"},
			{Type: "not_contains", Path: "image", Substring: ":"},
		},
	}
	r, _ := EvaluateCondition(c, d, d.root)
	if !r.Matched {
		t.Fatal("expected any_of match when last condition matches")
	}
}

func TestMatcher_AnyOf_NoneMatch(t *testing.T) {
	d := doc("image", "nginx:1.25")
	c := model.Condition{
		Type: "any_of",
		Conditions: []model.Condition{
			{Type: "regex", Path: "image", Pattern: ":latest$"},
			{Type: "not_contains", Path: "image", Substring: ":"},
		},
	}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected any_of no-match when none match")
	}
}

func TestMatcher_AnyOf_EmptyConditions(t *testing.T) {
	d := doc()
	c := model.Condition{Type: "any_of", Conditions: []model.Condition{}}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match for empty any_of")
	}
}

// ─── all_of (composite) ───────────────────────────────────────────────────────

func TestMatcher_AllOf_AllMatch(t *testing.T) {
	d := doc("ref", "#rest", "timeout", float64(0))
	c := model.Condition{
		Type: "all_of",
		Conditions: []model.Condition{
			{Type: "contains", Path: "ref", Substring: "rest"},
			{Type: "equals", Path: "timeout", Value: float64(0)},
		},
	}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected all_of match: err=%v", err)
	}
}

func TestMatcher_AllOf_OneFails(t *testing.T) {
	d := doc("ref", "#rest", "timeout", float64(30))
	c := model.Condition{
		Type: "all_of",
		Conditions: []model.Condition{
			{Type: "contains", Path: "ref", Substring: "rest"},
			{Type: "equals", Path: "timeout", Value: float64(0)},
		},
	}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no all_of match when one condition fails")
	}
}

func TestMatcher_AllOf_EmptyConditions_NoMatch(t *testing.T) {
	// all_of with zero sub-conditions must NOT match.
	// A vacuously-true all_of would fire against every scope item,
	// producing false positives — the corrected behaviour returns false.
	d := doc()
	c := model.Condition{Type: "all_of", Conditions: []model.Condition{}}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("all_of with empty conditions must not match (would cause false positives)")
	}
}

// ─── none_of (composite) ──────────────────────────────────────────────────────

func TestMatcher_NoneOf_NoneMatch(t *testing.T) {
	d := doc("status", "ok")
	c := model.Condition{
		Type: "none_of",
		Conditions: []model.Condition{
			{Type: "equals", Path: "status", Value: "error"},
			{Type: "equals", Path: "status", Value: "warning"},
		},
	}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected none_of match when none match: err=%v", err)
	}
}

func TestMatcher_NoneOf_OneMatches(t *testing.T) {
	d := doc("status", "error")
	c := model.Condition{
		Type: "none_of",
		Conditions: []model.Condition{
			{Type: "equals", Path: "status", Value: "error"},
			{Type: "equals", Path: "status", Value: "warning"},
		},
	}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no none_of match when one condition matches")
	}
}

func TestMatcher_NoneOf_EmptyConditions_NoMatch(t *testing.T) {
	// An empty none_of is a misconfigured rule — it must not fire on every scope
	// item. Mirrors the all_of: [] guard. Vacuous truth here would cause a
	// rule with no conditions to produce a finding for every scope item.
	d := doc()
	c := model.Condition{Type: "none_of", Conditions: []model.Condition{}}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match for empty none_of (misconfigured rule guard)")
	}
}

// ─── nested composites ────────────────────────────────────────────────────────

func TestMatcher_NestedComposite_AnyOfInsideAllOf(t *testing.T) {
	// all_of: [ref contains 'rest', any_of: [timeout=0, timeout missing]]
	d := doc("ref", "github.com/rest", "timeout", float64(0))
	c := model.Condition{
		Type: "all_of",
		Conditions: []model.Condition{
			{Type: "contains", Path: "ref", Substring: "rest"},
			{
				Type: "any_of",
				Conditions: []model.Condition{
					{Type: "equals", Path: "timeout", Value: float64(0)},
					{Type: "missing", Path: "timeout"},
				},
			},
		},
	}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected nested composite match: err=%v", err)
	}
}

func TestMatcher_NestedComposite_DeepNesting(t *testing.T) {
	d := doc("x", "target")
	c := model.Condition{
		Type: "any_of",
		Conditions: []model.Condition{
			{
				Type: "all_of",
				Conditions: []model.Condition{
					{
						Type: "none_of",
						Conditions: []model.Condition{
							{Type: "equals", Path: "x", Value: "other"},
						},
					},
					{Type: "equals", Path: "x", Value: "target"},
				},
			},
		},
	}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected deep nested match: err=%v", err)
	}
}

// ─── expression (phase 2 placeholder) ────────────────────────────────────────

func TestMatcher_Expression_ReturnsError(t *testing.T) {
	d := doc("key", "val")
	_, err := EvaluateCondition(cond("expression", "key"), d, d.root)
	if err == nil {
		t.Fatal("expected error for expression type (Phase 2 placeholder)")
	}
}

// ─── unknown type ─────────────────────────────────────────────────────────────

func TestMatcher_UnknownType_Error(t *testing.T) {
	d := doc()
	_, err := EvaluateCondition(cond("superfluous_type", "key"), d, d.root)
	if err == nil {
		t.Fatal("expected error for unknown match type")
	}
}

// ─── none_contain ─────────────────────────────────────────────────────────────

func TestMatcher_NoneContain_EmptyKeysAndSubstrings_NoMatch(t *testing.T) {
	// Misconfigured rule with no keys or substrings must NOT vacuously match.
	d := doc("headers", map[string]interface{}{"Authorization": "Bearer token"})
	c := model.Condition{Type: "none_contain", Path: "headers"}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("none_contain with no keys or substrings must not match (would cause false positives)")
	}
}

func TestMatcher_NoneContain_KeyPresent_NoMatch(t *testing.T) {
	d := doc("headers", map[string]interface{}{"Authorization": "Bearer token"})
	c := model.Condition{Type: "none_contain", Path: "headers", Keys: []string{"Authorization"}}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match when forbidden key is present")
	}
}

func TestMatcher_NoneContain_KeyAbsent_Matches(t *testing.T) {
	d := doc("headers", map[string]interface{}{"Content-Type": "application/json"})
	c := model.Condition{Type: "none_contain", Path: "headers", Keys: []string{"Authorization"}}
	r, _ := EvaluateCondition(c, d, d.root)
	if !r.Matched {
		t.Fatal("expected match when none of the forbidden keys are present")
	}
}

func TestMatcher_NoneContain_SubstringPresent_NoMatch(t *testing.T) {
	d := doc("envVars", []interface{}{"DB_PASSWORD=secret", "DEBUG=true"})
	c := model.Condition{Type: "none_contain", Path: "envVars", Substrings: []string{"PASSWORD"}}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match when a forbidden substring is present in an array item")
	}
}

func TestMatcher_NoneContain_SubstringAbsent_Matches(t *testing.T) {
	d := doc("envVars", []interface{}{"APP_NAME=myapp", "DEBUG=true"})
	c := model.Condition{Type: "none_contain", Path: "envVars", Substrings: []string{"PASSWORD"}}
	r, _ := EvaluateCondition(c, d, d.root)
	if !r.Matched {
		t.Fatal("expected match when no array item contains the forbidden substring")
	}
}

func TestMatcher_NoneContain_AbsentPath_Matches(t *testing.T) {
	// Path not found → nothing to contain → matches (none contain)
	d := doc()
	c := model.Condition{Type: "none_contain", Path: "headers", Keys: []string{"Authorization"}}
	r, _ := EvaluateCondition(c, d, d.root)
	if !r.Matched {
		t.Fatal("expected match when path does not exist (nothing can contain the forbidden key)")
	}
}

// ─── credential_header_literal ────────────────────────────────────────────────

func TestMatcher_CredentialHeaderLiteral_LiteralValue_Matches(t *testing.T) {
	d := doc("input", map[string]interface{}{"Authorization": "Bearer hardcoded-token"})
	c := model.Condition{Type: "credential_header_literal", Path: "input", HeaderNames: []string{"Authorization"}}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected match for literal credential header: err=%v", err)
	}
	// The credential VALUE must be redacted in the match result output.
	if r.Value == "Authorization: Bearer hardcoded-token" {
		t.Fatal("credential value must not be surfaced in match result; expected [REDACTED]")
	}
}

func TestMatcher_CredentialHeaderLiteral_ExpressionValue_NoMatch(t *testing.T) {
	d := doc("input", map[string]interface{}{"Authorization": "=$activity[GetToken].output.token"})
	c := model.Condition{Type: "credential_header_literal", Path: "input", HeaderNames: []string{"Authorization"}}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match for Flogo expression value (not a literal)")
	}
}

func TestMatcher_CredentialHeaderLiteral_UnrelatedHeader_NoMatch(t *testing.T) {
	d := doc("input", map[string]interface{}{"Content-Type": "application/json"})
	c := model.Condition{Type: "credential_header_literal", Path: "input", HeaderNames: []string{"Authorization"}}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match when the credential header is not present")
	}
}

// ─── regex_not_match ──────────────────────────────────────────────────────────

func TestMatcher_RegexNotMatch_PatternNotMatched_Fires(t *testing.T) {
	d := doc("image", "nginx:1.25")
	c := model.Condition{Type: "regex_not_match", Path: "image", Pattern: ":latest$"}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected match when pattern is NOT found: err=%v", err)
	}
}

func TestMatcher_RegexNotMatch_PatternMatched_NoFire(t *testing.T) {
	d := doc("image", "nginx:latest")
	c := model.Condition{Type: "regex_not_match", Path: "image", Pattern: ":latest$"}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match when pattern IS found")
	}
}

func TestMatcher_RegexNotMatch_RequiresContains_GuardFails_NoFire(t *testing.T) {
	// requires_contains guard: value must contain "image" to apply the check.
	// If the guard string is absent, the rule is not applicable → returns false.
	d := doc("cmd", "/bin/sh")
	c := model.Condition{Type: "regex_not_match", Path: "cmd", Pattern: ":latest$", RequiresContains: "image"}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match when requires_contains guard is not satisfied")
	}
}

// ─── duplicate_values ─────────────────────────────────────────────────────────

func TestMatcher_DuplicateValues_HasDuplicate_Matches(t *testing.T) {
	d := doc("ports", []interface{}{"8080", "9090", "8080"})
	c := model.Condition{Type: "duplicate_values", Path: "ports"}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected match for array with duplicate values: err=%v", err)
	}
}

func TestMatcher_DuplicateValues_AllUnique_NoMatch(t *testing.T) {
	d := doc("ports", []interface{}{"8080", "9090", "3000"})
	c := model.Condition{Type: "duplicate_values", Path: "ports"}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match for array with all unique values")
	}
}

func TestMatcher_DuplicateValues_NonArray_NoMatch(t *testing.T) {
	d := doc("name", "myapp")
	c := model.Condition{Type: "duplicate_values", Path: "name"}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match for non-array value")
	}
}

// ─── all_missing ──────────────────────────────────────────────────────────────

func TestMatcher_AllMissing_AllPathsAbsent_Matches(t *testing.T) {
	d := doc("name", "myapp")
	c := model.Condition{Type: "all_missing", Paths: []string{"timeout", "retries"}}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected match when all paths are absent: err=%v", err)
	}
}

func TestMatcher_AllMissing_OnePathPresent_NoMatch(t *testing.T) {
	d := doc("timeout", float64(30))
	c := model.Condition{Type: "all_missing", Paths: []string{"timeout", "retries"}}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match when one of the paths exists")
	}
}

func TestMatcher_AllMissing_EmptyPaths_NoMatch(t *testing.T) {
	// all_missing with no paths is vacuously under-specified → must not match.
	d := doc()
	c := model.Condition{Type: "all_missing", Paths: []string{}}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("all_missing with empty paths must not match")
	}
}

// ─── count_greater_than ───────────────────────────────────────────────────────

func TestMatcher_CountGreaterThan_LargeArray_Matches(t *testing.T) {
	d := doc("items", []interface{}{"a", "b", "c", "d"})
	c := model.Condition{Type: "count_greater_than", Path: "items", MinCount: 3}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected match for 4 items > 3: err=%v", err)
	}
}

func TestMatcher_CountGreaterThan_ExactCount_NoMatch(t *testing.T) {
	d := doc("items", []interface{}{"a", "b", "c"})
	c := model.Condition{Type: "count_greater_than", Path: "items", MinCount: 3}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match for exact count (strict greater-than)")
	}
}

func TestMatcher_CountGreaterThan_NonArray_NoMatch(t *testing.T) {
	d := doc("count", "five")
	c := model.Condition{Type: "count_greater_than", Path: "count", MinCount: 1}
	r, _ := EvaluateCondition(c, d, d.root)
	if r.Matched {
		t.Fatal("expected no match for non-array value")
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func TestStringify_String(t *testing.T) {
	if stringify("hello") != "hello" {
		t.Fatal("expected passthrough for string")
	}
}

func TestStringify_Int(t *testing.T) {
	s := stringify(42)
	if s != "42" {
		t.Fatalf("expected '42', got %q", s)
	}
}

func TestToFloat64_Float(t *testing.T) {
	f, err := toFloat64(float64(3.14))
	if err != nil || f != 3.14 {
		t.Fatalf("unexpected: f=%v err=%v", f, err)
	}
}

func TestToFloat64_Int(t *testing.T) {
	f, err := toFloat64(42)
	if err != nil || f != 42.0 {
		t.Fatalf("unexpected: f=%v err=%v", f, err)
	}
}

func TestToFloat64_StringNumber(t *testing.T) {
	f, err := toFloat64("3.14")
	if err != nil || f != 3.14 {
		t.Fatalf("unexpected: f=%v err=%v", f, err)
	}
}

func TestToFloat64_InvalidString(t *testing.T) {
	_, err := toFloat64("not_a_number")
	if err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

func TestToFloat64_UnsupportedType(t *testing.T) {
	_, err := toFloat64([]string{"a"})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

// ─── toFloat64: additional type coverage ──────────────────────────────────────

func TestToFloat64_Float32(t *testing.T) {
	f, err := toFloat64(float32(2.5))
	if err != nil || f != float64(float32(2.5)) {
		t.Fatalf("unexpected: f=%v err=%v", f, err)
	}
}

func TestToFloat64_Int64(t *testing.T) {
	f, err := toFloat64(int64(1000))
	if err != nil || f != 1000.0 {
		t.Fatalf("unexpected: f=%v err=%v", f, err)
	}
}

func TestToFloat64_JsonNumber(t *testing.T) {
	f, err := toFloat64(json.Number("99.9"))
	if err != nil || f != 99.9 {
		t.Fatalf("unexpected: f=%v err=%v", f, err)
	}
}

func TestToFloat64_JsonNumber_Invalid(t *testing.T) {
	_, err := toFloat64(json.Number("not-a-number"))
	if err == nil {
		t.Fatal("expected error for invalid json.Number")
	}
}

// ─── stringify: json.Number coverage ─────────────────────────────────────────

func TestStringify_JsonNumber(t *testing.T) {
	s := stringify(json.Number("42.5"))
	if s != "42.5" {
		t.Fatalf("expected '42.5', got %q", s)
	}
}

// ─── toStringMap: non-map type ────────────────────────────────────────────────

func TestToStringMap_StringStringMap_Passthrough(t *testing.T) {
	m := map[string]interface{}{"k": "v"}
	got, ok := toStringMap(m)
	if !ok || got["k"] != "v" {
		t.Fatalf("expected passthrough: ok=%v got=%v", ok, got)
	}
}

func TestToStringMap_InterfaceKeyMap_Converted(t *testing.T) {
	m := map[interface{}]interface{}{"key": "val", 42: "num"}
	got, ok := toStringMap(m)
	if !ok {
		t.Fatal("expected ok=true for map[interface{}]interface{}")
	}
	if got["key"] != "val" {
		t.Fatalf("expected key 'key'='val', got %v", got)
	}
	if got["42"] != "num" {
		t.Fatalf("expected key '42'='num', got %v", got)
	}
}

func TestToStringMap_NonMap_ReturnsNil(t *testing.T) {
	_, ok := toStringMap("not a map")
	if ok {
		t.Fatal("expected ok=false for non-map type")
	}
}

// ─── evalRegexNotMatch: missing branches ──────────────────────────────────────

func TestMatcher_RegexNotMatch_PathNotFound_NoFire(t *testing.T) {
	// Path doesn't exist in scope — early return false.
	d := doc("name", "myapp")
	c := model.Condition{Type: "regex_not_match", Path: "missing_field", Pattern: ".*"}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || r.Matched {
		t.Fatalf("expected no match when path is not found: err=%v matched=%v", err, r.Matched)
	}
}

func TestMatcher_RegexNotMatch_RequiresContains_GuardPasses_RegexRuns(t *testing.T) {
	// requires_contains guard is satisfied (value contains "nginx") and pattern
	// does NOT match → regex_not_match fires (Matched=true).
	d := doc("image_tag", "nginx:1.25")
	c := model.Condition{
		Type:             "regex_not_match",
		Path:             "image_tag",
		Pattern:          ":latest$",
		RequiresContains: "nginx",
	}
	r, err := EvaluateCondition(c, d, d.root)
	if err != nil || !r.Matched {
		t.Fatalf("expected match when guard passes and pattern not found: err=%v matched=%v", err, r.Matched)
	}
}

func TestMatcher_RegexNotMatch_InvalidPattern_ReturnsError(t *testing.T) {
	d := doc("value", "anything")
	c := model.Condition{Type: "regex_not_match", Path: "value", Pattern: "[invalid("}
	_, err := EvaluateCondition(c, d, d.root)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

// ─── evalLessThan: numeric error path ────────────────────────────────────────

func TestMatcher_LessThan_NonNumericValue_ReturnsError(t *testing.T) {
	d := doc("size", "big")
	c := model.Condition{Type: "less_than", Path: "size", Value: float64(10)}
	_, err := EvaluateCondition(c, d, d.root)
	if err == nil {
		t.Fatal("expected error when value is non-numeric")
	}
}

func TestMatcher_LessThan_NonNumericThreshold_ReturnsError(t *testing.T) {
	d := doc("replicas", float64(3))
	c := model.Condition{Type: "less_than", Path: "replicas", Value: "not-a-number"}
	_, err := EvaluateCondition(c, d, d.root)
	if err == nil {
		t.Fatal("expected error when threshold is non-numeric")
	}
}
