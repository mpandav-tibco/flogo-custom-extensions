package evaluator

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/model"
	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/parser"
)


// Run evaluates a set of rules against a parsed document and returns all findings.
func Run(rules []*model.RuleDef, doc parser.Document, fileName string) (findings []model.Finding, positives []model.Finding) {
	fileInfo := FileInfo{
		Name:      filepath.Base(fileName),
		Extension: strings.ToLower(filepath.Ext(fileName)),
	}

	// Sort rules: ERROR → WARNING → INFO → GOOD, then alphabetically by ID
	sorted := sortedRules(rules)

	for _, rule := range sorted {
		newFindings, newPositives := evalRule(rule, doc, fileInfo)
		if rule.Severity == model.SeverityGood {
			positives = append(positives, newPositives...)
		} else {
			findings = append(findings, newFindings...)
		}
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
			Severity: "INFO",
			Title:    fmt.Sprintf("Scope error for rule %s", rule.ID),
			Message:  err.Error(),
		})
		return
	}

	if len(scopeItems) == 0 {
		return
	}

	for _, scopeItem := range scopeItems {
		// Apply when pre-filter if present
		if rule.When != nil {
			preFilter, err := EvaluateCondition(*rule.When, doc, scopeItem)
			if err != nil || !preFilter.Matched {
				continue // skip this scope item
			}
		}

		// Evaluate the main match condition
		result, err := EvaluateCondition(rule.Match, doc, scopeItem)
		if err != nil {
			// Unknown or unimplemented match type — skip silently (logged by engine)
			continue
		}

		if !result.Matched {
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
