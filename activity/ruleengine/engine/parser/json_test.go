package parser

import (
	"strings"
	"testing"
)

// ─── fixtures ────────────────────────────────────────────────────────────────

const flogoJSON = `{
  "name": "test-app",
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

const arrayRootJSON = `[{"name":"a","val":1},{"name":"b","val":2},{"name":"c","val":3}]`

// ─── parse ────────────────────────────────────────────────────────────────────

func TestJSONParser_ValidJSON(t *testing.T) {
	doc, err := (&JSONParser{}).Parse(flogoJSON)
	if err != nil || doc == nil {
		t.Fatalf("expected non-nil doc, got err=%v", err)
	}
}

func TestJSONParser_InvalidJSON(t *testing.T) {
	_, err := (&JSONParser{}).Parse(`{bad json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestJSONParser_EmptyObject(t *testing.T) {
	doc, err := (&JSONParser{}).Parse(`{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	scope, _ := doc.ResolveScope("")
	if len(scope) != 1 {
		t.Fatalf("expected 1 root scope item, got %d", len(scope))
	}
}

func TestJSONParser_EmptyString(t *testing.T) {
	_, err := (&JSONParser{}).Parse(``)
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

// ─── ResolveScope ─────────────────────────────────────────────────────────────

func TestJSONParser_ResolveScope_Empty_ReturnsRoot(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, err := doc.ResolveScope("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 1 {
		t.Fatalf("expected 1 item (root), got %d", len(scope))
	}
}

func TestJSONParser_ResolveScope_Dollar_ReturnsRoot(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, err := doc.ResolveScope("$")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 1 {
		t.Fatalf("expected 1 item, got %d", len(scope))
	}
}

func TestJSONParser_ResolveScope_ArrayRoot_AllElements(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(arrayRootJSON)
	scope, err := doc.ResolveScope("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 3 {
		t.Fatalf("expected 3 elements from array root, got %d", len(scope))
	}
}

func TestJSONParser_ResolveScope_Wildcard_ExpandsArray(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, err := doc.ResolveScope("$.resources[*]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(scope))
	}
}

func TestJSONParser_ResolveScope_WildcardWithSuffix(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, err := doc.ResolveScope("$.resources[*].data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 2 {
		t.Fatalf("expected 2 data objects, got %d", len(scope))
	}
	// Each item must have a "name" field
	for i, item := range scope {
		m, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("scope[%d] is not a map", i)
		}
		if m["name"] == nil {
			t.Fatalf("scope[%d] missing 'name'", i)
		}
	}
}

func TestJSONParser_ResolveScope_PathNotFound_ReturnsEmpty(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, err := doc.ResolveScope("$.nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 0 {
		t.Fatalf("expected empty scope for missing path, got %d", len(scope))
	}
}

func TestJSONParser_ResolveScope_WildcardPathNotFound_ReturnsEmpty(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, err := doc.ResolveScope("$.missing[*]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 0 {
		t.Fatalf("expected empty scope, got %d", len(scope))
	}
}

func TestJSONParser_ResolveScope_ScalarPath_SingleItem(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, err := doc.ResolveScope("$.name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 1 {
		t.Fatalf("expected 1 scalar item, got %d", len(scope))
	}
	if scope[0] != "test-app" {
		t.Fatalf("expected 'test-app', got %v", scope[0])
	}
}

// ─── ResolvePath ──────────────────────────────────────────────────────────────

func TestJSONParser_ResolvePath_DotNotation(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, _ := doc.ResolveScope("")
	root := scope[0]

	val, ok := doc.ResolvePath(root, "name")
	if !ok || val != "test-app" {
		t.Fatalf("expected 'test-app', got %v (ok=%v)", val, ok)
	}
}

func TestJSONParser_ResolvePath_FullJSONPath(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, _ := doc.ResolveScope("")
	root := scope[0]

	val, ok := doc.ResolvePath(root, "$.name")
	if !ok || val != "test-app" {
		t.Fatalf("expected 'test-app' via JSONPath, got %v (ok=%v)", val, ok)
	}
}

func TestJSONParser_ResolvePath_NestedDotNotation(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, _ := doc.ResolveScope("$.resources[*].data")
	flow1 := scope[0]

	val, ok := doc.ResolvePath(flow1, "name")
	if !ok {
		t.Fatal("expected to find 'name' in data object")
	}
	if val != "flow1" {
		t.Fatalf("expected 'flow1', got %v", val)
	}
}

func TestJSONParser_ResolvePath_DeepNested(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, _ := doc.ResolveScope("$.resources[*].data")
	flow1 := scope[0]

	val, ok := doc.ResolvePath(flow1, "$.tasks[0].activity.ref")
	if !ok {
		t.Fatal("expected to find deeply nested path")
	}
	if val != "#rest" {
		t.Fatalf("expected '#rest', got %v", val)
	}
}

func TestJSONParser_ResolvePath_Missing(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, _ := doc.ResolveScope("")

	_, ok := doc.ResolvePath(scope[0], "nonexistent")
	if ok {
		t.Fatal("expected not-found for missing path")
	}
}

func TestJSONParser_ResolvePath_EmptyPath_ReturnsSelf(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(`{"x": 1}`)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "")
	if !ok || val == nil {
		t.Fatal("expected empty path to return the object itself")
	}
}

func TestJSONParser_ResolvePath_MissingNestedField(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, _ := doc.ResolveScope("$.resources[*].data")

	// flow1 has no errorHandler
	flow1 := scope[0]
	_, ok := doc.ResolvePath(flow1, "errorHandler.tasks")
	if ok {
		t.Fatal("expected 'errorHandler.tasks' to be missing in flow1")
	}
}

func TestJSONParser_ResolvePath_ExistsInSecondFlow(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, _ := doc.ResolveScope("$.resources[*].data")

	flow2 := scope[1]
	val, ok := doc.ResolvePath(flow2, "errorHandler.tasks")
	if !ok || val == nil {
		t.Fatal("expected errorHandler.tasks in flow2")
	}
}

// ─── Root ─────────────────────────────────────────────────────────────────────

func TestJSONParser_Root_ReturnsFullDocument(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	root := doc.Root()
	if root == nil {
		t.Fatal("expected non-nil root")
	}
	m, ok := root.(map[string]interface{})
	if !ok {
		t.Fatal("expected root to be a map")
	}
	if m["name"] != "test-app" {
		t.Fatalf("expected 'test-app', got %v", m["name"])
	}
}

// ─── corner cases ─────────────────────────────────────────────────────────────

func TestJSONParser_NullValue(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(`{"key": null}`)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "key")
	// null resolves as nil — found but nil
	_ = ok
	if val != nil {
		t.Fatalf("expected nil for JSON null, got %v", val)
	}
}

func TestJSONParser_NumericValue(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(`{"timeout": 30}`)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "timeout")
	if !ok || val == nil {
		t.Fatal("expected numeric value")
	}
}

func TestJSONParser_BooleanValue(t *testing.T) {
	doc, _ := (&JSONParser{}).Parse(`{"enabled": true}`)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "enabled")
	if !ok {
		t.Fatal("expected boolean to be found")
	}
	if val != true {
		t.Fatalf("expected true, got %v", val)
	}
}

func TestJSONParser_UnicodeContent(t *testing.T) {
	doc, err := (&JSONParser{}).Parse(`{"name": "テスト", "emoji": "🔥"}`)
	if err != nil {
		t.Fatalf("unexpected error with unicode: %v", err)
	}
	scope, _ := doc.ResolveScope("")
	val, ok := doc.ResolvePath(scope[0], "name")
	if !ok || val != "テスト" {
		t.Fatalf("expected unicode name, got %v", val)
	}
}

func TestJSONParser_LargeNestedDocument(t *testing.T) {
	// Build a large document to check for performance/memory issues
	var sb strings.Builder
	sb.WriteString(`{"items":[`)
	for i := 0; i < 100; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":`)
		sb.WriteString(string(rune('0'+i%10)))
		sb.WriteString(`}`)
	}
	sb.WriteString(`]}`)

	doc, err := (&JSONParser{}).Parse(sb.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	scope, _ := doc.ResolveScope("$.items[*]")
	if len(scope) != 100 {
		t.Fatalf("expected 100 items, got %d", len(scope))
	}
}

// ─── resolveArray edge cases ──────────────────────────────────────────────────

func TestJSONParser_ResolveScope_WildcardOnObjectRoot_Empty(t *testing.T) {
	// Root is a map, not an array. expandWildcard calls resolveArray("$"),
	// which finds the root is NOT []interface{} → returns nil → empty scope.
	doc, _ := (&JSONParser{}).Parse(`{"name":"app"}`)
	scope, err := doc.ResolveScope("$[*]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 0 {
		t.Fatalf("expected empty scope when root is not an array, got %d items", len(scope))
	}
}

func TestJSONParser_ResolveScope_PathLeadsToNonArray_Empty(t *testing.T) {
	// "$.name" resolves to a string, not an array; expandWildcard should
	// return an empty scope rather than erroring.
	doc, _ := (&JSONParser{}).Parse(`{"name":"myapp","items":[1,2,3]}`)
	scope, err := doc.ResolveScope("$.name[*]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 0 {
		t.Fatalf("expected empty scope for non-array wildcard, got %d items", len(scope))
	}
}

func TestJSONParser_ResolveScope_UnknownPath_Empty(t *testing.T) {
	// JSONPath lookup for a completely absent path should yield empty scope,
	// not an error — rules that don't match the document structure are skipped.
	doc, _ := (&JSONParser{}).Parse(flogoJSON)
	scope, err := doc.ResolveScope("$.nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 0 {
		t.Fatalf("expected empty scope for unknown path, got %d items", len(scope))
	}
}
