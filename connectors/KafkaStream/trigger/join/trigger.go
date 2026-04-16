// Package join provides a Flogo trigger that consumes messages from two or
// more Kafka topics and fires the associated flow when messages sharing the
// same joinKeyField value have been received from every configured topic within
// joinWindowMs milliseconds — a classic stream-join / stream-enrichment pattern.
package join

import (
	"context"
	"encoding/json"
	"fmt"
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

// joinEntry holds the in-flight state for a single join key.
// closed is set to true by whichever goroutine wins the race to complete or
// time out the join, preventing a concurrent worker from double-firing.
type joinEntry struct {
	mu            sync.Mutex
	contributions map[string]map[string]interface{} // topic → decoded payload
	createdAt     time.Time
	closed        bool // true after join completes or times out
}

// handler pairs a Flogo flow runner with its resolved HandlerSettings.
type handler struct {
	runner    trigger.Handler
	hs        *HandlerSettings
	eventType string // resolved from hs.EventType with default fallback
}

// Trigger is the Kafka Stream Join (Merge) Trigger.
// It owns one Kafka consumer group per topic and fires the attached flow when
// messages with the same joinKeyField value arrive from all configured topics
// within joinWindowMs milliseconds.
type Trigger struct {
	settings     *Settings
	logger       log.Logger
	handlers     []*handler
	topics       []string
	clients      []sarama.ConsumerGroup // one ConsumerGroup client per topic
	joinRegistry sync.Map               // joinKey (string) → *joinEntry
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// Factory creates Trigger instances.
type Factory struct{}

func (*Factory) Metadata() *trigger.Metadata { return triggerMd }

// New creates a new, uninitialised Trigger from the design-time config.
func (*Factory) New(config *trigger.Config) (trigger.Trigger, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(config.Settings, s, true); err != nil {
		return nil, fmt.Errorf("kafka-stream/join-trigger: failed to map settings: %w", err)
	}
	var err error
	s.Connection, err = kafkaconn.GetSharedConfiguration(config.Settings["kafkaConnection"])
	if err != nil {
		return nil, fmt.Errorf("kafka-stream/join-trigger: failed to resolve kafkaConnection: %w", err)
	}
	if err := validateSettings(s); err != nil {
		return nil, fmt.Errorf("kafka-stream/join-trigger: invalid settings: %w", err)
	}
	return &Trigger{settings: s}, nil
}

func (t *Trigger) Metadata() *trigger.Metadata { return triggerMd }

// Initialize wires up handlers and creates one Kafka consumer group per topic.
func (t *Trigger) Initialize(ctx trigger.InitContext) error {
	t.logger = ctx.Logger()
	t.topics = t.settings.TopicList()

	for _, h := range ctx.GetHandlers() {
		hs := &HandlerSettings{}
		if err := metadata.MapToStruct(h.Settings(), hs, true); err != nil {
			return fmt.Errorf("kafka-stream/join-trigger: failed to map handler settings: %w", err)
		}
		et := hs.EventType
		if et == "" {
			et = EventTypeJoined
		}
		t.handlers = append(t.handlers, &handler{runner: h, hs: hs, eventType: et})
	}

	ksc := t.settings.Connection.(*kafkaconn.KafkaSharedConfigManager)
	clientCfg := ksc.GetClientConfiguration()
	brokers := clientCfg.Brokers

	for i, topic := range t.topics {
		saramaConfig := clientCfg.CreateConsumerConfig()
		saramaConfig.Consumer.Return.Errors = true
		// Disable Sarama's background auto-commit so that session.MarkMessage is
		// the sole mechanism for committing offsets, preserving at-least-once
		// semantics for the completing (last-arriving) message.
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

		// Each topic gets its own consumer group ID so Kafka tracks their offsets
		// independently: "<base>-<sanitisedTopicName>".
		groupID := t.settings.ConsumerGroup + "-" + sanitizeGroupSuffix(topic)
		client, err := sarama.NewConsumerGroup(brokers, groupID, saramaConfig)
		if err != nil {
			// Clean up already-created clients before returning the error.
			for j := 0; j < i; j++ {
				_ = t.clients[j].Close()
			}
			return fmt.Errorf("kafka-stream/join-trigger: failed to create consumer group [topic=%q group=%q]: %w",
				topic, groupID, err)
		}
		t.clients = append(t.clients, client)
	}

	t.logger.Infof("kafka-stream/join-trigger: initialised — brokers=%v topics=%v group=%q joinKeyField=%q joinWindowMs=%d handlers=%d",
		brokers, t.topics, t.settings.ConsumerGroup, t.settings.JoinKeyField, t.settings.JoinWindowMs, len(t.handlers))
	return nil
}

// Start launches one consumer goroutine per topic and the timeout-sweep goroutine.
func (t *Trigger) Start() error {
	t.ctx, t.cancel = context.WithCancel(context.Background())
	for i, topic := range t.topics {
		t.wg.Add(1)
		go t.consumeLoop(t.clients[i], topic)
	}
	t.wg.Add(1)
	go t.timeoutSweepLoop()
	t.logger.Infof("kafka-stream/join-trigger: started — topics=%v", t.topics)
	return nil
}

// Stop signals all goroutines to stop and closes the Kafka consumer group clients.
func (t *Trigger) Stop() error {
	t.cancel()
	t.wg.Wait()
	for i, client := range t.clients {
		if err := client.Close(); err != nil {
			t.logger.Warnf("kafka-stream/join-trigger: consumer group close error for topic=%q: %v", t.topics[i], err)
		}
	}
	t.logger.Infof("kafka-stream/join-trigger: stopped — topics=%v", t.topics)
	return nil
}

// consumeLoop drives the Kafka consumer group session for a single topic,
// retrying on non-fatal errors.
func (t *Trigger) consumeLoop(client sarama.ConsumerGroup, topic string) {
	defer t.wg.Done()
	cgh := &consumerGroupHandler{t: t, topic: topic}
	for {
		if err := client.Consume(t.ctx, []string{topic}, cgh); err != nil {
			if t.ctx.Err() != nil {
				return
			}
			t.logger.Errorf("kafka-stream/join-trigger: consumer error topic=%q (retrying in 5s): %v", topic, err)
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

// timeoutSweepLoop periodically evicts join entries that have exceeded
// joinWindowMs without receiving contributions from all topics.
func (t *Trigger) timeoutSweepLoop() {
	defer t.wg.Done()
	sweepInterval := time.Duration(t.settings.JoinWindowMs/4) * time.Millisecond
	if sweepInterval < 100*time.Millisecond {
		sweepInterval = 100 * time.Millisecond
	}
	deadline := time.Duration(t.settings.JoinWindowMs) * time.Millisecond
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			t.joinRegistry.Range(func(rawKey, rawVal interface{}) bool {
				entry := rawVal.(*joinEntry)
				entry.mu.Lock()
				if entry.closed || now.Sub(entry.createdAt) < deadline {
					entry.mu.Unlock()
					return true
				}
				// Mark closed before releasing the lock so a concurrent
				// processPayload call sees it and does not double-fire.
				entry.closed = true
				partial := make(map[string]interface{}, len(entry.contributions))
				for topicKey, payload := range entry.contributions {
					partial[topicKey] = payload
				}
				createdAt := entry.createdAt
				entry.mu.Unlock()

				joinKey := rawKey.(string)
				t.joinRegistry.Delete(joinKey)

				missing := t.missingTopics(partial)
				t.logger.Warnf("kafka-stream/join-trigger: join timed out — key=%q age=%s missingTopics=%v",
					joinKey, now.Sub(createdAt), missing)

				out := &Output{
					TimeoutResult: TimeoutResult{
						PartialMessages: partial,
						JoinKey:         joinKey,
						MissingTopics:   missing,
						CreatedAt:       createdAt.UnixMilli(),
					},
					EventType: EventTypeTimeout,
				}
				eventId := fmt.Sprintf("join-timeout:%s", joinKey)
				t.fireHandlers(context.Background(), eventId, EventTypeTimeout, out)
				return true
			})
		case <-t.ctx.Done():
			return
		}
	}
}

// processPayload is the core join logic.  It records a topic's contribution,
// and when all topics have contributed returns the joined Output.  An empty
// eventType return means the contribution is recorded but the join is not yet
// complete.  This method is kept separate from handleMessage to allow
// unit-testing without a live Kafka broker.
func (t *Trigger) processPayload(topic string, payload map[string]interface{}) (*Output, string, error) {
	// Extract join key.
	rawKey, ok := payload[t.settings.JoinKeyField]
	if !ok {
		return nil, "", fmt.Errorf("joinKeyField %q not found in message from topic %q", t.settings.JoinKeyField, topic)
	}
	joinKey, err := coerce.ToString(rawKey)
	if err != nil || strings.TrimSpace(joinKey) == "" {
		return nil, "", fmt.Errorf("joinKeyField %q value cannot be coerced to a non-empty string (topic %q)", t.settings.JoinKeyField, topic)
	}

	// Load or create the join entry for this key.
	actual, _ := t.joinRegistry.LoadOrStore(joinKey, &joinEntry{
		contributions: make(map[string]map[string]interface{}),
		createdAt:     time.Now(),
	})
	entry := actual.(*joinEntry)

	entry.mu.Lock()
	// If the entry was already closed by the timeout sweep, start a fresh one
	// so this message begins a new join window rather than being silently lost.
	if entry.closed {
		entry.mu.Unlock()
		fresh := &joinEntry{
			contributions: make(map[string]map[string]interface{}),
			createdAt:     time.Now(),
		}
		// CompareAndSwap is not available on sync.Map pre-Go 1.20; use Store to
		// overwrite.  A race between two goroutines here is benign: both would
		// store identical fresh entries and only one contribution would proceed.
		t.joinRegistry.Store(joinKey, fresh)
		entry = fresh
		entry.mu.Lock()
	}

	// Record this topic's contribution (last-write-wins on duplicate).
	entry.contributions[topic] = payload
	complete := len(entry.contributions) == len(t.topics)

	var merged map[string]interface{}
	var contributingTopics []string
	if complete {
		// Capture state while holding the lock, then mark closed.
		entry.closed = true
		merged = make(map[string]interface{}, len(t.topics))
		contributingTopics = make([]string, 0, len(t.topics))
		for _, topicName := range t.topics {
			merged[topicName] = entry.contributions[topicName]
			contributingTopics = append(contributingTopics, topicName)
		}
	}
	entry.mu.Unlock()

	if !complete {
		if t.logger.DebugEnabled() {
			t.logger.Debugf("kafka-stream/join-trigger: contribution recorded — key=%q topic=%q (%d/%d topics)",
				joinKey, topic, len(entry.contributions), len(t.topics))
		}
		return nil, "", nil // still waiting for other topics
	}

	// All topics contributed — clean up and return the joined result.
	t.joinRegistry.Delete(joinKey)
	t.logger.Infof("kafka-stream/join-trigger: join complete — key=%q topics=%v", joinKey, contributingTopics)

	out := &Output{
		JoinResult: JoinedMessage{
			Messages: merged,
			JoinKey:  joinKey,
			Topics:   contributingTopics,
			JoinedAt: time.Now().UnixMilli(),
		},
		EventType: EventTypeJoined,
	}
	return out, EventTypeJoined, nil
}

// handleMessage decodes a raw Kafka message from the given topic and dispatches
// it through the join pipeline.
func (t *Trigger) handleMessage(session sarama.ConsumerGroupSession, msg *sarama.ConsumerMessage, topic string) {
	if t.logger.DebugEnabled() {
		t.logger.Debugf("kafka-stream/join-trigger: record received — topic=%s partition=%d offset=%d",
			topic, msg.Partition, msg.Offset)
	}

	// OTel trace propagation (mirrors other triggers in this workspace).
	eventId := fmt.Sprintf("%s#%d#%d", topic, msg.Partition, msg.Offset)
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
		// Poison-pill — always mark the offset so the consumer does not stall.
		t.logger.Errorf("kafka-stream/join-trigger: cannot decode JSON topic=%q offset=%d partition=%d — skipping (poison-pill): %v",
			topic, msg.Offset, msg.Partition, err)
		session.MarkMessage(msg, "")
		return
	}

	out, eventType, err := t.processPayload(topic, payload)
	if err != nil {
		t.logger.Errorf("kafka-stream/join-trigger: processPayload error topic=%q offset=%d: %v", topic, msg.Offset, err)
		session.MarkMessage(msg, "")
		return
	}

	if eventType == "" || out == nil {
		// Contribution recorded; this is not the completing message.
		// Always mark the offset — we cannot hold a Sarama session open
		// waiting for contributions from other topics.
		session.MarkMessage(msg, "")
		return
	}

	// This message completes the join.  Honour commitOnSuccess semantics.
	handlersOK := t.fireHandlers(ctx, eventId, eventType, out)
	if !t.settings.CommitOnSuccess || handlersOK {
		session.MarkMessage(msg, "")
	} else {
		t.logger.Warnf("kafka-stream/join-trigger: handler failed — not marking offset=%d topic=%q (will be redelivered after restart/rebalance)",
			msg.Offset, topic)
	}
}

// fireHandlers invokes each registered handler whose eventType matches.
// Returns true if every matching handler completed without error.
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
			t.logger.Errorf("kafka-stream/join-trigger: handler fire error eventType=%s: %v", eventType, err)
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

// missingTopics returns the topic names that have not yet contributed to the join.
func (t *Trigger) missingTopics(contributions map[string]interface{}) []string {
	var missing []string
	for _, topic := range t.topics {
		if _, ok := contributions[topic]; !ok {
			missing = append(missing, topic)
		}
	}
	return missing
}

// ---------------------------------------------------------------------------
// Settings validation
// ---------------------------------------------------------------------------

func validateSettings(s *Settings) error {
	if strings.TrimSpace(s.Topics) == "" {
		return fmt.Errorf("topics must not be empty")
	}
	topics := s.TopicList()
	if len(topics) < 2 {
		return fmt.Errorf("topics must list at least 2 comma-separated topics for a join; got %d", len(topics))
	}
	seen := make(map[string]bool, len(topics))
	for _, t := range topics {
		if seen[t] {
			return fmt.Errorf("duplicate topic %q in topics list", t)
		}
		seen[t] = true
	}
	if strings.TrimSpace(s.ConsumerGroup) == "" {
		return fmt.Errorf("consumerGroup must not be empty")
	}
	if strings.TrimSpace(s.JoinKeyField) == "" {
		return fmt.Errorf("joinKeyField must not be empty")
	}
	if s.JoinWindowMs <= 0 {
		return fmt.Errorf("joinWindowMs must be > 0, got %d", s.JoinWindowMs)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
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

// sanitizeGroupSuffix replaces characters that are invalid in a Kafka consumer
// group ID with hyphens.  Kafka allows [A-Za-z0-9._-].
func sanitizeGroupSuffix(topic string) string {
	var b strings.Builder
	for _, r := range topic {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Sarama ConsumerGroupHandler adapter
// ---------------------------------------------------------------------------

type consumerGroupHandler struct {
	t     *Trigger
	topic string
}

func (*consumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (*consumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

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
			h.t.handleMessage(session, msg, h.topic)
		case <-session.Context().Done():
			return nil
		}
	}
}
