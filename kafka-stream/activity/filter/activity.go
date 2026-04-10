package filter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() {
	_ = activity.Register(&Activity{}, New)
}

// Activity implements the Kafka Stream Filter activity.
// It evaluates a predicate (or a chain of predicates) against the incoming
// message and sets passed=true/false so the flow can branch accordingly.
// Enterprise features: multi-predicate AND/OR chains, ErrorMessage DLQ output.
type Activity struct {
	settings   *Settings
	logger     log.Logger
	compiled   *regexp.Regexp // non-nil only for single-predicate regex
	predicates []compiledPred // non-nil only when multi-predicate mode is active
}

// compiledPred holds a parsed predicate with its optional compiled regex.
type compiledPred struct {
	p        Predicate
	compiled *regexp.Regexp
}

// Metadata returns the activity's metadata.
func (a *Activity) Metadata() *activity.Metadata {
	return activityMd
}

// New creates and initialises a new filter activity instance.
// Regex patterns are compiled at init time so misconfigured patterns fail fast.
func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("kafka-stream/filter: failed to map settings: %w", err)
	}

	act := &Activity{settings: s}

	// Multi-predicate mode takes priority over single-predicate.
	preds, err := s.ParsedPredicates()
	if err != nil {
		return nil, fmt.Errorf("kafka-stream/filter: invalid predicates JSON: %w", err)
	}

	if len(preds) > 0 {
		// Validate and pre-compile multi-predicate chain.
		validOps := validOperators()
		compiled := make([]compiledPred, 0, len(preds))
		for i, p := range preds {
			if !validOps[p.Operator] {
				return nil, fmt.Errorf("kafka-stream/filter: predicate[%d] unsupported operator %q", i, p.Operator)
			}
			cp := compiledPred{p: p}
			if p.Operator == "regex" {
				cp.compiled, err = regexp.Compile(p.Value)
				if err != nil {
					return nil, fmt.Errorf("kafka-stream/filter: predicate[%d] invalid regex %q: %w", i, p.Value, err)
				}
			}
			compiled = append(compiled, cp)
		}
		act.predicates = compiled

		mode := s.PredicateMode
		if mode == "" {
			mode = "and"
		}
		act.logger = ctx.Logger()
		act.logger.Infof("Kafka Stream Filter initialised: multi-predicate mode=%s count=%d passThroughOnMissing=%v",
			mode, len(preds), s.PassThroughOnMissing)
	} else {
		// Single-predicate mode (backwards-compatible).
		if s.Field == "" || s.Operator == "" {
			return nil, fmt.Errorf("kafka-stream/filter: either 'field'+'operator'+'value' or 'predicates' must be configured")
		}
		if !validOperators()[s.Operator] {
			return nil, fmt.Errorf("kafka-stream/filter: unsupported operator %q", s.Operator)
		}
		if s.Operator == "regex" {
			act.compiled, err = regexp.Compile(s.Value)
			if err != nil {
				return nil, fmt.Errorf("kafka-stream/filter: invalid regex pattern %q: %w", s.Value, err)
			}
		}
		act.logger = ctx.Logger()
		act.logger.Infof("Kafka Stream Filter initialised: field=%q operator=%s value=%q passThroughOnMissing=%v",
			s.Field, s.Operator, s.Value, s.PassThroughOnMissing)
	}

	return act, nil
}

// Eval evaluates the configured predicate(s) against the incoming message.
func (a *Activity) Eval(ctx activity.Context) (done bool, err error) {
	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("kafka-stream/filter: failed to get input: %w", err)
	}

	if len(a.predicates) > 0 {
		return a.evalMulti(ctx, input)
	}
	return a.evalSingle(ctx, input)
}

// ---------------------------------------------------------------------------
// Single-predicate path (backwards-compatible)
// ---------------------------------------------------------------------------

func (a *Activity) evalSingle(ctx activity.Context, input *Input) (bool, error) {
	rawField, exists := input.Message[a.settings.Field]
	if !exists {
		if a.settings.PassThroughOnMissing {
			return a.writeOutput(ctx, true, input.Message, "", "")
		}
		return a.writeOutput(ctx, false, nil,
			fmt.Sprintf("field %q not found in message", a.settings.Field), "")
	}

	passed, reason, evalErr := evaluatePredicate(rawField, a.settings.Field, a.settings.Operator, a.settings.Value, a.compiled)
	if evalErr != nil {
		errMsg := fmt.Sprintf("evaluation error: %v", evalErr)
		a.logger.Errorf("kafka-stream/filter: %s", errMsg)
		return a.writeOutput(ctx, false, nil, "", errMsg)
	}

	var passMsg map[string]interface{}
	if passed {
		passMsg = input.Message
	}
	return a.writeOutput(ctx, passed, passMsg, reason, "")
}

// ---------------------------------------------------------------------------
// Multi-predicate path (enterprise)
// ---------------------------------------------------------------------------

