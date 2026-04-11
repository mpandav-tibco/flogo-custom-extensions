package filter

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
	"golang.org/x/time/rate"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() {
	_ = activity.Register(&Activity{}, New)
}

// Activity implements the Kafka Stream Filter activity.
// It evaluates a predicate (or a chain of predicates) against the incoming
// message and sets passed=true/false so the flow can branch accordingly.
type Activity struct {
	settings *Settings
	logger   log.Logger
	dedup    *dedupStore   // nil when dedup disabled
	limiter  *rate.Limiter // nil when rate limiting disabled
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
func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("kafka-stream/filter: failed to map settings: %w", err)
	}

	logger := ctx.Logger()
	act := &Activity{
		settings: s,
		logger:   logger,
	}

	// --- Deduplication store (opt-in) ---
	if s.EnableDedup {
		window := 10 * time.Minute
		if s.DedupWindow != "" {
			d, err := time.ParseDuration(s.DedupWindow)
			if err != nil {
				return nil, fmt.Errorf("kafka-stream/filter: invalid dedupWindow %q: %w", s.DedupWindow, err)
			}
			window = d
		}
		maxEntries := int64(100_000)
		if s.DedupMaxEntries > 0 {
			maxEntries = s.DedupMaxEntries
		}
		act.dedup = newDedupStore(window, maxEntries)
		logger.Infof("kafka-stream/filter: dedup enabled — window=%s maxEntries=%d", window, maxEntries)
	}

	// --- Rate limiter (opt-in) ---
	if s.RateLimitRPS > 0 {
		burst := s.RateLimitBurst
		if burst <= 0 {
			burst = int(s.RateLimitRPS) // default burst = 1 second worth
			if burst < 1 {
				burst = 1
			}
		}
		act.limiter = rate.NewLimiter(rate.Limit(s.RateLimitRPS), burst)
		mode := s.RateLimitMode
		if mode == "" {
			mode = "drop"
		}
		logger.Infof("kafka-stream/filter: rate limit enabled — rps=%.1f burst=%d mode=%s", s.RateLimitRPS, burst, mode)
	}

	logger.Infof("Kafka Stream Filter initialised: defaultOperator=%q defaultPredicateMode=%q passThroughOnMissing=%v",
		s.Operator, s.PredicateMode, s.PassThroughOnMissing)
	return act, nil
}

// Eval evaluates the configured predicate(s) against the incoming message.
// Input values for operator and predicateMode override the settings defaults
// when non-empty; otherwise the settings defaults are used.
func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	start := time.Now()

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("kafka-stream/filter: failed to get input: %w", err)
	}

	// Apply settings defaults when not overridden at runtime.
	if input.Operator == "" {
		input.Operator = a.settings.Operator
	}
	if input.PredicateMode == "" {
		input.PredicateMode = a.settings.PredicateMode
	}

	mode := "single"
	if input.PredicatesJSON != "" {
		mode = "multi"
	}

	if a.logger.DebugEnabled() {
		a.logger.Debugf("kafka-stream/filter: eval start — mode=%s field=%q op=%q value=%q predicateMode=%q msgKeys=%s",
			mode, input.Field, input.Operator, input.Value, input.PredicateMode, sortedKeys(input.Message))
	}

	// --- Rate limit check (before predicate evaluation) ---
	if a.limiter != nil {
		allowed, waitMs := a.checkRateLimit()
		if !allowed {
			if a.logger.DebugEnabled() {
				a.logger.Debugf("kafka-stream/filter: RATE_LIMITED — dropped after %.1fms wait", waitMs)
			}
			elapsed := float64(time.Since(start).Microseconds()) / 1000.0
			a.setTracingTags(ctx, mode, input, "rate_limited", elapsed)
			return a.writeOutput(ctx, false, input.Message, "rate_limited", "")
		}
		if waitMs > 0 && a.logger.DebugEnabled() {
			a.logger.Debugf("kafka-stream/filter: rate limit wait=%.1fms", waitMs)
		}
	}

	// --- Deduplication check (before predicate evaluation) ---
	// input.DedupField carries the resolved event ID value (e.g. mapped from
	// =$flow.jsonValue.device_id), not a field name.
	if a.dedup != nil && input.DedupField != "" {
		if a.dedup.isDuplicate(input.DedupField) {
			if a.logger.DebugEnabled() {
				a.logger.Debugf("kafka-stream/filter: DUPLICATE — id=%q", input.DedupField)
			}
			elapsed := float64(time.Since(start).Microseconds()) / 1000.0
			a.setTracingTags(ctx, mode, input, "duplicate", elapsed)
			return a.writeOutput(ctx, false, input.Message, "duplicate", "")
		}
	}

	// Run pure evaluation logic.
	var passed bool
	var reason, errMsg string
	if mode == "multi" {
		passed, reason, errMsg = a.runMulti(input)
	} else {
		passed, reason, errMsg = a.runSingle(input)
	}

	elapsed := float64(time.Since(start).Microseconds()) / 1000.0

	resultLabel := "blocked"
	if errMsg != "" {
		resultLabel = "error"
	} else if passed {
		resultLabel = "passed"
	}
	a.setTracingTags(ctx, mode, input, resultLabel, elapsed)

	if a.logger.DebugEnabled() {
		a.logger.Debugf("kafka-stream/filter: eval done — result=%s mode=%s elapsed=%.3fms", resultLabel, mode, elapsed)
	}

	return a.writeOutput(ctx, passed, input.Message, reason, errMsg)
}

