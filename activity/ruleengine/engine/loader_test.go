package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/model"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func writeRuleFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write rule file: %v", err)
	}
}

func writeInSubdir(t *testing.T, dir, sub, filename, content string) {
	t.Helper()
	subdir := filepath.Join(dir, sub)
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	writeRuleFile(t, subdir, filename, content)
}

// ─── rule YAML fixtures ───────────────────────────────────────────────────────

const ruleValid = `
rule:
  id: TEST-001
  severity: ERROR
  title: Missing Required Field
  description: The field is absent.
  tags: [test, unit]
  applies_to: [.json, .flogo]
  match:
    type: missing
    path: required_field
`

const ruleValidWarning = `
rule:
  id: TEST-002
  severity: WARNING
  title: Optional Field Exists
  match:
    type: exists
    path: optional_field
`

const ruleValidGood = `
rule:
  id: TEST-003
  severity: GOOD
  title: Has Error Handler
  match:
    type: exists
    path: errorHandler
`

const ruleDisabled = `
rule:
  id: TEST-DIS
  enabled: false
  severity: ERROR
  title: Disabled Rule
  match:
    type: missing
    path: anything
`

const ruleDuplicateID = `
rule:
  id: TEST-001
  severity: WARNING
  title: Duplicate of TEST-001 (should be ignored)
  match:
    type: exists
    path: y
`

const ruleInvalidYAML = `{this is [invalid yaml`

const ruleMissingID = `
rule:
  severity: ERROR
  title: Rule Without ID
  match:
    type: missing
    path: x
`

const ruleMissingTitle = `
rule:
  id: NO-TITLE-001
  severity: ERROR
  match:
    type: missing
    path: x
`

const ruleMissingMatchType = `
rule:
  id: NO-MATCH-001
  severity: ERROR
  title: Missing Match Type
  match:
    path: x
`

const ruleInvalidSeverity = `
rule:
  id: BAD-SEV-001
  severity: CRITICAL
  title: Invalid Severity
  match:
    type: missing
    path: x
`

// ─── loadRules ────────────────────────────────────────────────────────────────

func TestLoadRules_SingleValidRule(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "test-001.yaml", ruleValid)

	rules, warnings := loadRules(dir)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
	if rules[0].ID != "TEST-001" {
		t.Fatalf("expected ID=TEST-001, got %q", rules[0].ID)
	}
	if rules[0].Severity != "ERROR" {
		t.Fatalf("expected ERROR severity, got %q", rules[0].Severity)
	}
	if len(rules[0].Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(rules[0].Tags))
	}
}

func TestLoadRules_MultipleRules_AllLoaded(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "a.yaml", ruleValid)
	writeRuleFile(t, dir, "b.yaml", ruleValidWarning)
	writeRuleFile(t, dir, "c.yaml", ruleValidGood)

	rules, _ := loadRules(dir)
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
}

func TestLoadRules_DisabledRule_Skipped(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "disabled.yaml", ruleDisabled)

	rules, _ := loadRules(dir)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules (disabled), got %d", len(rules))
	}
}

func TestLoadRules_DuplicateID_FirstWins(t *testing.T) {
	dir := t.TempDir()
	// alphabetical walk: a_ loads first
	writeRuleFile(t, dir, "a_first.yaml", ruleValid)           // TEST-001 ERROR
	writeRuleFile(t, dir, "b_duplicate.yaml", ruleDuplicateID) // TEST-001 WARNING

	rules, warnings := loadRules(dir)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule (first-wins), got %d", len(rules))
	}
	if rules[0].Severity != "ERROR" {
		t.Fatalf("expected first (ERROR) rule to win, got %q", rules[0].Severity)
	}
	if len(warnings) == 0 {
		t.Fatal("expected duplicate warning")
	}
}

func TestLoadRules_InvalidYAML_WarningEmitted(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "bad.yaml", ruleInvalidYAML)

	rules, warnings := loadRules(dir)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for invalid YAML")
	}
}

