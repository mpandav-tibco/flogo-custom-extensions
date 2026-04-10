package window

import (
	"errors"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// TumblingTimeWindow
// ---------------------------------------------------------------------------

// TumblingTimeWindow accumulates events until windowSize milliseconds of
// event-time have elapsed since the window opened, then emits an aggregate
// result and resets. Supports overflow back-pressure, deduplication,
// watermarks, late-event detection, and idle-timeout auto-close.
type TumblingTimeWindow struct {
	mu          sync.Mutex
	cfg         WindowConfig
	events      []float64
	windowStart time.Time
	watermark   time.Time       // highest event timestamp seen
	lastEventAt time.Time       // for idle-timeout tracking
	seen        map[string]bool // MessageID deduplication set

	// stats (monotonic counters)
	messagesIn      int64
	messagesLate    int64
	messagesDropped int64
	windowsClosed   int64
	lateInWindow    int64 // late-but-accepted count for current window
	droppedInWindow int64 // overflow-dropped count for current window
}

// NewTumblingTimeWindow creates a new time-based tumbling window.
func NewTumblingTimeWindow(cfg WindowConfig) *TumblingTimeWindow {
	return &TumblingTimeWindow{
		cfg:    cfg,
		events: make([]float64, 0, 64),
		// windowStart is intentionally left as zero — it is lazily set to the
		// first accepted event's timestamp in Add(). This ensures event-time
		// processing works correctly for historical replay where event
		// timestamps may be far in the past relative to wall-clock time.
		seen: make(map[string]bool),
	}
}

func (w *TumblingTimeWindow) Config() WindowConfig { return w.cfg }

func (w *TumblingTimeWindow) Snapshot() WindowSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return WindowSnapshot{
		Name:            w.cfg.Name,
		BufferSize:      int64(len(w.events)),
		Watermark:       w.watermark,
		LastEventAt:     w.lastEventAt,
		MessagesIn:      w.messagesIn,
		MessagesLate:    w.messagesLate,
		MessagesDropped: w.messagesDropped,
		WindowsClosed:   w.windowsClosed,
	}
}

func (w *TumblingTimeWindow) CheckIdle() (*WindowResult, bool) {
	if w.cfg.IdleTimeoutMs <= 0 {
		return nil, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.events) == 0 || w.lastEventAt.IsZero() {
		return nil, false
	}
	if time.Since(w.lastEventAt).Milliseconds() < w.cfg.IdleTimeoutMs {
		return nil, false
	}
	// Idle threshold crossed — emit partial result.
	result := w.closeWindow(w.lastEventAt)
	// Reset windowStart so the next Add() uses that event's timestamp as the
	// new window origin rather than the wall-clock time recorded in lastEventAt.
	w.windowStart = time.Time{}
	return result, true
}

func (w *TumblingTimeWindow) Add(event WindowEvent) (*WindowResult, bool, *LateEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.messagesIn++
	w.lastEventAt = time.Now()

	// --- 1. Deduplication ---
	if event.MessageID != "" && w.seen[event.MessageID] {
		return nil, false, nil, nil
	}

	// --- 2. Watermark advancement ---
	if event.Timestamp.After(w.watermark) {
		w.watermark = event.Timestamp
	}

	// --- 3. Late-event detection (time-based windows only) ---
	// An event is "late" if it is older than (watermark - AllowedLateness).
	if !w.watermark.IsZero() && event.Timestamp.Before(w.watermark) {
		lateness := w.watermark.Sub(event.Timestamp).Milliseconds()
		if lateness > w.cfg.AllowedLateness {
			w.messagesLate++
			l := &LateEvent{
				Event:      event,
				WindowName: w.cfg.Name,
				Watermark:  w.watermark,
				Reason:     "event older than watermark - allowedLateness",
			}
			return nil, false, l, nil
		}
		// Within tolerance — accept but count it.
		w.messagesLate++
		w.lateInWindow++
	}

	// --- 4. Overflow check ---
	if w.cfg.MaxBufferSize > 0 && int64(len(w.events)) >= w.cfg.MaxBufferSize {
		policy := effectiveOverflow(w.cfg)
		switch policy {
		case OverflowErrorStop:
			w.messagesDropped++
			return nil, false, nil, &WindowError{Event: event, Cause: errors.New("buffer full"), Window: w.cfg.Name}
		case OverflowDropNewest:
			w.messagesDropped++
			w.droppedInWindow++
			return nil, false, nil, nil
		case OverflowDropOldest:
			w.messagesDropped++
			w.droppedInWindow++
			if len(w.events) > 0 {
				w.events = w.events[1:]
			}
		}
	}

	// --- 5. Accept event ---
	if event.MessageID != "" {
		w.seen[event.MessageID] = true
	}
	w.events = append(w.events, event.Value)

	// --- 6. Window boundary check ---
	// Lazy initialisation: set windowStart to the first accepted event's
	// timestamp. This is correct for both real-time and historical replay.
	if w.windowStart.IsZero() {
		w.windowStart = event.Timestamp
	}
	elapsed := event.Timestamp.Sub(w.windowStart).Milliseconds()
	if elapsed >= w.cfg.Size {
		result := w.closeWindow(event.Timestamp)
		return result, true, nil, nil
	}
	return nil, false, nil, nil
}

