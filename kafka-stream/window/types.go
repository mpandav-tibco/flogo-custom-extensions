package window

import (
	"fmt"
	"time"
)

// WindowType defines the type of streaming window
type WindowType string

const (
	WindowTumblingTime  WindowType = "TumblingTime"
	WindowTumblingCount WindowType = "TumblingCount"
	WindowSlidingTime   WindowType = "SlidingTime"
	WindowSlidingCount  WindowType = "SlidingCount"
)

// AggregateFunc defines the aggregation function applied over a window
type AggregateFunc string

const (
	FuncSum   AggregateFunc = "sum"
	FuncCount AggregateFunc = "count"
	FuncAvg   AggregateFunc = "avg"
	FuncMin   AggregateFunc = "min"
	FuncMax   AggregateFunc = "max"
)

// OverflowPolicy controls what happens when MaxBufferSize is exceeded.
type OverflowPolicy string

const (
	// OverflowDropOldest evicts the oldest buffered event to make room (default).
	OverflowDropOldest OverflowPolicy = "drop_oldest"
	// OverflowDropNewest discards the incoming event silently.
	OverflowDropNewest OverflowPolicy = "drop_newest"
	// OverflowErrorStop returns an error to the caller; no event is added.
	OverflowErrorStop OverflowPolicy = "error"
)

// WindowConfig holds the configuration for a window instance.
type WindowConfig struct {
	Name     string
	Type     WindowType
	Size     int64
	Function AggregateFunc

	// EventTimeField is the message field containing the event timestamp.
	// Accepted formats: Unix-ms int64/float64, or RFC-3339 string.
	// When empty, wall-clock time is used (fine for dev; not for production).
	EventTimeField string

	// AllowedLateness is how many milliseconds past the current watermark an
	// event is still accepted into the window. 0 = reject all late events.
	AllowedLateness int64

	// MaxBufferSize caps the number of values stored per window. 0 = unlimited.
	MaxBufferSize int64

	// OverflowPolicy decides what to do when MaxBufferSize is exceeded.
	// Defaults to OverflowDropOldest when MaxBufferSize > 0.
	OverflowPolicy OverflowPolicy

	// IdleTimeoutMs: close a keyed sub-window that receives no event for this
	// many milliseconds and emit its partial result. 0 = disabled.
	IdleTimeoutMs int64

	// MaxKeys caps the number of distinct keyed sub-windows. 0 = unlimited.
	MaxKeys int64
}

// WindowEvent is a single data point pushed into a window.
type WindowEvent struct {
	Value     float64
	Timestamp time.Time
	Key       string

	// MessageID is an optional idempotency key. A second Add call with the
	// same MessageID inside the same window is silently ignored.
	MessageID string
}

// WindowResult is emitted when a window closes (tumbling) or on every event (sliding).
type WindowResult struct {
	Value      float64
	Count      int64
	WindowName string
	Key        string
	ClosedAt   time.Time

	// LateEventCount is the number of events accepted within AllowedLateness.
	LateEventCount int64
	// DroppedCount is the number of events dropped due to overflow.
	DroppedCount int64
}

// LateEvent is produced for events whose timestamp falls outside AllowedLateness.
// The caller should route these to a dead-letter output.
type LateEvent struct {
	Event      WindowEvent
	WindowName string
	Watermark  time.Time
	Reason     string
}

// WindowError wraps a hard processing error together with the offending event.
type WindowError struct {
	Event  WindowEvent
	Cause  error
	Window string
}

func (e *WindowError) Error() string {
	return fmt.Sprintf("window %q: %v (key=%q ts=%s)", e.Window, e.Cause, e.Event.Key, e.Event.Timestamp)
}

// WindowSnapshot is a point-in-time observability view of a window store.
type WindowSnapshot struct {
	Name            string
	BufferSize      int64
	Watermark       time.Time
	LastEventAt     time.Time
	MessagesIn      int64
	MessagesLate    int64
	MessagesDropped int64
	WindowsClosed   int64
}

// WindowStore is the interface all window implementations must satisfy.
type WindowStore interface {
	// Add inserts a new event.
	//   result — non-nil when the window closes or (sliding) on every event.
	//   closed — true when a window boundary was crossed.
	//   late   — non-nil when the event was beyond AllowedLateness; route to DLQ.
	//   err    — non-nil on hard failure (overflow=error, cardinality limit, etc.)
	Add(event WindowEvent) (result *WindowResult, closed bool, late *LateEvent, err error)

	// Config returns this store's immutable configuration.
	Config() WindowConfig

	// Snapshot returns a read-only observability view without modifying state.
	Snapshot() WindowSnapshot

	// CheckIdle emits a partial result if the window has been idle longer than
	// IdleTimeoutMs. Returns (nil, false) when idle-timeout is disabled or not
	// yet reached.
	CheckIdle() (*WindowResult, bool)
}

// effectiveOverflow returns the configured overflow policy, defaulting to
// OverflowDropOldest when MaxBufferSize > 0 and no policy is set.
func effectiveOverflow(cfg WindowConfig) OverflowPolicy {
	if cfg.MaxBufferSize <= 0 {
		return OverflowDropOldest // irrelevant when limit is disabled
	}
	if cfg.OverflowPolicy == "" {
		return OverflowDropOldest
	}
	return cfg.OverflowPolicy
}

// compute applies an aggregation function to a slice of float64 values.
func compute(fn AggregateFunc, values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	switch fn {
	case FuncCount:
		return float64(len(values))
	case FuncSum:
		var s float64
		for _, v := range values {
			s += v
		}
		return s
	case FuncAvg:
		var s float64
		for _, v := range values {
			s += v
		}
		return s / float64(len(values))
	case FuncMin:
		m := values[0]
		for _, v := range values[1:] {
			if v < m {
				m = v
			}
		}
		return m
	case FuncMax:
		m := values[0]
		for _, v := range values[1:] {
			if v > m {
				m = v
			}
		}
		return m
	default:
		return 0
	}
}
