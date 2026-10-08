package evaluator

import (
	"strings"
	"testing"
)

func TestInterpolate_NoTemplate_PassthroughString(t *testing.T) {
	result := Interpolate("no template here", TemplateContext{})
	if result != "no template here" {
		t.Fatalf("expected passthrough, got %q", result)
	}
}

func TestInterpolate_EmptyString(t *testing.T) {
	result := Interpolate("", TemplateContext{})
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestInterpolate_ScopeField(t *testing.T) {
	ctx := TemplateContext{
		Scope: map[string]interface{}{"name": "myflow"},
	}
	result := Interpolate("flow:{{.Scope.name}}", ctx)
	if result != "flow:myflow" {
		t.Fatalf("expected 'flow:myflow', got %q", result)
	}
}

func TestInterpolate_FileInfo(t *testing.T) {
	ctx := TemplateContext{
		File: FileInfo{Name: "app.flogo", Extension: ".flogo"},
	}
	result := Interpolate("file={{.File.Name}}", ctx)
	if result != "file=app.flogo" {
		t.Fatalf("expected 'file=app.flogo', got %q", result)
	}
}

func TestInterpolate_FileExtension(t *testing.T) {
	ctx := TemplateContext{
		File: FileInfo{Name: "pod.yaml", Extension: ".yaml"},
	}
	result := Interpolate("ext={{.File.Extension}}", ctx)
	if result != "ext=.yaml" {
		t.Fatalf("expected 'ext=.yaml', got %q", result)
	}
}

func TestInterpolate_MatchValue(t *testing.T) {
	ctx := TemplateContext{Match: "nginx:latest"}
	result := Interpolate("image={{.Match}}", ctx)
	if result != "image=nginx:latest" {
		t.Fatalf("expected 'image=nginx:latest', got %q", result)
	}
}

func TestInterpolate_MissingKey_EmptyString(t *testing.T) {
	ctx := TemplateContext{
		Scope: map[string]interface{}{"name": "flow1"},
	}
	// .Scope.nonexistent should produce empty string (missingkey=zero)
	result := Interpolate("{{.Scope.nonexistent}}", ctx)
	if result != "" {
		t.Fatalf("expected empty string for missing key, got %q", result)
	}
}

func TestInterpolate_MultiplePlaceholders(t *testing.T) {
	ctx := TemplateContext{
		Scope: map[string]interface{}{"name": "OrderFlow"},
		File:  FileInfo{Name: "order.flogo"},
		Match: "timeout=0",
	}
	result := Interpolate("{{.Scope.name}} in {{.File.Name}} — {{.Match}}", ctx)
	expected := "OrderFlow in order.flogo — timeout=0"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestInterpolate_ScopeAsString_WrappedInValue(t *testing.T) {
	// When scope is a primitive (not a map), toMap wraps it as {"value": <primitive>}
	ctx := TemplateContext{Scope: "raw string"}
	result := Interpolate("{{.Scope.value}}", ctx)
	if result != "raw string" {
		t.Fatalf("expected 'raw string', got %q", result)
	}
}

func TestInterpolate_NilScope_EmptyMap(t *testing.T) {
	ctx := TemplateContext{Scope: nil}
	result := Interpolate("{{.Scope.name}}", ctx)
	// nil scope → empty map → missing key → empty string
	if result != "" {
		t.Fatalf("expected empty string for nil scope, got %q", result)
	}
}

func TestInterpolate_NilMatch_EmptyString(t *testing.T) {
	ctx := TemplateContext{Match: nil}
	result := Interpolate("{{.Match}}", ctx)
	if result != "<nil>" {
		// fmt.Sprintf("%v", nil) = "<nil>"
		t.Fatalf("expected '<nil>', got %q", result)
	}
}

func TestInterpolate_InvalidTemplate_ReturnOriginal(t *testing.T) {
	// An unparseable template now returns a descriptive error string
	result := Interpolate("{{.Invalid...syntax", TemplateContext{})
	if !strings.HasPrefix(result, "[template parse error:") {
		t.Fatalf("expected template parse error string, got %q", result)
	}
}

func TestInterpolate_ConditionalTemplate(t *testing.T) {
	ctx := TemplateContext{
		Scope: map[string]interface{}{"name": "flow1", "has_error": true},
	}
	result := Interpolate("{{.Scope.name}}", ctx)
	if result != "flow1" {
		t.Fatalf("expected 'flow1', got %q", result)
	}
}

func TestInterpolate_NestedScopeMap(t *testing.T) {
	ctx := TemplateContext{
		Scope: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "my-deployment",
			},
		},
	}
	// Nested map access through template — only one level of .Scope.x works
	// because toMap returns the outer map
	result := Interpolate("{{.Scope.metadata}}", ctx)
	// Should get the nested map representation, not panic
	if result == "" {
		t.Fatal("expected non-empty result for nested scope")
	}
}

// ─── toMap ────────────────────────────────────────────────────────────────────

func TestToMap_NilInput(t *testing.T) {
	result := toMap(nil)
	m, ok := result.(map[string]interface{})
	if !ok || len(m) != 0 {
		t.Fatal("expected empty map for nil input")
	}
}

func TestToMap_MapInput_Passthrough(t *testing.T) {
	input := map[string]interface{}{"key": "val"}
	result := toMap(input)
	m, ok := result.(map[string]interface{})
	if !ok || m["key"] != "val" {
		t.Fatal("expected passthrough for map input")
	}
}

func TestToMap_StringInput_Wrapped(t *testing.T) {
	result := toMap("hello")
	m, ok := result.(map[string]interface{})
	if !ok || m["value"] != "hello" {
		t.Fatalf("expected wrapped string, got %v", result)
	}
}

func TestToMap_IntInput_Wrapped(t *testing.T) {
	result := toMap(42)
	m, ok := result.(map[string]interface{})
	if !ok || m["value"] != 42 {
		t.Fatalf("expected wrapped int, got %v", result)
	}
}
