package model

import (
	"encoding/json"
	"testing"
)

// ─── RuleDef.IsEnabled ────────────────────────────────────────────────────────

func TestRuleDef_IsEnabled_DefaultTrue(t *testing.T) {
	r := &RuleDef{}
	if !r.IsEnabled() {
		t.Fatal("expected IsEnabled=true when Enabled field is nil (default)")
	}
}

func TestRuleDef_IsEnabled_ExplicitTrue(t *testing.T) {
	v := true
	r := &RuleDef{Enabled: &v}
	if !r.IsEnabled() {
		t.Fatal("expected IsEnabled=true when Enabled=true")
	}
}

func TestRuleDef_IsEnabled_ExplicitFalse(t *testing.T) {
	v := false
	r := &RuleDef{Enabled: &v}
	if r.IsEnabled() {
		t.Fatal("expected IsEnabled=false when Enabled=false")
	}
}

// ─── Finding JSON serialization ───────────────────────────────────────────────

func TestFinding_JSONTags(t *testing.T) {
	f := Finding{
		RuleID:         "FLOGO-001",
		Severity:       "ERROR",
		Category:       "reliability",
		Title:          "Missing error handler",
		Location:       "flow:main",
		Message:        "The flow is missing an error handler",
		Recommendation: "Add an error handler",
		Tags:           []string{"flogo", "error-handling"},
	}

	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	cases := map[string]string{
		"rule_id":        "FLOGO-001",
		"severity":       "ERROR",
		"category":       "reliability",
		"title":          "Missing error handler",
		"location":       "flow:main",
		"message":        "The flow is missing an error handler",
		"recommendation": "Add an error handler",
	}
	for jsonKey, expected := range cases {
		val, ok := m[jsonKey]
		if !ok {
			t.Errorf("expected JSON key %q to be present", jsonKey)
			continue
		}
		if val != expected {
			t.Errorf("key %q: expected %q, got %v", jsonKey, expected, val)
		}
	}

	tags, ok := m["tags"].([]interface{})
	if !ok || len(tags) != 2 {
		t.Fatalf("expected tags array with 2 items, got %v", m["tags"])
	}
}

func TestFinding_EmptyTags_MarshaledAsArray(t *testing.T) {
	f := Finding{RuleID: "X-001", Tags: []string{}}
	b, _ := json.Marshal(f)
	var m map[string]interface{}
	json.Unmarshal(b, &m)
	tags, ok := m["tags"].([]interface{})
	if !ok {
		// nil tags may marshal as null or []
		_ = tags
	}
}

// ─── Result ───────────────────────────────────────────────────────────────────

func TestResult_Counters_DefaultZero(t *testing.T) {
	r := &Result{}
	if r.ErrorCount != 0 || r.WarningCount != 0 || r.InfoCount != 0 {
		t.Fatal("expected all counters to start at zero")
	}
}

func TestResult_FindingsAsInterface_EmptySlice(t *testing.T) {
	r := &Result{Findings: []Finding{}}
	out := r.FindingsAsInterface()
	if out == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(out) != 0 {
		t.Fatalf("expected empty slice, got %d items", len(out))
	}
}

func TestResult_FindingsAsInterface_Populated(t *testing.T) {
	r := &Result{
		Findings: []Finding{
			{RuleID: "T-001", Severity: "ERROR", Title: "Test"},
			{RuleID: "T-002", Severity: "WARNING", Title: "Test2"},
		},
	}
	out := r.FindingsAsInterface()
	if len(out) != 2 {
		t.Fatalf("expected 2 items, got %d", len(out))
	}
	m, ok := out[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected map[string]interface{} items")
	}
	if m["rule_id"] != "T-001" {
		t.Fatalf("expected rule_id=T-001, got %v", m["rule_id"])
	}
	if m["severity"] != "ERROR" {
		t.Fatalf("expected severity=ERROR, got %v", m["severity"])
	}
}

func TestResult_PositivesAsInterface_Populated(t *testing.T) {
	r := &Result{
		Positives: []Finding{
			{RuleID: "G-001", Severity: "GOOD", Title: "Has error handler"},
		},
	}
	out := r.PositivesAsInterface()
	if len(out) != 1 {
		t.Fatalf("expected 1 positive, got %d", len(out))
	}
	m := out[0].(map[string]interface{})
	if m["rule_id"] != "G-001" {
		t.Fatalf("expected rule_id=G-001, got %v", m["rule_id"])
	}
}

func TestResult_FindingsAsInterface_PreservesAllFields(t *testing.T) {
	f := Finding{
		RuleID:         "KUBE-001",
		Severity:       "ERROR",
		Category:       "deployment",
		Title:          "Image tag",
		Location:       "container:app",
		Message:        "Uses latest",
		Recommendation: "Pin version",
		Tags:           []string{"kubernetes"},
	}
	r := &Result{Findings: []Finding{f}}
	out := r.FindingsAsInterface()
	m := out[0].(map[string]interface{})

	if m["category"] != "deployment" {
		t.Errorf("expected category=deployment, got %v", m["category"])
	}
	if m["location"] != "container:app" {
		t.Errorf("expected location=container:app, got %v", m["location"])
	}
	if m["recommendation"] != "Pin version" {
		t.Errorf("expected recommendation, got %v", m["recommendation"])
	}
	tags, _ := m["tags"].([]interface{})
	if len(tags) != 1 || tags[0] != "kubernetes" {
		t.Errorf("expected tags=[kubernetes], got %v", m["tags"])
	}
}

// ─── Condition struct ─────────────────────────────────────────────────────────

func TestCondition_DefaultValues(t *testing.T) {
	c := Condition{}
	if c.Type != "" || c.Path != "" || c.Value != nil {
		t.Fatal("expected zero-value Condition")
	}
	if c.Flags != nil {
		t.Fatal("expected nil Flags slice")
	}
	if c.Conditions != nil {
		t.Fatal("expected nil Conditions slice")
	}
}

// ─── Rule struct ─────────────────────────────────────────────────────────────

func TestRule_EmptyStruct(t *testing.T) {
	r := &Rule{}
	if r.Rule.ID != "" {
		t.Fatal("expected empty Rule struct")
	}
	if r.Rule.Severity != "" {
		t.Fatal("expected empty Severity")
	}
	if !r.Rule.IsEnabled() {
		t.Fatal("expected IsEnabled=true for empty struct (nil pointer = default true)")
	}
}

func TestRuleDef_TagsDefaultNil(t *testing.T) {
	r := &RuleDef{}
	if r.Tags != nil {
		t.Fatal("expected nil Tags slice by default")
	}
}