// setTracingTags attaches filter-specific tags to the engine-managed span.
func (a *Activity) setTracingTags(ctx activity.Context, mode string, input *Input, result string, elapsedMs float64) {
	if tc := ctx.GetTracingContext(); tc != nil {
		tc.SetTag("filter.mode", mode)
		tc.SetTag("filter.field", input.Field)
		tc.SetTag("filter.operator", input.Operator)
		tc.SetTag("filter.value", input.Value)
		tc.SetTag("filter.result", result)
		tc.SetTag("filter.eval_duration_ms", elapsedMs)
	}
}

// checkRateLimit applies the token bucket. Returns (allowed, waitMs).
// "drop" mode: returns immediately false if no token; never waits.
// "wait" mode: blocks up to RateLimitMaxWaitMs for a token.
func (a *Activity) checkRateLimit() (allowed bool, waitMs float64) {
	mode := a.settings.RateLimitMode
	if mode == "" {
		mode = "drop"
	}
	if mode == "wait" {
		maxWait := time.Duration(a.settings.RateLimitMaxWaitMs) * time.Millisecond
		if maxWait <= 0 {
			maxWait = 500 * time.Millisecond
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), maxWait)
		defer cancel()
		waitStart := time.Now()
		if err := a.limiter.Wait(waitCtx); err != nil {
			// Timeout expired — drop
			return false, float64(time.Since(waitStart).Microseconds()) / 1000.0
		}
		return true, float64(time.Since(waitStart).Microseconds()) / 1000.0
	}
	// "drop" mode
	return a.limiter.Allow(), 0
}

// ---------------------------------------------------------------------------
// Single-predicate path (pure logic — no IO)
// ---------------------------------------------------------------------------

func (a *Activity) runSingle(input *Input) (passed bool, reason, errMsg string) {
	field := input.Field
	if field == "" {
		return false, "", "filter not configured: set 'field' for single-predicate mode or 'predicates' for multi-predicate mode"
	}
	operator := input.Operator
	if operator == "" || !validOperators()[operator] {
		return false, "", fmt.Sprintf("unsupported or missing operator %q", operator)
	}

	rawField, exists := input.Message[field]
	if !exists {
		if a.settings.PassThroughOnMissing {
			if a.logger.DebugEnabled() {
				a.logger.Debugf("kafka-stream/filter: field=%q absent — passThroughOnMissing=true → passing through", field)
			}
			return true, "", ""
		}
		msg := fmt.Sprintf("field %q not found in message", field)
		if a.logger.DebugEnabled() {
			a.logger.Debugf("kafka-stream/filter: %s — blocking", msg)
		}
		return false, msg, ""
	}

	var compiled *regexp.Regexp
	if operator == "regex" {
		var compErr error
		compiled, compErr = regexp.Compile(input.Value)
		if compErr != nil {
			return false, "", fmt.Sprintf("invalid regex pattern %q: %v", input.Value, compErr)
		}
	}

	if a.logger.DebugEnabled() {
		a.logger.Debugf("kafka-stream/filter: evaluating field=%q fieldValue=%v op=%q against=%q",
			field, rawField, operator, input.Value)
	}

	ok, r, evalErr := evaluatePredicate(rawField, field, operator, input.Value, compiled)
	if evalErr != nil {
		return false, "", fmt.Sprintf("evaluation error: %v", evalErr)
	}

	if a.logger.DebugEnabled() {
		if ok {
			a.logger.Debugf("kafka-stream/filter: PASSED — field=%q value=%v %s %q", field, rawField, operator, input.Value)
		} else {
			a.logger.Debugf("kafka-stream/filter: BLOCKED — %s", r)
		}
	}
	return ok, r, ""
}

