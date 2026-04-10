package filter

import (
	"encoding/json"

	"github.com/project-flogo/core/data/coerce"
)

// Predicate is a single condition used in multi-predicate evaluation.
type Predicate struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// Settings hold per-activity-instance configuration, set at design time.
type Settings struct {
	// Single-predicate settings (kept for backwards compatibility).
	Field                string `md:"field"`
	Operator             string `md:"operator"`
	Value                string `md:"value"`
	PassThroughOnMissing bool   `md:"passThroughOnMissing"`

	// Multi-predicate settings (enterprise).
	// PredicatesJSON is a JSON array of Predicate objects, e.g.
	//   [{"field":"price","operator":"gt","value":"100"},
	//    {"field":"region","operator":"eq","value":"US"}]
	// When non-empty, all predicates must pass (AND logic) unless
	// PredicateMode is "or".
	PredicatesJSON string `md:"predicates"`
	// PredicateMode controls how multiple predicates are combined.
	// Accepted values: "and" (default), "or".
	PredicateMode string `md:"predicateMode"`
}

// ParsedPredicates returns the multi-predicate slice from PredicatesJSON.
// Returns nil when PredicatesJSON is empty.
func (s *Settings) ParsedPredicates() ([]Predicate, error) {
	if s.PredicatesJSON == "" {
		return nil, nil
	}
	var ps []Predicate
	if err := json.Unmarshal([]byte(s.PredicatesJSON), &ps); err != nil {
		return nil, err
	}
	return ps, nil
}

// Input is the per-execution input, mapped from the Flogo flow.
type Input struct {
	Message map[string]interface{} `md:"message"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"message": i.Message,
	}
}

func (i *Input) FromMap(values map[string]interface{}) error {
	var err error
	i.Message, err = coerce.ToObject(values["message"])
	return err
}

// Output is the per-execution output written back to the Flogo flow.
type Output struct {
	Passed  bool                   `md:"passed"`
	Message map[string]interface{} `md:"message"`
	Reason  string                 `md:"reason"`

	// ErrorMessage is non-empty when evaluation itself failed (e.g. type
	// coercion error). The caller can route this to a dead-letter output
	// separate from normal filter failures (Passed=false / Reason set).
	ErrorMessage string `md:"errorMessage"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"passed":       o.Passed,
		"message":      o.Message,
		"reason":       o.Reason,
		"errorMessage": o.ErrorMessage,
	}
}

func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	o.Passed, err = coerce.ToBool(values["passed"])
	if err != nil {
		return err
	}
	o.Message, err = coerce.ToObject(values["message"])
	if err != nil {
		return err
	}
	o.Reason, err = coerce.ToString(values["reason"])
	if err != nil {
		return err
	}
	o.ErrorMessage, err = coerce.ToString(values["errorMessage"])
	return err
}
