// Package filter provides a Flogo trigger that consumes messages from a Kafka
// topic and fires the associated flow only for messages that satisfy the
// configured predicate(s).  It is the trigger-native equivalent of the
// kafka-stream-filter activity, with the Kafka transport built in.
package filter

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/trace"
	"github.com/project-flogo/core/trigger"
	kafkaconn "github.com/tibco/wi-plugins/contributions/kafka/src/app/Kafka/connector/kafka"
	"golang.org/x/time/rate"
)

var triggerMd = trigger.NewMetadata(&Settings{}, &HandlerSettings{}, &Output{})

func init() {
	_ = trigger.Register(&Trigger{}, &Factory{})
}

// Trigger is the Kafka Stream Filter Trigger.
// It owns a Kafka consumer group and fires the attached flow for every message
// that passes the configured filter predicate(s).
type Trigger struct {
	settings *Settings
	logger   log.Logger
	handlers []*handler
	client   sarama.ConsumerGroup
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// Shared dedup store and rate-limiter (trigger-level, not per-handler).
	dedup    *dedupStore
	limiter  *rate.Limiter
	msgCount atomic.Int64 // for periodic dedup state persistence
}

// handler pairs a Flogo flow runner with its filter HandlerSettings.
type handler struct {
	runner      trigger.Handler
	hs          *HandlerSettings
	eventType   string                 // resolved from hs.EventType; default EventTypePass
	singleRegex *regexp.Regexp         // pre-compiled regex for single-predicate mode
	multiRegex  map[int]*regexp.Regexp // pre-compiled regexes for multi-predicate mode (index → compiled)
	parsedPreds []Predicate            // cached parsed predicates; avoids JSON unmarshal per message
}

// Factory creates Trigger instances.
type Factory struct{}

// Metadata returns the trigger factory metadata.
func (*Factory) Metadata() *trigger.Metadata { return triggerMd }

// New creates a new, uninitialised Trigger from the design-time config.
func (*Factory) New(config *trigger.Config) (trigger.Trigger, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(config.Settings, s, true); err != nil {
		return nil, fmt.Errorf("kafka-stream/filter-trigger: failed to map settings: %w", err)
	}
	var err error
	s.Connection, err = kafkaconn.GetSharedConfiguration(config.Settings["kafkaConnection"])
	if err != nil {
		return nil, fmt.Errorf("kafka-stream/filter-trigger: failed to resolve kafkaConnection: %w", err)
	}
	if err := validateSettings(s); err != nil {
		return nil, fmt.Errorf("kafka-stream/filter-trigger: invalid settings: %w", err)
	}
	return &Trigger{settings: s}, nil
}

// Metadata returns the trigger metadata.
func (t *Trigger) Metadata() *trigger.Metadata { return triggerMd }

