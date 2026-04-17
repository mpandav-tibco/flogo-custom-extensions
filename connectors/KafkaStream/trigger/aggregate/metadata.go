package aggregate

import (
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/support/connection"
)

// EventType constants used in HandlerSettings to choose which events a handler
// receives.
const (
	// EventTypeWindowClose is the default — the handler fires when a window
	// closes and emits its aggregate result.
	EventTypeWindowClose = "windowClose"
	// EventTypeLateEvent routes messages that arrive past the watermark +
	// allowedLateness, providing dead-letter-queue (DLQ) semantics.
	EventTypeLateEvent = "lateEvent"
	// EventTypeAll fires the handler for both window-close and late events.
	EventTypeAll = "all"
)

// Settings hold trigger-level configuration defined at design time.
type Settings struct {
	// ── Kafka Connection ─────────────────────────────────────────────────────
	// Connection is the TIBCO Kafka shared connection (brokers, auth, TLS).
	Connection    connection.Manager `md:"kafkaConnection,required"`
	Topic         string             `md:"topic,required"`
	ConsumerGroup string             `md:"consumerGroup,required"`
	// InitialOffset: "newest" (default) | "oldest"
	InitialOffset string `md:"initialOffset"`

	// ── Window configuration (required) ──────────────────────────────────────
	WindowName string `md:"windowName,required"`
	WindowType string `md:"windowType,required"` // TumblingTime|TumblingCount|SlidingTime|SlidingCount
	WindowSize int64  `md:"windowSize,required"` // ms for time-based; count for count-based
	Function   string `md:"function,required"`   // sum|count|avg|min|max

	// ── Message field mappings ────────────────────────────────────────────────
	ValueField     string `md:"valueField,required"` // numeric field to aggregate
	KeyField       string `md:"keyField"`            // for keyed (per-key) sub-windows
	EventTimeField string `md:"eventTimeField"`      // event-time field; wall-clock fallback
	MessageIDField string `md:"messageIDField"`      // for per-window message deduplication

	// ── Enterprise settings ───────────────────────────────────────────────────
	AllowedLateness int64  `md:"allowedLateness"` // ms; 0 = no late events accepted
	MaxBufferSize   int64  `md:"maxBufferSize"`   // 0 = unlimited
	OverflowPolicy  string `md:"overflowPolicy"`  // drop_oldest|drop_newest|error
	IdleTimeoutMs   int64  `md:"idleTimeoutMs"`   // 0 = disabled
	MaxKeys         int64  `md:"maxKeys"`         // 0 = unlimited

	// State persistence
	PersistPath   string `md:"persistPath"`   // file path for gob state snapshots; "" = disabled
	PersistEveryN int64  `md:"persistEveryN"` // snapshot every N messages; 0 = shutdown-only

	// ── Consumer group rebalance ──────────────────────────────────────────────
	// BalanceStrategy sets the Kafka consumer group rebalance strategy.
	// "roundrobin" (default) | "sticky" (recommended for aggregate — minimises
	// partition reassignments and reduces in-flight window state loss) | "range"
	BalanceStrategy string `md:"balanceStrategy"`

	// ── Offset commit ────────────────────────────────────────────────────────
	// CommitOnSuccess controls when the Kafka offset is marked as processed.
	// true (default): only mark when all handlers complete without error —
	// guarantees at-least-once redelivery if the downstream flow fails.
	// false: always mark regardless of handler result (at-most-once).
	CommitOnSuccess bool `md:"commitOnSuccess"`

	// HandlerTimeoutMs is the maximum time (milliseconds) allowed for all handlers
	// to complete for a single message. 0 (default) means no timeout is enforced.
	// When the deadline is exceeded the handler is treated as failed: the error is
	// logged and, if commitOnSuccess=true, the offset is not marked.
	HandlerTimeoutMs int64 `md:"handlerTimeoutMs"`

	// OnSchemaError controls behaviour when a message passes JSON decode but fails
	// schema validation (missing valueField, non-numeric value, window.Add error).
	// "skip" (default): mark the offset and discard the message — at-most-once
	// for schema errors but prevents the consumer from stalling indefinitely.
	// "retry": do not mark the offset; the message will be redelivered after a
	// restart or rebalance — safe only when the schema error is transient (e.g.
	// a bug fix is deployed) and the consumer is idempotent.
	OnSchemaError string `md:"onSchemaError"`
}

