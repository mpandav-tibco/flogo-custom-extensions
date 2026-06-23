package model

// Severity constants for rule definitions and findings.
const (
	SeverityError   = "ERROR"
	SeverityWarning = "WARNING"
	SeverityInfo    = "INFO"
	SeverityGood    = "GOOD"
)

// Rule is the top-level structure for a YAML rule file.
type Rule struct {
	Rule RuleDef `yaml:"rule"`
}

// RuleFile is the flexible top-level structure for a YAML rule file. It accepts
// EITHER a single `rule:` (legacy one-rule-per-file) OR a `rules:` list
// (multiple rules per file). The loader collects whichever form is present.
type RuleFile struct {
	Rule  *RuleDef   `yaml:"rule"`
	Rules []*RuleDef `yaml:"rules"`
}

// RuleDef holds the full definition of a single analysis rule.
type RuleDef struct {
	ID             string     `yaml:"id"`
	Enabled        *bool      `yaml:"enabled"`  // pointer so we can distinguish false from absent
	Severity       string     `yaml:"severity"` // ERROR | WARNING | INFO | GOOD (case-insensitive on load)
	Category       string     `yaml:"category"`
	Title          string     `yaml:"title"`
	Description    string     `yaml:"description"`
	Message        string     `yaml:"message"` // user-facing finding text; supports {{ field }} interpolation (condition rules)
	Tags           []string   `yaml:"tags"`
	AppliesTo      []string   `yaml:"applies_to"`
	Parser         string     `yaml:"parser"` // override parser for this rule
	Scope          string     `yaml:"scope"`  // JSONPath / XPath / dot-path to scope array
	When           *Condition `yaml:"when"`   // optional pre-filter on scope items
	Match          Condition  `yaml:"match"`
	// Condition is an alternative to Match: a human-readable DSL expression
	// (e.g. `source_system is null OR source_system == ""`). When set, the rule
	// is evaluated via the condition-DSL evaluator instead of the structured Match.
	Condition string `yaml:"condition"`
	// Field names the spec field a signal/normalization rule pertains to (metadata
	// for NLU/normalization rules; surfaced in the finding location).
	Field string `yaml:"field"`
	// Action is a descriptive normalization directive (e.g. `set interface_type = "pub"`).
	// The engine is read-only and does not mutate documents; this is metadata only.
	Action         string `yaml:"action"`
	Location       string `yaml:"location"`       // go template: "flow:{{.Scope.name}}"
	Recommendation string `yaml:"recommendation"` // go template
	// Log-specific
	RootCauses []string `yaml:"root_causes"`
	Fixes      []string `yaml:"fixes"`

	// MaxOccurrences caps the number of findings emitted for this rule per
	// evaluation run. 0 means unlimited. When the cap is hit, a single INFO
	// summary finding is appended to indicate suppressed matches.
	MaxOccurrences int `yaml:"max_occurrences"`
}

// IsEnabled returns true when the rule is active (default true when field is absent).
func (r *RuleDef) IsEnabled() bool {
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// Condition represents a match condition or a when pre-filter.
type Condition struct {
	Type       string      `yaml:"type"`
	Path       string      `yaml:"path"`
	Value      interface{} `yaml:"value"`
	Substring  string      `yaml:"substring"`
	Substrings []string    `yaml:"substrings"`
	Pattern    string      `yaml:"pattern"`
	Flags      []string    `yaml:"flags"`
	JS         string      `yaml:"js"`         // for expression type (Phase 2)
	Conditions []Condition `yaml:"conditions"` // for any_of / all_of / none_of

	// Extended fields used by newer match types.
	Paths            []string `yaml:"paths"`             // all_missing: list of paths to check
	Keys             []string `yaml:"keys"`              // none_contain: object keys to look for
	HeaderNames      []string `yaml:"header_names"`      // credential_header_literal
	MinCount         int      `yaml:"min_count"`         // count_greater_than, duplicate_values
	RequiresContains string   `yaml:"requires_contains"` // regex_not_match pre-guard
}