// Initialize wires up handlers, creates the Kafka consumer group client, and
// initialises optional dedup/rate-limit state.
func (t *Trigger) Initialize(ctx trigger.InitContext) error {
	t.logger = ctx.Logger()

	for _, h := range ctx.GetHandlers() {
		hs := &HandlerSettings{}
		if err := metadata.MapToStruct(h.Settings(), hs, true); err != nil {
			return fmt.Errorf("kafka-stream/filter-trigger: failed to map handler settings: %w", err)
		}
		et := hs.EventType
		if et == "" {
			et = EventTypePass
		}
		if hs.PredicateMode != "" && strings.ToLower(hs.PredicateMode) != "and" && strings.ToLower(hs.PredicateMode) != "or" {
			return fmt.Errorf("kafka-stream/filter-trigger: handler predicateMode %q is invalid (accepted: \"and\", \"or\")", hs.PredicateMode)
		}
		h := &handler{runner: h, hs: hs, eventType: et}
		// Pre-compile regex patterns so they are not recompiled on every message.
		if hs.Predicates != "" {
			preds, err := hs.ParsedPredicates()
			if err != nil {
				return fmt.Errorf("kafka-stream/filter-trigger: invalid predicates JSON in handler: %w", err)
			}
			// Validate operators at startup so bad configs surface immediately
			// rather than at runtime when the first matching message arrives.
			ops := validOps()
			for i, p := range preds {
				if !ops[p.Operator] {
					return fmt.Errorf("kafka-stream/filter-trigger: handler predicate[%d]: unsupported operator %q", i, p.Operator)
				}
			}
			// Cache to avoid JSON unmarshal on every message.
			h.parsedPreds = preds
			h.multiRegex = make(map[int]*regexp.Regexp)
			for i, p := range preds {
				if p.Operator == "regex" {
					compiled, err := regexp.Compile(p.Value)
					if err != nil {
						return fmt.Errorf("kafka-stream/filter-trigger: handler predicate[%d] invalid regex %q: %w", i, p.Value, err)
					}
					h.multiRegex[i] = compiled
				}
			}
		} else if hs.Operator == "regex" && hs.Value != "" {
			compiled, err := regexp.Compile(hs.Value)
			if err != nil {
				return fmt.Errorf("kafka-stream/filter-trigger: handler regex %q: %w", hs.Value, err)
			}
			h.singleRegex = compiled
		} else if hs.Field != "" {
			// Single-predicate mode: validate the resolved operator at startup.
			resolvedOp := hs.Operator
			if resolvedOp == "" {
				resolvedOp = t.settings.Operator
			}
			if resolvedOp == "" || !validOps()[resolvedOp] {
				return fmt.Errorf("kafka-stream/filter-trigger: handler single-predicate field=%q has unsupported or missing operator %q", hs.Field, resolvedOp)
			}
		}
		t.handlers = append(t.handlers, h)
	}

	ksc := t.settings.Connection.(*kafkaconn.KafkaSharedConfigManager)
	clientCfg := ksc.GetClientConfiguration()
	saramaConfig := clientCfg.CreateConsumerConfig()
	saramaConfig.Consumer.Return.Errors = true
	// Disable Sarama's background auto-commit so that session.MarkMessage is the
	// sole mechanism for committing offsets. Without this, the auto-commit ticker
	// would commit the high-water mark of all fetched messages on its own schedule,
	// bypassing the commitOnSuccess gate and silently breaking at-least-once semantics.
	saramaConfig.Consumer.Offsets.AutoCommit.Enable = false
	saramaConfig.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
		resolveBalanceStrategy(t.settings.BalanceStrategy),
	}
	switch strings.ToLower(t.settings.InitialOffset) {
	case "oldest":
		saramaConfig.Consumer.Offsets.Initial = sarama.OffsetOldest
	default:
		saramaConfig.Consumer.Offsets.Initial = sarama.OffsetNewest
	}
	brokers := clientCfg.Brokers
	var err error
	t.client, err = sarama.NewConsumerGroup(brokers, t.settings.ConsumerGroup, saramaConfig)
	if err != nil {
		return fmt.Errorf("kafka-stream/filter-trigger: failed to create consumer group [brokers=%v group=%q]: %w",
			brokers, t.settings.ConsumerGroup, err)
	}

	// Optional deduplication store.
	if t.settings.EnableDedup {
		window := 10 * time.Minute
		if t.settings.DedupWindow != "" {
			d, err := time.ParseDuration(t.settings.DedupWindow)
			if err != nil {
				return fmt.Errorf("kafka-stream/filter-trigger: invalid dedupWindow %q: %w", t.settings.DedupWindow, err)
			}
			window = d
		}
		maxEntries := int64(100_000)
		if t.settings.DedupMaxEntries > 0 {
			maxEntries = t.settings.DedupMaxEntries
		}
		t.dedup = newDedupStore(window, maxEntries)
		// Restore persisted dedup state so duplicates from before the last
		// restart are still suppressed within the dedup window.
		if t.settings.DedupPersistPath != "" {
			if err := t.dedup.loadFromFile(t.settings.DedupPersistPath); err != nil {
				t.logger.Warnf("kafka-stream/filter-trigger: dedup state restore from %q failed (ignored): %v", t.settings.DedupPersistPath, err)
			} else {
				t.logger.Infof("kafka-stream/filter-trigger: dedup state restored from %q", t.settings.DedupPersistPath)
			}
		}
		t.logger.Infof("kafka-stream/filter-trigger: dedup enabled — window=%s maxEntries=%d persistPath=%q", window, maxEntries, t.settings.DedupPersistPath)
	}

	// Optional rate limiter.
	if t.settings.RateLimitRPS > 0 {
		burst := t.settings.RateLimitBurst
		if burst <= 0 {
			burst = int(t.settings.RateLimitRPS)
			if burst < 1 {
				burst = 1
			}
		}
		t.limiter = rate.NewLimiter(rate.Limit(t.settings.RateLimitRPS), burst)
		t.logger.Infof("kafka-stream/filter-trigger: rate-limit enabled — rps=%.1f burst=%d mode=%s",
			t.settings.RateLimitRPS, burst, t.settings.RateLimitMode)
	}

	t.logger.Infof("kafka-stream/filter-trigger: initialised — brokers=%v topic=%q group=%q handlers=%d",
		brokers, t.settings.Topic, t.settings.ConsumerGroup, len(t.handlers))
	if t.settings.HandlerTimeoutMs <= 0 {
		t.logger.Warnf("kafka-stream/filter-trigger: handlerTimeoutMs not set — handlers can block indefinitely; with AutoCommit disabled this will hold the partition claim until session rebalance fires")
	}
	return nil
}

