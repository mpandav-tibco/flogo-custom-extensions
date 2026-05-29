package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/model"
	"gopkg.in/yaml.v3"
)

// ── Rule cache ────────────────────────────────────────────────────────────────

type rulesEntry struct {
	rules    []*model.RuleDef
	warnings []string
	dirMtime time.Time
}

// rulesCache stores parsed rule sets keyed by rulesPath.
// Entries are invalidated when the directory's own modification time changes
// (i.e., when files are added or removed). For content changes inside existing
// files the service must be restarted — rules are treated as static at runtime.
var rulesCache sync.Map // rulesPath → *rulesEntry

// loadRules returns the rules for rulesPath, using a directory-mtime cache to
// avoid re-parsing on every Flogo activity invocation.
func loadRules(rulesPath string) ([]*model.RuleDef, []string) {
	// Stat the directory first so we can check the cache.
	info, statErr := os.Stat(rulesPath)

	if statErr == nil {
		if cached, ok := rulesCache.Load(rulesPath); ok {
			entry := cached.(*rulesEntry)
			if !info.ModTime().After(entry.dirMtime) {
				return entry.rules, entry.warnings
			}
		}
	}

	// Cache miss or stale — load fresh.
	rules, warnings := loadRulesFresh(rulesPath)

	if statErr == nil {
		rulesCache.Store(rulesPath, &rulesEntry{
			rules:    rules,
			warnings: warnings,
			dirMtime: info.ModTime(),
		})
	}
	return rules, warnings
}

// loadRulesFresh reads all .yaml files from rulesPath (recursively) and returns
// the validated rule definitions. Invalid rules are collected as errors and
// skipped — they never crash the engine or affect other rules.
func loadRulesFresh(rulesPath string) ([]*model.RuleDef, []string) {
	var rules []*model.RuleDef
	var warnings []string
	seen := make(map[string]string) // rule ID → first file that defined it

	// Collect all YAML file paths first, then sort them explicitly.
	// filepath.Walk order is filesystem-dependent and not guaranteed to be
	// alphabetical — sorting here ensures deterministic dedup ("first loaded wins")
	// across all operating systems and filesystem types.
	var yamlPaths []string
	walkErr := filepath.Walk(rulesPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cannot access %s: %v", path, err))
			return nil // continue walking
		}
		if !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".yaml") {
			yamlPaths = append(yamlPaths, path)
		}
		return nil
	})
	if walkErr != nil {
		warnings = append(warnings, fmt.Sprintf("rules directory walk error: %v", walkErr))
	}

	sort.Strings(yamlPaths)

	for _, path := range yamlPaths {
		rule, warn := parseRuleFile(path)
		if warn != "" {
			warnings = append(warnings, warn)
			continue
		}
		if !rule.IsEnabled() {
			continue
		}

		// Deduplication: first loaded wins (deterministic alphabetical order)
		if prev, dup := seen[rule.ID]; dup {
			warnings = append(warnings, fmt.Sprintf(
				"duplicate rule ID %q in %s — already loaded from %s, skipping", rule.ID, path, prev))
			continue
		}
		seen[rule.ID] = path
		rules = append(rules, rule)
	}

	return rules, warnings
}

func parseRuleFile(path string) (*model.RuleDef, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Sprintf("cannot read %s: %v", path, err)
	}

	var wrapper model.Rule
	if err := yaml.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Sprintf("YAML error in %s: %v", path, err)
	}

	rule := &wrapper.Rule

	if err := validateRule(rule, path); err != nil {
		return nil, err.Error()
	}
	return rule, ""
}

func validateRule(r *model.RuleDef, path string) error {
	if r.ID == "" {
		return fmt.Errorf("rule in %s is missing required field: id", path)
	}
	if r.Title == "" {
		return fmt.Errorf("rule %s in %s is missing required field: title", r.ID, path)
	}
	if r.Match.Type == "" {
		return fmt.Errorf("rule %s in %s is missing match.type", r.ID, path)
	}
	validSeverities := map[string]bool{
		model.SeverityError:   true,
		model.SeverityWarning: true,
		model.SeverityInfo:    true,
		model.SeverityGood:    true,
	}
	if !validSeverities[r.Severity] {
		return fmt.Errorf("rule %s in %s has invalid severity %q (must be ERROR|WARNING|INFO|GOOD)", r.ID, path, r.Severity)
	}
	// Validate Go template syntax at load time so authors get immediate feedback
	// rather than seeing unexpanded template literals like "flow:{{.Scope.nmae}}" in output.
	for _, tmplStr := range []string{r.Location, r.Recommendation, r.Description} {
		if tmplStr != "" && strings.Contains(tmplStr, "{{") {
			if _, err := template.New("").Option("missingkey=zero").Parse(tmplStr); err != nil {
				return fmt.Errorf("rule %s in %s has invalid template syntax: %v", r.ID, path, err)
			}
		}
	}
	return nil
}

// filterRules applies disabled list, applies_to, and tag filter to a loaded rule set.
// Both ext (e.g. ".yaml") and parserName (e.g. "yaml") are checked against applies_to
// so rules can target by extension OR by parser name.
func filterRules(rules []*model.RuleDef, ext, parserName string, disabled []string, tags []string) []*model.RuleDef {
	disabledSet := toSet(disabled)
	tagsSet := toSet(tags)

	var out []*model.RuleDef
	for _, r := range rules {
		if disabledSet[r.ID] {
			continue
		}
		if !appliesToExt(r, ext, parserName) {
			continue
		}
		if len(tagsSet) > 0 && !hasAnyTag(r, tagsSet) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func appliesToExt(r *model.RuleDef, ext, parserName string) bool {
	if len(r.AppliesTo) == 0 {
		return true // no restriction — applies to all
	}
	ext = strings.ToLower(ext)
	parserName = strings.ToLower(parserName)
	for _, e := range r.AppliesTo {
		e = strings.ToLower(e)
		if e == ext || e == parserName {
			return true
		}
	}
	return false
}

func hasAnyTag(r *model.RuleDef, want map[string]bool) bool {
	for _, t := range r.Tags {
		if want[t] {
			return true
		}
	}
	return false
}

func toSet(slice []string) map[string]bool {
	m := make(map[string]bool, len(slice))
	for _, s := range slice {
		m[s] = true
	}
	return m
}

// filterByParser removes rules whose parser field explicitly names a parser
// different from the one actually used for this evaluation.
// Rules with no parser restriction (parser: "") always pass through.
func filterByParser(rules []*model.RuleDef, parserName string) []*model.RuleDef {
	var out []*model.RuleDef
	for _, r := range rules {
		if r.Parser == "" || r.Parser == parserName {
			out = append(out, r)
		}
	}
	return out
}
