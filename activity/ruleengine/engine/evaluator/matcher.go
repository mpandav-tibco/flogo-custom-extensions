package evaluator

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/model"
	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/parser"
)

// compiledRegexps caches compiled regular expressions keyed by the full pattern
// string (including any flag prefix). Compiling a regex is expensive and the
// same pattern is typically applied to every scope item for a given rule.
var compiledRegexps sync.Map

// MatchResult carries whether the condition matched and the actual value involved.
type MatchResult struct {
	Matched bool
	Value   interface{} // actual value for template interpolation via {{.Match}}
}

// EvaluateCondition dispatches to the appropriate match implementation.
// Composite types (any_of, all_of, none_of) recurse back into this function.
func EvaluateCondition(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	switch cond.Type {
	case "missing":
		return evalMissing(cond, doc, scope)
	case "exists":
		return evalExists(cond, doc, scope)
	case "not_exists":
		return evalNotExists(cond, doc, scope)
	case "equals":
		return evalEquals(cond, doc, scope)
	case "not_equals":
		return evalNotEquals(cond, doc, scope)
	case "contains":
		return evalContains(cond, doc, scope)
	case "not_contains":
		return evalNotContains(cond, doc, scope)
	case "contains_any":
		return evalContainsAny(cond, doc, scope)
	case "regex":
		return evalRegex(cond, doc, scope)
	case "greater_than":
		return evalGreaterThan(cond, doc, scope)
	case "less_than":
		return evalLessThan(cond, doc, scope)
	case "count_exceeds":
		return evalCountExceeds(cond, doc, scope)
	case "any_of":
		return evalAnyOf(cond, doc, scope)
	case "all_of":
		return evalAllOf(cond, doc, scope)
	case "none_of":
		return evalNoneOf(cond, doc, scope)
	// ── Aliases for readability ───────────────────────────────────────────
	case "not_empty", "not_missing":
		return evalExists(cond, doc, scope)
	case "regex_match":
		return evalRegex(cond, doc, scope)
	// count_greater_than is a readability alias: it accepts min_count (int) instead of
	// value (interface{}) but delegates to evalCountExceeds which handles both.
	case "count_greater_than":
		return evalCountExceeds(cond, doc, scope)
	case "all_missing":
		return evalAllMissing(cond, doc, scope)
	case "none_contain":
		return evalNoneContain(cond, doc, scope)
	case "regex_not_match":
		return evalRegexNotMatch(cond, doc, scope)
	case "duplicate_values":
		return evalDuplicateValues(cond, doc, scope)
	case "credential_header_literal":
		return evalCredentialHeaderLiteral(cond, doc, scope)

	case "expression":
		// Phase 2: JavaScript sandbox via goja — not available in this build.
		// Use regex, any_of, or all_of for complex multi-condition logic instead.
		return MatchResult{}, fmt.Errorf("expression match type is not supported in this build; use regex, any_of, or all_of for complex conditions (Phase 2 feature)")
	default:
		return MatchResult{}, fmt.Errorf("unknown match type: %q", cond.Type)
	}
}

// ── Primitive match types ─────────────────────────────────────────────────────

func evalMissing(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		return MatchResult{Matched: true}, nil
	}
	switch v := val.(type) {
	case string:
		return MatchResult{Matched: v == ""}, nil
	case []interface{}:
		return MatchResult{Matched: len(v) == 0}, nil
	}
	return MatchResult{Matched: false, Value: val}, nil
}

func evalExists(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		return MatchResult{Matched: false}, nil
	}
	switch v := val.(type) {
	case string:
		return MatchResult{Matched: v != "", Value: val}, nil
	case []interface{}:
		return MatchResult{Matched: len(v) > 0, Value: val}, nil
	}
	return MatchResult{Matched: true, Value: val}, nil
}

// evalNotExists fires when the path is completely absent from the document.
// Unlike "missing" (absent OR empty), "not_exists" fires ONLY when the key is
// not present at all — empty strings, zero values, and empty arrays do NOT match.
func evalNotExists(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	_, found := doc.ResolvePath(scope, cond.Path)
	return MatchResult{Matched: !found}, nil
}

func evalEquals(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found {
		return MatchResult{Matched: false}, nil
	}
	matched := reflect.DeepEqual(val, cond.Value) ||
		fmt.Sprintf("%v", val) == fmt.Sprintf("%v", cond.Value)
	return MatchResult{Matched: matched, Value: val}, nil
}

func evalNotEquals(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	r, err := evalEquals(cond, doc, scope)
	if err != nil {
		return r, err
	}
	return MatchResult{Matched: !r.Matched, Value: r.Value}, nil
}

func evalContains(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		return MatchResult{Matched: false}, nil
	}
	s := stringify(val)
	return MatchResult{Matched: strings.Contains(s, cond.Substring), Value: val}, nil
}