// Start launches the Kafka consumer goroutine.
func (t *Trigger) Start() error {
	t.ctx, t.cancel = context.WithCancel(context.Background())
	t.wg.Add(1)
	go t.consumeLoop()
	t.logger.Infof("kafka-stream/filter-trigger: started — topic=%q", t.settings.Topic)
	return nil
}

// Stop signals the consumer loop to stop and waits for the goroutine to finish.
func (t *Trigger) Stop() error {
	t.cancel()
	t.wg.Wait()
	// Stop the dedup eviction goroutine before persisting state.
	if t.dedup != nil {
		t.dedup.stop()
	}
	// Persist dedup state on graceful shutdown.
	if t.dedup != nil && t.settings.DedupPersistPath != "" {
		if err := t.dedup.saveToFile(t.settings.DedupPersistPath); err != nil {
			t.logger.Warnf("kafka-stream/filter-trigger: dedup state save on stop failed: %v", err)
		}
	}
	if err := t.client.Close(); err != nil {
		t.logger.Warnf("kafka-stream/filter-trigger: consumer group close error: %v", err)
	}
	t.logger.Infof("kafka-stream/filter-trigger: stopped — topic=%q", t.settings.Topic)
	return nil
}

// consumeLoop drives the Kafka consumer group session, retrying on non-fatal errors.
func (t *Trigger) consumeLoop() {
	defer t.wg.Done()
	cgh := &consumerGroupHandler{t: t}
	topics := []string{t.settings.Topic}
	for {
		if err := t.client.Consume(t.ctx, topics, cgh); err != nil {
			if t.ctx.Err() != nil {
				return // normal shutdown
			}
			t.logger.Errorf("kafka-stream/filter-trigger: consumer error (retrying in 5s): %v", err)
			select {
			case <-time.After(5 * time.Second):
			case <-t.ctx.Done():
				return
			}
		}
		if t.ctx.Err() != nil {
			return
		}
	}
}