// closeWindow must be called with w.mu held. It emits the result and resets state.
func (w *TumblingTimeWindow) closeWindow(closedAt time.Time) *WindowResult {
	result := &WindowResult{
		Value:          compute(w.cfg.Function, w.events),
		Count:          int64(len(w.events)),
		WindowName:     w.cfg.Name,
		ClosedAt:       closedAt,
		LateEventCount: w.lateInWindow,
		DroppedCount:   w.droppedInWindow,
	}
	w.events = w.events[:0]
	w.seen = make(map[string]bool)
	w.windowStart = closedAt
	w.lateInWindow = 0
	w.droppedInWindow = 0
	w.windowsClosed++
	return result
}

// ---------------------------------------------------------------------------
// TumblingCountWindow
// ---------------------------------------------------------------------------

// TumblingCountWindow accumulates exactly windowSize events, then emits an
// aggregate result and resets. Supports overflow back-pressure and
// deduplication.
type TumblingCountWindow struct {
	mu     sync.Mutex
	cfg    WindowConfig
	events []float64
	seen   map[string]bool

	lastEventAt     time.Time
	messagesIn      int64
	messagesDropped int64
	windowsClosed   int64
	droppedInWindow int64
}

// NewTumblingCountWindow creates a new count-based tumbling window.
func NewTumblingCountWindow(cfg WindowConfig) *TumblingCountWindow {
	cap := int(cfg.Size)
	if cap <= 0 {
		cap = 64
	}
	return &TumblingCountWindow{
		cfg:    cfg,
		events: make([]float64, 0, cap),
		seen:   make(map[string]bool),
	}
}

func (w *TumblingCountWindow) Config() WindowConfig { return w.cfg }

func (w *TumblingCountWindow) Snapshot() WindowSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return WindowSnapshot{
		Name:            w.cfg.Name,
		BufferSize:      int64(len(w.events)),
		LastEventAt:     w.lastEventAt,
		MessagesIn:      w.messagesIn,
		MessagesDropped: w.messagesDropped,
		WindowsClosed:   w.windowsClosed,
	}
}

func (w *TumblingCountWindow) CheckIdle() (*WindowResult, bool) {
	if w.cfg.IdleTimeoutMs <= 0 {
		return nil, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.events) == 0 || w.lastEventAt.IsZero() {
		return nil, false
	}
	if time.Since(w.lastEventAt).Milliseconds() < w.cfg.IdleTimeoutMs {
		return nil, false
	}
	result := &WindowResult{
		Value:        compute(w.cfg.Function, w.events),
		Count:        int64(len(w.events)),
		WindowName:   w.cfg.Name,
		ClosedAt:     w.lastEventAt,
		DroppedCount: w.droppedInWindow,
	}
	w.events = w.events[:0]
	w.seen = make(map[string]bool)
	w.droppedInWindow = 0
	w.windowsClosed++
	return result, true
}

func (w *TumblingCountWindow) Add(event WindowEvent) (*WindowResult, bool, *LateEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.messagesIn++
	w.lastEventAt = time.Now()

	// Deduplication
	if event.MessageID != "" && w.seen[event.MessageID] {
		return nil, false, nil, nil
	}

	// Overflow check
	if w.cfg.MaxBufferSize > 0 && int64(len(w.events)) >= w.cfg.MaxBufferSize {
		policy := effectiveOverflow(w.cfg)
		switch policy {
		case OverflowErrorStop:
			w.messagesDropped++
			return nil, false, nil, &WindowError{Event: event, Cause: errors.New("buffer full"), Window: w.cfg.Name}
		case OverflowDropNewest:
			w.messagesDropped++
			w.droppedInWindow++
			return nil, false, nil, nil
		case OverflowDropOldest:
			w.messagesDropped++
			w.droppedInWindow++
			if len(w.events) > 0 {
				w.events = w.events[1:]
			}
		}
	}

	if event.MessageID != "" {
		w.seen[event.MessageID] = true
	}
	w.events = append(w.events, event.Value)

	if int64(len(w.events)) >= w.cfg.Size {
		result := &WindowResult{
			Value:        compute(w.cfg.Function, w.events),
			Count:        int64(len(w.events)),
			WindowName:   w.cfg.Name,
			Key:          event.Key,
			ClosedAt:     event.Timestamp,
			DroppedCount: w.droppedInWindow,
		}
		w.events = w.events[:0]
		w.seen = make(map[string]bool)
		w.droppedInWindow = 0
		w.windowsClosed++
		return result, true, nil, nil
	}
	return nil, false, nil, nil
}
