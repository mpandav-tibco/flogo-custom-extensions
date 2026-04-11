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
// Operator and PredicateMode act as defaults; the corresponding Input fields
// override them when non-empty at runtime.
type Settings struct {
	Operator             string `md:"operator"`
	PredicateMode        string `md:"predicateMode"`
	PassThroughOnMissing bool   `md:"passThroughOnMissing"`

	// Deduplication (opt-in — EnableDedup must be true to activate, zero overhead otherwise).
	// When enabled, the DedupField input at runtime identifies the message field used as
	// the unique event ID. DedupWindow controls TTL; DedupMaxEntries caps in-memory usage.
	EnableDedup     bool   `md:"enableDedup"`
	DedupWindow     string `md:"dedupWindow"`
	DedupMaxEntries int64  `md:"dedupMaxEntries"`

	// Rate limiting (opt-in — RateLimitRPS == 0 disables, zero overhead).
	// RateLimitMode: "drop" returns passed=false immediately; "wait" blocks
	// up to RateLimitMaxWaitMs milliseconds before dropping.
	RateLimitRPS       float64 `md:"rateLimitRPS"`
	RateLimitBurst     int     `md:"rateLimitBurst"`
	RateLimitMode      string  `md:"rateLimitMode"`
	RateLimitMaxWaitMs int64   `md:"rateLimitMaxWaitMs"`
}

// Input is the per-execution input, mapped from the Flogo flow.
// All predicate fields are runtime inputs so they can be mapped from upstream
// activities or app properties at flow design time.
type Input struct {
	Message        map[string]interface{} `md:"message"`
	Field          string                 `md:"field"`
	Operator       string                 `md:"operator"`
	Value          string                 `md:"value"`
	PredicatesJSON string                 `md:"predicates"`
	PredicateMode  string                 `md:"predicateMode"`
	// DedupField is the message field to use as the unique event ID for deduplication.
	// Only active when the 'enableDedup' setting is true.
	DedupField string `md:"dedupField"`
}

// ParsedPredicates returns the multi-predicate slice from PredicatesJSON.
// Returns nil when PredicatesJSON is empty.
func (i *Input) ParsedPredicates() ([]Predicate, error) {
	if i.PredicatesJSON == "" {
		return nil, nil
	}
	var ps []Predicate
	if err := json.Unmarshal([]byte(i.PredicatesJSON), &ps); err != nil {
		return nil, err
	}
	return ps, nil
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"message":       i.Message,
		"field":         i.Field,
		"operator":      i.Operator,
		"value":         i.Value,
		"predicates":    i.PredicatesJSON,
		"predicateMode": i.PredicateMode,
		"dedupField":    i.DedupField,
	}
}

func (i *Input) FromMap(values map[string]interface{}) error {
	var err error
	i.Message, err = coerce.ToObject(values["message"])
	if err != nil {
		return err
	}
	i.Field, err = coerce.ToString(values["field"])
	if err != nil {
		return err
	}
	i.Operator, err = coerce.ToString(values["operator"])
	if err != nil {
		return err
	}
	i.Value, err = coerce.ToString(values["value"])
	if err != nil {
		return err
	}
	i.PredicatesJSON, err = coerce.ToString(values["predicates"])
	if err != nil {
		return err
	}
	i.PredicateMode, err = coerce.ToString(values["predicateMode"])
	if err != nil {
		return err
	}
	i.DedupField, err = coerce.ToString(values["dedupField"])
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