// handleMessage decodes a raw Kafka message, evaluates the filter predicate for
// each handler, and fires the Flogo flow for handlers whose predicate passes.
// Handlers with eventType=evalError (or all) receive messages that fail
// predicate evaluation so they can be routed to a dead-letter flow.
func (t *Trigger) handleMessage(session sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage) {
	if t.logger.DebugEnabled() {
		t.logger.Debugf("kafka-stream/filter-trigger: record received — topic=%s partition=%d offset=%d key=%q len=%d",
			msg.Topic, msg.Partition, msg.Offset, string(msg.Key), len(msg.Value))
	}

	// Build context: extract OTel trace propagation headers from the Kafka message (OOTB pattern).
	eventId := fmt.Sprintf("%s#%d#%d", msg.Topic, msg.Partition, msg.Offset)
	ctx := context.Background()
	if trace.Enabled() {
		tracingHeader := make(map[string]string)
		for _, h := range msg.Headers {
			tracingHeader[string(h.Key)] = string(h.Value)
		}
		if tc, _ := trace.GetTracer().Extract(trace.TextMap, tracingHeader); tc != nil {
			ctx = trace.AppendTracingContext(ctx, tc)
		}
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		// Hard decode failure — this is a poison-pill message (malformed JSON that
		// can never be successfully processed). We always mark the offset regardless
		// of commitOnSuccess so the consumer does not get stuck retrying a message
		// it can never parse. Route via a DLQ or schema-enforcement upstream.
		t.logger.Errorf("kafka-stream/filter-trigger: cannot decode JSON offset=%d partition=%d — skipping (poison-pill): %v",
			msg.Offset, msg.Partition, err)
		session.MarkMessage(msg, "")
		return
	}

	// Trigger-level rate limit applies before per-handler evaluation.
	if t.limiter != nil && !t.checkRateLimit() {
		// The offset is permanently advanced: a dropped message is NOT redelivered
		// even when commitOnSuccess=true. Rate-limit drops are best-effort
		// (at-most-once) by design — use rateLimitMode=wait for at-least-once needs.
		t.logger.Warnf("kafka-stream/filter-trigger: RATE_LIMITED offset=%d — offset marked permanently (not covered by commitOnSuccess)", msg.Offset)
		session.MarkMessage(msg, "")
		return
	}

	msgKey := string(msg.Key)

	// handlerFailed tracks whether any handler that fired returned an error.
	// Used by the commitOnSuccess gate at the end: when true the Kafka offset will
	// not be marked (at-least-once redelivery on restart/rebalance).
	handlerFailed := false

	for _, h := range t.handlers {
		passed, reason, evalErr := t.evaluate(payload, h)

		if evalErr != "" {
			// Route to evalError or all handlers; log at WARN when no handler is configured.
			if h.eventType == EventTypeEvalError || h.eventType == EventTypeAll {
				t.logger.Warnf("kafka-stream/filter-trigger: eval error offset=%d — routing to evalError handler: %s", msg.Offset, evalErr)
				out := &Output{
					Message:         payload,
					Topic:           msg.Topic,
					Partition:       int64(msg.Partition),
					Offset:          msg.Offset,
					Key:             msgKey,
					EvalError:       true,
					EvalErrorReason: evalErr,
				}
				hCtx, cancel := t.handlerContext(ctx)
				_, err := h.runner.Handle(trigger.NewContextWithEventId(hCtx, eventId), out.ToMap())
				cancel()
				if err != nil {
					t.logger.Errorf("kafka-stream/filter-trigger: evalError handler fire error offset=%d: %v", msg.Offset, err)
					handlerFailed = true
				}
			} else {
				t.logger.Warnf("kafka-stream/filter-trigger: eval error offset=%d: %s — skipping (configure an evalError handler for DLQ routing)", msg.Offset, evalErr)
			}
			continue
		}

		if !passed {
			if t.logger.DebugEnabled() {
				t.logger.Debugf("kafka-stream/filter-trigger: BLOCKED offset=%d reason=%q", msg.Offset, reason)
			}
			continue
		}

		// Message passes — skip handlers that only want eval errors.
		if h.eventType == EventTypeEvalError {
			continue
		}

		// Per-handler dedup check (applied only for pass events).
		if t.dedup != nil && h.hs.DedupField != "" {
			dedupID := ""
			if raw, ok := payload[h.hs.DedupField]; ok {
				dedupID, _ = coerce.ToString(raw)
			}
			if dedupID != "" && t.dedup.isDuplicate(dedupID) {
				if t.logger.DebugEnabled() {
					t.logger.Debugf("kafka-stream/filter-trigger: DUPLICATE id=%q offset=%d — skipping handler", dedupID, msg.Offset)
				}
				continue
			}
		}

		out := &Output{
			Message:   payload,
			Topic:     msg.Topic,
			Partition: int64(msg.Partition),
			Offset:    msg.Offset,
			Key:       msgKey,
		}
		hCtx, cancel := t.handlerContext(ctx)
		_, err := h.runner.Handle(trigger.NewContextWithEventId(hCtx, eventId), out.ToMap())
		cancel()
		if err != nil {
			t.logger.Errorf("kafka-stream/filter-trigger: handler fire error offset=%d: %v", msg.Offset, err)
			handlerFailed = true
		} else {
			t.logger.Infof("kafka-stream/filter-trigger: record from topic=%s partition=%d offset=%d successfully processed",
				msg.Topic, msg.Partition, msg.Offset)
		}
	}

	// Periodic dedup state persistence.
	t.maybePersistDedup()
	// Commit semantics: only mark the offset when all handlers that fired
	// succeeded (at-least-once), unless commitOnSuccess=false (at-most-once /
	// backward-compat mode) or no handler failed this message.
	if !t.settings.CommitOnSuccess || !handlerFailed {
		session.MarkMessage(msg, "")
	} else {
		t.logger.Warnf("kafka-stream/filter-trigger: handler failed — not marking offset=%d (message will be redelivered after restart/rebalance)",
			msg.Offset)
	}
}

