// Package split provides a Flogo trigger that consumes messages from a Kafka
// topic and routes each message to one or more handler branches based on
// content-based predicates — content-based routing / stream-split pattern.
//
// Routing modes:
//   - "first-match" (default): evaluate matched-type handlers in priority order;
//     the first handler whose predicates pass receives the message.  Behaves
//     like a switch/case chain — only one branch fires per message.
//   - "all-match": evaluate all matched-type handlers; every handler whose
//     predicates pass receives the message (fan-out).
//
// Handler event types:
//   - "matched"   (default) — fires when this handler's predicates pass.
//   - "unmatched"           — fires when no matched handler's predicates pass (catch-all).
//   - "evalError"           — fires when predicate evaluation fails (error DLQ).
//   - "all"                 — fires for every message regardless of routing outcome (tap).
package split

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/trace"
	"github.com/project-flogo/core/trigger"
	kafkaconn "github.com/tibco/wi-plugins/contributions/kafka/src/app/Kafka/connector/kafka"
)

var triggerMd = trigger.NewMetadata(&Settings{}, &HandlerSettings{}, &Output{})

func init() {
	_ = trigger.Register(&Trigger{}, &Factory{})
}

// Trigger is the Kafka Stream Split Trigger.
// It owns a Kafka consumer group, decodes each message from the configured
// topic, evaluates the predicates of every matched-type handler in priority
// order, and fires the appropriate branch handler(s).
type Trigger struct {
	settings *Settings
	logger   log.Logger

	// Pre-categorized handler slices for efficient per-message routing.
	// matchedHandlers is sorted by HandlerSettings.Priority (ascending) in Initialize().
	matchedHandlers   []*handler // eventType=matched — evaluated in priority order
	unmatchedHandlers []*handler // eventType=unmatched — fired when no match
	evalErrorHandlers []*handler // eventType=evalError — fired on predicate eval failure
	allHandlers       []*handler // eventType=all — tap, fired for every message

	client   sarama.ConsumerGroup
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopOnce sync.Once
}

// handler pairs a Flogo flow runner with its resolved HandlerSettings and
// pre-compiled predicate artefacts.
type handler struct {
	runner      trigger.Handler
	hs          *HandlerSettings
	eventType   string                 // resolved from hs.EventType with default fallback
	singleRegex *regexp.Regexp         // pre-compiled for single-predicate regex mode
	multiRegex  map[int]*regexp.Regexp // pre-compiled for multi-predicate regex entries
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
		return nil, fmt.Errorf("kafka-stream/split-trigger: failed to map settings: %w", err)
	}
	var err error
	s.Connection, err = kafkaconn.GetSharedConfiguration(config.Settings["kafkaConnection"])
	if err != nil {
		return nil, fmt.Errorf("kafka-stream/split-trigger: failed to resolve kafkaConnection: %w", err)
	}
	if err := validateSettings(s); err != nil {
		return nil, fmt.Errorf("kafka-stream/split-trigger: invalid settings: %w", err)
	}
	return &Trigger{settings: s}, nil
}

// Metadata returns the trigger metadata.
func (t *Trigger) Metadata() *trigger.Metadata { return triggerMd }

