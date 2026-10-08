package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cacheProbeRule(message string) string {
	return "rules:\n  - id: cache_probe\n    description: probe\n    condition: x == \"y\"\n    message: " + message + "\n    severity: error\n"
}

func firstMessage(t *testing.T, dir string) string {
	t.Helper()
	rules, warnings := loadRules(dir)
	if len(rules) != 1 {
		t.Fatalf("expected one rule, got %d (warnings=%v)", len(rules), warnings)
	}
	return rules[0].Message
}

func TestLoadRules_ReusesCacheWhileFilesAreUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeStrictFile(t, dir, "r.yaml", cacheProbeRule("first"))

	a, _ := loadRules(dir)
	b, _ := loadRules(dir)
	if len(a) != 1 || len(b) != 1 || a[0] != b[0] {
		t.Fatal("an unchanged rules directory should be served from the cache")
	}
}

// Editing a rule file in place does not change the directory's own mtime, so the
// previous directory-mtime cache kept serving the old rules until a restart.
func TestLoadRules_PicksUpAnEditedRuleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.yaml")
	writeStrictFile(t, dir, "r.yaml", cacheProbeRule("first"))
	if got := firstMessage(t, dir); got != "first" {
		t.Fatalf("got %q", got)
	}

	// Different size.
	writeStrictFile(t, dir, "r.yaml", cacheProbeRule("second, longer"))
	if got := firstMessage(t, dir); got != "second, longer" {
		t.Fatalf("an edit that changes the file size was not picked up: %q", got)
	}

	// Same size, only the modification time moves.
	writeStrictFile(t, dir, "r.yaml", cacheProbeRule("second, LONGER"))
	later := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if got := firstMessage(t, dir); got != "second, LONGER" {
		t.Fatalf("an edit that keeps the size but changes the mtime was not picked up: %q", got)
	}
}

func TestLoadRules_PicksUpAnEditInASubdirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "team", "a")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeStrictFile(t, sub, "r.yaml", cacheProbeRule("first"))
	if got := firstMessage(t, dir); got != "first" {
		t.Fatalf("got %q", got)
	}

	writeStrictFile(t, sub, "r.yaml", cacheProbeRule("changed in a nested directory"))
	if got := firstMessage(t, dir); got != "changed in a nested directory" {
		t.Fatalf("a nested edit was not picked up: %q", got)
	}
}

func TestLoadRules_PicksUpAddedAndRemovedFiles(t *testing.T) {
	dir := t.TempDir()
	writeStrictFile(t, dir, "one.yaml", cacheProbeRule("one"))
	if rules, _ := loadRules(dir); len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}

	extra := "rules:\n  - id: second_probe\n    description: probe\n    condition: x == \"y\"\n    message: two\n    severity: error\n"
	writeStrictFile(t, dir, "two.yaml", extra)
	if rules, _ := loadRules(dir); len(rules) != 2 {
		t.Fatalf("an added file was not picked up: %d rules", len(rules))
	}

	if err := os.Remove(filepath.Join(dir, "two.yaml")); err != nil {
		t.Fatal(err)
	}
	if rules, _ := loadRules(dir); len(rules) != 1 {
		t.Fatalf("a removed file was not dropped: %d rules", len(rules))
	}
}
