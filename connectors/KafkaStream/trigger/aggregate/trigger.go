// Package aggregate provides a Flogo trigger that consumes messages from a
// Kafka topic, accumulates them into stateful windows, and fires the associated
// flow when a window closes (tumbling) or on every message (sliding).  It is
// the trigger-native equivalent of the kafka-stream-aggregate activity, with
// the Kafka transport built in.
package aggregate

import (
	"context"
	"encoding/json"
	"fmt"
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

	kafkastream "github.com/milindpandav/flogo-extensions/kafkastream"
	"github.com/milindpandav/flogo-extensions/kafkastream/window"
)

var triggerMd = trigger.NewMetadata(&Settings{}, &HandlerSettings{}, &Output{})

// validWindowTypes lists the accepted windowType values for settings validation.
var validWindowTypes = map[string]bool{
	"TumblingTime": true, "TumblingCount": true,
	"SlidingTime": true, "SlidingCount": true,
}

// validAggregateFuncs lists the accepted function values for settings validation.
var validAggregateFuncs = map[string]bool{
	"sum": true, "count": true, "avg": true, "min": true, "max": true,
}

func init() {
	_ = trigger.Register(&Trigger{}, &Factory{})
}

// Trigger is the Kafka Stream Aggregate Trigger.
// It owns a Kafka consumer group, feeds each message into the configured window
// store, and fires the attached flow when the window closes (tumbling) or on
// every event (sliding).  Multiple handlers can be registered with different
// EventType settings for windowClose vs. lateEvent routing.
type Trigger struct {
	settings *Settings
	logger   log.Logger
	handlers []*handler
	client   sarama.ConsumerGroup
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

type handler struct {
	runner    trigger.Handler
	hs        *HandlerSettings
	eventType string // resolved from hs.EventType with default fallback
}

// Factory creates Trigger instances.
type Factory struct{}

func (*Factory) Metadata() *trigger.Metadata { return triggerMd }

func (*Factory) New(config *trigger.Config) (trigger.Trigger, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(config.Settings, s, true); err != nil {
		return nil, fmt.Errorf("kafka-stream/aggregate-trigger: failed to map settings: %w", err)
	}
	var err error
	s.Connection, err = kafkaconn.GetSharedConfiguration(config.Settings["kafkaConnection"])
	if err != nil {
		return nil, fmt.Errorf("kafka-stream/aggregate-trigger: failed to resolve kafkaConnection: %w", err)
	}
	if err := validateSettings(s); err != nil {
		return nil, fmt.Errorf("kafka-stream/aggregate-trigger: invalid settings: %w", err)
	}
	return &Trigger{settings: s}, nil
}

func (t *Trigger) Metadata() *trigger.Metadata { return triggerMd }

// Initialize wires up handlers, creates the Kafka consumer group client, and
// pre-registers the base window store for the configured window name.
func (t *Trigger) Initialize(ctx trigger.InitContext) error {
	t.logger = ctx.Logger()

	for _, h := range ctx.GetHandlers() {
		hs := &HandlerSettings{}
		if err := metadata.MapToStruct(h.Settings(), hs, true); err != nil {
			return fmt.Errorf("kafka-stream/aggregate-trigger: failed to map handler settings: %w", err)
		}
		et := hs.EventType
		if et == "" {
			et = EventTypeWindowClose
		}
		t.handlers = append(t.handlers, &handler{runner: h, hs: hs, eventType: et})
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
		return fmt.Errorf("kafka-stream/aggregate-trigger: failed to create consumer group [brokers=%v group=%q]: %w",
			brokers, t.settings.ConsumerGroup, err)
	}

	// Pre-register the base window store so configuration errors (bad function
	// name, invalid sizes, etc.) surface at startup rather than at the first
	// message.  For keyed windows the base name is never used for data — each
	// keyed sub-window registers itself as "<windowName>:<key>" on demand.
	// Pre-registering the base entry in that case would consume one slot of the
	// MaxKeys cardinality budget, causing the first real keyed sub-window to
	// count as entry #2 and the second real sub-window to be rejected when
	// MaxKeys=2.  Skip pre-registration when keyField is configured.
	if t.settings.KeyField == "" {
		baseCfg := buildWindowConfig(t.settings, t.settings.WindowName)
		if _, err := kafkastream.GetOrCreateWindowStore(baseCfg); err != nil {
			return fmt.Errorf("kafka-stream/aggregate-trigger: failed to initialise window %q: %w", t.settings.WindowName, err)
		}
	}

	// Restore persisted state (if configured).
	if t.settings.PersistPath != "" {
		// Safety reminder: each trigger instance must use a unique PersistPath.
		// Sharing a path across multiple instances (e.g. two triggers in the same
		// Flogo app) will cause the second instance to clobber the first's state.
		t.logger.Warnf("kafka-stream/aggregate-trigger: state persistence enabled at %q — ensure no other trigger instance writes to the same path", t.settings.PersistPath)
		if err := kafkastream.RestoreStateFrom(t.settings.PersistPath); err != nil {
			t.logger.Warnf("kafka-stream/aggregate-trigger: state restore from %q failed (ignored): %v", t.settings.PersistPath, err)
		}
	}

	if t.settings.EventTimeField == "" {
		t.logger.Warnf("kafka-stream/aggregate-trigger: window=%q — eventTimeField not set; wall-clock will be used (not suitable for out-of-order replay)", t.settings.WindowName)
	}
	if t.settings.HandlerTimeoutMs <= 0 {
		t.logger.Warnf("kafka-stream/aggregate-trigger: handlerTimeoutMs not set — handlers can block indefinitely; with AutoCommit disabled this will hold the partition claim until session rebalance fires")
	}

	t.logger.Infof("kafka-stream/aggregate-trigger: initialised — brokers=%v topic=%q group=%q window=%q(%s/%d) fn=%s handlers=%d",
		brokers, t.settings.Topic, t.settings.ConsumerGroup,
		t.settings.WindowName, t.settings.WindowType, t.settings.WindowSize, t.settings.Function,
		len(t.handlers))
	return nil
}

// Start launches the Kafka consumer goroutine and, when idleTimeoutMs is set,
// a background goroutine that sweeps idle windows and fires their handlers.
func (t *Trigger) Start() error {
	t.ctx, t.cancel = context.WithCancel(context.Background())
	t.wg.Add(1)
	go t.consumeLoop()

	if t.settings.IdleTimeoutMs > 0 {
		t.wg.Add(1)
		go t.idleSweepLoop()
	}

	t.logger.Infof("kafka-stream/aggregate-trigger: started — topic=%q window=%q", t.settings.Topic, t.settings.WindowName)
	return nil
}

// Stop signals the consumer and sweep goroutines to stop and persists state.
func (t *Trigger) Stop() error {
	t.cancel()
	t.wg.Wait()

	// Persist state on graceful shutdown.
	if t.settings.PersistPath != "" {
		if err := kafkastream.SaveStateTo(t.settings.PersistPath); err != nil {
			t.logger.Warnf("kafka-stream/aggregate-trigger: state save on stop failed: %v", err)
		}
	}

	if err := t.client.Close(); err != nil {
		t.logger.Warnf("kafka-stream/aggregate-trigger: consumer group close error: %v", err)
	}
	t.logger.Infof("kafka-stream/aggregate-trigger: stopped — topic=%q window=%q", t.settings.Topic, t.settings.WindowName)
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
				return
			}
			t.logger.Errorf("kafka-stream/aggregate-trigger: consumer error (retrying in 5s): %v", err)
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

// idleSweepLoop periodically checks for windows that have exceeded their idle
// timeout and fires the windowClose handler for each closed window.
func (t *Trigger) idleSweepLoop() {
	defer t.wg.Done()
	sweepInterval := time.Duration(t.settings.IdleTimeoutMs/4) * time.Millisecond
	if sweepInterval < 100*time.Millisecond {
		sweepInterval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			results := kafkastream.SweepIdleFor(t.settings.WindowName)
			for _, r := range results {
				t.logger.Infof("kafka-stream/aggregate-trigger: window %q closed: %s=%.4f count=%d droppedCount=%d lateCount=%d key=%q (idle sweep)",
					r.WindowName, t.settings.Function, r.Value, r.Count, r.DroppedCount, r.LateEventCount, r.Key)
				out := &Output{
					WindowResult: WindowResult{
						Result:       r.Value,
						Count:        r.Count,
						WindowName:   r.WindowName,
						Key:          r.Key,
						WindowClosed: true,
						DroppedCount: r.DroppedCount,
					},
				}
				idleEventId := fmt.Sprintf("%s#idle", r.WindowName)
				// NOTE: idle-sweep results have no associated Kafka offset — there is no
				// session.MarkMessage to call here. If the handler fails the partial result
				// is unrecoverable (the window was already closed and removed from the
				// registry). We log at ERROR so operations can detect and investigate.
				if ok := t.fireHandlers(context.Background(), idleEventId, EventTypeWindowClose, out); !ok {
					t.logger.Errorf("kafka-stream/aggregate-trigger: one or more handlers failed for idle-swept window %q — result may be lost (no Kafka offset to retry)", r.WindowName)
				}
			}
		case <-t.ctx.Done():
			return
		}
	}
}