func evalNotContains(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	r, err := evalContains(cond, doc, scope)
	if err != nil {
		return r, err
	}
	return MatchResult{Matched: !r.Matched, Value: r.Value}, nil
}

func evalContainsAny(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		return MatchResult{Matched: false}, nil
	}
	s := stringify(val)
	for _, sub := range cond.Substrings {
		if strings.Contains(s, sub) {
			return MatchResult{Matched: true, Value: val}, nil
		}
	}
	return MatchResult{Matched: false, Value: val}, nil
}

func evalRegex(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	var s string
	if cond.Path != "" {
		val, found := doc.ResolvePath(scope, cond.Path)
		if !found || val == nil {
			return MatchResult{Matched: false}, nil
		}
		s = stringify(val)
	} else {
		s = stringify(scope)
	}

	pattern := cond.Pattern
	if len(cond.Flags) > 0 {
		pattern = "(?" + strings.Join(cond.Flags, "") + ")" + pattern
	}

	var re *regexp.Regexp
	if cached, ok := compiledRegexps.Load(pattern); ok {
		re = cached.(*regexp.Regexp)
	} else {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return MatchResult{}, fmt.Errorf("invalid regex %q: %w", cond.Pattern, err)
		}
		compiledRegexps.Store(pattern, re)
	}
	return MatchResult{Matched: re.MatchString(s), Value: s}, nil
}

// ── Numeric match types ───────────────────────────────────────────────────────

func evalGreaterThan(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		return MatchResult{Matched: false}, nil
	}
	f, err := toFloat64(val)
	if err != nil {
		return MatchResult{}, err
	}
	thresh, err := toFloat64(cond.Value)
	if err != nil {
		return MatchResult{}, err
	}
	return MatchResult{Matched: f > thresh, Value: val}, nil
}

func evalLessThan(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		return MatchResult{Matched: false}, nil
	}
	f, err := toFloat64(val)
	if err != nil {
		return MatchResult{}, err
	}
	thresh, err := toFloat64(cond.Value)
	if err != nil {
		return MatchResult{}, err
	}
	return MatchResult{Matched: f < thresh, Value: val}, nil
}

func evalCountExceeds(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		return MatchResult{Matched: false}, nil
	}
	arr, ok := val.([]interface{})
	if !ok {
		return MatchResult{Matched: false}, nil
	}
	// Accept threshold from value field (interface{}) or min_count field (int).
	// min_count takes precedence when both are set.
	var thresh float64
	if cond.Value != nil {
		var err error
		thresh, err = toFloat64(cond.Value)
		if err != nil {
			return MatchResult{}, err
		}
	} else {
		thresh = float64(cond.MinCount)
	}
	return MatchResult{Matched: float64(len(arr)) > thresh, Value: len(arr)}, nil
}

// ── Composite match types (recursive) ────────────────────────────────────────

func evalAnyOf(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	for _, sub := range cond.Conditions {
		r, err := EvaluateCondition(sub, doc, scope)
		if err != nil {
			return MatchResult{}, err
		}
		if r.Matched {
			return MatchResult{Matched: true, Value: r.Value}, nil
		}
	}
	return MatchResult{Matched: false}, nil
}

func evalAllOf(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	if len(cond.Conditions) == 0 {
		// Vacuous all_of with no sub-conditions must not match — an empty rule
		// would fire against every scope item, producing false positives.
		return MatchResult{Matched: false}, nil
	}
	for _, sub := range cond.Conditions {
		r, err := EvaluateCondition(sub, doc, scope)
		if err != nil {
			return MatchResult{}, err
		}
		if !r.Matched {
			return MatchResult{Matched: false}, nil
		}
	}
	return MatchResult{Matched: true}, nil
}

func evalNoneOf(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	if len(cond.Conditions) == 0 {
		// Vacuous none_of with no sub-conditions must not match — a misconfigured
		// rule would otherwise fire against every scope item, producing false positives.
		// Mirrors the same guard in evalAllOf.
		return MatchResult{Matched: false}, nil
	}
	for _, sub := range cond.Conditions {
		r, err := EvaluateCondition(sub, doc, scope)
		if err != nil {
			return MatchResult{}, err
		}
		if r.Matched {
			return MatchResult{Matched: false}, nil
		}
	}
	return MatchResult{Matched: true}, nil
}

// ── Extended match implementations ───────────────────────────────────────────

// evalAllMissing fires when every path in cond.Paths resolves to missing/empty.
func evalAllMissing(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	if len(cond.Paths) == 0 {
		return MatchResult{Matched: false}, nil
	}
	for _, p := range cond.Paths {
		sub := model.Condition{Type: "missing", Path: p}
		r, err := evalMissing(sub, doc, scope)
		if err != nil {
			return MatchResult{}, err
		}
		if !r.Matched {
			return MatchResult{Matched: false}, nil
		}
	}
	return MatchResult{Matched: true}, nil
}

