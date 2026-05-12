package parser

import (
	"testing"
)

// ─── fixtures ────────────────────────────────────────────────────────────────

const sampleLog = "2024-01-01 ERROR Something went wrong\n2024-01-01 INFO Processing request\n2024-01-01 WARN Memory usage high"

const windowsLineEndings = "line one\r\nline two\r\nline three"

// ─── parse ────────────────────────────────────────────────────────────────────

func TestLinesParser_BasicParse(t *testing.T) {
	doc, err := (&LinesParser{}).Parse(sampleLog)
	if err != nil || doc == nil {
		t.Fatalf("expected non-nil doc, got err=%v", err)
	}
}

func TestLinesParser_EmptyString_SingleEmptyLine(t *testing.T) {
	doc, err := (&LinesParser{}).Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty string splits into one empty line
	scope, _ := doc.ResolveScope("")
	if len(scope) < 1 {
		t.Fatal("expected at least one scope item for empty content")
	}
}

func TestLinesParser_LineCount(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, err := doc.ResolveScope("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(scope))
	}
}

func TestLinesParser_WindowsLineEndings_Stripped(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(windowsLineEndings)
	scope, _ := doc.ResolveScope("")

	for i, item := range scope {
		m, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("scope[%d] not a map", i)
		}
		line := m["line"].(string)
		if len(line) > 0 && line[len(line)-1] == '\r' {
			t.Fatalf("line %d still has \\r: %q", i, line)
		}
	}
}

// ─── ResolveScope ─────────────────────────────────────────────────────────────

func TestLinesParser_ResolveScope_EmptyPath_AllLines(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, err := doc.ResolveScope("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scope) != 3 {
		t.Fatalf("expected 3 items, got %d", len(scope))
	}
}

func TestLinesParser_ResolveScope_Dollar_AllLines(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, _ := doc.ResolveScope("$")
	if len(scope) != 3 {
		t.Fatalf("expected 3 items via '$', got %d", len(scope))
	}
}

func TestLinesParser_ResolveScope_DollarStar_AllLines(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, _ := doc.ResolveScope("$[*]")
	if len(scope) != 3 {
		t.Fatalf("expected 3 items via '$[*]', got %d", len(scope))
	}
}

func TestLinesParser_ResolveScope_DotLine_ExtractStrings(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, _ := doc.ResolveScope("$[*].line")
	if len(scope) != 3 {
		t.Fatalf("expected 3 line strings, got %d", len(scope))
	}
	// Items should be raw strings
	if scope[0] != "2024-01-01 ERROR Something went wrong" {
		t.Fatalf("unexpected first line: %v", scope[0])
	}
}

// ─── line object structure ────────────────────────────────────────────────────

func TestLinesParser_ScopeItem_HasLineField(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, _ := doc.ResolveScope("")

	item, ok := scope[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected map scope item")
	}
	line, ok := item["line"].(string)
	if !ok {
		t.Fatal("expected 'line' string field")
	}
	if line != "2024-01-01 ERROR Something went wrong" {
		t.Fatalf("unexpected line text: %q", line)
	}
}

func TestLinesParser_ScopeItem_HasNumberField(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, _ := doc.ResolveScope("")

	for i, item := range scope {
		m := item.(map[string]interface{})
		num, ok := m["number"]
		if !ok {
			t.Fatalf("line %d missing 'number' field", i)
		}
		expectedNum := i + 1
		if num != expectedNum {
			t.Fatalf("line %d: expected number=%d, got %v", i, expectedNum, num)
		}
	}
}

func TestLinesParser_ScopeItem_LineNumbers_OneBased(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse("first\nsecond\nthird")
	scope, _ := doc.ResolveScope("")

	nums := []int{}
	for _, item := range scope {
		m := item.(map[string]interface{})
		nums = append(nums, m["number"].(int))
	}
	if nums[0] != 1 || nums[1] != 2 || nums[2] != 3 {
		t.Fatalf("expected 1-based numbering, got %v", nums)
	}
}

// ─── ResolvePath ──────────────────────────────────────────────────────────────

func TestLinesParser_ResolvePath_LineField(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[1], "line")
	if !ok || val != "2024-01-01 INFO Processing request" {
		t.Fatalf("expected INFO line, got %v (ok=%v)", val, ok)
	}
}

func TestLinesParser_ResolvePath_NumberField(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[2], "number")
	if !ok || val != 3 {
		t.Fatalf("expected number=3, got %v (ok=%v)", val, ok)
	}
}

func TestLinesParser_ResolvePath_EmptyPath_ReturnsSelf(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, _ := doc.ResolveScope("")

	val, ok := doc.ResolvePath(scope[0], "")
	if !ok || val == nil {
		t.Fatal("empty path should return scope item itself")
	}
}

func TestLinesParser_ResolvePath_MissingField(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	scope, _ := doc.ResolveScope("")

	_, ok := doc.ResolvePath(scope[0], "nonexistent")
	if ok {
		t.Fatal("expected not-found for missing field")
	}
}

func TestLinesParser_ResolvePath_OnStringObject(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	// Simulate a raw string scope (from $[*].line extraction)
	val, ok := doc.ResolvePath("raw line text", "line")
	if !ok || val != "raw line text" {
		t.Fatalf("expected raw string returned for 'line' path, got %v (ok=%v)", val, ok)
	}
}

func TestLinesParser_ResolvePath_StringObject_WrongField(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	_, ok := doc.ResolvePath("raw line text", "number")
	if ok {
		t.Fatal("expected not-found when string object and non-'line' path")
	}
}

// ─── Root ─────────────────────────────────────────────────────────────────────

func TestLinesParser_Root_ReturnsAllLines(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse(sampleLog)
	root := doc.Root()
	lines, ok := root.([]interface{})
	if !ok {
		t.Fatal("expected []interface{} root")
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines in root, got %d", len(lines))
	}
}

// ─── multiline edge cases ─────────────────────────────────────────────────────

func TestLinesParser_SingleLine(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse("only one line")
	scope, _ := doc.ResolveScope("")
	if len(scope) != 1 {
		t.Fatalf("expected 1 line, got %d", len(scope))
	}
	m := scope[0].(map[string]interface{})
	if m["line"] != "only one line" || m["number"] != 1 {
		t.Fatalf("unexpected content: %v", m)
	}
}

func TestLinesParser_BlankLines_Included(t *testing.T) {
	doc, _ := (&LinesParser{}).Parse("line1\n\nline3")
	scope, _ := doc.ResolveScope("")
	if len(scope) != 3 {
		t.Fatalf("expected 3 scope items (including blank line), got %d", len(scope))
	}
	m := scope[1].(map[string]interface{})
	if m["line"] != "" {
		t.Fatalf("expected blank second line, got %q", m["line"])
	}
}
