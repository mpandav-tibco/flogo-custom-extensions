package engine

import (
	"fmt"
	"strings"
)

// maxStrictProblems caps how many load problems are spelled out in the error text.
const maxStrictProblems = 3

// StrictError is returned by Evaluate in strict mode when the rule set cannot be
// trusted: rules or rule files were skipped while loading, or no rule was
// evaluated. Both cases would otherwise look like a clean result.
type StrictError struct {
	RulesPath string
	Warnings  []string // problems found while loading (skipped rule files or rules)
	message   string
}

func (e *StrictError) Error() string { return e.message }

// checkStrict returns a *StrictError when the loaded rule set is not intact
// (any load warning) or when no rule was evaluated for this request.
func checkStrict(req Request, warnings []string, rulesRun int) error {
	if len(warnings) > 0 {
		shown, more := warnings, ""
		if len(shown) > maxStrictProblems {
			more = fmt.Sprintf(" (+%d more)", len(shown)-maxStrictProblems)
			shown = shown[:maxStrictProblems]
		}
		return &StrictError{
			RulesPath: req.RulesPath,
			Warnings:  warnings,
			message: fmt.Sprintf("strict mode: %d problem(s) loading rules from %q: %s%s",
				len(warnings), req.RulesPath, strings.Join(shown, "; "), more),
		}
	}
	if rulesRun == 0 {
		return &StrictError{
			RulesPath: req.RulesPath,
			message: fmt.Sprintf("strict mode: no rules were evaluated for %q (rulesPath=%q, tags=%v, disabledRules=%v)",
				req.FileName, req.RulesPath, req.Tags, req.DisabledRules),
		}
	}
	return nil
}