func TestLoadRules_MissingID_WarningEmitted(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "no-id.yaml", ruleMissingID)

	rules, warnings := loadRules(dir)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for missing ID")
	}
}

func TestLoadRules_MissingTitle_WarningEmitted(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "no-title.yaml", ruleMissingTitle)

	rules, warnings := loadRules(dir)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for missing title")
	}
}

func TestLoadRules_MissingMatchType_WarningEmitted(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "no-match.yaml", ruleMissingMatchType)

	rules, warnings := loadRules(dir)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for missing match.type")
	}
}

func TestLoadRules_InvalidSeverity_WarningEmitted(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "bad-sev.yaml", ruleInvalidSeverity)

	rules, warnings := loadRules(dir)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for invalid severity")
	}
}

func TestLoadRules_NonYAMLFiles_Ignored(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "rule.yaml", ruleValid)
	writeRuleFile(t, dir, "readme.txt", "not a rule")
	writeRuleFile(t, dir, "config.json", `{"ignore":"me"}`)
	writeRuleFile(t, dir, "notes.md", "# notes")

	rules, _ := loadRules(dir)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule (only YAML), got %d", len(rules))
	}
}

func TestLoadRules_RecursiveWalk(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "root.yaml", ruleValid)
	writeInSubdir(t, dir, "flogo", "warn.yaml", ruleValidWarning)
	writeInSubdir(t, dir, "kube", "good.yaml", ruleValidGood)

	rules, _ := loadRules(dir)
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules (root + 2 subdirs), got %d", len(rules))
	}
}

func TestLoadRules_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	rules, warnings := loadRules(dir)
	if len(rules) != 0 || len(warnings) != 0 {
		t.Fatalf("expected empty results, got rules=%d warnings=%d", len(rules), len(warnings))
	}
}

func TestLoadRules_NonexistentDirectory(t *testing.T) {
	rules, warnings := loadRules(filepath.Join(t.TempDir(), "nonexistent"))
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for nonexistent directory")
	}
}

func TestLoadRules_MixedValidAndInvalid(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "valid.yaml", ruleValid)
	writeRuleFile(t, dir, "invalid.yaml", ruleInvalidYAML)
	writeRuleFile(t, dir, "no-id.yaml", ruleMissingID)

	rules, warnings := loadRules(dir)
	if len(rules) != 1 {
		t.Fatalf("expected 1 valid rule, got %d", len(rules))
	}
	if len(warnings) < 2 {
		t.Fatalf("expected ≥2 warnings, got %d: %v", len(warnings), warnings)
	}
}

// ─── filterRules ─────────────────────────────────────────────────────────────

func TestFilterRules_NoFilters_PassesAll(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "a.yaml", ruleValid)        // applies_to: [.json, .flogo]
	writeRuleFile(t, dir, "b.yaml", ruleValidWarning) // no applies_to restriction
	rules, _ := loadRules(dir)

	filtered := filterRules(rules, ".json", "", nil, nil)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 rules for .json, got %d", len(filtered))
	}
}

func TestFilterRules_Extension_Matches(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "a.yaml", ruleValid) // applies_to: [.json, .flogo]
	rules, _ := loadRules(dir)

	for _, ext := range []string{".json", ".flogo"} {
		filtered := filterRules(rules, ext, "", nil, nil)
		if len(filtered) != 1 {
			t.Fatalf("expected 1 rule for ext %s, got %d", ext, len(filtered))
		}
	}
}

func TestFilterRules_Extension_NoMatch(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "a.yaml", ruleValid) // applies_to: [.json, .flogo]
	rules, _ := loadRules(dir)

	filtered := filterRules(rules, ".yaml", "", nil, nil)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 rules for .yaml extension, got %d", len(filtered))
	}
}

