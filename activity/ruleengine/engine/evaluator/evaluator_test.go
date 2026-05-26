package evaluator

import (
	"testing"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/model"
	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/parser"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func makeScopedDoc(scopes []map[string]interface{}) parser.Document {
	return &multiScopeDoc{items: scopes}
}

type multiScopeDoc struct {
	items []map[string]interface{}
}

func (d *multiScopeDoc) Root() interface{} { return d.items }
func (d *multiScopeDoc) ResolveScope(path string) ([]interface{}, error) {
	if path == "" {
		out := make([]interface{}, len(d.items))
		for i, m := range d.items {
			out[i] = m
		}
		return out, nil
	}
	return nil, nil
}
func (d *multiScopeDoc) ResolvePath(obj interface{}, path string) (interface{}, bool) {
	m, ok := obj.(map[string]interface{})
	if !ok {
		return nil, false
	}
	v, found := m[path]
	return v, found
}

func rule(id, severity, matchType, path string, extras ...interface{}) *model.RuleDef {
	r := &model.RuleDef{
		ID:       id,
		Severity: severity,
		Title:    "Test rule " + id,
		Scope:    "",
		Match:    model.Condition{Type: matchType, Path: path},
	}
	for i := 0; i+1 < len(extras); i += 2 {
		switch extras[i].(string) {
		case "value":
			r.Match.Value = extras[i+1]
		case "substring":
			r.Match.Substring = extras[i+1].(string)
		case "pattern":
			r.Match.Pattern = extras[i+1].(string)
		case "scope":
			r.Scope = extras[i+1].(string)
		case "when":
			wc := extras[i+1].(model.Condition)
			r.When = &wc
		case "location":
			r.Location = extras[i+1].(string)
		case "description":
			r.Description = extras[i+1].(string)
		case "tags":
			r.Tags = extras[i+1].([]string)
		}
	}
	return r
}

// ─── Run: basic finding generation ───────────────────────────────────────────

func TestEvaluator_Run_ErrorRuleFired(t *testing.T) {
	d := &mapDoc{root: map[string]interface{}{"key": "value"}}
	rules := []*model.RuleDef{
		rule("T-001", "ERROR", "exists", "key"),
	}
	findings, positives := Run(rules, d, "test.json")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != "ERROR" {
		t.Fatalf("expected ERROR severity, got %q", findings[0].Severity)
	}
	if len(positives) != 0 {
		t.Fatalf("expected 0 positives, got %d", len(positives))
	}
}

func TestEvaluator_Run_WarningRuleFired(t *testing.T) {
	d := &mapDoc{root: map[string]interface{}{"level": "DEBUG"}}
	rules := []*model.RuleDef{
		rule("T-002", "WARNING", "equals", "level", "value", "DEBUG"),
	}
	findings, _ := Run(rules, d, "config.properties")
	if len(findings) != 1 || findings[0].Severity != "WARNING" {
		t.Fatalf("expected 1 WARNING finding, got %v", findings)
	}
}

func TestEvaluator_Run_GoodRule_GoesToPositives(t *testing.T) {
	d := &mapDoc{root: map[string]interface{}{"errorHandler": "present"}}
	rules := []*model.RuleDef{
		rule("T-003", "GOOD", "exists", "errorHandler"),
	}
	findings, positives := Run(rules, d, "test.json")
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for GOOD rule, got %d", len(findings))
	}
	if len(positives) != 1 {
		t.Fatalf("expected 1 positive, got %d", len(positives))
	}
}

func TestEvaluator_Run_ConditionFalse_NoFinding(t *testing.T) {
	d := &mapDoc{root: map[string]interface{}{"timeout": float64(30)}}
	rules := []*model.RuleDef{
		rule("T-004", "ERROR", "missing", "timeout"),
	}
	findings, _ := Run(rules, d, "test.json")
	if len(findings) != 0 {
		t.Fatalf("expected no findings when condition is false, got %d", len(findings))
	}
}

// ─── Run: multiple scope items ────────────────────────────────────────────────

func TestEvaluator_Run_MultipleScopes_OneMatches(t *testing.T) {
	d := makeScopedDoc([]map[string]interface{}{
		{"name": "flow1"}, // no errorHandler
		{"name": "flow2", "errorHandler": "present"},
	})
	rules := []*model.RuleDef{
		rule("T-005", "ERROR", "missing", "errorHandler", "scope", ""),
	}
	findings, _ := Run(rules, d, "app.flogo")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for the flow without errorHandler, got %d", len(findings))
	}
}

