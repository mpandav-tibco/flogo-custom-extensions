// Package split provides a Flogo trigger that consumes messages from a Kafka
// topic and routes each message to one or more handler branches based on
// content-based predicates — a classic stream-split / content-based-routing
// pattern.  It is the trigger-native equivalent of routing tables in Kafka
// Streams topologies.
package split

import (
	"encoding/json"

	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/support/connection"
)

// EventType constants used in HandlerSettings to choose which events a handler receives.
const (
	// EventTypeMatched is the default — fires when this handler's predicates match
	// the incoming message.  Each matched handler represents one routing branch.
	EventTypeMatched = "matched"

	// EventTypeUnmatched fires when no matched-type handler's predicates passed.
	// Use to implement a catch-all / dead-letter-queue branch for unrouted messages.
	EventTypeUnmatched = "unmatched"

	// EventTypeEvalError fires when predicate evaluation fails for any handler
	// (e.g. bad operator, field coercion error, invalid regex).
	// Use to route structurally problematic messages to an error DLQ.
	EventTypeEvalError = "evalError"

	// EventTypeAll fires for every message regardless of the routing outcome.
	// The handler acts as a tap — useful for logging, monitoring, or audit trails.
	// Predicate fields are ignored for all-type handlers.
	EventTypeAll = "all"
)

// RoutingMode constants control how the trigger distributes matched messages.
const (
	// RoutingModeFirstMatch routes to the first handler (by priority order) whose
	// predicates match.  Behaves like an if-else / switch-case chain — only one
	// branch fires per message.  This is the default.
	RoutingModeFirstMatch = "first-match"

	// RoutingModeAllMatch routes to ALL handlers whose predicates match.
	// Enables fan-out — every matching branch receives the same message.
	// Useful when a message legitimately belongs to multiple categories.
	RoutingModeAllMatch = "all-match"
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
	Connection    connection.Manager `md:"kafkaConnection,required"`
	Topic         string             `md:"topic,required"`
	ConsumerGroup string             `md:"consumerGroup,required"`
	// InitialOffset controls where consumption starts when no committed offset
	// exists for the consumer group. "newest" (default) | "oldest".
	InitialOffset string `md:"initialOffset"`

	// ── Routing ───────────────────────────────────────────────────────────────
	// RoutingMode controls whether the trigger routes to the first matching
	// handler ("first-match", default) or to all matching handlers ("all-match").
	RoutingMode string `md:"routingMode"`

	// ── Consumer group rebalance ──────────────────────────────────────────────
	// BalanceStrategy sets the Kafka consumer group rebalance strategy.
	// "roundrobin" (default) | "sticky" | "range"
	BalanceStrategy string `md:"balanceStrategy"`

	// ── Offset commit ────────────────────────────────────────────────────────
	// CommitOnSuccess controls when the Kafka offset is marked as processed.
	// true (default): only mark when all handlers complete without error —
	// guarantees at-least-once redelivery if the downstream flow fails.
	// false: always mark regardless of handler result (at-most-once).
	CommitOnSuccess bool `md:"commitOnSuccess"`

	// HandlerTimeoutMs is the maximum time (milliseconds) allowed for each
	// individual handler invocation.  0 means no per-handler timeout.
	HandlerTimeoutMs int64 `md:"handlerTimeoutMs"`

	// MessageTimeoutMs is the maximum total time (milliseconds) allowed for ALL
	// handler invocations combined for a single message (matched + unmatched +
	// evalError + tap handlers). In all-match mode multiple handlers fire
	// sequentially; without this cap, per-message latency can reach
	// N×HandlerTimeoutMs, risking a Kafka session timeout and unnecessary
	// consumer-group rebalance. 0 means no per-message cap (default).
	// Recommended: set to HandlerTimeoutMs × (expected max matching handlers).
	MessageTimeoutMs int64 `md:"messageTimeoutMs"`
}

// HandlerSettings define the routing predicate for a specific branch handler (flow).
type HandlerSettings struct {
	// EventType controls when this handler fires.
	// "matched" (default): fires when this handler's predicates match the message.
	// "unmatched": fires when no matched-type handler matched (catch-all/DLQ).
	// "evalError": fires when predicate evaluation fails (error DLQ).
	// "all": fires for every message regardless of routing outcome (tap/monitor).
	EventType string `md:"eventType"`

	// ── Single-predicate mode: set Field, Operator, Value ────────────────────
	Field    string `md:"field"`
	Operator string `md:"operator"`
	Value    string `md:"value"`

	// ── Multi-predicate mode: set Predicates (JSON array of Predicate) ───────
	// Example: [{"field":"status","operator":"eq","value":"ok"},{"field":"region","operator":"eq","value":"us-east"}]
	Predicates    string `md:"predicates"`
	PredicateMode string `md:"predicateMode"` // "and" (default) | "or"

	// Priority is the evaluation order for first-match routing mode.
	// Handlers with lower Priority values are evaluated first.
	// Handlers with equal Priority are evaluated in registration order.
	// Ignored for unmatched / evalError / all event types.
	Priority int `md:"priority"`
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

// Output is emitted to the Flogo flow for each routed handler invocation.
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
	// MatchedHandlerName is the name of the handler branch that was matched.
	// Empty string for unmatched, evalError, and all-type handler invocations.
	MatchedHandlerName string `md:"matchedHandlerName"`
	// RoutingMode is the routing mode that was applied ("first-match" | "all-match").
	RoutingMode string `md:"routingMode"`
	// EvalError is true when this output was produced due to a predicate
	// evaluation error.  Always false for matched/unmatched events.
	EvalError bool `md:"evalError"`
	// EvalErrorReason describes why evaluation failed.
	// Empty string when EvalError is false.
	EvalErrorReason string `md:"evalErrorReason"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"message":            o.Message,
		"topic":              o.Topic,
		"partition":          o.Partition,
		"offset":             o.Offset,
		"key":                o.Key,
		"matchedHandlerName": o.MatchedHandlerName,
		"routingMode":        o.RoutingMode,
		"evalError":          o.EvalError,
		"evalErrorReason":    o.EvalErrorReason,
	}
}

func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	if o.Message, err = coerce.ToObject(values["message"]); err != nil {
		return err
	}
	if o.Topic, err = coerce.ToString(values["topic"]); err != nil {
		return err
	}
	if o.Partition, err = coerce.ToInt64(values["partition"]); err != nil {
		return err
	}
	if o.Offset, err = coerce.ToInt64(values["offset"]); err != nil {
		return err
	}
	if o.Key, err = coerce.ToString(values["key"]); err != nil {
		return err
	}
	if o.MatchedHandlerName, err = coerce.ToString(values["matchedHandlerName"]); err != nil {
		return err
	}
	if o.RoutingMode, err = coerce.ToString(values["routingMode"]); err != nil {
		return err
	}
	if o.EvalError, err = coerce.ToBool(values["evalError"]); err != nil {
		return err
	}
	if o.EvalErrorReason, err = coerce.ToString(values["evalErrorReason"]); err != nil {
		return err
	}
	return nil
}