func TestFilterRules_NoAppliesTo_MatchesAllExtensions(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "b.yaml", ruleValidWarning) // no applies_to
	rules, _ := loadRules(dir)

	for _, ext := range []string{".json", ".yaml", ".xml", ".log", ".conf"} {
		filtered := filterRules(rules, ext, "", nil, nil)
		if len(filtered) != 1 {
			t.Fatalf("expected rule with no applies_to to match %s, got %d", ext, len(filtered))
		}
	}
}

func TestFilterRules_DisabledList_ExcludesRule(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "a.yaml", ruleValid)        // TEST-001
	writeRuleFile(t, dir, "b.yaml", ruleValidWarning) // TEST-002
	rules, _ := loadRules(dir)

	filtered := filterRules(rules, ".json", "", []string{"TEST-001"}, nil)
	if len(filtered) != 1 || filtered[0].ID != "TEST-002" {
		t.Fatalf("expected only TEST-002, got %d rules", len(filtered))
	}
}

func TestFilterRules_DisabledList_AllRules(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "a.yaml", ruleValid)
	writeRuleFile(t, dir, "b.yaml", ruleValidWarning)
	rules, _ := loadRules(dir)

	filtered := filterRules(rules, ".json", "", []string{"TEST-001", "TEST-002"}, nil)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 rules when all disabled, got %d", len(filtered))
	}
}

func TestFilterRules_TagFilter_Match(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "a.yaml", ruleValid) // tags: [test, unit]
	rules, _ := loadRules(dir)

	for _, tag := range []string{"test", "unit"} {
		filtered := filterRules(rules, ".json", "", nil, []string{tag})
		if len(filtered) != 1 {
			t.Fatalf("expected 1 rule for tag %q, got %d", tag, len(filtered))
		}
	}
}

func TestFilterRules_TagFilter_NoMatch(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "a.yaml", ruleValid) // tags: [test, unit]
	rules, _ := loadRules(dir)

	filtered := filterRules(rules, ".json", "", nil, []string{"kubernetes"})
	if len(filtered) != 0 {
		t.Fatalf("expected 0 rules for non-matching tag, got %d", len(filtered))
	}
}

func TestFilterRules_TagFilter_RuleWithNoTags_ExcludedWhenTagFilterSet(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "no-tags.yaml", ruleValidWarning) // no tags on rule
	rules, _ := loadRules(dir)

	filtered := filterRules(rules, ".json", "", nil, []string{"flogo"})
	if len(filtered) != 0 {
		t.Fatalf("expected rule with no tags to be excluded when tag filter set, got %d", len(filtered))
	}
}

func TestFilterRules_EmptyTagFilter_ReturnsAll(t *testing.T) {
	dir := t.TempDir()
	writeRuleFile(t, dir, "a.yaml", ruleValid)
	writeRuleFile(t, dir, "b.yaml", ruleValidWarning)
	rules, _ := loadRules(dir)

	filtered := filterRules(rules, ".json", "", nil, []string{})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 rules with empty tag filter, got %d", len(filtered))
	}
}

// ─── validateRule ─────────────────────────────────────────────────────────────

func TestValidateRule_Valid(t *testing.T) {
	r := &model.RuleDef{
		ID:       "X-001",
		Title:    "Valid Rule",
		Severity: "ERROR",
		Match:    model.Condition{Type: "missing", Path: "key"},
	}
	if err := validateRule(r, "test.yaml"); err != nil {
		t.Fatalf("unexpected error for valid rule: %v", err)
	}
}

func TestValidateRule_MissingID(t *testing.T) {
	r := &model.RuleDef{Title: "No ID", Severity: "ERROR", Match: model.Condition{Type: "missing"}}
	if err := validateRule(r, "test.yaml"); err == nil {
		t.Fatal("expected error for missing ID")
	}
}

