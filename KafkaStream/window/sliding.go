package window

import (
	"errors"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// SlidingTimeWindow
// ---------------------------------------------------------------------------

// SlidingTimeWindow maintains a rolling buffer of events within the last
// windowSize milliseconds. It emits an updated aggregate result on every new
// event. Supports overflow back-pressure and deduplication.
type SlidingTimeWindow struct {
	mu     sync.Mutex
	cfg    WindowConfig
	events []WindowEvent
	seen   map[string]bool

	lastEventAt     time.Time
	messagesIn      int64
	messagesDropped int64
	windowsClosed   int64
}

// NewSlidingTimeWindow creates a new time-based sliding window.
func NewSlidingTimeWindow(cfg WindowConfig) *SlidingTimeWindow {
	return &SlidingTimeWindow{
		cfg:    cfg,
		events: make([]WindowEvent, 0, 128),
		seen:   make(map[string]bool),
	}
}

func (w *SlidingTimeWindow) Config() WindowConfig { return w.cfg }

func (w *SlidingTimeWindow) Snapshot() WindowSnapshot {
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

func (w *SlidingTimeWindow) SaveState() PersistedWindowState {
	w.mu.Lock()
	defer w.mu.Unlock()
	ts := make([]time.Time, len(w.events))
	vs := make([]float64, len(w.events))
	for i, e := range w.events {
		vs[i] = e.Value
		ts[i] = e.Timestamp
	}
	return PersistedWindowState{
		Name:       w.cfg.Name,
		Type:       string(w.cfg.Type),
		Values:     vs,
		Timestamps: ts,
		EventCount: w.messagesIn,
		SavedAt:    time.Now(),
	}
}

func (w *SlidingTimeWindow) LoadState(s PersistedWindowState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = make([]WindowEvent, len(s.Values))
	for i := range s.Values {
		ts := time.Time{}
		if i < len(s.Timestamps) {
			ts = s.Timestamps[i]
		}
		w.events[i] = WindowEvent{Value: s.Values[i], Timestamp: ts}
	}
	w.messagesIn = s.EventCount
}

func (w *SlidingTimeWindow) CheckIdle() (*WindowResult, bool) {
	// Sliding windows emit on every event so idle-timeout is not applicable.
	return nil, false
}

func (w *SlidingTimeWindow) Add(event WindowEvent) (*WindowResult, bool, *LateEvent, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.messagesIn++
	w.lastEventAt = time.Now()

	// Deduplication
	if event.MessageID != "" && w.seen[event.MessageID] {
		return nil, false, nil, nil
	}

	// Overflow check (before appending)
	if w.cfg.MaxBufferSize > 0 && int64(len(w.events)) >= w.cfg.MaxBufferSize {
		policy := effectiveOverflow(w.cfg)
		switch policy {
		case OverflowErrorStop:
			w.messagesDropped++
			return nil, false, nil, &WindowError{Event: event, Cause: errors.New("buffer full"), Window: w.cfg.Name}
		case OverflowDropNewest:
			w.messagesDropped++
			return nil, false, nil, nil
		case OverflowDropOldest:
			w.messagesDropped++
			if len(w.events) > 0 {
				w.events = w.events[1:]
			}
		}
	}

	if event.MessageID != "" {
		w.seen[event.MessageID] = true
	}
	w.events = append(w.events, event)

	// Evict events that have fallen outside the sliding window boundary,
	// releasing their MessageIDs from the dedup set to prevent unbounded growth.
	cutoff := event.Timestamp.Add(-time.Duration(w.cfg.Size) * time.Millisecond)
	start := 0
	for start < len(w.events) && w.events[start].Timestamp.Before(cutoff) {
		start++
	}
	if start > 0 {
		for _, evicted := range w.events[:start] {
			if evicted.MessageID != "" {
				delete(w.seen, evicted.MessageID)
			}
		}
		w.events = w.events[start:]
	}

	values := make([]float64, len(w.events))
	for i, e := range w.events {
		values[i] = e.Value
	}

	w.windowsClosed++
	result := &WindowResult{
		Value:      compute(w.cfg.Function, values),
		Count:      int64(len(values)),
		WindowName: w.cfg.Name,
		Key:        event.Key,
		ClosedAt:   event.Timestamp,
	}
	return result, true, nil, nil
}

// ---------------------------------------------------------------------------
// SlidingCountWindow
// ---------------------------------------------------------------------------

// SlidingCountWindow maintains a rolling buffer of the last windowSize events.
// It emits an updated aggregate result on every new event. Supports overflow
// back-pressure and deduplication.
// Events are stored as WindowEvent (not float64) so that MessageIDs of evicted
// events can be removed from the dedup set, preventing unbounded memory growth.
type SlidingCountWindow struct {
	mu     sync.Mutex
	cfg    WindowConfig
	events []WindowEvent
	seen   map[string]bool

	lastEventAt     time.Time
	messagesIn      int64
	messagesDropped int64
	windowsClosed   int64
}

// NewSlidingCountWindow creates a new count-based sliding window.
func NewSlidingCountWindow(cfg WindowConfig) *SlidingCountWindow {
	cap := int(cfg.Size)
	if cap <= 0 {
		cap = 64
	}
	return &SlidingCountWindow{
		cfg:    cfg,
		events: make([]WindowEvent, 0, cap),
		seen:   make(map[string]bool),
	}
}

func (w *SlidingCountWindow) Config() WindowConfig { return w.cfg }

func (w *SlidingCountWindow) Snapshot() WindowSnapshot {
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

func (w *SlidingCountWindow) SaveState() PersistedWindowState {
	w.mu.Lock()
	defer w.mu.Unlock()
	vs := make([]float64, len(w.events))
	ts := make([]time.Time, len(w.events))
	for i, e := range w.events {
		vs[i] = e.Value
		ts[i] = e.Timestamp
	}
	return PersistedWindowState{
		Name:       w.cfg.Name,
		Type:       string(w.cfg.Type),
		Values:     vs,
		Timestamps: ts,
		EventCount: w.messagesIn,
		SavedAt:    time.Now(),
	}
}

func (w *SlidingCountWindow) LoadState(s PersistedWindowState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = make([]WindowEvent, len(s.Values))
	for i := range s.Values {
		ts := time.Time{}
		if i < len(s.Timestamps) {
			ts = s.Timestamps[i]
		}
		w.events[i] = WindowEvent{Value: s.Values[i], Timestamp: ts}
	}
	w.messagesIn = s.EventCount
}

func (w *SlidingCountWindow) CheckIdle() (*WindowResult, bool) {
	// Sliding windows emit on every event so idle-timeout is not applicable.
	return nil, false
}

func (w *SlidingCountWindow) Add(event WindowEvent) (*WindowResult, bool, *LateEvent, error) {
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
			return nil, false, nil, nil
		case OverflowDropOldest:
			w.messagesDropped++
			if len(w.events) > 0 {
				// Remove the evicted event's MessageID from the dedup set so it can
				// be reused once the event has left the window.
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

	// Keep only the last windowSize events, releasing evicted MessageIDs from
	// the dedup set to prevent unbounded memory growth.
	if int64(len(w.events)) > w.cfg.Size {
		drop := int64(len(w.events)) - w.cfg.Size
		for _, evicted := range w.events[:drop] {
			if evicted.MessageID != "" {
				delete(w.seen, evicted.MessageID)
			}
		}
		w.events = w.events[drop:]
	}

	values := make([]float64, len(w.events))
	for i, e := range w.events {
		values[i] = e.Value
	}

	w.windowsClosed++
	result := &WindowResult{
		Value:      compute(w.cfg.Function, values),
		Count:      int64(len(values)),
		WindowName: w.cfg.Name,
		Key:        event.Key,
		ClosedAt:   event.Timestamp,
	}
	return result, true, nil, nil
}