// Initialize wires up handlers, pre-compiles predicate artefacts, sorts
// matched handlers by priority, and creates the Kafka consumer group client.
func (t *Trigger) Initialize(ctx trigger.InitContext) error {
	t.logger = ctx.Logger()

	for _, h := range ctx.GetHandlers() {
		hs := &HandlerSettings{}
		if err := metadata.MapToStruct(h.Settings(), hs, true); err != nil {
			return fmt.Errorf("kafka-stream/split-trigger: failed to map handler settings: %w", err)
		}
		et := hs.EventType
		if et == "" {
			et = EventTypeMatched
		}
		switch et {
		case EventTypeMatched, EventTypeUnmatched, EventTypeEvalError, EventTypeAll:
			// valid
		default:
			return fmt.Errorf("kafka-stream/split-trigger: handler %q eventType %q is invalid (accepted: matched, unmatched, evalError, all)", h.Name(), et)
		}

		hh := &handler{runner: h, hs: hs, eventType: et}

		// Pre-compile regex/predicate artefacts for matched-type handlers only.
		// unmatched, evalError, and all (tap) handlers fire based on routing outcome,
		// not on predicates — any predicate fields on those event types are ignored.
		if et == EventTypeMatched && hs.Predicates != "" {
			preds, err := hs.ParsedPredicates()
			if err != nil {
				return fmt.Errorf("kafka-stream/split-trigger: handler %q invalid predicates JSON: %w", h.Name(), err)
			}
			for i, p := range preds {
				if !supportedOps[p.Operator] {
					return fmt.Errorf("kafka-stream/split-trigger: handler %q predicate[%d]: unsupported operator %q", h.Name(), i, p.Operator)
				}
			}
			hh.parsedPreds = preds
			hh.multiRegex = make(map[int]*regexp.Regexp)
			for i, p := range preds {
				if p.Operator == "regex" {
					compiled, err := regexp.Compile(p.Value)
					if err != nil {
						return fmt.Errorf("kafka-stream/split-trigger: handler %q predicate[%d] invalid regex %q: %w", h.Name(), i, p.Value, err)
					}
					hh.multiRegex[i] = compiled
				}
			}
		} else if et == EventTypeMatched && hs.Field != "" {
			if hs.Operator == "" || !supportedOps[hs.Operator] {
				return fmt.Errorf("kafka-stream/split-trigger: handler %q single-predicate field=%q has unsupported or missing operator %q", h.Name(), hs.Field, hs.Operator)
			}
			if hs.Operator == "regex" && hs.Value != "" {
				compiled, err := regexp.Compile(hs.Value)
				if err != nil {
					return fmt.Errorf("kafka-stream/split-trigger: handler %q regex %q: %w", h.Name(), hs.Value, err)
				}
				hh.singleRegex = compiled
			}
		}

		switch et {
		case EventTypeMatched:
			t.matchedHandlers = append(t.matchedHandlers, hh)
		case EventTypeUnmatched:
			if hs.Field != "" || hs.Predicates != "" {
				t.logger.Warnf("kafka-stream/split-trigger: handler %q (unmatched) has predicates configured — they will be ignored; unmatched handlers fire when no matched handler passes", h.Name())
			}
			t.unmatchedHandlers = append(t.unmatchedHandlers, hh)
		case EventTypeEvalError:
			if hs.Field != "" || hs.Predicates != "" {
				t.logger.Warnf("kafka-stream/split-trigger: handler %q (evalError) has predicates configured — they will be ignored; evalError handlers fire when predicate evaluation fails", h.Name())
			}
			t.evalErrorHandlers = append(t.evalErrorHandlers, hh)
		case EventTypeAll:
			if hs.Field != "" || hs.Predicates != "" {
				t.logger.Warnf("kafka-stream/split-trigger: handler %q (all/tap) has predicates configured — they will be ignored; all-type handlers fire unconditionally for every message", h.Name())
			}
			t.allHandlers = append(t.allHandlers, hh)
		}

		t.logger.Debugf("kafka-stream/split-trigger: registered handler — name=%q eventType=%q priority=%d", h.Name(), et, hs.Priority)
	}

	// Sort matched handlers by priority so first-match evaluation is deterministic.
	// sort.SliceStable preserves relative order for handlers with equal priority.
	sort.SliceStable(t.matchedHandlers, func(i, j int) bool {
		return t.matchedHandlers[i].hs.Priority < t.matchedHandlers[j].hs.Priority
	})

	// ── Create the Kafka consumer group ──────────────────────────────────────
	ksc, ok := t.settings.Connection.(*kafkaconn.KafkaSharedConfigManager)
	if !ok {
		return fmt.Errorf("kafka-stream/split-trigger: kafkaConnection is not a *KafkaSharedConfigManager (got %T) — verify the connection type in trigger settings", t.settings.Connection)
	}
	clientCfg := ksc.GetClientConfiguration()
	saramaConfig := clientCfg.CreateConsumerConfig()
	saramaConfig.Consumer.Return.Errors = true
	// Disable Sarama's background auto-commit so that session.MarkMessage is the
	// sole mechanism for committing offsets, preserving at-least-once semantics.
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
		return fmt.Errorf("kafka-stream/split-trigger: failed to create consumer group [brokers=%v group=%q]: %w",
			brokers, t.settings.ConsumerGroup, err)
	}

	// Normalise RoutingMode once so handleMessage does not need to default it
	// on every message (the hot path). routeMessage keeps its own fallback for
	// unit-test callers that bypass Initialize.
	if t.settings.RoutingMode == "" {
		t.settings.RoutingMode = RoutingModeFirstMatch
	}
	t.logger.Infof("kafka-stream/split-trigger: initialised — brokers=%v topic=%q group=%q routingMode=%q matchedHandlers=%d unmatchedHandlers=%d evalErrorHandlers=%d tapHandlers=%d",
		brokers, t.settings.Topic, t.settings.ConsumerGroup,
		t.settings.RoutingMode,
		len(t.matchedHandlers), len(t.unmatchedHandlers), len(t.evalErrorHandlers), len(t.allHandlers))
	if t.settings.HandlerTimeoutMs <= 0 {
		t.logger.Warnf("kafka-stream/split-trigger: handlerTimeoutMs not set — handlers can block indefinitely; with AutoCommit disabled this will hold the partition claim until session rebalance fires")
	}
	if t.settings.HandlerTimeoutMs > 0 && t.settings.MessageTimeoutMs == 0 && len(t.matchedHandlers) > 1 {
		t.logger.Warnf("kafka-stream/split-trigger: messageTimeoutMs not set — with %d matched handlers and handlerTimeoutMs=%dms, all-match mode can hold the partition claim for up to %dms; set messageTimeoutMs to cap total per-message processing time",
			len(t.matchedHandlers), t.settings.HandlerTimeoutMs, int64(len(t.matchedHandlers))*t.settings.HandlerTimeoutMs)
	}
	return nil
}