// ---------------------------------------------------------------------------
// Filter evaluation logic (inline to avoid coupling with the activity package)
// ---------------------------------------------------------------------------

// evaluate runs the predicate(s) from h against payload using trigger-level
// defaults for any fields left empty in h.hs. Pre-compiled regex patterns from
// h.singleRegex / h.multiRegex are used so compilation is not repeated per message.
func (t *Trigger) evaluate(payload map[string]interface{}, h *handler) (passed bool, reason, errMsg string) {
	hs := h.hs
	op := hs.Operator
	if op == "" {
		op = t.settings.Operator
	}
	predMode := hs.PredicateMode
	if predMode == "" {
		predMode = t.settings.PredicateMode
	}
	passThroughOnMissing := t.settings.PassThroughOnMissing

	if hs.Predicates != "" {
		// Use the pre-parsed predicates cached in Initialize() — avoids JSON
		// unmarshal on every message (can be 10 k+ msg/s in production).
		return evalMulti(payload, h.parsedPreds, predMode, passThroughOnMissing, h.multiRegex)
	}
	return evalSingle(payload, hs.Field, op, hs.Value, passThroughOnMissing, h.singleRegex)
}

func evalSingle(msg map[string]interface{}, field, operator, value string, passThroughOnMissing bool, compiled *regexp.Regexp) (bool, string, string) {
	if field == "" {
		return false, "", "filter not configured: set 'field' for single-predicate mode"
	}
	if operator == "" || !validOps()[operator] {
		return false, "", fmt.Sprintf("unsupported or missing operator %q", operator)
	}
	rawField, exists := msg[field]
	if !exists {
		if passThroughOnMissing {
			return true, "", ""
		}
		return false, fmt.Sprintf("field %q not found in message", field), ""
	}
	if operator == "regex" {
		if compiled == nil {
			var err error
			if compiled, err = regexp.Compile(value); err != nil {
				return false, "", fmt.Sprintf("invalid regex %q: %v", value, err)
			}
		}
	}
	ok, r, evalErr := evalPredicate(rawField, field, operator, value, compiled)
	if evalErr != nil {
		return false, "", fmt.Sprintf("evaluation error: %v", evalErr)
	}
	return ok, r, ""
}

func evalMulti(msg map[string]interface{}, preds []Predicate, mode string, passThroughOnMissing bool, compiledRegexes map[int]*regexp.Regexp) (bool, string, string) {
	if len(preds) == 0 {
		return true, "", ""
	}
	// Operator validity is guaranteed by Initialize(); no per-message re-check needed.

	isAnd := strings.ToLower(mode) != "or"
	var reasons []string

	for i, p := range preds {
		rawField, exists := msg[p.Field]
		if !exists {
			if passThroughOnMissing {
				if !isAnd {
					return true, "", "" // OR short-circuit: one pass is enough
				}
				continue // AND: treat missing as pass-through
			}
			r := fmt.Sprintf("field %q not found", p.Field)
			if isAnd {
				return false, r, ""
			}
			reasons = append(reasons, r)
			continue
		}
		var compiled *regexp.Regexp
		if p.Operator == "regex" {
			if compiledRegexes != nil {
				compiled = compiledRegexes[i]
			}
			if compiled == nil {
				var err error
				if compiled, err = regexp.Compile(p.Value); err != nil {
					return false, "", fmt.Sprintf("invalid regex %q: %v", p.Value, err)
				}
			}
		}
		ok, r, evalErr := evalPredicate(rawField, p.Field, p.Operator, p.Value, compiled)
		if evalErr != nil {
			return false, "", fmt.Sprintf("predicate evaluation error: %v", evalErr)
		}
		if isAnd && !ok {
			return false, r, "" // AND short-circuit on first failure
		}
		if !isAnd && ok {
			return true, "", "" // OR short-circuit on first pass
		}
		if !ok {
			reasons = append(reasons, r)
		}
	}

	if isAnd {
		return true, "", "" // all passed
	}
	return false, strings.Join(reasons, "; "), "" // none passed in OR mode
}