func TestEvaluator_Run_MultipleScopes_AllMatch(t *testing.T) {
	d := makeScopedDoc([]map[string]interface{}{
		{"name": "flow1"},
		{"name": "flow2"},
	})
	rules := []*model.RuleDef{
		rule("T-006", "ERROR", "missing", "errorHandler", "scope", ""),
	}
	findings, _ := Run(rules, d, "app.flogo")
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings (one per scope), got %d", len(findings))
	}
}

func TestEvaluator_Run_EmptyScope_NoFindings(t *testing.T) {
	// Document has no items at the scope path
	d := makeScopedDoc([]map[string]interface{}{})
	rules := []*model.RuleDef{
		rule("T-007", "ERROR", "missing", "x"),
	}
	findings, _ := Run(rules, d, "empty.json")
	if len(findings) != 0 {
		t.Fatalf("expected no findings for empty scope, got %d", len(findings))
	}
}

// ─── Run: when pre-filter ─────────────────────────────────────────────────────

func TestEvaluator_Run_WhenFilter_SkipsNonMatching(t *testing.T) {
	d := makeScopedDoc([]map[string]interface{}{
		{"ref": "#rest", "timeout": float64(0)}, // ref matches when → should find
		{"ref": "#log", "timeout": float64(0)},  // ref doesn't match when → skip
	})
	whenCond := model.Condition{Type: "contains", Path: "ref", Substring: "rest"}
	r := rule("T-008", "WARNING", "equals", "timeout", "value", float64(0),
		"when", whenCond, "scope", "")

	findings, _ := Run([]*model.RuleDef{r}, d, "app.flogo")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (REST activity only), got %d", len(findings))
	}
}

func TestEvaluator_Run_WhenFilter_AllSkipped(t *testing.T) {
	d := makeScopedDoc([]map[string]interface{}{
		{"ref": "#log"},
		{"ref": "#mapper"},
	})
	whenCond := model.Condition{Type: "contains", Path: "ref", Substring: "rest"}
	r := rule("T-009", "ERROR", "missing", "timeout", "when", whenCond, "scope", "")

	findings, _ := Run([]*model.RuleDef{r}, d, "test.json")
	if len(findings) != 0 {
		t.Fatalf("expected no findings when when-filter excludes all, got %d", len(findings))
	}
}

// ─── Run: sorting ─────────────────────────────────────────────────────────────

func TestEvaluator_Run_SortedBySeverity(t *testing.T) {
	d := &mapDoc{root: map[string]interface{}{"x": "y"}}
	rules := []*model.RuleDef{
		rule("T-INFO", "INFO", "exists", "x"),
		rule("T-ERR", "ERROR", "exists", "x"),
		rule("T-WARN", "WARNING", "exists", "x"),
	}
	findings, _ := Run(rules, d, "test.json")
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(findings))
	}
	// ERROR first, then WARNING, then INFO
	if findings[0].Severity != "ERROR" {
		t.Fatalf("expected ERROR first, got %q", findings[0].Severity)
	}
	if findings[1].Severity != "WARNING" {
		t.Fatalf("expected WARNING second, got %q", findings[1].Severity)
	}
	if findings[2].Severity != "INFO" {
		t.Fatalf("expected INFO third, got %q", findings[2].Severity)
	}
}

func TestEvaluator_Run_SortedAlphabeticallyWithinSeverity(t *testing.T) {
	d := &mapDoc{root: map[string]interface{}{"x": "y"}}
	rules := []*model.RuleDef{
		rule("B-001", "ERROR", "exists", "x"),
		rule("A-001", "ERROR", "exists", "x"),
		rule("C-001", "ERROR", "exists", "x"),
	}
	findings, _ := Run(rules, d, "test.json")
	if findings[0].RuleID != "A-001" || findings[1].RuleID != "B-001" || findings[2].RuleID != "C-001" {
		t.Fatalf("expected alphabetical order within severity, got %v %v %v",
			findings[0].RuleID, findings[1].RuleID, findings[2].RuleID)
	}
}

// ─── Run: finding fields ──────────────────────────────────────────────────────