// Start launches the Kafka consumer goroutine.
func (t *Trigger) Start() error {
	t.ctx, t.cancel = context.WithCancel(context.Background())
	t.wg.Add(1)
	go t.consumeLoop()
	// Drain the Sarama consumer-group errors channel. Without a reader the
	// channel fills (default buffer: 256) under sustained broker errors and
	// then blocks the Sarama broker reader goroutine, stalling consumption.
	go func() {
		for err := range t.client.Errors() {
			t.logger.Errorf("kafka-stream/split-trigger: sarama consumer error: %v", err)
		}
	}()
	t.logger.Infof("kafka-stream/split-trigger: started — topic=%q", t.settings.Topic)
	return nil
}

// Stop signals the consumer loop to stop and waits for the goroutine to finish.
// It is safe to call Stop more than once; the Sarama client is closed exactly once.
func (t *Trigger) Stop() error {
	t.cancel()
	t.wg.Wait()
	t.stopOnce.Do(func() {
		if err := t.client.Close(); err != nil {
			t.logger.Warnf("kafka-stream/split-trigger: consumer group close error: %v", err)
		}
	})
	t.logger.Infof("kafka-stream/split-trigger: stopped — topic=%q", t.settings.Topic)
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
			t.logger.Errorf("kafka-stream/split-trigger: consumer error (retrying in 5s): %v", err)
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

// ---------------------------------------------------------------------------
// Routing logic
// ---------------------------------------------------------------------------

// routingDecision captures which handler categories should fire and the reason
// for any evaluation error.  All slice fields may be nil (empty) — callers
// must check length before iterating.
type routingDecision struct {
	matched       []*handler // matched-type handlers whose predicates passed
	unmatched     []*handler // unmatched-type handlers (fired when no match)
	evalErrors    []*handler // evalError-type handlers (fired on predicate eval failure)
	tap           []*handler // all-type handlers (fired for every message)
	evalErrReason string     // reason from the first predicate evaluation error
}

// routeMessage is the core content-based routing logic.  It evaluates
// matched-type handlers in priority order and returns a routingDecision
// describing which handler categories should fire for the given payload.
//
// This method is purposely free of Kafka/Flogo runtime dependencies — it can
// be called directly from unit tests without a live broker.
func (t *Trigger) routeMessage(payload map[string]interface{}) routingDecision {
	routingMode := t.settings.RoutingMode
	if routingMode == "" {
		routingMode = RoutingModeFirstMatch
	}

	var decision routingDecision
	var hasEvalError bool

	// ── Step 1: evaluate matched-type handlers in priority order ─────────────
	for _, h := range t.matchedHandlers {
		passed, _, evalErr := t.evaluateHandler(payload, h)
		if evalErr != "" {
			hasEvalError = true
			// Only the first eval error reason is surfaced in Output.EvalErrorReason;
			// subsequent handler errors are suppressed here but all evalError handlers
			// still fire once with the first reason.  Additional detail is in logs.
			if decision.evalErrReason == "" {
				decision.evalErrReason = evalErr
			}
			// Evaluation failed for this handler — do not count as a match.
			// Continue to the next handler so a later handler may still match.
			continue
		}
		if passed {
			decision.matched = append(decision.matched, h)
			if routingMode == RoutingModeFirstMatch {
				// First-match: stop at the first successful evaluation.
				break
			}
			// All-match: continue to collect all passing handlers.
		}
	}

	// ── Step 2: if no matched handler fired, collect unmatched handlers ───────
	if len(decision.matched) == 0 {
		decision.unmatched = t.unmatchedHandlers
	}

	// ── Step 3: if any eval error occurred, collect evalError handlers ────────
	if hasEvalError {
		decision.evalErrors = t.evalErrorHandlers
	}

	// ── Step 4: tap handlers always fire for every message ────────────────────
	decision.tap = t.allHandlers

	return decision
}

// handleMessage decodes a raw Kafka message, routes it via routeMessage, fires
// the appropriate handler branches, and manages offset commitment.
func (t *Trigger) handleMessage(session sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage) {
	if t.logger.DebugEnabled() {
		t.logger.Debugf("kafka-stream/split-trigger: record received — topic=%s partition=%d offset=%d key=%q len=%d",
			msg.Topic, msg.Partition, msg.Offset, string(msg.Key), len(msg.Value))
	}

	// ── Build OTel trace propagation context ─────────────────────────────────
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

	// ── Decode JSON payload ───────────────────────────────────────────────────
	var payload map[string]interface{}
	if err := json.Unmarshal(msg.Value, &payload); err != nil {
		// Poison-pill — malformed JSON that can never be processed.  Always mark
		// the offset so the consumer does not stall on an undecodable message.
		t.logger.Errorf("kafka-stream/split-trigger: cannot decode JSON offset=%d partition=%d — skipping (poison-pill): %v",
			msg.Offset, msg.Partition, err)
		session.MarkMessage(msg, "")
		return
	}

	msgKey := string(msg.Key)
	routingMode := t.settings.RoutingMode // normalised to RoutingModeFirstMatch in Initialize()

	// ── Per-message context: cap total handler processing time ────────────────
	// In all-match mode N handlers fire sequentially; without a per-message cap
	// the partition claim is held for up to N×HandlerTimeoutMs, risking a Kafka
	// session timeout and an unwanted consumer-group rebalance.
	//
	// NOTE: do NOT use defer here — handleMessage is called inside ConsumeClaim's
	// for-loop. defer runs at function return, not loop iteration, so deferred
	// cancels accumulate across messages and leak until the session ends.
	// Instead, msgCancel is called explicitly at the bottom of this function.
	msgCtx := ctx
	msgCancel := context.CancelFunc(func() {})
	if t.settings.MessageTimeoutMs > 0 {
		msgCtx, msgCancel = context.WithTimeout(ctx, time.Duration(t.settings.MessageTimeoutMs)*time.Millisecond)
	}

	decision := t.routeMessage(payload)

	// routingFailed tracks failures in routing-critical handlers (matched,
	// unmatched, evalError). A failure withholds the offset commit so the
	// message is redelivered on restart/rebalance (at-least-once semantics).
	// Tap handler failures are logged but do NOT set routingFailed — a
	// monitoring tap must never stall or cause redelivery of routed messages.
	routingFailed := false

	// ── Fire matched handlers ─────────────────────────────────────────────────
	for _, h := range decision.matched {
		out := &Output{
			Message:            payload,
			Topic:              msg.Topic,
			Partition:          int64(msg.Partition),
			Offset:             msg.Offset,
			Key:                msgKey,
			MatchedHandlerName: h.runner.Name(),
			RoutingMode:        routingMode,
		}
		hCtx, cancel := t.handlerContext(msgCtx)
		_, err := h.runner.Handle(trigger.NewContextWithEventId(hCtx, eventId), out.ToMap())
		cancel()
		if err != nil {
			t.logger.Errorf("kafka-stream/split-trigger: matched handler %q fire error offset=%d: %v", h.runner.Name(), msg.Offset, err)
			routingFailed = true
		} else {
			t.logger.Infof("kafka-stream/split-trigger: routed to matched handler %q — topic=%s partition=%d offset=%d",
				h.runner.Name(), msg.Topic, msg.Partition, msg.Offset)
		}
	}

	// ── Fire unmatched handlers ───────────────────────────────────────────────
	if len(decision.unmatched) > 0 {
		t.logger.Warnf("kafka-stream/split-trigger: no matched handler for offset=%d — routing to unmatched handler(s)", msg.Offset)
		for _, h := range decision.unmatched {
			out := &Output{
				Message:     payload,
				Topic:       msg.Topic,
				Partition:   int64(msg.Partition),
				Offset:      msg.Offset,
				Key:         msgKey,
				RoutingMode: routingMode,
			}
			hCtx, cancel := t.handlerContext(msgCtx)
			_, err := h.runner.Handle(trigger.NewContextWithEventId(hCtx, eventId), out.ToMap())
			cancel()
			if err != nil {
				t.logger.Errorf("kafka-stream/split-trigger: unmatched handler %q fire error offset=%d: %v", h.runner.Name(), msg.Offset, err)
				routingFailed = true
			}
		}
	} else if len(decision.tap) == 0 {
		// decision.unmatched is only populated when decision.matched is empty,
		// so reaching this branch already implies no matched handler fired.
		t.logger.Warnf("kafka-stream/split-trigger: no handler matched offset=%d and no unmatched/tap handler configured — message silently discarded", msg.Offset)
	}

	// ── Fire evalError handlers ───────────────────────────────────────────────
	if len(decision.evalErrors) > 0 {
		t.logger.Warnf("kafka-stream/split-trigger: eval error offset=%d reason=%q — routing to evalError handler(s)", msg.Offset, decision.evalErrReason)
		for _, h := range decision.evalErrors {
			out := &Output{
				Message:         payload,
				Topic:           msg.Topic,
				Partition:       int64(msg.Partition),
				Offset:          msg.Offset,
				Key:             msgKey,
				RoutingMode:     routingMode,
				EvalError:       true,
				EvalErrorReason: decision.evalErrReason,
			}
			hCtx, cancel := t.handlerContext(msgCtx)
			_, err := h.runner.Handle(trigger.NewContextWithEventId(hCtx, eventId), out.ToMap())
			cancel()
			if err != nil {
				t.logger.Errorf("kafka-stream/split-trigger: evalError handler %q fire error offset=%d: %v", h.runner.Name(), msg.Offset, err)
				routingFailed = true
			}
		}
	} else if decision.evalErrReason != "" {
		t.logger.Warnf("kafka-stream/split-trigger: eval error offset=%d: %s — no evalError handler configured (configure an evalError handler for DLQ routing)", msg.Offset, decision.evalErrReason)
	}

	// ── Fire tap (all) handlers ───────────────────────────────────────────────
	// Tap failures are logged but intentionally do NOT set routingFailed.
	// A monitoring/audit tap must not stall redelivery of the routed message.
	// EvalError is forwarded so tap flows can distinguish normal routing from
	// routing that encountered a predicate evaluation error.
	for _, h := range decision.tap {
		out := &Output{
			Message:         payload,
			Topic:           msg.Topic,
			Partition:       int64(msg.Partition),
			Offset:          msg.Offset,
			Key:             msgKey,
			RoutingMode:     routingMode,
			EvalError:       decision.evalErrReason != "",
			EvalErrorReason: decision.evalErrReason,
		}
		hCtx, cancel := t.handlerContext(msgCtx)
		_, err := h.runner.Handle(trigger.NewContextWithEventId(hCtx, eventId), out.ToMap())
		cancel()
		if err != nil {
			t.logger.Errorf("kafka-stream/split-trigger: tap handler %q fire error offset=%d: %v", h.runner.Name(), msg.Offset, err)
			// intentionally not setting routingFailed
		}
	}

	// ── Commit offset ─────────────────────────────────────────────────────────
	// Only mark the offset when all routing-critical handlers succeeded
	// (at-least-once), unless commitOnSuccess=false (at-most-once).
	// Tap failures never block the commit.
	if !t.settings.CommitOnSuccess || !routingFailed {
		session.MarkMessage(msg, "")
	} else {
		t.logger.Warnf("kafka-stream/split-trigger: routing handler failed — not marking offset=%d (message will be redelivered after restart/rebalance)", msg.Offset)
	}

	// Release the per-message context. Must be called explicitly (not via
	// defer) because this function runs inside a for-loop in ConsumeClaim;
	// deferred calls accumulate across iterations and leak until session ends.
	msgCancel()
}

// ---------------------------------------------------------------------------
// Predicate evaluation logic
// ---------------------------------------------------------------------------

// evaluateHandler runs the predicate(s) configured on h against payload.
// Returns (true, "", "") when the predicate passes.
// Returns (false, reason, "") when the predicate fails with a normal non-match.
// Returns (false, "", evalErr) when evaluation itself encounters an error.
//
// When no predicate is configured (no Field and no Predicates) the handler is
// treated as always-passing — useful for default/catch-all matched branches.
func (t *Trigger) evaluateHandler(payload map[string]interface{}, h *handler) (passed bool, reason, evalErr string) {
	hs := h.hs
	predMode := hs.PredicateMode
	if predMode == "" {
		predMode = "and"
	}

	if hs.Predicates != "" {
		// Use the pre-parsed predicates cached in Initialize() — avoids JSON
		// unmarshal on every message (can be 10k+ msg/s in production).
		return evalMulti(payload, h.parsedPreds, predMode, false, h.multiRegex)
	}
	if hs.Field != "" {
		return evalSingle(payload, hs.Field, hs.Operator, hs.Value, false, h.singleRegex)
	}
	// No predicates: always pass (default branch / unconstrained tap).
	return true, "", ""
}

func evalSingle(msg map[string]interface{}, field, operator, value string, passThroughOnMissing bool, compiled *regexp.Regexp) (bool, string, string) {
	if field == "" {
		return false, "", "filter not configured: set 'field' for single-predicate mode"
	}
	if operator == "" || !supportedOps[operator] {
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
		// compiled is always non-nil for regex operators after Initialize().
		// A nil value here means the handler was not properly initialised —
		// fail fast rather than silently compile per message at 10k+ msg/s.
		if compiled == nil {
			return false, "", fmt.Sprintf("regex not pre-compiled for field %q — handler was not fully initialised (this is a bug)", field)
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

	isAnd := strings.ToLower(mode) != "or"
	var reasons []string

	for i, p := range preds {
		rawField, exists := msg[p.Field]
		if !exists {
			if passThroughOnMissing {
				// Treat a missing field as "no opinion" — skip the predicate in
				// both AND and OR mode rather than short-circuiting to true.
				// AND: absence is not a failure; other predicates still apply.
				// OR:  absence is not a pass; other predicates must still match.
				continue
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
			// compiled is always non-nil for regex operators after Initialize().
			// A nil value means the handler was not properly initialised —
			// fail fast rather than silently compile per message at 10k+ msg/s.
			if compiled == nil {
				return false, "", fmt.Sprintf("regex not pre-compiled for field %q predicate[%d] — handler was not fully initialised (this is a bug)", p.Field, i)
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
// compiled must be non-nil only when operator is "regex".
func evalPredicate(rawField interface{}, fieldName, op, compareVal string, compiled *regexp.Regexp) (bool, string, error) {
	// ── String-specific operators ─────────────────────────────────────────────
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

	// ── Numeric comparison operators — try numeric path first ─────────────────
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

	// ── String fallback for eq / neq ─────────────────────────────────────────
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

// supportedOps is the set of predicate operators recognised by this trigger.
// Defined as a package-level var to allocate the map once at startup.
var supportedOps = map[string]bool{
	"eq": true, "neq": true,
	"gt": true, "gte": true, "lt": true, "lte": true,
	"contains": true, "startsWith": true, "endsWith": true,
	"regex": true,
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
	if s.RoutingMode != "" && s.RoutingMode != RoutingModeFirstMatch && s.RoutingMode != RoutingModeAllMatch {
		return fmt.Errorf("unsupported routingMode %q (accepted: %q, %q)", s.RoutingMode, RoutingModeFirstMatch, RoutingModeAllMatch)
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
// When HandlerTimeoutMs is 0 the parent context is returned with a no-op cancel.
func (t *Trigger) handlerContext(parent context.Context) (context.Context, context.CancelFunc) {
	if t.settings.HandlerTimeoutMs <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, time.Duration(t.settings.HandlerTimeoutMs)*time.Millisecond)
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
