// Package engine is the generic YAML-driven document analysis engine.
// It has zero Flogo dependencies — the activity.go wrapper adapts it for Flogo.
// The same engine.Evaluate() function can be wrapped by an HTTP handler (Phase 2)
// to run as a standalone microservice without any code changes here.
package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/evaluator"
	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/model"
	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/parser"
)

// Request is the input to a single evaluation run.
type Request struct {
	Content        string   // raw file content
	FileName       string   // used for extension → parser detection + template context
	RulesPath      string   // directory of YAML rule files
	ParserOverride string   // "json"|"xml"|"yaml"|"kv"|"lines" — overrides extension detection
	DisabledRules  []string // rule IDs to skip
	Tags           []string // only evaluate rules with at least one of these tags
}

// Evaluate runs all applicable rules against the provided document content.
// This is the single public entry point — both the Flogo activity and the
// future HTTP microservice call this function.
func Evaluate(req Request) (*model.Result, error) {
	// 1. Resolve parser
	doc, parserName, err := resolveParser(parser.DefaultRegistry(), req)
	if err != nil {
		return nil, err
	}

	// 2. Load + filter rules
	allRules, warnings := loadRules(req.RulesPath)
	ext := strings.ToLower(filepath.Ext(req.FileName))
	rules := filterRules(allRules, ext, parserName, req.DisabledRules, req.Tags)
	rules = filterByParser(rules, parserName)

	// 3. Run evaluation
	findings, positives := evaluator.Run(rules, doc, req.FileName)

	// 4. Build result
	result := &model.Result{
		Findings:  findings,
		Positives: positives,
		Overview:  buildOverview(doc, req.FileName, parserName, len(rules), warnings),
	}
	for _, f := range findings {
		switch f.Severity {
		case model.SeverityError:
			result.ErrorCount++
		case model.SeverityWarning:
			result.WarningCount++
		case model.SeverityInfo:
			result.InfoCount++
		}
	}
	result.Markdown = buildMarkdown(result, req.FileName)

	return result, nil
}

func resolveParser(reg *parser.Registry, req Request) (parser.Document, string, error) {
	var p parser.Parser
	var name string

	if req.ParserOverride != "" {
		var ok bool
		p, ok = reg.Get(req.ParserOverride)
		if !ok {
			return nil, "", fmt.Errorf("unknown parser %q", req.ParserOverride)
		}
		name = req.ParserOverride
	} else {
		var ok bool
		p, name, ok = reg.Detect(req.FileName)
		if !ok {
			return nil, "", fmt.Errorf("no parser registered for file %q — use parserOverride to specify one", req.FileName)
		}
	}

	doc, err := p.Parse(req.Content)
	if err != nil {
		return nil, name, fmt.Errorf("parse error (%s): %w", name, err)
	}
	return doc, name, nil
}

func buildOverview(doc parser.Document, fileName, parserName string, ruleCount int, warnings []string) map[string]interface{} {
	overview := map[string]interface{}{
		"file":      filepath.Base(fileName),
		"extension": strings.ToLower(filepath.Ext(fileName)),
		"parser":    parserName,
		"rules_run": ruleCount,
	}
	if len(warnings) > 0 {
		overview["warnings"] = warnings
	}
	// Attempt to extract common metadata from the document root
	if root, ok := doc.Root().(map[string]interface{}); ok {
		if name, ok := root["name"].(string); ok {
			overview["name"] = name
		}
		if version, ok := root["version"].(string); ok {
			overview["version"] = version
		}
	}
	return overview
}

func buildMarkdown(result *model.Result, fileName string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Analysis Report — %s\n\n", filepath.Base(fileName)))
	sb.WriteString(fmt.Sprintf("**Errors:** %d  |  **Warnings:** %d  |  **Info:** %d\n\n",
		result.ErrorCount, result.WarningCount, result.InfoCount))

	if len(result.Findings) > 0 {
		sb.WriteString("### Findings\n\n")
		sb.WriteString("| Severity | Rule | Location | Message |\n")
		sb.WriteString("|----------|------|----------|---------|\n")
		for _, f := range result.Findings {
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				f.Severity, f.RuleID, mdEscape(f.Location), mdEscape(f.Message)))
		}
		sb.WriteString("\n")
	}

	if len(result.Positives) > 0 {
		sb.WriteString("### Strengths\n\n")
		sb.WriteString("| Rule | Title | Location |\n")
		sb.WriteString("|------|-------|------------|\n")
		for _, f := range result.Positives {
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n",
				f.RuleID, mdEscape(f.Title), mdEscape(f.Location)))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// mdEscape sanitises a string for inclusion in a markdown table cell.
// Pipes are escaped so they don't break column boundaries; newlines are
// replaced with a space so they don't inject extra rows into the table.
func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ReplaceAll(s, "|", `\|`)
}
