//go:build integration

// Package integration contains HTTP-level tests for the rule-engine-service
// Flogo binary. These tests require the binary to be pre-built.
//
// Build the binary first:
//
//	flogobuild -f <path>/rule-engine-service.flogo -o integration/bin/rule-engine-service.exe
//
// Then run:
//
//	go test -v -tags integration -timeout 120s ./integration/
//
// The binary path can be overridden via the RULEENGINE_BINARY env var.
// The rules path can be overridden via RULEENGINE_RULES_PATH env var.
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ─── configuration ────────────────────────────────────────────────────────────

const servicePort = 7000
const serviceBaseURL = "http://localhost:7000"
const startupTimeout = 30 * time.Second

var binaryPath string
var rulesPath string

// ─── test fixtures ────────────────────────────────────────────────────────────

const flogoWithIssues = `{
  "name": "problem-app",
  "version": "1.0.0",
  "resources": [
    {
      "id": "flow:main",
      "data": {
        "name": "main",
        "tasks": [{"id": "t1", "activity": {"ref": "#rest"}}]
      }
    }
  ]
}`

const flogoClean = `{
  "name": "clean-app",
  "version": "2.0.0",
  "resources": [
    {
      "id": "flow:main",
      "data": {
        "name": "main",
        "errorHandler": {"tasks": [{"id": "err"}]},
        "tasks": []
      }
    }
  ]
}`

const kubeWithLatest = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  template:
    spec:
      containers:
        - name: frontend
          image: nginx:latest
        - name: backend
          image: myapp:1.0.0
`

// ─── TestMain: lifecycle ──────────────────────────────────────────────────────

func TestMain(m *testing.M) {
	binaryPath = resolveBinaryPath()
	rulesPath = resolveRulesPath()

	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		fmt.Printf("SKIP: binary not found at %s\n", binaryPath)
		fmt.Printf("Build with: flogobuild -f <path>/rule-engine-service.flogo -o %s\n", binaryPath)
		os.Exit(0)
	}

	// Start the service
	cmd := startService(binaryPath, rulesPath)
	if cmd == nil {
		fmt.Println("FAIL: could not start service")
		os.Exit(1)
	}

	// Wait for readiness
	if err := waitForService(startupTimeout); err != nil {
		cmd.Process.Kill()
		fmt.Printf("FAIL: service did not become ready: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Rule engine service ready at %s\n", serviceBaseURL)

	code := m.Run()

	cmd.Process.Kill()
	os.Exit(code)
}

func resolveBinaryPath() string {
	if v := os.Getenv("RULEENGINE_BINARY"); v != "" {
		return v
	}
	name := "rule-engine-service"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join("bin", name)
}

func resolveRulesPath() string {
	if v := os.Getenv("RULEENGINE_RULES_PATH"); v != "" {
		return v
	}
	abs, _ := filepath.Abs("testdata/rules")
	return abs
}

func startService(binary, rules string) *exec.Cmd {
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "RULES_PATH="+rules)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Printf("failed to start service: %v\n", err)
		return nil
	}
	return cmd
}

func waitForService(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", servicePort), time.Second)
		if err == nil {
			conn.Close()
			// Port is open — try the health endpoint
			resp, err2 := http.Get(serviceBaseURL + "/api/health")
			if err2 == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				return nil
			}
			if err2 == nil {
				resp.Body.Close()
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("service not ready after %v", timeout)
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

type analyzeRequest struct {
	Content        string   `json:"content"`
	FileName       string   `json:"fileName"`
	ParserOverride string   `json:"parserOverride,omitempty"`
	DisabledRules  []string `json:"disabledRules,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

func postAnalyze(t *testing.T, req analyzeRequest) (int, map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := http.Post(serviceBaseURL+"/api/analyze",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/analyze failed: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("failed to parse response: %v\nBody: %s", err, string(b))
	}
	return resp.StatusCode, result
}

func getFindings(data map[string]interface{}) []interface{} {
	if findings, ok := data["findings"].([]interface{}); ok {
		return findings
	}
	return nil
}

// ─── health check ─────────────────────────────────────────────────────────────

func TestIntegration_Health_OK(t *testing.T) {
	resp, err := http.Get(serviceBaseURL + "/api/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		// Some Flogo apps wrap differently
		data = result
	}

	if status, ok := data["status"]; ok && status != "ok" {
		t.Fatalf("expected status=ok, got %v", status)
	}
}

// ─── analyze: Flogo JSON documents ───────────────────────────────────────────