// ---------------------------------------------------------------------------
// Multi-predicate path (pure logic — no IO)
// ---------------------------------------------------------------------------

func (a *Activity) runMulti(input *Input) (passed bool, reason, errMsg string) {
	preds, err := input.ParsedPredicates()
	if err != nil {
		return false, "", fmt.Sprintf("invalid predicates JSON: %v", err)
	}
	validOps := validOperators()
	compiled := make([]compiledPred, 0, len(preds))
	for i, p := range preds {
		if !validOps[p.Operator] {
			return false, "", fmt.Sprintf("predicate[%d] unsupported operator %q", i, p.Operator)
		}
		cp := compiledPred{p: p}
		if p.Operator == "regex" {
			re, reErr := regexp.Compile(p.Value)
			if reErr != nil {
				return false, "", fmt.Sprintf("predicate[%d] invalid regex %q: %v", i, p.Value, reErr)
			}
			cp.compiled = re
		}
		compiled = append(compiled, cp)
	}

	mode := input.PredicateMode
	if mode == "" {
		mode = "and"
	}

	debugEnabled := a.logger.DebugEnabled()
	if debugEnabled {
		a.logger.Debugf("kafka-stream/filter: multi-predicate — count=%d mode=%s", len(compiled), mode)
	}

	var reasons []string
	overallPassed := (mode == "and") // AND starts true; OR starts false

	for i, cp := range compiled {
		rawField, exists := input.Message[cp.p.Field]
		if !exists {
			if a.settings.PassThroughOnMissing {
				if debugEnabled {
					a.logger.Debugf("kafka-stream/filter: predicate[%d] field=%q absent — passThroughOnMissing=true", i, cp.p.Field)
				}
				if mode == "or" {
					overallPassed = true
					break
				}
				continue
			}
			r := fmt.Sprintf("predicate[%d]: field %q not found", i, cp.p.Field)
			if debugEnabled {
				a.logger.Debugf("kafka-stream/filter: predicate[%d] field=%q absent — %s short-circuits", i, cp.p.Field, mode)
			}
			if mode == "and" {
				overallPassed = false
				reasons = append(reasons, r)
				break
			}
			reasons = append(reasons, r)
			continue
		}

		ok, r, evalErr := evaluatePredicate(rawField, cp.p.Field, cp.p.Operator, cp.p.Value, cp.compiled)
		if evalErr != nil {
			em := fmt.Sprintf("predicate[%d] evaluation error: %v", i, evalErr)
			a.logger.Errorf("kafka-stream/filter: %s", em)
			return false, "", em
		}

		if debugEnabled {
			a.logger.Debugf("kafka-stream/filter: predicate[%d] field=%q value=%v op=%q against=%q → passed=%v",
				i, cp.p.Field, rawField, cp.p.Operator, cp.p.Value, ok)
		}

		if mode == "and" {
			if !ok {
				overallPassed = false
				reasons = append(reasons, fmt.Sprintf("predicate[%d] field=%q: %s", i, cp.p.Field, r))
				break // short-circuit AND
			}
		} else { // "or"
			if ok {
				overallPassed = true
				break // short-circuit OR
			}
			reasons = append(reasons, fmt.Sprintf("predicate[%d] field=%q: %s", i, cp.p.Field, r))
		}
	}

	reasonStr := ""
	if !overallPassed {
		reasonStr = strings.Join(reasons, "; ")
	}
	return overallPassed, reasonStr, ""
}

