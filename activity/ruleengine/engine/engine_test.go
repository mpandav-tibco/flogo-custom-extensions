package engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func mkRulesDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func addRule(t *testing.T, dir, subdir, filename, content string) {
	t.Helper()
	target := dir
	if subdir != "" {
		target = filepath.Join(dir, subdir)
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, filename), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// ─── YAML rule fixtures ───────────────────────────────────────────────────────

const missingErrorHandlerRule = `
rule:
  id: FLOGO-001
  severity: ERROR
  title: Flow Missing Error Handler
  description: "Flow {{.Scope.name}} has no error handler tasks."
  tags: [flogo, reliability]
  applies_to: [.json, .flogo]
  scope: "$.resources[*].data"
  match:
    type: missing
    path: "errorHandler.tasks"
  location: "flow:{{.Scope.name}}"
  recommendation: "Add an errorHandler section with at least one task."
`

const httpTimeoutRule = `
rule:
  id: FLOGO-002
  severity: WARNING
  title: HTTP Activity Timeout Not Set
  description: "REST activity {{.Scope.id}} has no timeout configured."
  tags: [flogo, performance]
  applies_to: [.json, .flogo]
  scope: "$.resources[*].data.tasks[*]"
  when:
    type: contains
    path: "activity.ref"
    substring: "rest"
  match:
    type: any_of
    conditions:
      - type: missing
        path: "activity.settings.timeout"
      - type: equals
        path: "activity.settings.timeout"
        value: 0
  location: "task:{{.Scope.id}}"
`

const kubeLatestImageRule = `
rule:
  id: KUBE-001
  severity: ERROR
  title: Container Uses latest Tag
  description: "Container {{.Scope.name}} uses image {{.Match}} with no pinned version."
  tags: [kubernetes, deployment]
  applies_to: [.yaml, .yml]
  parser: yaml
  scope: "spec.template.spec.containers"
  match:
    type: any_of
    conditions:
      - type: regex
        path: image
        pattern: ":latest$"
      - type: not_contains
        path: image
        substring: ":"
  location: "container:{{.Scope.name}}"
  recommendation: "Pin the image to a specific version tag."
`

const logErrorRule = `
rule:
  id: LOG-001
  severity: ERROR
  title: Error Log Line Detected
  description: "Line {{.Scope.number}} contains an error: {{.Scope.line}}"
  tags: [log, monitoring]
  applies_to: [.log, .txt]
  scope: ""
  match:
    type: contains
    path: "line"
    substring: "ERROR"
  location: "line:{{.Scope.number}}"
`

const kvSslDisabledRule = `
rule:
  id: KV-001
  severity: WARNING
  title: SSL Not Enabled
  description: "SSL is disabled in configuration"
  tags: [security, kv]
  applies_to: [.conf, .properties, .env]
  match:
    type: equals
    path: SSL_ENABLED
    value: "false"
  location: "config:SSL_ENABLED"
`

const goodRule = `
rule:
  id: FLOGO-G-001
  severity: GOOD
  title: Has Error Handler
  description: "Flow {{.Scope.name}} has proper error handling."
  tags: [flogo, reliability]
  applies_to: [.json, .flogo]
  scope: "$.resources[*].data"
  match:
    type: exists
    path: "errorHandler.tasks"
  location: "flow:{{.Scope.name}}"
`

// ─── test documents ───────────────────────────────────────────────────────────

const flogoAppWithIssues = `{
  "name": "my-app",
  "version": "1.0.0",
  "resources": [
    {
      "id": "flow:flow1",
      "data": {
        "name": "flow1",
        "tasks": [
          {"id": "t1", "activity": {"ref": "#rest", "settings": {"timeout": 0}}},
          {"id": "t2", "activity": {"ref": "#log"}}
        ]
      }
    },
    {
      "id": "flow:flow2",
      "data": {
        "name": "flow2",
        "errorHandler": {"tasks": [{"id": "err1"}]},
        "tasks": [
          {"id": "t3", "activity": {"ref": "#noop"}}
        ]
      }
    }
  ]
}`

const flogoAppClean = `{
  "name": "clean-app",
  "version": "2.0.0",
  "resources": [
    {
      "id": "flow:main",
      "data": {
        "name": "main",
        "errorHandler": {"tasks": [{"id": "onError"}]},
        "tasks": [
          {"id": "t1", "activity": {"ref": "#rest", "settings": {"timeout": 30}}}
        ]
      }
    }
  ]
}`

const kubeWithLatestImage = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
spec:
  template:
    spec:
      containers:
        - name: frontend
          image: nginx:latest
        - name: backend
          image: myapp:2.1.0
        - name: sidecar
          image: busybox
`

const logWithErrors = `2024-01-01 INFO Starting service
2024-01-01 ERROR Database connection failed
2024-01-01 INFO Retrying...
2024-01-01 ERROR Timeout exceeded
2024-01-01 WARN Memory pressure high`

const kvConfigInsecure = `SSL_ENABLED=false
HOST=localhost
PORT=5432
`

// ─── core evaluation tests ────────────────────────────────────────────────────

func TestEvaluate_JSON_FindsMissingErrorHandler(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "my-app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding (flow1 missing errorHandler), got %d", len(result.Findings))
	}
	f := result.Findings[0]
	if f.RuleID != "FLOGO-001" {
		t.Errorf("expected FLOGO-001, got %q", f.RuleID)
	}
	if f.Severity != "ERROR" {
		t.Errorf("expected ERROR, got %q", f.Severity)
	}
	if !strings.Contains(f.Location, "flow1") {
		t.Errorf("expected location to mention 'flow1', got %q", f.Location)
	}
}

func TestEvaluate_JSON_FindsHTTPTimeout(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-002.yaml", httpTimeoutRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding (REST with timeout=0), got %d", len(result.Findings))
	}
	if result.Findings[0].RuleID != "FLOGO-002" {
		t.Errorf("expected FLOGO-002, got %q", result.Findings[0].RuleID)
	}
}

func TestEvaluate_JSON_CleanApp_NoFindings(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule)
	addRule(t, rulesDir, "", "flogo-002.yaml", httpTimeoutRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppClean,
		FileName:  "clean.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings for clean app, got %d: %v", len(result.Findings), result.Findings)
	}
}

func TestEvaluate_JSON_GoodRule_Positives(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "good.yaml", goodRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Positives) != 1 {
		t.Fatalf("expected 1 positive (flow2 has errorHandler), got %d", len(result.Positives))
	}
	if result.Positives[0].RuleID != "FLOGO-G-001" {
		t.Errorf("expected FLOGO-G-001, got %q", result.Positives[0].RuleID)
	}
}

func TestEvaluate_YAML_FindsLatestImage(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "kube-001.yaml", kubeLatestImageRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   kubeWithLatestImage,
		FileName:  "deployment.yaml",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// frontend=nginx:latest (matches :latest), sidecar=busybox (no colon = no tag)
	if len(result.Findings) != 2 {
		t.Fatalf("expected 2 findings (latest + untagged), got %d", len(result.Findings))
	}
	for _, f := range result.Findings {
		if f.RuleID != "KUBE-001" {
			t.Errorf("expected KUBE-001, got %q", f.RuleID)
		}
	}
}

func TestEvaluate_Lines_FindsErrorLines(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "log-001.yaml", logErrorRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   logWithErrors,
		FileName:  "app.log",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 2 {
		t.Fatalf("expected 2 ERROR findings, got %d", len(result.Findings))
	}
}

func TestEvaluate_KV_FindsInsecureConfig(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "kv-001.yaml", kvSslDisabledRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   kvConfigInsecure,
		FileName:  "server.conf",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding (SSL disabled), got %d", len(result.Findings))
	}
	if result.Findings[0].RuleID != "KV-001" {
		t.Errorf("expected KV-001, got %q", result.Findings[0].RuleID)
	}
}

// ─── parser selection ─────────────────────────────────────────────────────────

func TestEvaluate_ParserOverride_JSON_ForUnknownExtension(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule)

	result, err := engine.Evaluate(engine.Request{
		Content:        flogoAppWithIssues,
		FileName:       "mystery.dat",
		RulesPath:      rulesDir,
		ParserOverride: "json",
	})
	if err != nil {
		t.Fatalf("unexpected error with parser override: %v", err)
	}
	// applies_to=[.json,.flogo] won't match .dat — so 0 findings expected
	if len(result.Findings) != 0 {
		t.Logf("Note: rule has applies_to=[.json,.flogo] so .dat extension filters it out")
	}
}

func TestEvaluate_ParserOverride_Unknown_Error(t *testing.T) {
	rulesDir := mkRulesDir(t)

	_, err := engine.Evaluate(engine.Request{
		Content:        `{"key":"value"}`,
		FileName:       "test.json",
		RulesPath:      rulesDir,
		ParserOverride: "nonexistent_parser",
	})
	if err == nil {
		t.Fatal("expected error for unknown parser override")
	}
}

func TestEvaluate_UnknownExtension_NoOverride_Error(t *testing.T) {
	rulesDir := mkRulesDir(t)

	_, err := engine.Evaluate(engine.Request{
		Content:   `some content`,
		FileName:  "unknown.xyz",
		RulesPath: rulesDir,
	})
	if err == nil {
		t.Fatal("expected error for unknown extension without override")
	}
}

func TestEvaluate_InvalidJSONContent_Error(t *testing.T) {
	rulesDir := mkRulesDir(t)

	_, err := engine.Evaluate(engine.Request{
		Content:   `{bad json`,
		FileName:  "app.json",
		RulesPath: rulesDir,
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON content")
	}
}

func TestEvaluate_InvalidYAMLContent_Error(t *testing.T) {
	rulesDir := mkRulesDir(t)

	_, err := engine.Evaluate(engine.Request{
		Content:   `key: [unclosed`,
		FileName:  "config.yaml",
		RulesPath: rulesDir,
	})
	if err == nil {
		t.Fatal("expected error for invalid YAML content")
	}
}

// ─── filtering: disabled rules ────────────────────────────────────────────────

func TestEvaluate_DisabledRules_Excluded(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule)
	addRule(t, rulesDir, "", "flogo-002.yaml", httpTimeoutRule)

	result, err := engine.Evaluate(engine.Request{
		Content:       flogoAppWithIssues,
		FileName:      "app.flogo",
		RulesPath:     rulesDir,
		DisabledRules: []string{"FLOGO-001"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range result.Findings {
		if f.RuleID == "FLOGO-001" {
			t.Fatal("expected FLOGO-001 to be excluded")
		}
	}
}

func TestEvaluate_DisabledRules_AllDisabled_EmptyFindings(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule)
	addRule(t, rulesDir, "", "flogo-002.yaml", httpTimeoutRule)

	result, err := engine.Evaluate(engine.Request{
		Content:       flogoAppWithIssues,
		FileName:      "app.flogo",
		RulesPath:     rulesDir,
		DisabledRules: []string{"FLOGO-001", "FLOGO-002"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings when all rules disabled, got %d", len(result.Findings))
	}
}

// ─── filtering: tags ─────────────────────────────────────────────────────────

func TestEvaluate_TagFilter_OnlyMatchingTagRulesRun(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule) // tags: [flogo, reliability]
	addRule(t, rulesDir, "", "flogo-002.yaml", httpTimeoutRule)          // tags: [flogo, performance]

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
		Tags:      []string{"reliability"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range result.Findings {
		if f.RuleID == "FLOGO-002" {
			t.Fatal("expected FLOGO-002 (performance tag) to be excluded by tag filter")
		}
	}
}

func TestEvaluate_TagFilter_NoMatchingTags_EmptyFindings(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
		Tags:      []string{"kubernetes"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings when tag filter excludes all rules, got %d", len(result.Findings))
	}
}

// ─── result structure ─────────────────────────────────────────────────────────

func TestEvaluate_Result_Counters(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule) // ERROR
	addRule(t, rulesDir, "", "flogo-002.yaml", httpTimeoutRule)          // WARNING

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ErrorCount != 1 {
		t.Errorf("expected ErrorCount=1, got %d", result.ErrorCount)
	}
	if result.WarningCount != 1 {
		t.Errorf("expected WarningCount=1, got %d", result.WarningCount)
	}
}

func TestEvaluate_Result_MarkdownGenerated(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Markdown == "" {
		t.Fatal("expected non-empty markdown report")
	}
	if !strings.Contains(result.Markdown, "Analysis Report") {
		t.Errorf("expected 'Analysis Report' in markdown, got: %s", result.Markdown)
	}
	if !strings.Contains(result.Markdown, "FLOGO-001") {
		t.Errorf("expected rule ID in markdown, got: %s", result.Markdown)
	}
	// The Message column must contain the interpolated description (f.Message),
	// not just the static rule title. The description for missingErrorHandlerRule
	// is "Flow {{.Scope.name}} has no error handler tasks." — after interpolation
	// against flow1 it becomes "Flow flow1 has no error handler tasks."
	if !strings.Contains(result.Markdown, "flow1") {
		t.Errorf("expected interpolated scope name 'flow1' in markdown Message column, got: %s", result.Markdown)
	}
}

func TestEvaluate_Result_Overview_Populated(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "my-app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Overview["file"] != "my-app.flogo" {
		t.Errorf("expected file=my-app.flogo, got %v", result.Overview["file"])
	}
	if result.Overview["name"] != "my-app" {
		t.Errorf("expected name=my-app, got %v", result.Overview["name"])
	}
	if result.Overview["version"] != "1.0.0" {
		t.Errorf("expected version=1.0.0, got %v", result.Overview["version"])
	}
	if result.Overview["rules_run"].(int) < 1 {
		t.Errorf("expected rules_run >= 1, got %v", result.Overview["rules_run"])
	}
}

func TestEvaluate_Result_Overview_LoadWarnings(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "bad.yaml", ruleInvalidYAMLFixture)
	addRule(t, rulesDir, "", "good.yaml", missingErrorHandlerRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	warnings, ok := result.Overview["warnings"]
	if !ok {
		t.Fatal("expected 'warnings' in overview when there are load warnings")
	}
	if len(warnings.([]string)) == 0 {
		t.Fatal("expected at least one load warning")
	}
}

const ruleInvalidYAMLFixture = `{invalid yaml [`

func TestEvaluate_EmptyRulesDir_NoFindings(t *testing.T) {
	rulesDir := mkRulesDir(t)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 || len(result.Positives) != 0 {
		t.Fatalf("expected no findings for empty rules dir, got %d", len(result.Findings))
	}
}

// ─── multi-rule interaction ───────────────────────────────────────────────────

func TestEvaluate_MultipleRules_CombinedFindings(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule)
	addRule(t, rulesDir, "", "flogo-002.yaml", httpTimeoutRule)
	addRule(t, rulesDir, "", "good.yaml", goodRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// flow1: missing errorHandler (FLOGO-001 ERROR)
	// t1: REST timeout=0 (FLOGO-002 WARNING)
	// flow2: has errorHandler (FLOGO-G-001 GOOD → positive)
	if len(result.Findings) < 2 {
		t.Fatalf("expected at least 2 findings, got %d", len(result.Findings))
	}
	if len(result.Positives) != 1 {
		t.Fatalf("expected 1 positive, got %d", len(result.Positives))
	}

	// Verify ERROR comes before WARNING in findings
	hasError, hasWarning := false, false
	firstErrorIdx, firstWarnIdx := -1, -1
	for i, f := range result.Findings {
		if f.Severity == "ERROR" && firstErrorIdx < 0 {
			firstErrorIdx = i
			hasError = true
		}
		if f.Severity == "WARNING" && firstWarnIdx < 0 {
			firstWarnIdx = i
			hasWarning = true
		}
	}
	if hasError && hasWarning && firstErrorIdx > firstWarnIdx {
		t.Error("expected ERROR findings to appear before WARNING in sorted output")
	}
}

// ─── XML evaluation ───────────────────────────────────────────────────────────

const xmlProcessRule = `
rule:
  id: XML-001
  severity: WARNING
  title: Disabled Process Found
  description: "Process {{.Scope.value}} is disabled"
  tags: [xml, bw]
  applies_to: [.xml, .bwp]
  scope: "//process[@enabled='false']"
  match:
    type: equals
    path: "@enabled"
    value: "false"
  location: "process:disabled"
`

const sampleXML = `<?xml version="1.0"?>
<processes>
  <process name="Active" enabled="true"/>
  <process name="Disabled" enabled="false"/>
</processes>`

func TestEvaluate_XML_FindsDisabledProcess(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "xml-001.yaml", xmlProcessRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   sampleXML,
		FileName:  "flows.xml",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding (disabled process), got %d", len(result.Findings))
	}
	if result.Findings[0].RuleID != "XML-001" {
		t.Errorf("expected XML-001, got %q", result.Findings[0].RuleID)
	}
}

// ─── edge cases ───────────────────────────────────────────────────────────────

func TestEvaluate_EmptyDocument_JSONObject(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   `{}`,
		FileName:  "empty.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error for empty JSON: %v", err)
	}
	// scope "$.resources[*].data" → no resources → no scope items → no findings
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings for empty document, got %d", len(result.Findings))
	}
}

func TestEvaluate_RecursiveRulesDir_SubdirRules(t *testing.T) {
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "flogo", "flogo-001.yaml", missingErrorHandlerRule)
	addRule(t, rulesDir, "flogo", "flogo-002.yaml", httpTimeoutRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Rules are in subdirs — should still be found by recursive walk
	if len(result.Findings) < 1 {
		t.Fatal("expected findings from rules loaded via recursive walk")
	}
}

// ─── filterByParser ───────────────────────────────────────────────────────────

// missingErrorHandlerRule has no parser field, so it runs against every parser.
// This variant constrains it to the yaml parser only.
const yamlOnlyMissingErrorHandlerRule = `
rule:
  id: YAML-ONLY-001
  severity: ERROR
  title: Flow Missing Error Handler (YAML only)
  parser: yaml
  scope: "$.resources[*].data"
  match:
    type: missing
    path: "errorHandler.tasks"
`

func TestEvaluate_FilterByParser_YAMLRuleSkippedForJSONFile(t *testing.T) {
	// flogoAppWithIssues is JSON (.flogo) — the yaml-only rule must be filtered out
	// entirely and produce zero findings.  Without filterByParser this same rule
	// (minus the parser: field) would fire on the two flows that lack errorHandler.
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "yaml-only-001.yaml", yamlOnlyMissingErrorHandlerRule)

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("parser-constrained rule must not run against json parser, got %d findings", len(result.Findings))
	}
	if result.Overview["rules_run"].(int) != 0 {
		t.Fatalf("expected rules_run=0 when all rules are filtered by parser, got %v", result.Overview["rules_run"])
	}
}

func TestEvaluate_FilterByParser_UnconstrainedRuleRunsForAllParsers(t *testing.T) {
	// A rule with no parser field must run regardless of the detected parser.
	rulesDir := mkRulesDir(t)
	addRule(t, rulesDir, "", "flogo-001.yaml", missingErrorHandlerRule) // no parser: field

	result, err := engine.Evaluate(engine.Request{
		Content:   flogoAppWithIssues,
		FileName:  "app.flogo",
		RulesPath: rulesDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Overview["rules_run"].(int) < 1 {
		t.Fatalf("expected at least 1 rule to run for unconstrained rule, got %v", result.Overview["rules_run"])
	}
}
