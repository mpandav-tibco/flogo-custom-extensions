package evaluator

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/model"
	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/parser"
)

// Run evaluates a set of rules against a parsed document and returns all findings.
// Rules are evaluated in parallel (one goroutine per rule); the Document
// implementations are read-only after construction so concurrent access is safe.
// Output order follows severity priority (ERROR → WARNING → INFO) then rule ID,
// matching the sort applied by sortedRules.
func Run(rules []*model.RuleDef, doc parser.Document, fileName string) (findings []model.Finding, positives []model.Finding) {
	if len(rules) == 0 {
		return
	}

	fileInfo := FileInfo{
		Name:      filepath.Base(fileName),
		Extension: strings.ToLower(filepath.Ext(fileName)),
	}

	sorted := sortedRules(rules)

	type result struct {
		findings  []model.Finding
		positives []model.Finding
	}

	results := make([]result, len(sorted))
	var wg sync.WaitGroup
	for i, rule := range sorted {
		wg.Add(1)
		go func(idx int, r *model.RuleDef) {
			defer wg.Done()
			f, p := evalRule(r, doc, fileInfo)
			results[idx] = result{f, p}
		}(i, rule)
	}
	wg.Wait()

	for _, r := range results {
		findings = append(findings, r.findings...)
		positives = append(positives, r.positives...)
	}
	return
}

func evalRule(rule *model.RuleDef, doc parser.Document, fileInfo FileInfo) (findings []model.Finding, positives []model.Finding) {
	// Expand scope — the set of objects this rule evaluates against
	scopeItems, err := doc.ResolveScope(rule.Scope)
	if err != nil {
		// Scope resolution error — create a single diagnostic finding
		findings = append(findings, model.Finding{
			RuleID:   rule.ID,
			Severity: model.SeverityInfo,
			Title:    fmt.Sprintf("Scope error for rule %s", rule.ID),
			Message:  err.Error(),
		})
		return
	}

	if len(scopeItems) == 0 {
		return
	}

	maxOcc := rule.MaxOccurrences // 0 = unlimited
	matchCount := 0               // total matches (including suppressed ones)
	var whenErr, matchErr error
	for _, scopeItem := range scopeItems {
		// Apply when pre-filter if present
		if rule.When != nil {
			preFilter, err := EvaluateCondition(*rule.When, doc, scopeItem)
			if err != nil {
				whenErr = err
				break // same condition definition applies to all items — stop early
			}
			if !preFilter.Matched {
				continue
			}
		}

		// Evaluate the main match condition
		result, err := EvaluateCondition(rule.Match, doc, scopeItem)
		if err != nil {
			matchErr = err
			break // same condition definition applies to all items — stop early
		}

		if !result.Matched {
			continue
		}

		matchCount++

		// Enforce max_occurrences — count all matches but only emit up to the cap.
		if maxOcc > 0 && matchCount > maxOcc {
			continue
		}

		ctx := TemplateContext{
			Scope: scopeItem,
			Root:  doc.Root(),
			File:  fileInfo,
			Match: result.Value,
		}

		finding := model.Finding{
			RuleID:         rule.ID,
			Severity:       rule.Severity,
			Category:       rule.Category,
			Title:          rule.Title,
			Location:       Interpolate(rule.Location, ctx),
			Message:        Interpolate(rule.Description, ctx),
			Recommendation: Interpolate(rule.Recommendation, ctx),
			Tags:           rule.Tags,
			RootCauses:     rule.RootCauses,
			Fixes:          rule.Fixes,
		}

		if rule.Severity == model.SeverityGood {
			positives = append(positives, finding)
		} else {
			findings = append(findings, finding)
		}
	}

	// If max_occurrences was hit, append a single INFO finding summarising how
	// many additional matches were suppressed.
	if maxOcc > 0 && matchCount > maxOcc {
		suppressed := matchCount - maxOcc
		findings = append(findings, model.Finding{
			RuleID:   rule.ID,
			Severity: model.SeverityInfo,
			Title:    fmt.Sprintf("[%s] %d additional occurrence(s) suppressed", rule.ID, suppressed),
			Message:  fmt.Sprintf("max_occurrences limit of %d reached; %d further match(es) not shown", maxOcc, suppressed),
			Tags:     rule.Tags,
		})
	}

	// Surface evaluation errors as diagnostic INFO findings rather than
	// swallowing them silently — a rule author needs to know their rule is broken.
	if whenErr != nil {
		findings = append(findings, model.Finding{
			RuleID:   rule.ID,
			Severity: model.SeverityInfo,
			Title:    fmt.Sprintf("Rule %s: when-condition error", rule.ID),
			Message:  whenErr.Error(),
		})
	}
	if matchErr != nil {
		findings = append(findings, model.Finding{
			RuleID:   rule.ID,
			Severity: model.SeverityInfo,
			Title:    fmt.Sprintf("Rule %s: match error", rule.ID),
			Message:  matchErr.Error(),
		})
	}
	return
}

// severityOrder controls the sort priority of findings output.
var severityOrder = map[string]int{
	model.SeverityError:   0,
	model.SeverityWarning: 1,
	model.SeverityInfo:    2,
	model.SeverityGood:    3,
}

func sortedRules(rules []*model.RuleDef) []*model.RuleDef {
	sorted := make([]*model.RuleDef, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		si := severityOrder[sorted[i].Severity]
		sj := severityOrder[sorted[j].Severity]
		if si != sj {
			return si < sj
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
}