// processPayload decodes a raw Kafka message value and runs it through the
// window store.  It returns the Output to emit, the event type ("windowClose"
// or "lateEvent"), and any hard error.  An empty eventType means no event is
// to be emitted for this message (window accumulating, not yet closed).
//
// This method is exported for unit-testing without a live Kafka broker.
func (t *Trigger) processPayload(
	ctx context.Context,
	payload map[string]interface{},
	kafkaTopic string,
	kafkaPartition int32,
	kafkaOffset int64,
) (*Output, string, error) {
	// ── Extract numeric value ─────────────────────────────────────────────────
	rawValue, ok := payload[t.settings.ValueField]
	if !ok {
		return nil, "", fmt.Errorf("field %q not found in message", t.settings.ValueField)
	}
	value, err := coerce.ToFloat64(rawValue)
	if err != nil {
		return nil, "", fmt.Errorf("field %q cannot be coerced to float64: %w", t.settings.ValueField, err)
	}

	// ── Extract grouping key ──────────────────────────────────────────────────
	key := ""
	if t.settings.KeyField != "" {
		if rawKey, exists := payload[t.settings.KeyField]; exists {
			key, _ = coerce.ToString(rawKey)
		}
	}

	// ── Extract event time ────────────────────────────────────────────────────
	eventTime := extractEventTime(payload, t.settings.EventTimeField)

	// ── Extract MessageID for per-window deduplication ────────────────────────
	messageID := ""
	if t.settings.MessageIDField != "" {
		if raw, exists := payload[t.settings.MessageIDField]; exists {
			messageID, _ = coerce.ToString(raw)
		}
	}

	// ── Resolve effective window name (keyed) ─────────────────────────────────
	windowName := t.settings.WindowName
	if key != "" {
		windowName = windowName + ":" + key
	}

	// ── Get or lazily create the keyed window store ───────────────────────────
	store, storeExists := kafkastream.GetWindowStore(windowName)
	if !storeExists {
		cfg := buildWindowConfig(t.settings, windowName)
		store, err = kafkastream.GetOrCreateWindowStore(cfg)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create keyed window %q: %w", windowName, err)
		}
	}

	// ── Idle-timeout: emit partial result before adding the new event ─────────
	if idleResult, idleClosed := store.CheckIdle(); idleClosed {
		t.logger.Infof("kafka-stream/aggregate-trigger: window %q auto-closed (idle timeout): %s=%.4f count=%d key=%q",
			windowName, t.settings.Function, idleResult.Value, idleResult.Count, key)
		// Seed the fresh window with the incoming event.
		store.Add(window.WindowEvent{ //nolint:errcheck — post-idle-close Add cannot overflow
			Value:     value,
			Timestamp: eventTime,
			Key:       key,
			MessageID: messageID,
		})
		out := &Output{
			WindowResult: WindowResult{
				Result:       idleResult.Value,
				Count:        idleResult.Count,
				WindowName:   windowName,
				Key:          key,
				WindowClosed: true,
				DroppedCount: idleResult.DroppedCount,
			},
			Source: Source{
				Topic:     kafkaTopic,
				Partition: int64(kafkaPartition),
				Offset:    kafkaOffset,
			},
		}
		return out, EventTypeWindowClose, nil
	}

	// ── Add event to window ───────────────────────────────────────────────────
	result, closed, late, addErr := store.Add(window.WindowEvent{
		Value:     value,
		Timestamp: eventTime,
		Key:       key,
		MessageID: messageID,
	})
	if addErr != nil {
		return nil, "", fmt.Errorf("window.Add failed for %q: %w", windowName, addErr)
	}

	// ── Late-event DLQ ────────────────────────────────────────────────────────
	if late != nil {
		t.logger.Warnf("kafka-stream/aggregate-trigger: late event — window=%q watermark=%s eventTime=%s reason=%q",
			windowName, late.Watermark, eventTime, late.Reason)
		out := &Output{
			WindowResult: WindowResult{
				WindowName: windowName,
				Key:        key,
			},
			Source: Source{
				Topic:      kafkaTopic,
				Partition:  int64(kafkaPartition),
				Offset:     kafkaOffset,
				LateEvent:  true,
				LateReason: late.Reason,
			},
		}
		return out, EventTypeLateEvent, nil
	}

	// ── Window closed (or sliding window update) ──────────────────────────────
	if result != nil && closed {
		t.logger.Infof("kafka-stream/aggregate-trigger: window %q closed: %s=%.4f count=%d droppedCount=%d lateCount=%d key=%q",
			windowName, t.settings.Function, result.Value, result.Count,
			result.DroppedCount, result.LateEventCount, key)
		out := &Output{
			WindowResult: WindowResult{
				Result:         result.Value,
				Count:          result.Count,
				WindowName:     windowName,
				Key:            key,
				WindowClosed:   true,
				DroppedCount:   result.DroppedCount,
				LateEventCount: result.LateEventCount,
			},
			Source: Source{
				Topic:     kafkaTopic,
				Partition: int64(kafkaPartition),
				Offset:    kafkaOffset,
			},
		}

		return out, EventTypeWindowClose, nil
	}

	// Window still accumulating (tumbling, not yet closed).
	if t.logger.DebugEnabled() && result != nil {
		snap := store.Snapshot()
		t.logger.Debugf("kafka-stream/aggregate-trigger: event added to window=%q key=%q value=%.4f buffer=%d",
			windowName, key, value, snap.BufferSize)
	}
	return nil, "", nil // nothing to emit yet
}