// evalNoneContain fires when the resolved object/map has none of the keys listed
// in cond.Keys, OR when none of the array items at path contain any of cond.Substrings.
func evalNoneContain(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	if len(cond.Keys) == 0 && len(cond.Substrings) == 0 {
		// Misconfigured rule: no keys or substrings to check.
		// Return false to avoid vacuously matching every scope item
		// (same reasoning as the all_of: [] guard).
		return MatchResult{Matched: false}, nil
	}
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		// Nothing resolved → no headers/items present → treat as match (none contain).
		return MatchResult{Matched: true}, nil
	}

	// Key-based check: object must not contain any of the specified keys.
	if len(cond.Keys) > 0 {
		m, ok := toStringMap(val)
		if !ok {
			return MatchResult{Matched: true}, nil
		}
		for _, key := range cond.Keys {
			for mapKey := range m {
				if strings.EqualFold(mapKey, key) {
					return MatchResult{Matched: false, Value: mapKey}, nil
				}
			}
		}
		return MatchResult{Matched: true}, nil
	}

	// Substring-based check: no array element contains any of cond.Substrings.
	arr, ok := val.([]interface{})
	if !ok {
		arr = []interface{}{val}
	}
	for _, item := range arr {
		s := stringify(item)
		for _, sub := range cond.Substrings {
			if strings.Contains(s, sub) {
				return MatchResult{Matched: false, Value: s}, nil
			}
		}
	}
	return MatchResult{Matched: true}, nil
}

// evalRegexNotMatch fires when the value at path does NOT match the pattern.
// If RequiresContains is set, the value must contain that string first; if it
// doesn't, the rule is not applicable and returns false (no finding).
func evalRegexNotMatch(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		return MatchResult{Matched: false}, nil
	}
	s := stringify(val)

	if cond.RequiresContains != "" && !strings.Contains(strings.ToUpper(s), strings.ToUpper(cond.RequiresContains)) {
		return MatchResult{Matched: false}, nil
	}

	var re *regexp.Regexp
	if cached, ok := compiledRegexps.Load(cond.Pattern); ok {
		re = cached.(*regexp.Regexp)
	} else {
		var err error
		re, err = regexp.Compile(cond.Pattern)
		if err != nil {
			return MatchResult{}, fmt.Errorf("invalid regex %q: %w", cond.Pattern, err)
		}
		compiledRegexps.Store(cond.Pattern, re)
	}
	return MatchResult{Matched: !re.MatchString(s), Value: s}, nil
}

// evalDuplicateValues fires when the array at path contains at least MinCount
// occurrences of the same value (default MinCount=2 means any duplicate).
func evalDuplicateValues(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		return MatchResult{Matched: false}, nil
	}
	arr, ok := val.([]interface{})
	if !ok {
		return MatchResult{Matched: false}, nil
	}

	minCount := cond.MinCount
	if minCount < 2 {
		minCount = 2
	}

	counts := make(map[string]int, len(arr))
	for _, item := range arr {
		k := stringify(item)
		counts[k]++
		if counts[k] >= minCount {
			return MatchResult{Matched: true, Value: k}, nil
		}
	}
	return MatchResult{Matched: false}, nil
}

// evalCredentialHeaderLiteral fires when any header named in cond.HeaderNames
// has a literal value (i.e. not a Flogo expression starting with "=$").
func evalCredentialHeaderLiteral(cond model.Condition, doc parser.Document, scope interface{}) (MatchResult, error) {
	val, found := doc.ResolvePath(scope, cond.Path)
	if !found || val == nil {
		return MatchResult{Matched: false}, nil
	}
	headers, ok := toStringMap(val)
	if !ok {
		return MatchResult{Matched: false}, nil
	}

	for _, name := range cond.HeaderNames {
		for headerKey, headerVal := range headers {
			if !strings.EqualFold(headerKey, name) {
				continue
			}
			s := stringify(headerVal)
			// A Flogo expression always starts with "=$" — a literal value does not.
			if s != "" && !strings.HasPrefix(s, "=$") {
				// Return only the header name — never the value itself.
				// The value is a credential; surfacing it in findings output would
				// re-expose the secret to downstream systems and logs.
				return MatchResult{Matched: true, Value: fmt.Sprintf("%s: [REDACTED]", headerKey)}, nil
			}
		}
	}
	return MatchResult{Matched: false}, nil
}

// toStringMap coerces a value to map[string]interface{} where possible.
func toStringMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			out[fmt.Sprintf("%v", k)] = val
		}
		return out, true
	}
	return nil, false
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func stringify(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case json.Number:
		return s.String()
	}
	return fmt.Sprintf("%v", v)
}

func toFloat64(v interface{}) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	case string:
		var f float64
		_, err := fmt.Sscanf(n, "%f", &f)
		return f, err
	}
	return 0, fmt.Errorf("cannot convert %T to number", v)
}
