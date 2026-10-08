package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const strictValidSpec = `{"source_system":"s4hana","target_system":"kafka","interface_type":"pub","email":"dev@tibco.com"}`
const strictBadSpec = `{"source_system":"s4hana","target_system":"kafka","interface_type":"badtype","email":"dev@tibco.com"}`

// brokenRuleFile has neither a `rule:` nor a `rules:` block, so the loader rejects it.
const brokenRuleFile = "- id: not_a_rule_file\n  severity: error\n"

func strictRequest(dir, content string, strict bool, tags ...string) Request {
	return Request{
		Content:        content,
		FileName:       "spec.json",
		RulesPath:      dir,
		ParserOverride: "json",
		Tags:           tags,
		Strict:         strict,
	}
}

func writeStrictFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func warningsOf(overview map[string]interface{}) []string {
	w, _ := overview["warnings"].([]string)
	return w
}

// A rules directory that loads nothing must not look like "no findings" in strict mode.
func TestEvaluate_Strict_FailsWhenNoRuleFileLoads(t *testing.T) {
	dir := t.TempDir()
	writeStrictFile(t, dir, "broken.yaml", brokenRuleFile)

	lenient, err := Evaluate(strictRequest(dir, strictBadSpec, false, "validation"))
	if err != nil {
		t.Fatalf("lenient mode must not fail: %v", err)
	}
	if lenient.ErrorCount != 0 || lenient.Overview["rules_run"] != 0 || len(warningsOf(lenient.Overview)) != 1 {
		t.Fatalf("lenient mode should report zero rules run plus one warning, got errors=%d rules_run=%v warnings=%v",
			lenient.ErrorCount, lenient.Overview["rules_run"], warningsOf(lenient.Overview))
	}

	_, err = Evaluate(strictRequest(dir, strictBadSpec, true, "validation"))
	var se *StrictError
	if !errors.As(err, &se) {
		t.Fatalf("strict mode must return *StrictError, got %v", err)
	}
	if len(se.Warnings) != 1 || !strings.Contains(err.Error(), "broken.yaml") {
		t.Fatalf("error should name the rejected file: %v (warnings=%v)", err, se.Warnings)
	}
}

func TestEvaluate_Strict_FailsWhenRulesDirectoryIsMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	if _, err := Evaluate(strictRequest(dir, strictValidSpec, false, "validation")); err != nil {
		t.Fatalf("lenient mode must not fail: %v", err)
	}
	_, err := Evaluate(strictRequest(dir, strictValidSpec, true, "validation"))
	var se *StrictError
	if !errors.As(err, &se) || len(se.Warnings) == 0 {
		t.Fatalf("strict mode must fail for a missing directory, got %v", err)
	}
}

// One rejected file is enough: the remaining rules still run in lenient mode, but a
// validation gate cannot trust a partial rule set.
func TestEvaluate_Strict_FailsWhenOneFileIsSkipped(t *testing.T) {
	dir := writeTempRules(t, multiRuleValidation)
	writeStrictFile(t, dir, "broken.yaml", brokenRuleFile)

	lenient, err := Evaluate(strictRequest(dir, strictBadSpec, false, "validation"))
	if err != nil {
		t.Fatalf("lenient mode must not fail: %v", err)
	}
	if lenient.ErrorCount == 0 || len(warningsOf(lenient.Overview)) != 1 {
		t.Fatalf("lenient mode should still evaluate the good rules, got errors=%d warnings=%v",
			lenient.ErrorCount, warningsOf(lenient.Overview))
	}

	_, err = Evaluate(strictRequest(dir, strictBadSpec, true, "validation"))
	var se *StrictError
	if !errors.As(err, &se) {
		t.Fatalf("strict mode must fail when any rule file is skipped, got %v", err)
	}
}

func TestEvaluate_Strict_FailsWhenTagsSelectNoRule(t *testing.T) {
	dir := writeTempRules(t, multiRuleValidation)

	lenient, err := Evaluate(strictRequest(dir, strictValidSpec, false, "no-such-tag"))
	if err != nil || lenient.Overview["rules_run"] != 0 {
		t.Fatalf("lenient mode should run zero rules without failing, got err=%v rules_run=%v", err, lenient.Overview["rules_run"])
	}

	_, err = Evaluate(strictRequest(dir, strictValidSpec, true, "no-such-tag"))
	var se *StrictError
	if !errors.As(err, &se) {
		t.Fatalf("strict mode must fail when no rule is evaluated, got %v", err)
	}
	if len(se.Warnings) != 0 || !strings.Contains(err.Error(), "no rules were evaluated") {
		t.Fatalf("unexpected error for the zero-rules case: %v (warnings=%v)", err, se.Warnings)
	}
}

// Strict mode adds checks only; an intact rule set gives exactly the lenient result.
func TestEvaluate_Strict_DoesNotChangeResultsForAnIntactRuleSet(t *testing.T) {
	dir := writeTempRules(t, multiRuleValidation)

	for name, spec := range map[string]string{"valid": strictValidSpec, "invalid": strictBadSpec} {
		lenient, err := Evaluate(strictRequest(dir, spec, false, "validation"))
		if err != nil {
			t.Fatalf("%s: lenient: %v", name, err)
		}
		strict, err := Evaluate(strictRequest(dir, spec, true, "validation"))
		if err != nil {
			t.Fatalf("%s: strict mode must pass for an intact rule set: %v", name, err)
		}
		if strict.ErrorCount != lenient.ErrorCount || strict.WarningCount != lenient.WarningCount {
			t.Fatalf("%s: strict result differs from lenient: %d/%d vs %d/%d", name,
				strict.ErrorCount, strict.WarningCount, lenient.ErrorCount, lenient.WarningCount)
		}
	}
	bad, _ := Evaluate(strictRequest(dir, strictBadSpec, true, "validation"))
	if bad.ErrorCount != 1 {
		t.Fatalf("the invalid spec should produce exactly one error, got %d", bad.ErrorCount)
	}
}

func TestCheckStrict_CapsTheProblemsListedInTheMessage(t *testing.T) {
	err := checkStrict(Request{RulesPath: "rules/"}, []string{"a", "b", "c", "d", "e"}, 0)
	if err == nil || !strings.Contains(err.Error(), "5 problem(s)") || !strings.Contains(err.Error(), "(+2 more)") {
		t.Fatalf("message should count all problems but list only %d: %v", maxStrictProblems, err)
	}
	if strings.Contains(err.Error(), "d;") || strings.Contains(err.Error(), "; e") {
		t.Fatalf("message lists more than %d problems: %v", maxStrictProblems, err)
	}
}