// handleMessage decodes a raw Kafka message and dispatches it through the window pipeline.
func (t *Trigger) handleMessage(session sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage) {
	if t.logger.DebugEnabled() {
		t.logger.Debugf("kafka-stream/aggregate-trigger: record received — topic=%s partition=%d offset=%d",
			msg.Topic, msg.Partition, msg.Offset)
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
		t.logger.Errorf("kafka-stream/aggregate-trigger: cannot decode JSON offset=%d partition=%d — skipping (poison-pill): %v",
			msg.Offset, msg.Partition, err)
		session.MarkMessage(msg, "")
		return
	}

	out, eventType, err := t.processPayload(ctx, payload, msg.Topic, msg.Partition, msg.Offset)
	if err != nil {
		if t.settings.OnSchemaError == "retry" {
			// Do not mark the offset — the message will be redelivered after a
			// restart or partition rebalance. Only safe for transient schema errors.
			t.logger.Errorf("kafka-stream/aggregate-trigger: processPayload schema error offset=%d (onSchemaError=retry — offset NOT marked): %v", msg.Offset, err)
		} else {
			t.logger.Errorf("kafka-stream/aggregate-trigger: processPayload error offset=%d: %v", msg.Offset, err)
			session.MarkMessage(msg, "")
		}
		return
	}

	handlersOK := true
	if eventType != "" && out != nil {
		handlersOK = t.fireHandlers(ctx, eventId, eventType, out)
	}
	// Periodic state persistence — called after every successfully decoded message
	// (not just on window close) so that persistEveryN=N saves state every N
	// messages even if the window has not yet accumulated enough events to close.
	t.maybePersist()
	if t.logger.DebugEnabled() {
		t.logger.Debugf("kafka-stream/aggregate-trigger: record from topic=%s partition=%d offset=%d successfully processed",
			msg.Topic, msg.Partition, msg.Offset)
	}
	// Commit semantics: only mark when handlers succeed (at-least-once) unless
	// commitOnSuccess=false (at-most-once / backward-compat mode).
	// When the window is still accumulating (no handler fired, handlersOK=true)
	// we always mark — there is nothing to retry for a not-yet-closed window.
	if !t.settings.CommitOnSuccess || handlersOK {
		session.MarkMessage(msg, "")
	} else {
		t.logger.Warnf("kafka-stream/aggregate-trigger: handler failed — not marking offset=%d (message will be redelivered after restart/rebalance)",
			msg.Offset)
	}
}