func TestValidateRule_MissingTitle(t *testing.T) {
	r := &model.RuleDef{ID: "X-001", Severity: "ERROR", Match: model.Condition{Type: "missing"}}
	if err := validateRule(r, "test.yaml"); err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestValidateRule_MissingMatchType(t *testing.T) {
	r := &model.RuleDef{ID: "X-001", Title: "Has Title", Severity: "ERROR", Match: model.Condition{}}
	if err := validateRule(r, "test.yaml"); err == nil {
		t.Fatal("expected error for missing match type")
	}
}

func TestValidateRule_ValidSeverities(t *testing.T) {
	for _, sev := range []string{"ERROR", "WARNING", "INFO", "GOOD"} {
		r := &model.RuleDef{
			ID:       "X-001",
			Title:    "Test",
			Severity: sev,
			Match:    model.Condition{Type: "missing"},
		}
		if err := validateRule(r, "test.yaml"); err != nil {
			t.Fatalf("unexpected error for valid severity %q: %v", sev, err)
		}
	}
}

func TestValidateRule_InvalidSeverities(t *testing.T) {
	for _, sev := range []string{"CRITICAL", "FATAL", "DEBUG", "low", ""} {
		r := &model.RuleDef{
			ID:       "X-001",
			Title:    "Test",
			Severity: sev,
			Match:    model.Condition{Type: "missing"},
		}
		if err := validateRule(r, "test.yaml"); err == nil {
			t.Fatalf("expected error for invalid severity %q", sev)
		}
	}
}

// ─── filterByParser ───────────────────────────────────────────────────────────

func TestFilterByParser_NoParserField_AlwaysIncluded(t *testing.T) {
	rules := []*model.RuleDef{{ID: "X-001", Severity: "ERROR", Title: "no parser"}}
	out := filterByParser(rules, "json")
	if len(out) != 1 {
		t.Fatal("rule with no parser field should always be included regardless of active parser")
	}
}

func TestFilterByParser_MatchingParser_Included(t *testing.T) {
	rules := []*model.RuleDef{{ID: "X-002", Severity: "ERROR", Title: "json only", Parser: "json"}}
	out := filterByParser(rules, "json")
	if len(out) != 1 {
		t.Fatal("rule with matching parser should be included")
	}
}

func TestFilterByParser_MismatchedParser_Excluded(t *testing.T) {
	// A rule scoped to "yaml" must not run when the active parser is "json".
	rules := []*model.RuleDef{{ID: "X-003", Severity: "ERROR", Title: "yaml only", Parser: "yaml"}}
	out := filterByParser(rules, "json")
	if len(out) != 0 {
		t.Fatalf("rule with parser=yaml must be excluded when active parser is json; got %d", len(out))
	}
}

func TestFilterByParser_Mixed(t *testing.T) {
	rules := []*model.RuleDef{
		{ID: "X-001", Severity: "ERROR", Title: "no restriction"},            // no restriction
		{ID: "X-002", Severity: "ERROR", Title: "json only", Parser: "json"}, // json only
		{ID: "X-003", Severity: "ERROR", Title: "yaml only", Parser: "yaml"}, // yaml only
	}
	out := filterByParser(rules, "json")
	if len(out) != 2 {
		t.Fatalf("expected 2 rules for json parser (no-restriction + json), got %d", len(out))
	}
	if out[0].ID != "X-001" || out[1].ID != "X-002" {
		t.Fatalf("unexpected rule IDs: %v, %v", out[0].ID, out[1].ID)
	}
}

func TestFilterByParser_EmptyParserName_OnlyUnrestrictedPass(t *testing.T) {
	// resolveParser always returns a named parser when it succeeds, so parserName=""
	// won't occur in normal operation. If it did, only rules without a parser
	// restriction would pass (since no parser name can match a non-empty restriction).
	rules := []*model.RuleDef{
		{ID: "X-001", Parser: "yaml"},
		{ID: "X-002", Parser: "json"},
		{ID: "X-003"}, // no restriction → always passes
	}
	out := filterByParser(rules, "")
	if len(out) != 1 || out[0].ID != "X-003" {
		t.Fatalf("expected only the unrestricted rule when parserName is empty, got %d rules", len(out))
	}
}
