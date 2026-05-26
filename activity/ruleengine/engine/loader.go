package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/model"
	"gopkg.in/yaml.v3"
)

// loadRules reads all .yaml files from rulesPath (recursively) and returns
// the validated rule definitions. Invalid rules are collected as errors and
// skipped — they never crash the engine or affect other rules.
func loadRules(rulesPath string) ([]*model.RuleDef, []string) {
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
	return nil
}

// filterRules applies disabled list and tag filter to a loaded rule set.
func filterRules(rules []*model.RuleDef, ext string, disabled []string, tags []string) []*model.RuleDef {
	disabledSet := toSet(disabled)
	tagsSet := toSet(tags)

	var out []*model.RuleDef
	for _, r := range rules {
		if disabledSet[r.ID] {
			continue
		}
		if !appliesToExt(r, ext) {
			continue
		}
		if len(tagsSet) > 0 && !hasAnyTag(r, tagsSet) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func appliesToExt(r *model.RuleDef, ext string) bool {
	if len(r.AppliesTo) == 0 {
		return true // no restriction — applies to all
	}
	ext = strings.ToLower(ext)
	for _, e := range r.AppliesTo {
		if strings.ToLower(e) == ext {
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