// HandlerSettings define which event type a particular handler (flow) should
// receive from this trigger.
type HandlerSettings struct {
	// EventType controls when this handler fires.
	// "windowClose" (default): fires when the window closes and results are ready.
	// "lateEvent": fires when a message is rejected as a late arrival (DLQ routing).
	// "all": fires for both window-close and late-event situations.
	EventType string `md:"eventType"`
}

// WindowResult carries the aggregate result for the closed (or updated) window.
type WindowResult struct {
	Result         float64 `md:"result"`
	Count          int64   `md:"count"`
	WindowName     string  `md:"windowName"`
	Key            string  `md:"key"`
	WindowClosed   bool    `md:"windowClosed"`
	DroppedCount   int64   `md:"droppedCount"`
	LateEventCount int64   `md:"lateEventCount"`
}

// Source carries the Kafka origin metadata and late-event DLQ fields.
type Source struct {
	Topic      string `md:"topic"`
	Partition  int64  `md:"partition"`
	Offset     int64  `md:"offset"`
	LateEvent  bool   `md:"lateEvent"`
	LateReason string `md:"lateReason"`
}

// Output is the data sent to the Flogo flow when this trigger fires.
type Output struct {
	WindowResult WindowResult `md:"windowResult"`
	Source       Source       `md:"source"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"windowResult": map[string]interface{}{
			"result":         o.WindowResult.Result,
			"count":          o.WindowResult.Count,
			"windowName":     o.WindowResult.WindowName,
			"key":            o.WindowResult.Key,
			"windowClosed":   o.WindowResult.WindowClosed,
			"droppedCount":   o.WindowResult.DroppedCount,
			"lateEventCount": o.WindowResult.LateEventCount,
		},
		"source": map[string]interface{}{
			"topic":      o.Source.Topic,
			"partition":  o.Source.Partition,
			"offset":     o.Source.Offset,
			"lateEvent":  o.Source.LateEvent,
			"lateReason": o.Source.LateReason,
		},
	}
}

func (o *Output) FromMap(values map[string]interface{}) error {
	wrRaw, err := coerce.ToObject(values["windowResult"])
	if err != nil {
		return err
	}
	o.WindowResult.Result, err = coerce.ToFloat64(wrRaw["result"])
	if err != nil {
		return err
	}
	o.WindowResult.Count, err = coerce.ToInt64(wrRaw["count"])
	if err != nil {
		return err
	}
	o.WindowResult.WindowName, err = coerce.ToString(wrRaw["windowName"])
	if err != nil {
		return err
	}
	o.WindowResult.Key, err = coerce.ToString(wrRaw["key"])
	if err != nil {
		return err
	}
	o.WindowResult.WindowClosed, err = coerce.ToBool(wrRaw["windowClosed"])
	if err != nil {
		return err
	}
	o.WindowResult.DroppedCount, err = coerce.ToInt64(wrRaw["droppedCount"])
	if err != nil {
		return err
	}
	o.WindowResult.LateEventCount, err = coerce.ToInt64(wrRaw["lateEventCount"])
	if err != nil {
		return err
	}

	srcRaw, err := coerce.ToObject(values["source"])
	if err != nil {
		return err
	}
	o.Source.Topic, err = coerce.ToString(srcRaw["topic"])
	if err != nil {
		return err
	}
	o.Source.Partition, err = coerce.ToInt64(srcRaw["partition"])
	if err != nil {
		return err
	}
	o.Source.Offset, err = coerce.ToInt64(srcRaw["offset"])
	if err != nil {
		return err
	}
	o.Source.LateEvent, err = coerce.ToBool(srcRaw["lateEvent"])
	if err != nil {
		return err
	}
	o.Source.LateReason, err = coerce.ToString(srcRaw["lateReason"])
	return err
}