func TestEvaluator_Run_FindingFields_Populated(t *testing.T) {
	d := &mapDoc{root: map[string]interface{}{"name": "myflow"}}
	r := rule("T-010", "ERROR", "missing", "errorHandler",
		"location", "flow:{{.Scope.name}}",
		"description", "Flow {{.Scope.name}} is missing error handler",
		"tags", []string{"flogo", "error-handling"})
	r.Category = "reliability"

	findings, _ := Run([]*model.RuleDef{r}, d, "app.flogo")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.RuleID != "T-010" {
		t.Errorf("unexpected RuleID: %q", f.RuleID)
	}
	if f.Severity != "ERROR" {
		t.Errorf("unexpected Severity: %q", f.Severity)
	}
	if f.Category != "reliability" {
		t.Errorf("unexpected Category: %q", f.Category)
	}
	if f.Location != "flow:myflow" {
		t.Errorf("expected interpolated location, got %q", f.Location)
	}
	if f.Message != "Flow myflow is missing error handler" {
		t.Errorf("unexpected Message: %q", f.Message)
	}
	if len(f.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(f.Tags))
	}
}

// ─── Run: no rules ────────────────────────────────────────────────────────────

func TestEvaluator_Run_NoRules_NoFindings(t *testing.T) {
	d := &mapDoc{root: map[string]interface{}{"x": "y"}}
	findings, positives := Run([]*model.RuleDef{}, d, "test.json")
	if len(findings) != 0 || len(positives) != 0 {
		t.Fatal("expected no findings for no rules")
	}
}

// ─── sortedRules ──────────────────────────────────────────────────────────────

func TestSortedRules_Order(t *testing.T) {
	rules := []*model.RuleDef{
		{ID: "G-001", Severity: "GOOD"},
		{ID: "I-001", Severity: "INFO"},
		{ID: "E-001", Severity: "ERROR"},
		{ID: "W-001", Severity: "WARNING"},
		{ID: "E-002", Severity: "ERROR"},
	}
	sorted := sortedRules(rules)
	expected := []string{"E-001", "E-002", "W-001", "I-001", "G-001"}
	for i, r := range sorted {
		if r.ID != expected[i] {
			t.Fatalf("position %d: expected %q, got %q", i, expected[i], r.ID)
		}
	}
}

func TestSortedRules_OriginalUnchanged(t *testing.T) {
	origRules := []*model.RuleDef{
		{ID: "Z-001", Severity: "INFO"},
		{ID: "A-001", Severity: "ERROR"},
	}
	_ = sortedRules(origRules)
	// Original should not be mutated
	if origRules[0].ID != "Z-001" {
		t.Fatal("sortedRules mutated the original slice")
	}
}

// ─── Run: evaluation error surfacing ─────────────────────────────────────────

func TestEvaluator_Run_MatchError_SurfacedAsDiagnostic(t *testing.T) {
	// A rule whose match condition produces an error (invalid regex) must surface
	// a diagnostic INFO finding rather than silently swallowing the error.
	d := &mapDoc{root: map[string]interface{}{"image": "nginx:latest"}}
	r := &model.RuleDef{
		ID:       "BAD-001",
		Severity: "ERROR",
		Title:    "Bad regex rule",
		Match:    model.Condition{Type: "regex", Path: "image", Pattern: "[invalid"},
	}
	findings, _ := Run([]*model.RuleDef{r}, d, "test.json")
	if len(findings) != 1 {
		t.Fatalf("expected 1 diagnostic finding for match error, got %d", len(findings))
	}
	if findings[0].Severity != model.SeverityInfo {
		t.Fatalf("expected INFO severity for match-error diagnostic, got %q", findings[0].Severity)
	}
	if findings[0].RuleID != "BAD-001" {
		t.Fatalf("expected RuleID=BAD-001 in diagnostic, got %q", findings[0].RuleID)
	}
}

func TestEvaluator_Run_WhenError_SurfacedAsDiagnostic(t *testing.T) {
	// A rule whose when-filter produces an error (invalid regex) must surface
	// a diagnostic INFO finding — the match phase must not run.
	d := &mapDoc{root: map[string]interface{}{"ref": "#rest"}}
	whenCond := model.Condition{Type: "regex", Path: "ref", Pattern: "[invalid"}
	r := &model.RuleDef{
		ID:       "BAD-002",
		Severity: "ERROR",
		Title:    "Bad when rule",
		When:     &whenCond,
		Match:    model.Condition{Type: "exists", Path: "ref"},
	}
	findings, _ := Run([]*model.RuleDef{r}, d, "test.json")
	if len(findings) != 1 {
		t.Fatalf("expected 1 diagnostic finding for when-error, got %d", len(findings))
	}
	if findings[0].Severity != model.SeverityInfo {
		t.Fatalf("expected INFO severity for when-error diagnostic, got %q", findings[0].Severity)
	}
}
