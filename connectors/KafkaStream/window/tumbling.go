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
	mu  sync.Mutex
	cfg WindowConfig
	// events stores the full WindowEvent (not just float64) so that per-event
	// MessageIDs can be tracked during overflow eviction and DropOldest can
	// correctly clean the dedup set. Consistent with the sliding window types.
	events      []WindowEvent
	windowStart time.Time
	watermark   time.Time       // highest event timestamp seen
	lastEventAt time.Time       // for idle-timeout tracking
	lastKey     string          // key of the most recent accepted event (for idle-close result)
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
		events: make([]WindowEvent, 0, 64),
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
		WindowStart:     w.windowStart,
		LastEventAt:     w.lastEventAt,
		MessagesIn:      w.messagesIn,
		MessagesLate:    w.messagesLate,
		MessagesDropped: w.messagesDropped,
		WindowsClosed:   w.windowsClosed,
	}
}

func (w *TumblingTimeWindow) SaveState() PersistedWindowState {
	w.mu.Lock()
	defer w.mu.Unlock()
	vs := make([]float64, len(w.events))
	ts := make([]time.Time, len(w.events))
	ids := make([]string, len(w.events))
	for i, e := range w.events {
		vs[i] = e.Value
		ts[i] = e.Timestamp
		ids[i] = e.MessageID
	}
	return PersistedWindowState{
		Name:            w.cfg.Name,
		Type:            string(w.cfg.Type),
		Values:          vs,
		Timestamps:      ts,
		EventMessageIDs: ids,
		Watermark:       w.watermark,
		WindowStart:     w.windowStart,
		EventCount:      w.messagesIn,
		SavedAt:         time.Now(),
	}
}

func (w *TumblingTimeWindow) LoadState(s PersistedWindowState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = make([]WindowEvent, len(s.Values))
	w.seen = make(map[string]bool)
	if len(s.EventMessageIDs) > 0 {
		// New format: per-event MessageIDs aligned with Values/Timestamps.
		for i := range s.Values {
			ts := time.Time{}
			if i < len(s.Timestamps) {
				ts = s.Timestamps[i]
			}
			msgID := ""
			if i < len(s.EventMessageIDs) {
				msgID = s.EventMessageIDs[i]
			}
			w.events[i] = WindowEvent{Value: s.Values[i], Timestamp: ts, MessageID: msgID}
			if msgID != "" {
				w.seen[msgID] = true
			}
		}
	} else {
		// Old format (pre-fix): Values only + window-level MessageIDs set.
		// Reconstruct events with no per-event IDs (backwards-compatible).
		for i, v := range s.Values {
			w.events[i] = WindowEvent{Value: v}
		}
		for k := range s.MessageIDs {
			w.seen[k] = true
		}
	}
	w.watermark = s.Watermark
	w.windowStart = s.WindowStart
	w.messagesIn = s.EventCount
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
	result := w.closeWindow(w.lastEventAt, w.lastKey)
	// Reset windowStart so the next Add() uses that event's timestamp as the
	// new window origin rather than the wall-clock time recorded in lastEventAt.
	w.windowStart = time.Time{}
	return result, true
}

func (w *TumblingTimeWindow) Add(event WindowEvent) (*WindowResult, bool, *LateEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.messagesIn++

	// --- 1. Deduplication ---
	// lastEventAt is updated after this check so that duplicate events do NOT
	// reset the idle timer. A window receiving only re-delivered duplicates
	// must still be closed by CheckIdle once the timeout elapses.
	if event.MessageID != "" && w.seen[event.MessageID] {
		return nil, false, nil, nil
	}

	w.lastEventAt = time.Now()

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
				// Remove the evicted event's MessageID from the dedup set so
				// a re-delivery of that event is accepted rather than lost.
				if w.events[0].MessageID != "" {
					delete(w.seen, w.events[0].MessageID)
				}
				w.events = w.events[1:]
			}
		}
	}

	// --- 5. Window boundary check (before accepting the event) ---
	// Lazy initialisation: set windowStart to the first accepted event's
	// timestamp. This is correct for both real-time and historical replay.
	if w.windowStart.IsZero() {
		w.windowStart = event.Timestamp
	}
	elapsed := event.Timestamp.Sub(w.windowStart).Milliseconds()
	if elapsed >= w.cfg.Size {
		// Close the current window WITHOUT the triggering event.
		// The boundary-crossing event is the first event of the NEW window,
		// not the last event of the expired one.
		result := w.closeWindow(event.Timestamp, event.Key)
		// Seed the new window with the triggering event.
		if event.MessageID != "" {
			w.seen[event.MessageID] = true
		}
		w.events = append(w.events, event)
		w.lastKey = event.Key
		return result, true, nil, nil
	}

	// --- 6. Accept event into the current window ---
	if event.MessageID != "" {
		w.seen[event.MessageID] = true
	}
	w.events = append(w.events, event)
	w.lastKey = event.Key
	return nil, false, nil, nil
}