// ---------------------------------------------------------------------------
// Output writing
// ---------------------------------------------------------------------------

func (a *Activity) writeOutput(ctx activity.Context, passed bool, msg map[string]interface{}, reason, errorMessage string) (bool, error) {
	switch {
	case errorMessage != "":
		a.logger.Errorf("kafka-stream/filter: routing to DLQ — %s", errorMessage)
	case passed:
		if a.logger.DebugEnabled() {
			a.logger.Debugf("kafka-stream/filter: message PASSED — forwarding downstream")
		}
	default:
		if a.logger.DebugEnabled() {
			a.logger.Debugf("kafka-stream/filter: message BLOCKED — reason: %s", reason)
		}
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

// ---------------------------------------------------------------------------
// Core evaluation logic (pure functions — shared by single and multi paths)
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

	return false, "", fmt.Errorf("operator %q requires numeric operands; field %q value %q is not numeric", op, fieldName, fmt.Sprintf("%v", rawField))
}

// validOperators returns the set of supported operator names.
func validOperators() map[string]bool {
	return map[string]bool{
		"eq": true, "neq": true,
		"gt": true, "gte": true, "lt": true, "lte": true,
		"contains": true, "startsWith": true, "endsWith": true,
		"regex": true,
	}
}

// ---------------------------------------------------------------------------
// Deduplication store
// ---------------------------------------------------------------------------

// dedupEntry holds the expiry time for a seen event ID.
type dedupEntry struct {
	expiresAt time.Time
}

// dedupStore is an in-memory TTL map for deduplication.
// A background goroutine periodically evicts expired entries.
type dedupStore struct {
	mu         sync.Mutex
	seen       map[string]dedupEntry
	window     time.Duration
	maxEntries int64
}

func newDedupStore(window time.Duration, maxEntries int64) *dedupStore {
	ds := &dedupStore{
		seen:       make(map[string]dedupEntry),
		window:     window,
		maxEntries: maxEntries,
	}
	// Background eviction every window/4, minimum 30s
	evictInterval := window / 4
	if evictInterval < 30*time.Second {
		evictInterval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(evictInterval)
		defer ticker.Stop()
		for range ticker.C {
			ds.evict()
		}
	}()
	return ds
}

// isDuplicate returns true if id was seen within the dedup window.
// First-time IDs are recorded and return false. Expired entries are replaced.
func (ds *dedupStore) isDuplicate(id string) bool {
	now := time.Now()
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if entry, exists := ds.seen[id]; exists && now.Before(entry.expiresAt) {
		return true
	}
	// Enforce max entries — evict oldest on overflow rather than rejecting.
	if ds.maxEntries > 0 && int64(len(ds.seen)) >= ds.maxEntries {
		ds.evictOldestLocked(now)
	}
	ds.seen[id] = dedupEntry{expiresAt: now.Add(ds.window)}
	return false
}

// evict removes all expired entries (called by background goroutine).
func (ds *dedupStore) evict() {
	now := time.Now()
	ds.mu.Lock()
	defer ds.mu.Unlock()
	for id, e := range ds.seen {
		if now.After(e.expiresAt) {
			delete(ds.seen, id)
		}
	}
}

// evictOldestLocked removes the single oldest entry. Caller must hold mu.
func (ds *dedupStore) evictOldestLocked(now time.Time) {
	var oldestID string
	var oldestExp time.Time
	for id, e := range ds.seen {
		if oldestID == "" || e.expiresAt.Before(oldestExp) {
			oldestID = id
			oldestExp = e.expiresAt
		}
	}
	if oldestID != "" {
		delete(ds.seen, oldestID)
	}
}

// size returns the current number of tracked IDs (test helper).
func (ds *dedupStore) size() int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return len(ds.seen)
}

// ---------------------------------------------------------------------------

// sortedKeys returns the message map keys as a sorted bracketed string for debug logs.
func sortedKeys(m map[string]interface{}) string {
	if len(m) == 0 {
		return "[]"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return "[" + strings.Join(keys, ",") + "]"
}