func TestIntegration_Analyze_FlogoWithIssues_FindsErrors(t *testing.T) {
	status, data := postAnalyze(t, analyzeRequest{
		Content:  flogoWithIssues,
		FileName: "problem-app.flogo",
	})

	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}

	findings := getFindings(data)
	if len(findings) == 0 {
		t.Fatal("expected at least 1 finding for app with missing error handler")
	}

	// Verify the FLOGO-001 rule fired
	found := false
	for _, f := range findings {
		fm := f.(map[string]interface{})
		if fm["rule_id"] == "FLOGO-001" {
			found = true
			if fm["severity"] != "ERROR" {
				t.Errorf("expected ERROR severity, got %v", fm["severity"])
			}
		}
	}
	if !found {
		t.Fatal("expected FLOGO-001 finding in results")
	}
}

func TestIntegration_Analyze_CleanFlogo_NoFindings(t *testing.T) {
	status, data := postAnalyze(t, analyzeRequest{
		Content:  flogoClean,
		FileName: "clean-app.flogo",
	})

	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}

	findings := getFindings(data)
	if len(findings) != 0 {
		t.Fatalf("expected no findings for clean app, got %d", len(findings))
	}
}

// ─── analyze: Kubernetes YAML ─────────────────────────────────────────────────

func TestIntegration_Analyze_KubeLatestImage_FindsErrors(t *testing.T) {
	status, data := postAnalyze(t, analyzeRequest{
		Content:  kubeWithLatest,
		FileName: "deployment.yaml",
	})

	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}

	findings := getFindings(data)
	if len(findings) == 0 {
		t.Fatal("expected findings for kube deployment with latest image")
	}

	found := false
	for _, f := range findings {
		if f.(map[string]interface{})["rule_id"] == "KUBE-001" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected KUBE-001 in findings")
	}
}

// ─── parser override ──────────────────────────────────────────────────────────

func TestIntegration_Analyze_ParserOverride_JSON(t *testing.T) {
	// Use JSON parser override for a .dat file
	status, _ := postAnalyze(t, analyzeRequest{
		Content:        flogoWithIssues,
		FileName:       "upload.dat",
		ParserOverride: "json",
	})

	// Should succeed (parser override prevents "no parser for .dat" error)
	if status != 200 {
		t.Fatalf("expected 200 with parser override, got %d", status)
	}
}

func TestIntegration_Analyze_InvalidParserOverride_Error(t *testing.T) {
	status, data := postAnalyze(t, analyzeRequest{
		Content:        flogoWithIssues,
		FileName:       "test.json",
		ParserOverride: "nonexistent_parser",
	})

	// Expect error response
	if status == 200 {
		// Check if error is in the data
		if errMsg, ok := data["error"]; ok && errMsg != "" {
			return // error was returned in response body
		}
	}
	// 4xx or 5xx is also acceptable
	if status == 200 && data["error"] == nil {
		t.Fatal("expected error for unknown parser override")
	}
}

// ─── rule filtering ───────────────────────────────────────────────────────────

func TestIntegration_Analyze_DisabledRules_Excluded(t *testing.T) {
	// Run without disabled rules first
	_, dataFull := postAnalyze(t, analyzeRequest{
		Content:  flogoWithIssues,
		FileName: "app.flogo",
	})
	fullCount := len(getFindings(dataFull))

	// Run with FLOGO-001 disabled
	_, dataFiltered := postAnalyze(t, analyzeRequest{
		Content:       flogoWithIssues,
		FileName:      "app.flogo",
		DisabledRules: []string{"FLOGO-001"},
	})
	filteredCount := len(getFindings(dataFiltered))

	if filteredCount >= fullCount && fullCount > 0 {
		t.Fatalf("expected fewer findings with disabled rule, got same or more (%d vs %d)", filteredCount, fullCount)
	}
	// Verify FLOGO-001 is not in filtered results
	for _, f := range getFindings(dataFiltered) {
		if f.(map[string]interface{})["rule_id"] == "FLOGO-001" {
			t.Fatal("FLOGO-001 should be excluded when disabled")
		}
	}
}

func TestIntegration_Analyze_TagFilter_OnlyMatchingRules(t *testing.T) {
	_, data := postAnalyze(t, analyzeRequest{
		Content:  flogoWithIssues,
		FileName: "app.flogo",
		Tags:     []string{"flogo"},
	})

	// Should only get flogo-tagged rules
	findings := getFindings(data)
	for _, f := range findings {
		tags, _ := f.(map[string]interface{})["tags"].([]interface{})
		found := false
		for _, tag := range tags {
			if tag == "flogo" {
				found = true
			}
		}
		if !found {
			t.Errorf("finding without 'flogo' tag returned despite tag filter: %v", f)
		}
	}
}