// evalPredicate applies a single operator to rawField and compareVal.
// compiled may be non-nil only for the "regex" operator.
func evalPredicate(rawField interface{}, fieldName, op, compareVal string, compiled *regexp.Regexp) (bool, string, error) {
	// String-specific operators.
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
			return false, fmt.Sprintf("%q does not contain %q", fieldStr, compareVal), nil
		case "startsWith":
			if strings.HasPrefix(fieldStr, compareVal) {
				return true, "", nil
			}
			return false, fmt.Sprintf("%q does not start with %q", fieldStr, compareVal), nil
		case "endsWith":
			if strings.HasSuffix(fieldStr, compareVal) {
				return true, "", nil
			}
			return false, fmt.Sprintf("%q does not end with %q", fieldStr, compareVal), nil
		case "regex":
			if compiled.MatchString(fieldStr) {
				return true, "", nil
			}
			return false, fmt.Sprintf("%q does not match regex %q", fieldStr, compareVal), nil
		}
	}

	// Numeric comparison operators — try numeric path first.
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
			return false, fmt.Sprintf("%.4f == %.4f (equal)", fieldNum, compareNum), nil
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

	// String fallback for eq / neq.
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
		return false, fmt.Sprintf("%q == %q (equal)", fieldStr, compareVal), nil
	}
	return false, "", fmt.Errorf("operator %q requires numeric operands; field %q is not numeric", op, fieldName)
}

func validOps() map[string]bool {
	return supportedOps
}

// supportedOps is the set of valid predicate operators. Defined as a package-
// level var so it is allocated once rather than on every message evaluation.
var supportedOps = map[string]bool{
	"eq": true, "neq": true,
	"gt": true, "gte": true, "lt": true, "lte": true,
	"contains": true, "startsWith": true, "endsWith": true,
	"regex": true,
}

// ---------------------------------------------------------------------------
// Rate limiter
// ---------------------------------------------------------------------------

func (t *Trigger) checkRateLimit() bool {
	mode := t.settings.RateLimitMode
	if mode == "" {
		mode = "drop"
	}
	if mode == "wait" {
		maxWait := time.Duration(t.settings.RateLimitMaxWaitMs) * time.Millisecond
		if maxWait <= 0 {
			maxWait = 500 * time.Millisecond
		}
		wCtx, cancel := context.WithTimeout(context.Background(), maxWait)
		defer cancel()
		return t.limiter.Wait(wCtx) == nil
	}
	return t.limiter.Allow()
}

// ---------------------------------------------------------------------------
// Deduplication store
// ---------------------------------------------------------------------------

type dedupEntry struct{ expiresAt time.Time }

type dedupStore struct {
	mu         sync.Mutex
	seen       map[string]dedupEntry
	window     time.Duration
	maxEntries int64
	done       chan struct{} // closed by stop() to terminate the eviction goroutine
}

func newDedupStore(w time.Duration, maxEntries int64) *dedupStore {
	ds := &dedupStore{
		seen:       make(map[string]dedupEntry),
		window:     w,
		maxEntries: maxEntries,
		done:       make(chan struct{}),
	}
	evictInterval := w / 4
	if evictInterval < 30*time.Second {
		evictInterval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(evictInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ds.evict()
			case <-ds.done:
				return
			}
		}
	}()
	return ds
}

// stop terminates the background eviction goroutine. Safe to call multiple times.
func (ds *dedupStore) stop() {
	select {
	case <-ds.done: // already closed
	default:
		close(ds.done)
	}
}

func (ds *dedupStore) isDuplicate(id string) bool {
	now := time.Now()
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if e, ok := ds.seen[id]; ok && now.Before(e.expiresAt) {
		return true
	}
	if ds.maxEntries > 0 && int64(len(ds.seen)) >= ds.maxEntries {
		ds.evictOldestLocked()
	}
	ds.seen[id] = dedupEntry{expiresAt: now.Add(ds.window)}
	return false
}

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