// fireHandlers invokes each registered handler whose eventType matches.
// Returns true if every matching handler completed without error, false otherwise.
func (t *Trigger) fireHandlers(ctx context.Context, eventId string, eventType string, out *Output) bool {
	allOK := true
	for _, h := range t.handlers {
		if h.eventType != EventTypeAll && h.eventType != eventType {
			continue
		}
		hCtx, cancel := t.handlerContext(ctx)
		_, err := h.runner.Handle(trigger.NewContextWithEventId(hCtx, eventId), out.ToMap())
		cancel()
		if err != nil {
			t.logger.Errorf("kafka-stream/aggregate-trigger: handler fire error eventType=%s: %v", eventType, err)
			allOK = false
		}
	}
	return allOK
}

// handlerContext returns a child context scoped to HandlerTimeoutMs.
// When HandlerTimeoutMs is 0 the parent is returned with a no-op cancel.
func (t *Trigger) handlerContext(parent context.Context) (context.Context, context.CancelFunc) {
	if t.settings.HandlerTimeoutMs <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, time.Duration(t.settings.HandlerTimeoutMs)*time.Millisecond)
}

// maybePersist triggers a periodic state snapshot if PersistEveryN is set.
func (t *Trigger) maybePersist() {
	if t.settings.PersistPath == "" || t.settings.PersistEveryN <= 0 {
		return
	}
	n := kafkastream.IncrPersistCounter()
	if n%t.settings.PersistEveryN == 0 {
		if err := kafkastream.SaveStateTo(t.settings.PersistPath); err != nil {
			t.logger.Warnf("kafka-stream/aggregate-trigger: periodic state save failed: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Window helpers
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

func buildWindowConfig(s *Settings, name string) window.WindowConfig {
	return window.WindowConfig{
		Name:            name,
		Type:            window.WindowType(s.WindowType),
		Size:            s.WindowSize,
		Function:        window.AggregateFunc(s.Function),
		EventTimeField:  s.EventTimeField,
		AllowedLateness: s.AllowedLateness,
		MaxBufferSize:   s.MaxBufferSize,
		OverflowPolicy:  window.OverflowPolicy(s.OverflowPolicy),
		IdleTimeoutMs:   s.IdleTimeoutMs,
		MaxKeys:         s.MaxKeys,
	}
}

// extractEventTime extracts the event timestamp from fieldName in msg.
// Supported formats: Unix-ms int64/float64, RFC-3339 string.
// Falls back to wall-clock when the field is absent or unparseable.
func extractEventTime(msg map[string]interface{}, fieldName string) time.Time {
	if fieldName == "" {
		return time.Now()
	}
	raw, ok := msg[fieldName]
	if !ok {
		return time.Now()
	}
	switch v := raw.(type) {
	case int64:
		return time.UnixMilli(v)
	case float64:
		return time.UnixMilli(int64(v))
	case string:
		if ts, err := time.Parse(time.RFC3339, v); err == nil {
			return ts
		}
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.UnixMilli(ms)
		}
	}
	return time.Now()
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
	if strings.TrimSpace(s.WindowName) == "" {
		return fmt.Errorf("windowName must not be empty")
	}
	if !validWindowTypes[s.WindowType] {
		return fmt.Errorf("unsupported windowType %q", s.WindowType)
	}
	if !validAggregateFuncs[s.Function] {
		return fmt.Errorf("unsupported function %q", s.Function)
	}
	if s.WindowSize <= 0 {
		return fmt.Errorf("windowSize must be > 0")
	}
	if strings.TrimSpace(s.ValueField) == "" {
		return fmt.Errorf("valueField must not be empty")
	}
	if s.OnSchemaError != "" && s.OnSchemaError != "skip" && s.OnSchemaError != "retry" {
		return fmt.Errorf("unsupported onSchemaError %q (accepted: \"skip\", \"retry\")", s.OnSchemaError)
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