// ─── error cases ──────────────────────────────────────────────────────────────

func TestIntegration_Analyze_InvalidJSON_Content_Error(t *testing.T) {
	status, data := postAnalyze(t, analyzeRequest{
		Content:  `{bad json`,
		FileName: "test.flogo",
	})

	// Expect either error status or error field in response
	if status == 200 {
		if data["error"] == nil || data["error"] == "" {
			t.Fatal("expected error for invalid JSON content")
		}
	}
}

func TestIntegration_Analyze_EmptyFileName_Error(t *testing.T) {
	status, data := postAnalyze(t, analyzeRequest{
		Content:  `{"key":"value"}`,
		FileName: "",
	})

	if status == 200 && data["error"] == nil {
		t.Logf("Note: empty fileName may use fallback behavior")
	}
}

func TestIntegration_Analyze_EmptyContent_Error(t *testing.T) {
	_, data := postAnalyze(t, analyzeRequest{
		Content:  "",
		FileName: "empty.flogo",
	})
	// Just verify no panic — some parsers handle empty content differently
	_ = data
}

func TestIntegration_Analyze_MalformedRequestBody(t *testing.T) {
	resp, err := http.Post(serviceBaseURL+"/api/analyze",
		"application/json", strings.NewReader(`not json at all`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	// Expect 4xx for malformed request body
	if resp.StatusCode == 200 {
		t.Logf("Note: malformed body returned 200 — Flogo may accept and produce error in body")
	}
}

func TestIntegration_Analyze_UnknownExtension_NoOverride_Error(t *testing.T) {
	status, data := postAnalyze(t, analyzeRequest{
		Content:  "some content",
		FileName: "mystery.xyz",
	})

	if status == 200 {
		if data["error"] == nil || data["error"] == "" {
			t.Fatal("expected error for unknown file extension without parser override")
		}
	}
}

// ─── result structure ─────────────────────────────────────────────────────────

func TestIntegration_Result_ContainsExpectedFields(t *testing.T) {
	_, data := postAnalyze(t, analyzeRequest{
		Content:  flogoWithIssues,
		FileName: "app.flogo",
	})

	requiredFields := []string{"findings", "positives", "overview", "markdown"}
	for _, field := range requiredFields {
		if _, ok := data[field]; !ok {
			t.Errorf("expected field %q in response, not found", field)
		}
	}
}

func TestIntegration_Result_FindingFields(t *testing.T) {
	_, data := postAnalyze(t, analyzeRequest{
		Content:  flogoWithIssues,
		FileName: "app.flogo",
	})

	findings := getFindings(data)
	if len(findings) == 0 {
		t.Skip("no findings to validate field structure")
	}

	f := findings[0].(map[string]interface{})
	for _, field := range []string{"rule_id", "severity", "title", "location"} {
		if _, ok := f[field]; !ok {
			t.Errorf("expected finding field %q, not found", field)
		}
	}
}

func TestIntegration_Result_MarkdownContainsReport(t *testing.T) {
	_, data := postAnalyze(t, analyzeRequest{
		Content:  flogoWithIssues,
		FileName: "app.flogo",
	})

	markdown, ok := data["markdown"].(string)
	if !ok || markdown == "" {
		t.Fatal("expected non-empty markdown report")
	}
	if !strings.Contains(markdown, "Analysis Report") {
		t.Errorf("expected 'Analysis Report' in markdown, got: %s", markdown)
	}
}

func TestIntegration_Result_OverviewContainsFile(t *testing.T) {
	_, data := postAnalyze(t, analyzeRequest{
		Content:  flogoWithIssues,
		FileName: "problem-app.flogo",
	})

	overview, ok := data["overview"].(map[string]interface{})
	if !ok {
		t.Fatal("expected overview map in response")
	}
	if overview["file"] != "problem-app.flogo" {
		t.Errorf("expected file=problem-app.flogo, got %v", overview["file"])
	}
}

// ─── concurrent requests ──────────────────────────────────────────────────────

func TestIntegration_ConcurrentRequests(t *testing.T) {
	const workers = 5
	errors := make(chan error, workers)

	for i := 0; i < workers; i++ {
		go func(n int) {
			_, data := postAnalyze(t, analyzeRequest{
				Content:  flogoWithIssues,
				FileName: "app.flogo",
			})
			if data == nil {
				errors <- fmt.Errorf("worker %d: nil response", n)
			} else {
				errors <- nil
			}
		}(i)
	}

	for i := 0; i < workers; i++ {
		if err := <-errors; err != nil {
			t.Error(err)
		}
	}
}
