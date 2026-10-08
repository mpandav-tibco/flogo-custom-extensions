package ruleengine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/support/test"
)

const goodRules = `
rules:
  - id: required_source_system
    description: Source system is required
    tags: [validation]
    condition: source_system is null OR source_system == ""
    message: "Source system is required."
    severity: error
`

func rulesDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func run(t *testing.T, inputs map[string]interface{}) (done bool, err error, tc *test.TestActivityContext) {
	t.Helper()
	act, e := New(test.NewActivityInitContext(map[string]interface{}{}, nil))
	if e != nil {
		t.Fatal(e)
	}
	tc = test.NewActivityContext(act.Metadata())
	for k, v := range inputs {
		tc.SetInput(k, v)
	}
	done, err = act.Eval(tc)
	return done, err, tc
}

func baseInputs(dir, content string) map[string]interface{} {
	return map[string]interface{}{
		"content":        content,
		"fileName":       "spec.json",
		"rulesPath":      dir,
		"parserOverride": "json",
		"tags":           []interface{}{"validation"},
	}
}

func TestInput_FromMap_Strict(t *testing.T) {
	cases := []struct {
		name    string
		value   interface{}
		want    bool
		wantErr bool
	}{
		{"absent", nil, false, false},
		{"empty string", "", false, false},
		{"true", true, true, false},
		{"string true", "true", true, false},
		{"false", false, false, false},
		{"garbage", "maybe", false, true},
	}
	for _, c := range cases {
		in := &Input{}
		values := map[string]interface{}{"strict": c.value}
		err := in.FromMap(values)
		if (err != nil) != c.wantErr || in.Strict != c.want {
			t.Errorf("%s: Strict=%v err=%v, want Strict=%v wantErr=%v", c.name, in.Strict, err, c.want, c.wantErr)
		}
	}
}

// The failure that went unnoticed: a rules directory that loads nothing produced a
// clean result (success=true, errorCount=0), so a validation gate let everything pass.
func TestEval_Lenient_ReportsSuccessEvenWhenNoRuleLoaded(t *testing.T) {
	dir := rulesDir(t, map[string]string{"broken.yaml": "- id: x\n"})

	done, err, tc := run(t, baseInputs(dir, `{"source_system":""}`))
	if !done || err != nil {
		t.Fatalf("lenient mode should complete normally, got done=%v err=%v", done, err)
	}
	if tc.GetOutput("success") != true || tc.GetOutput("errorCount") != 0 {
		t.Fatalf("lenient mode reports success with zero errors, got success=%v errorCount=%v", tc.GetOutput("success"), tc.GetOutput("errorCount"))
	}
	overview, _ := tc.GetOutput("overview").(map[string]interface{})
	if w, _ := overview["warnings"].([]string); len(w) != 1 {
		t.Fatalf("the skipped file should at least be listed in overview.warnings, got %v", overview)
	}
}

func TestEval_Strict_RaisesACodedActivityErrorWhenNoRuleLoaded(t *testing.T) {
	dir := rulesDir(t, map[string]string{"broken.yaml": "- id: x\n"})
	in := baseInputs(dir, `{"source_system":""}`)
	in["strict"] = true

	done, err, _ := run(t, in)
	if done || err == nil {
		t.Fatalf("strict mode must fail the activity, got done=%v err=%v", done, err)
	}
	var ae *activity.Error
	if !errors.As(err, &ae) || ae.Code() != ErrCodeRuleSet {
		t.Fatalf("expected an activity error with code %s, got %#v", ErrCodeRuleSet, err)
	}
	if !strings.Contains(err.Error(), "broken.yaml") {
		t.Fatalf("the error should name the rejected file: %v", err)
	}
}

func TestEval_Strict_PassesAndStillReportsFindingsForAnIntactRuleSet(t *testing.T) {
	dir := rulesDir(t, map[string]string{"spec.yaml": goodRules})
	in := baseInputs(dir, `{"source_system":""}`)
	in["strict"] = true

	done, err, tc := run(t, in)
	if !done || err != nil {
		t.Fatalf("strict mode must pass for an intact rule set, got done=%v err=%v", done, err)
	}
	if tc.GetOutput("success") != true || tc.GetOutput("errorCount") != 1 {
		t.Fatalf("the missing source_system should be reported as one error, got success=%v errorCount=%v",
			tc.GetOutput("success"), tc.GetOutput("errorCount"))
	}
}

func TestEval_Strict_FailsWhenTheDocumentCannotBeParsed(t *testing.T) {
	dir := rulesDir(t, map[string]string{"spec.yaml": goodRules})
	in := baseInputs(dir, `{not json`)
	in["strict"] = true

	done, err, _ := run(t, in)
	var ae *activity.Error
	if done || !errors.As(err, &ae) || ae.Code() != ErrCodeEval {
		t.Fatalf("expected an activity error with code %s, got done=%v err=%#v", ErrCodeEval, done, err)
	}

	// Lenient mode keeps its established contract: success=false plus an error text.
	in["strict"] = false
	done, err, tc := run(t, in)
	if !done || err != nil || tc.GetOutput("success") != false || tc.GetOutput("error") == "" {
		t.Fatalf("lenient mode should report success=false with an error text, got done=%v err=%v success=%v",
			done, err, tc.GetOutput("success"))
	}
}

func TestEval_MissingRulesPath(t *testing.T) {
	in := baseInputs("", `{}`)
	in["strict"] = true
	done, err, _ := run(t, in)
	var ae *activity.Error
	if done || !errors.As(err, &ae) || ae.Code() != ErrCodeConfig {
		t.Fatalf("strict mode: expected code %s, got done=%v err=%#v", ErrCodeConfig, done, err)
	}

	in["strict"] = false
	done, err, tc := run(t, in)
	if !done || err != nil || tc.GetOutput("success") != false {
		t.Fatalf("lenient mode: expected success=false, got done=%v err=%v success=%v", done, err, tc.GetOutput("success"))
	}
}

func TestLoadProblems(t *testing.T) {
	in := &Input{RulesPath: "rules/", FileName: "spec.json", Tags: []string{"validation"}}

	if msg := loadProblems(in, map[string]interface{}{"rules_run": 13}); msg != "" {
		t.Fatalf("a healthy evaluation should log nothing, got %q", msg)
	}
	msg := loadProblems(in, map[string]interface{}{"rules_run": 0, "warnings": []string{"YAML error in a.yaml", "no rules found in b.yaml"}})
	if !strings.Contains(msg, "2 problem(s)") || !strings.Contains(msg, "a.yaml") || !strings.Contains(msg, "b.yaml") {
		t.Fatalf("load problems should be listed, got %q", msg)
	}
	msg = loadProblems(in, map[string]interface{}{"rules_run": 0})
	if !strings.Contains(msg, "no rules were evaluated") {
		t.Fatalf("zero evaluated rules should be called out, got %q", msg)
	}
}