// closeWindow must be called with w.mu held. It emits the result and resets state.
func (w *TumblingTimeWindow) closeWindow(closedAt time.Time, key string) *WindowResult {
	values := make([]float64, len(w.events))
	for i, e := range w.events {
		values[i] = e.Value
	}
	result := &WindowResult{
		Value:          compute(w.cfg.Function, values),
		Count:          int64(len(w.events)),
		WindowName:     w.cfg.Name,
		Key:            key,
		WindowStart:    w.windowStart,
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
	mu  sync.Mutex
	cfg WindowConfig
	// events stores the full WindowEvent (not just float64) so that per-event
	// MessageIDs can be tracked during overflow eviction. OverflowDropOldest
	// removes the evicted event's ID from seen, preventing a data-loss path
	// where a re-delivered evicted event is silently discarded.
	events      []WindowEvent
	seen        map[string]bool
	lastKey     string    // key of the most recent accepted event (for idle-close result)
	windowStart time.Time // timestamp of first accepted event in current window period

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
		events: make([]WindowEvent, 0, cap),
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

func (w *TumblingCountWindow) SaveState() PersistedWindowState {
	w.mu.Lock()
	defer w.mu.Unlock()
	vs := make([]float64, len(w.events))
	ts := make([]time.Time, len(w.events))
	ids := make([]string, len(w.events))
	for i, e := range w.events {
		vs[i] = e.Value
		ts[i] = e.Timestamp
		ids[i] = e.MessageID
	}
	return PersistedWindowState{
		Name:            w.cfg.Name,
		Type:            string(w.cfg.Type),
		Values:          vs,
		Timestamps:      ts,
		EventMessageIDs: ids,
		EventCount:      w.messagesIn,
		WindowStart:     w.windowStart,
		SavedAt:         time.Now(),
	}
}

func (w *TumblingCountWindow) LoadState(s PersistedWindowState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = make([]WindowEvent, len(s.Values))
	w.seen = make(map[string]bool)
	if len(s.EventMessageIDs) > 0 {
		// New format: per-event MessageIDs aligned with Values/Timestamps.
		for i := range s.Values {
			ts := time.Time{}
			if i < len(s.Timestamps) {
				ts = s.Timestamps[i]
			}
			msgID := ""
			if i < len(s.EventMessageIDs) {
				msgID = s.EventMessageIDs[i]
			}
			w.events[i] = WindowEvent{Value: s.Values[i], Timestamp: ts, MessageID: msgID}
			if msgID != "" {
				w.seen[msgID] = true
			}
		}
	} else {
		// Old format (pre-fix): Values only + window-level MessageIDs set.
		for i, v := range s.Values {
			w.events[i] = WindowEvent{Value: v}
		}
		for k := range s.MessageIDs {
			w.seen[k] = true
		}
	}
	w.messagesIn = s.EventCount
	w.windowStart = s.WindowStart
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
	values := make([]float64, len(w.events))
	for i, e := range w.events {
		values[i] = e.Value
	}
	result := &WindowResult{
		Value:        compute(w.cfg.Function, values),
		Count:        int64(len(w.events)),
		WindowName:   w.cfg.Name,
		Key:          w.lastKey,
		ClosedAt:     w.lastEventAt,
		WindowStart:  w.windowStart,
		DroppedCount: w.droppedInWindow,
	}
	w.events = w.events[:0]
	w.seen = make(map[string]bool)
	w.windowStart = time.Time{}
	w.droppedInWindow = 0
	w.windowsClosed++
	return result, true
}

func (w *TumblingCountWindow) Add(event WindowEvent) (*WindowResult, bool, *LateEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.messagesIn++

	// Deduplication — lastEventAt is updated after this check so that
	// duplicate events do not prevent idle-timeout from firing.
	if event.MessageID != "" && w.seen[event.MessageID] {
		return nil, false, nil, nil
	}

	w.lastEventAt = time.Now()

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
				// Remove the evicted event's MessageID from the dedup set so a
				// re-delivery of that event is accepted rather than lost.
				if w.events[0].MessageID != "" {
					delete(w.seen, w.events[0].MessageID)
				}
				w.events = w.events[1:]
			}
		}
	}

	if event.MessageID != "" {
		w.seen[event.MessageID] = true
	}
	w.events = append(w.events, event)
	w.lastKey = event.Key

	// Lazy initialisation: set windowStart to the first accepted event's
	// timestamp so downstream flows can reconstruct the window period.
	if w.windowStart.IsZero() {
		w.windowStart = event.Timestamp
	}

	if int64(len(w.events)) >= w.cfg.Size {
		values := make([]float64, len(w.events))
		for i, e := range w.events {
			values[i] = e.Value
		}
		result := &WindowResult{
			Value:        compute(w.cfg.Function, values),
			Count:        int64(len(w.events)),
			WindowName:   w.cfg.Name,
			Key:          event.Key,
			ClosedAt:     event.Timestamp,
			WindowStart:  w.windowStart,
			DroppedCount: w.droppedInWindow,
		}
		w.events = w.events[:0]
		w.seen = make(map[string]bool)
		w.windowStart = time.Time{}
		w.droppedInWindow = 0
		w.windowsClosed++
		return result, true, nil, nil
	}
	return nil, false, nil, nil
}