// evictOldestLocked removes the entry with the earliest expiry from the seen map.
// Called under ds.mu while the map is at capacity to make room for a new entry.
// Time complexity: O(n) map scan. Acceptable for DedupMaxEntries up to ~500k;
// replace with a min-heap if higher cardinality is required.
func (ds *dedupStore) evictOldestLocked() {
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

// ---------------------------------------------------------------------------
// Settings validation
// ---------------------------------------------------------------------------

func validateSettings(s *Settings) error {
	if strings.TrimSpace(s.Topic) == "" {
		return fmt.Errorf("topic must not be empty")
	}
	if strings.TrimSpace(s.ConsumerGroup) == "" {
		return fmt.Errorf("consumerGroup must not be empty")
	}
	if s.Operator != "" && !validOps()[s.Operator] {
		return fmt.Errorf("unsupported trigger-level operator %q", s.Operator)
	}
	if s.PredicateMode != "" && strings.ToLower(s.PredicateMode) != "and" && strings.ToLower(s.PredicateMode) != "or" {
		return fmt.Errorf("unsupported predicateMode %q (accepted: \"and\", \"or\")", s.PredicateMode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Balance strategy helper
// ---------------------------------------------------------------------------

// resolveBalanceStrategy maps the user-facing strategy name to a Sarama
// BalanceStrategy. Accepted values: "roundrobin" (default), "sticky", "range".
func resolveBalanceStrategy(s string) sarama.BalanceStrategy {
	switch strings.ToLower(s) {
	case "sticky":
		return sarama.NewBalanceStrategySticky()
	case "range":
		return sarama.NewBalanceStrategyRange()
	default: // "roundrobin" or empty
		return sarama.NewBalanceStrategyRoundRobin()
	}
}

// handlerContext returns a child context scoped to HandlerTimeoutMs.
// When HandlerTimeoutMs is 0 the parent is returned with a no-op cancel.
func (t *Trigger) handlerContext(parent context.Context) (context.Context, context.CancelFunc) {
	if t.settings.HandlerTimeoutMs <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, time.Duration(t.settings.HandlerTimeoutMs)*time.Millisecond)
}

// ---------------------------------------------------------------------------
// Periodic dedup persistence
// ---------------------------------------------------------------------------

// maybePersistDedup saves the dedup state every DedupPersistEveryN messages.
func (t *Trigger) maybePersistDedup() {
	if t.dedup == nil || t.settings.DedupPersistPath == "" || t.settings.DedupPersistEveryN <= 0 {
		return
	}
	n := t.msgCount.Add(1)
	if n%t.settings.DedupPersistEveryN == 0 {
		if err := t.dedup.saveToFile(t.settings.DedupPersistPath); err != nil {
			t.logger.Warnf("kafka-stream/filter-trigger: periodic dedup state save failed: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Dedup store persistence (gob)
// ---------------------------------------------------------------------------

// persistedDedupState is the gob-serialisable envelope for the dedup map.
type persistedDedupState struct {
	Seen map[string]time.Time // id → expiresAt
}

// saveToFile serialises the dedup seen-map to a gob file at path.
// Only non-expired entries are written. Atomic write (temp + rename).
func (ds *dedupStore) saveToFile(path string) error {
	now := time.Now()
	ds.mu.Lock()
	state := persistedDedupState{Seen: make(map[string]time.Time, len(ds.seen))}
	for id, e := range ds.seen {
		if now.Before(e.expiresAt) {
			state.Seen[id] = e.expiresAt
		}
	}
	ds.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("kafka-stream/filter-dedup: cannot create directory: %w", err)
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(state); err != nil {
		return fmt.Errorf("kafka-stream/filter-dedup: encode error: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("kafka-stream/filter-dedup: write error: %w", err)
	}
	return os.Rename(tmp, path)
}

// loadFromFile restores non-expired entries from a previously saved gob file.
// Returns nil (no error) when the file does not exist.
func (ds *dedupStore) loadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("kafka-stream/filter-dedup: read error: %w", err)
	}
	var state persistedDedupState
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&state); err != nil {
		return fmt.Errorf("kafka-stream/filter-dedup: decode error: %w", err)
	}
	now := time.Now()
	ds.mu.Lock()
	defer ds.mu.Unlock()
	for id, expiresAt := range state.Seen {
		if now.Before(expiresAt) { // skip already-expired entries
			ds.seen[id] = dedupEntry{expiresAt: expiresAt}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Sarama ConsumerGroupHandler adapter
// ---------------------------------------------------------------------------

type consumerGroupHandler struct{ t *Trigger }

func (h *consumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *consumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *consumerGroupHandler) ConsumeClaim(
	session sarama.ConsumerGroupSession,
	claim sarama.ConsumerGroupClaim,
) error {
	for {
		select {
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			h.t.handleMessage(session, msg)
		case <-session.Context().Done():
			return nil
		}
	}
}