func (a *Activity) evalMulti(ctx activity.Context, input *Input) (bool, error) {
	mode := a.settings.PredicateMode
	if mode == "" {
		mode = "and"
	}

	var reasons []string
	overallPassed := (mode == "and") // AND starts true; OR starts false

	for i, cp := range a.predicates {
		rawField, exists := input.Message[cp.p.Field]
		if !exists {
			if a.settings.PassThroughOnMissing {
				if mode == "or" {
					overallPassed = true
					break
				}
				continue // treat missing field as pass for AND
			}
			r := fmt.Sprintf("predicate[%d]: field %q not found", i, cp.p.Field)
			if mode == "and" {
				overallPassed = false
				reasons = append(reasons, r)
				break
			}
			reasons = append(reasons, r)
			continue
		}

		passed, reason, evalErr := evaluatePredicate(rawField, cp.p.Field, cp.p.Operator, cp.p.Value, cp.compiled)
		if evalErr != nil {
			errMsg := fmt.Sprintf("predicate[%d] evaluation error: %v", i, evalErr)
			a.logger.Errorf("kafka-stream/filter: %s", errMsg)
			return a.writeOutput(ctx, false, nil, "", errMsg)
		}

		if mode == "and" {
			if !passed {
				overallPassed = false
				reasons = append(reasons, fmt.Sprintf("predicate[%d] field=%q: %s", i, cp.p.Field, reason))
				break // short-circuit AND
			}
		} else { // "or"
			if passed {
				overallPassed = true
				break // short-circuit OR
			}
			reasons = append(reasons, fmt.Sprintf("predicate[%d] field=%q: %s", i, cp.p.Field, reason))
		}
	}

	var passMsg map[string]interface{}
	reasonStr := ""
	if overallPassed {
		passMsg = input.Message
	} else {
		reasonStr = strings.Join(reasons, "; ")
	}
	return a.writeOutput(ctx, overallPassed, passMsg, reasonStr, "")
}

// ---------------------------------------------------------------------------
// Core evaluation logic (shared)
// ---------------------------------------------------------------------------

// evaluatePredicate applies operator to rawField and compareVal.
// compiled may be nil for non-regex operators.
func evaluatePredicate(rawField interface{}, fieldName, op, compareVal string, compiled *regexp.Regexp) (bool, string, error) {
	// --- String-specific operators ---
	switch op {
	case "contains", "startsWith", "endsWith", "regex":
		fieldStr, err := coerce.ToString(rawField)
		if err != nil {
			return false, "", fmt.Errorf("field %q cannot be coerced to string: %w", fieldName, err)
		}
		switch op {
		case "contains":
			if strings.Contains(fieldStr, compareVal) {
				return true, "", nil
			}
			return false, fmt.Sprintf("value %q does not contain %q", fieldStr, compareVal), nil
		case "startsWith":
			if strings.HasPrefix(fieldStr, compareVal) {
				return true, "", nil
			}
			return false, fmt.Sprintf("value %q does not start with %q", fieldStr, compareVal), nil
		case "endsWith":
			if strings.HasSuffix(fieldStr, compareVal) {
				return true, "", nil
			}
			return false, fmt.Sprintf("value %q does not end with %q", fieldStr, compareVal), nil
		case "regex":
			if compiled.MatchString(fieldStr) {
				return true, "", nil
			}
			return false, fmt.Sprintf("value %q does not match regex %q", fieldStr, compareVal), nil
		}
	}

	// --- Numeric comparison operators ---
	compareNum, numErr := strconv.ParseFloat(compareVal, 64)
	fieldNum, fieldNumErr := coerce.ToFloat64(rawField)

	if numErr == nil && fieldNumErr == nil {
		switch op {
		case "eq":
			if fieldNum == compareNum {
				return true, "", nil
			}
			return false, fmt.Sprintf("%.4f != %.4f", fieldNum, compareNum), nil
		case "neq":
			if fieldNum != compareNum {
				return true, "", nil
			}
			return false, fmt.Sprintf("%.4f == %.4f (values are equal)", fieldNum, compareNum), nil
		case "gt":
			if fieldNum > compareNum {
				return true, "", nil
			}
			return false, fmt.Sprintf("%.4f is not > %.4f", fieldNum, compareNum), nil
		case "gte":
			if fieldNum >= compareNum {
				return true, "", nil
			}
			return false, fmt.Sprintf("%.4f is not >= %.4f", fieldNum, compareNum), nil
		case "lt":
			if fieldNum < compareNum {
				return true, "", nil
			}
			return false, fmt.Sprintf("%.4f is not < %.4f", fieldNum, compareNum), nil
		case "lte":
			if fieldNum <= compareNum {
				return true, "", nil
			}
			return false, fmt.Sprintf("%.4f is not <= %.4f", fieldNum, compareNum), nil
		}
	}

	// --- String fallback for eq / neq ---
	fieldStr, err := coerce.ToString(rawField)
	if err != nil {
		return false, "", fmt.Errorf("field %q cannot be coerced to string for comparison: %w", fieldName, err)
	}
	switch op {
	case "eq":
		if fieldStr == compareVal {
			return true, "", nil
		}
		return false, fmt.Sprintf("%q != %q", fieldStr, compareVal), nil
	case "neq":
		if fieldStr != compareVal {
			return true, "", nil
		}
		return false, fmt.Sprintf("%q == %q (values are equal)", fieldStr, compareVal), nil
	}

	return false, "", fmt.Errorf("operator %q requires numeric operands; field %q value %q is not numeric", op, fieldName, compareVal)
}

func (a *Activity) writeOutput(ctx activity.Context, passed bool, msg map[string]interface{}, reason, errorMessage string) (bool, error) {
	if errorMessage != "" {
		a.logger.Errorf("Message routing to DLQ: %s", errorMessage)
	} else if !passed && reason != "" {
		a.logger.Debugf("Message filtered out: %s", reason)
	}
	output := &Output{
		Passed:       passed,
		Message:      msg,
		Reason:       reason,
		ErrorMessage: errorMessage,
	}
	if err := ctx.SetOutputObject(output); err != nil {
		return false, fmt.Errorf("kafka-stream/filter: failed to set output: %w", err)
	}
	return true, nil
}

func validOperators() map[string]bool {
	return map[string]bool{
		"eq": true, "neq": true,
		"gt": true, "gte": true, "lt": true, "lte": true,
		"contains": true, "startsWith": true, "endsWith": true,
		"regex": true,
	}
}
