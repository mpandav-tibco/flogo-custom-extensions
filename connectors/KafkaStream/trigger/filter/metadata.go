package filter

import (
	"encoding/json"

	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/support/connection"
)

// EventType constants control when a handler fires.
const (
	// EventTypePass is the default — fires when the predicate evaluates to true.
	EventTypePass = "pass"
	// EventTypeEvalError fires when predicate evaluation fails (e.g. bad operator,
	// field coercion error, invalid regex). Use to route problem messages to a DLQ.
	EventTypeEvalError = "evalError"
	// EventTypeAll fires for both pass and evalError events.
	EventTypeAll = "all"
)

// Predicate is a single condition used in multi-predicate evaluation.
type Predicate struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// Settings hold trigger-level configuration settings defined at design time.
type Settings struct {
	// ── Kafka Connection ─────────────────────────────────────────────────────
	// Connection is the TIBCO Kafka shared connection (brokers, auth, TLS).
	Connection connection.Manager `md:"kafkaConnection,required"`
	// Topic is the Kafka topic to consume messages from.
	Topic string `md:"topic,required"`
	// ConsumerGroup is the Kafka consumer group ID.
	ConsumerGroup string `md:"consumerGroup,required"`
	// InitialOffset controls where consumption starts when no committed offset exists.
	// Accepted values: "newest" (default), "oldest".
	InitialOffset string `md:"initialOffset"`

	// ── Filter defaults (overridable per-handler) ─────────────────────────────
	Operator             string `md:"operator"`
	PredicateMode        string `md:"predicateMode"`
	PassThroughOnMissing bool   `md:"passThroughOnMissing"`

	// ── Deduplication (opt-in) ───────────────────────────────────────────────
	EnableDedup     bool   `md:"enableDedup"`
	DedupWindow     string `md:"dedupWindow"`     // Go duration string e.g. "10m"
	DedupMaxEntries int64  `md:"dedupMaxEntries"` // 0 = 100 000 default
	// DedupPersistPath is the file path for gob-encoded dedup state snapshots.
	// Persists seen event IDs across restarts so duplicates arriving after a
	// restart are still suppressed. Leave empty to disable.
	DedupPersistPath string `md:"dedupPersistPath"`
	// DedupPersistEveryN saves dedup state every N messages. 0 = shutdown-only.
	DedupPersistEveryN int64 `md:"dedupPersistEveryN"`
	// ── Rate limiting (opt-in) ───────────────────────────────────────────────
	RateLimitRPS       float64 `md:"rateLimitRPS"`       // 0 = disabled
	RateLimitBurst     int     `md:"rateLimitBurst"`     // 0 = same as RPS
	RateLimitMode      string  `md:"rateLimitMode"`      // "drop" (default) | "wait"
	RateLimitMaxWaitMs int64   `md:"rateLimitMaxWaitMs"` // used when mode=wait

	// ── Consumer group rebalance ──────────────────────────────────────────────
	// BalanceStrategy sets the Kafka consumer group rebalance strategy.
	// "roundrobin" (default) | "sticky" | "range"
	BalanceStrategy string `md:"balanceStrategy"`

	// CommitOnSuccess controls when the Kafka offset is committed.
	// true (default): only mark the offset as processed when all pass-handlers
	// complete without error — messages are redelivered on flow failure
	// (at-least-once semantics).
	// false: always mark the offset regardless of handler result (at-most-once).
	CommitOnSuccess bool `md:"commitOnSuccess"`

	// HandlerTimeoutMs is the maximum time (milliseconds) allowed for all handlers
	// to complete for a single message. 0 (default) means no timeout is enforced.
	// When exceeded the handler is treated as failed: the error is logged and,
	// if commitOnSuccess=true, the offset is not marked.
	HandlerTimeoutMs int64 `md:"handlerTimeoutMs"`
}

// HandlerSettings define the filter predicate for a specific handler (flow).
// Any non-empty field overrides the corresponding trigger-level Setting default.
type HandlerSettings struct {
	// EventType controls when this handler fires.
	// "pass" (default): fires when the predicate evaluates to true.
	// "evalError": fires when predicate evaluation fails (for DLQ routing).
	// "all": fires for both pass and evalError events.
	EventType string `md:"eventType"`

	// Single-predicate mode: set Field, Operator, Value.
	Field    string `md:"field"`
	Operator string `md:"operator"`
	Value    string `md:"value"`

	// Multi-predicate mode: set Predicates (JSON array of Predicate objects).
	// Example: [{"field":"status","operator":"eq","value":"200"},{"field":"region","operator":"eq","value":"us-east"}]
	Predicates    string `md:"predicates"`
	PredicateMode string `md:"predicateMode"` // "and" | "or"

	// DedupField is the message field whose value is used as the unique event ID.
	// Only active when enableDedup is true at the trigger level.
	DedupField string `md:"dedupField"`
}

// ParsedPredicates deserialises the Predicates JSON string.
// Returns nil when Predicates is empty.
func (hs *HandlerSettings) ParsedPredicates() ([]Predicate, error) {
	if hs.Predicates == "" {
		return nil, nil
	}
	var ps []Predicate
	if err := json.Unmarshal([]byte(hs.Predicates), &ps); err != nil {
		return nil, err
	}
	return ps, nil
}

// Output is emitted to the Flogo flow for every message that passes the filter
// (or causes an evaluation error when an evalError handler is configured).
// The trigger only fires the flow when the predicate evaluates to true (or on
// evaluation errors for evalError handlers) — the flow never sees silently
// blocked messages.
type Output struct {
	// Message is the decoded JSON payload of the Kafka message.
	Message map[string]interface{} `md:"message"`
	// Topic is the Kafka topic the message was consumed from.
	Topic string `md:"topic"`
	// Partition is the Kafka partition number.
	Partition int64 `md:"partition"`
	// Offset is the Kafka message offset.
	Offset int64 `md:"offset"`
	// Key is the Kafka message key (string representation).
	Key string `md:"key"`
	// EvalError is true when this output was produced due to a predicate
	// evaluation error (e.g. bad operator, field coercion failure, invalid regex).
	// Always false for normal pass events.
	EvalError bool `md:"evalError"`
	// EvalErrorReason describes why evaluation failed.
	// Empty string when EvalError is false.
	EvalErrorReason string `md:"evalErrorReason"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"message":         o.Message,
		"topic":           o.Topic,
		"partition":       o.Partition,
		"offset":          o.Offset,
		"key":             o.Key,
		"evalError":       o.EvalError,
		"evalErrorReason": o.EvalErrorReason,
	}
}

func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	o.Message, err = coerce.ToObject(values["message"])
	if err != nil {
		return err
	}
	o.Topic, err = coerce.ToString(values["topic"])
	if err != nil {
		return err
	}
	o.Partition, err = coerce.ToInt64(values["partition"])
	if err != nil {
		return err
	}
	o.Offset, err = coerce.ToInt64(values["offset"])
	if err != nil {
		return err
	}
	o.Key, err = coerce.ToString(values["key"])
	if err != nil {
		return err
	}
	o.EvalError, err = coerce.ToBool(values["evalError"])
	if err != nil {
		return err
	}
	o.EvalErrorReason, err = coerce.ToString(values["evalErrorReason"])
	return err
}
