package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// multiRuleValidation mirrors the app's spec-validation.yaml format: a top-level
// `rules:` list, lowercase severities, `condition:` DSL, and `message:` text.
const multiRuleValidation = `
rules:
  - id: required_source_system
    description: Source system is required
    tags: [validation, required]
    condition: source_system is null OR source_system == ""
    message: "Source system is required."
    severity: error

  - id: required_interface_type
    description: Interface type is required
    tags: [validation, required]
    condition: interface_type is null OR interface_type == ""
    message: "Interface type is required."
    severity: error

  - id: valid_interface_type
    description: Interface type must be pub, sub, or service
    tags: [validation, interface_type]
    condition: interface_type NOT in ["pub", "sub", "service"]
    message: "Invalid interface type '{{ interface_type }}'. Must be one of: pub, sub, service"
    severity: error

  - id: valid_email_domain
    description: Email domain must be allowed
    tags: [validation, email]
    condition: email is not null AND email != "NA" AND email domain NOT in ["adidas.com", "tibco.com"]
    message: "Email domain not allowed for '{{ email }}'."
    severity: error

  - id: source_not_equal_target
    description: Source and target should differ
    tags: [validation, logic]
    condition: source_system == target_system AND source_system != "rest"
    message: "Source and target are the same ('{{ source_system }}')."
    severity: warning
`

func writeTempRules(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spec-validation.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestEvaluate_MultiRuleConditionFile proves the full pipeline (multi-rule loader
// + condition-DSL evaluator + tag scoping + {{ field }} interpolation) works
// against the app's rule-file format.
func TestEvaluate_MultiRuleConditionFile(t *testing.T) {
	rulesDir := writeTempRules(t, multiRuleValidation)

	run := func(spec string) (errCount, warnCount int, markdown string) {
		res, err := Evaluate(Request{
			Content:        spec,
			FileName:       "spec-validation.yaml",
			RulesPath:      rulesDir,
			ParserOverride: "json",
			Tags:           []string{"validation"},
		})
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		return res.ErrorCount, res.WarningCount, res.Markdown
	}

	t.Run("valid spec → 0 errors", func(t *testing.T) {
		ec, _, _ := run(`{"source_system":"s4hana","target_system":"kafka","interface_type":"pub","email":"dev@adidas.com"}`)
		if ec != 0 {
			t.Fatalf("expected 0 errors, got %d", ec)
		}
	})

	t.Run("missing source → error", func(t *testing.T) {
		ec, _, _ := run(`{"target_system":"kafka","interface_type":"pub","email":"dev@adidas.com"}`)
		if ec < 1 {
			t.Fatalf("expected >=1 error, got %d", ec)
		}
	})

	t.Run("bad interface_type → error with interpolated message", func(t *testing.T) {
		ec, _, md := run(`{"source_system":"s4hana","target_system":"kafka","interface_type":"badtype","email":"dev@adidas.com"}`)
		if ec < 1 {
			t.Fatalf("expected >=1 error, got %d", ec)
		}
		// {{ interface_type }} must be interpolated to the actual value
		if !contains(md, "badtype") {
			t.Fatalf("expected markdown to interpolate interface_type 'badtype', got:\n%s", md)
		}
	})

	t.Run("bad email domain → error", func(t *testing.T) {
		ec, _, _ := run(`{"source_system":"s4hana","target_system":"kafka","interface_type":"pub","email":"dev@gmail.com"}`)
		if ec < 1 {
			t.Fatalf("expected >=1 error for blocked domain, got %d", ec)
		}
	})

	t.Run("same source/target → warning (not error)", func(t *testing.T) {
		ec, wc, _ := run(`{"source_system":"kafka","target_system":"kafka","interface_type":"pub","email":"dev@adidas.com"}`)
		if ec != 0 {
			t.Fatalf("expected 0 errors (same-system is a warning), got %d", ec)
		}
		if wc < 1 {
			t.Fatalf("expected >=1 warning, got %d", wc)
		}
	})
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
