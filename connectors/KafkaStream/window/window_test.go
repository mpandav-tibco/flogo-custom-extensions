package window_test

import (
	"testing"
	"time"

	"github.com/milindpandav/flogo-extensions/kafkastream/window"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func event(value float64, ts time.Time) window.WindowEvent {
	return window.WindowEvent{Value: value, Timestamp: ts, Key: ""}
}

func keyedEvent(value float64, key string, ts time.Time) window.WindowEvent {
	return window.WindowEvent{Value: value, Timestamp: ts, Key: key}
}

// ─── TumblingCountWindow ──────────────────────────────────────────────────────

func TestTumblingCountWindow_Sum(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 3, Function: window.FuncSum,
	})
	base := time.Now()
	for i, v := range []float64{10, 20} {
		_, closed, _, err := w.Add(event(v, base.Add(time.Duration(i)*time.Second)))
		require.NoError(t, err)
		assert.False(t, closed, "window must not close at event %d", i+1)
	}
	r, closed, _, err := w.Add(event(30, base.Add(2*time.Second)))
	require.NoError(t, err)
	assert.True(t, closed)
	require.NotNil(t, r)
	assert.Equal(t, 60.0, r.Value)
	assert.Equal(t, int64(3), r.Count)
}

func TestTumblingCountWindow_Avg(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 4, Function: window.FuncAvg,
	})
	base := time.Now()
	vals := []float64{10, 20, 30, 40}
	var r *window.WindowResult
	for i, v := range vals {
		var err error
		r, _, _, err = w.Add(event(v, base.Add(time.Duration(i)*time.Second)))
		require.NoError(t, err)
	}
	assert.Equal(t, 25.0, r.Value) // (10+20+30+40)/4
	assert.Equal(t, int64(4), r.Count)
}

func TestTumblingCountWindow_Min(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 3, Function: window.FuncMin,
	})
	base := time.Now()
	vals := []float64{50, 10, 30}
	var r *window.WindowResult
	for i, v := range vals {
		var err error
		r, _, _, err = w.Add(event(v, base.Add(time.Duration(i)*time.Second)))
		require.NoError(t, err)
	}
	assert.Equal(t, 10.0, r.Value)
}

func TestTumblingCountWindow_Max(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 3, Function: window.FuncMax,
	})
	base := time.Now()
	vals := []float64{50, 10, 30}
	var r *window.WindowResult
	for i, v := range vals {
		var err error
		r, _, _, err = w.Add(event(v, base.Add(time.Duration(i)*time.Second)))
		require.NoError(t, err)
	}
	assert.Equal(t, 50.0, r.Value)
}

func TestTumblingCountWindow_Count_Function(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 3, Function: window.FuncCount,
	})
	base := time.Now()
	vals := []float64{100, 200, 300}
	var r *window.WindowResult
	for i, v := range vals {
		var err error
		r, _, _, err = w.Add(event(v, base.Add(time.Duration(i)*time.Second)))
		require.NoError(t, err)
	}
	assert.Equal(t, 3.0, r.Value)
	assert.Equal(t, int64(3), r.Count)
}

func TestTumblingCountWindow_ResetsAfterClose(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 2, Function: window.FuncSum,
	})
	base := time.Now()
	w.Add(event(10, base))
	r1, c1, _, _ := w.Add(event(20, base.Add(time.Second)))
	assert.True(t, c1)
	assert.Equal(t, 30.0, r1.Value)
	// Second window must start fresh
	w.Add(event(5, base.Add(2*time.Second)))
	r2, c2, _, _ := w.Add(event(5, base.Add(3*time.Second)))
	assert.True(t, c2)
	assert.Equal(t, 10.0, r2.Value)
}

func TestTumblingCountWindow_KeyPreserved(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 1, Function: window.FuncSum,
	})
	r, closed, _, err := w.Add(keyedEvent(42, "device-1", time.Now()))
	require.NoError(t, err)
	assert.True(t, closed)
	assert.Equal(t, "device-1", r.Key)
}

// ─── TumblingTimeWindow ───────────────────────────────────────────────────────
//
// Implementation note: the event that crosses the window boundary IS included in
// the closed window's aggregate. After close, the buffer resets to empty and
// windowStart is set to the trigger event's timestamp.

func TestTumblingTimeWindow_Sum_IncludesTriggerEvent(t *testing.T) {
	// Window size = 5000ms. Events at t0, t+1s, t+2s, t+6s.
	// t+6s crosses the 5s boundary; all 4 events are in the buffer → sum=100.
	w := window.NewTumblingTimeWindow(window.WindowConfig{
		Type: window.WindowTumblingTime, Size: 5000, Function: window.FuncSum,
	})
	base := time.Now()
	for i, v := range []float64{10, 20, 30} {
		_, closed, _, err := w.Add(event(v, base.Add(time.Duration(i)*time.Second)))
		require.NoError(t, err)
		assert.False(t, closed, "should not close at event %d", i+1)
	}
	r, closed, _, err := w.Add(event(40, base.Add(6*time.Second)))
	require.NoError(t, err)
	assert.True(t, closed)
	// All 4 events (including the triggering one) are aggregated
	assert.Equal(t, 100.0, r.Value) // 10+20+30+40
	assert.Equal(t, int64(4), r.Count)
}

func TestTumblingTimeWindow_Avg(t *testing.T) {
	// Window size = 3000ms. Events: [10@t0, 30@t+1s], trigger at t+4s.
	// Buffer at close = [10, 30, 99] → avg = 46.33...
	w := window.NewTumblingTimeWindow(window.WindowConfig{
		Type: window.WindowTumblingTime, Size: 3000, Function: window.FuncAvg,
	})
	base := time.Now()
	w.Add(event(10, base))
	w.Add(event(30, base.Add(time.Second)))
	r, closed, _, err := w.Add(event(99, base.Add(4*time.Second)))
	require.NoError(t, err)
	assert.True(t, closed)
	expected := (10.0 + 30.0 + 99.0) / 3.0
	assert.InDelta(t, expected, r.Value, 0.001)
	assert.Equal(t, int64(3), r.Count)
}

func TestTumblingTimeWindow_Min_Max(t *testing.T) {
	// Use a non-zero trigger event value so min isn't accidentally 0.
	base := time.Now()
	wMin := window.NewTumblingTimeWindow(window.WindowConfig{Type: window.WindowTumblingTime, Size: 3000, Function: window.FuncMin})
	wMax := window.NewTumblingTimeWindow(window.WindowConfig{Type: window.WindowTumblingTime, Size: 3000, Function: window.FuncMax})
	ts := base
	for _, v := range []float64{50, 10, 30} {
		wMin.Add(event(v, ts))
		wMax.Add(event(v, ts))
		ts = ts.Add(500 * time.Millisecond)
	}
	// Trigger with value 15 (non-zero, not smallest or largest)
	trigger := ts.Add(4 * time.Second)
	rMin, _, _, _ := wMin.Add(event(15, trigger))
	rMax, _, _, _ := wMax.Add(event(15, trigger))
	// Buffer = [50, 10, 30, 15]
	assert.Equal(t, 10.0, rMin.Value)
	assert.Equal(t, 50.0, rMax.Value)
}

func TestTumblingTimeWindow_ResetsAfterClose(t *testing.T) {
	// Implementation: trigger event included in closing window; buffer fully reset after.
	// Window size = 1000ms.
	w := window.NewTumblingTimeWindow(window.WindowConfig{
		Type: window.WindowTumblingTime, Size: 1000, Function: window.FuncSum,
	})
	base := time.Now()
	w.Add(event(100, base))
	// t+2s crosses boundary; buffer=[100, 999] → sum=1099
	r1, c1, _, _ := w.Add(event(999, base.Add(2*time.Second)))
	assert.True(t, c1)
	assert.Equal(t, 1099.0, r1.Value)
	// New window: buffer reset, windowStart=t+2s.
	// Next event at t+4s: elapsed=2s >= 1s → closes immediately.
	// Buffer = [1] → sum=1
	r2, c2, _, _ := w.Add(event(1, base.Add(4*time.Second)))
	assert.True(t, c2)
	assert.Equal(t, 1.0, r2.Value)
}

// TestTumblingTimeWindow_HistoricalReplay verifies that event-time windows work
// correctly when events carry timestamps far in the past (e.g. historical replay).
// Previously, windowStart was initialised to time.Now() which caused elapsed to
// always be negative, preventing the window from ever closing.
func TestTumblingTimeWindow_HistoricalReplay(t *testing.T) {
	w := window.NewTumblingTimeWindow(window.WindowConfig{
		Type: window.WindowTumblingTime, Size: 5000, Function: window.FuncSum,
	})
	// Timestamps anchored years in the past — far behind wall-clock.
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, v := range []float64{10, 20, 30} {
		_, closed, _, err := w.Add(event(v, base.Add(time.Duration(i)*time.Second)))
		require.NoError(t, err)
		assert.False(t, closed, "window must not close at event %d", i+1)
	}
	// base+6s crosses the 5s window boundary in event-time.
	r, closed, _, err := w.Add(event(40, base.Add(6*time.Second)))
	require.NoError(t, err)
	assert.True(t, closed, "event-time window must close when event-time boundary is crossed")
	require.NotNil(t, r)
	assert.Equal(t, 100.0, r.Value) // 10+20+30+40
	assert.Equal(t, int64(4), r.Count)
}

// ─── SlidingCountWindow ───────────────────────────────────────────────────────

func TestSlidingCountWindow_Sum(t *testing.T) {
	w := window.NewSlidingCountWindow(window.WindowConfig{
		Type: window.WindowSlidingCount, Size: 3, Function: window.FuncSum,
	})
	base := time.Now()
	// rolling sum of last 3: [10]=10, [10,20]=30, [10,20,30]=60, [20,30,40]=90, [30,40,50]=120
	expected := []float64{10, 30, 60, 90, 120}
	vals := []float64{10, 20, 30, 40, 50}
	for i, v := range vals {
		r, closed, _, err := w.Add(event(v, base.Add(time.Duration(i)*time.Second)))
		require.NoError(t, err)
		assert.True(t, closed, "sliding window must always emit at event %d", i+1)
		assert.Equal(t, expected[i], r.Value, "event %d", i+1)
	}
}

func TestSlidingCountWindow_Min(t *testing.T) {
	w := window.NewSlidingCountWindow(window.WindowConfig{
		Type: window.WindowSlidingCount, Size: 3, Function: window.FuncMin,
	})
	base := time.Now()
	vals := []float64{50, 10, 30, 5, 20}
	// min of last 3: 50, 10, 10, 5, 5
	expected := []float64{50, 10, 10, 5, 5}
	for i, v := range vals {
		r, _, _, err := w.Add(event(v, base.Add(time.Duration(i)*time.Second)))
		require.NoError(t, err)
		assert.Equal(t, expected[i], r.Value, "event %d", i+1)
	}
}

func TestSlidingCountWindow_KeyPreserved(t *testing.T) {
	w := window.NewSlidingCountWindow(window.WindowConfig{
		Type: window.WindowSlidingCount, Size: 2, Function: window.FuncSum,
	})
	r, _, _, err := w.Add(keyedEvent(10, "sensor-X", time.Now()))
	require.NoError(t, err)
	assert.Equal(t, "sensor-X", r.Key)
}

// ─── SlidingTimeWindow ────────────────────────────────────────────────────────

func TestSlidingTimeWindow_Sum_EmitsOnEveryEvent(t *testing.T) {
	// Window size = 3000ms
	w := window.NewSlidingTimeWindow(window.WindowConfig{
		Type: window.WindowSlidingTime, Size: 3000, Function: window.FuncSum,
	})
	base := time.Now()
	// Event at t=0: window=[10@0] -> sum=10
	r1, c1, _, err := w.Add(event(10, base))
	require.NoError(t, err)
	assert.True(t, c1)
	assert.Equal(t, 10.0, r1.Value)
	// Event at t=1s: window=[10@0, 20@1] -> sum=30
	r2, c2, _, _ := w.Add(event(20, base.Add(time.Second)))
	assert.True(t, c2)
	assert.Equal(t, 30.0, r2.Value)
	// Event at t=4s: event@0 evicted (>3s) -> window=[20@1, 30@4] -> sum=50
	r3, c3, _, _ := w.Add(event(30, base.Add(4*time.Second)))
	assert.True(t, c3)
	assert.Equal(t, 50.0, r3.Value)
}

func TestSlidingTimeWindow_Avg(t *testing.T) {
	w := window.NewSlidingTimeWindow(window.WindowConfig{
		Type: window.WindowSlidingTime, Size: 5000, Function: window.FuncAvg,
	})
	base := time.Now()
	w.Add(event(10, base))
	r, closed, _, err := w.Add(event(30, base.Add(2*time.Second)))
	require.NoError(t, err)
	assert.True(t, closed)
	assert.Equal(t, 20.0, r.Value) // (10+30)/2
}

func TestSlidingTimeWindow_EvictsStaleEvents(t *testing.T) {
	// Eviction uses strict Before(cutoff), so an event exactly AT the cutoff boundary
	// is NOT evicted. Use t+4s as trigger so event@t+1s (cutoff=t+2s) IS evicted.
	w := window.NewSlidingTimeWindow(window.WindowConfig{
		Type: window.WindowSlidingTime, Size: 2000, Function: window.FuncCount,
	})
	base := time.Now()
	w.Add(event(1, base))                  // t=0
	w.Add(event(1, base.Add(time.Second))) // t=1s
	// t=4s: cutoff = 4s-2s = 2s. Events@0s and@1s are Before(2s) → both evicted.
	r, _, _, _ := w.Add(event(1, base.Add(4*time.Second)))
	assert.Equal(t, 1.0, r.Value) // only the t=4s event remains
}

// ─── WindowConfig round-trip ──────────────────────────────────────────────────

func TestWindowConfig_RoundTrip(t *testing.T) {
	cfg := window.WindowConfig{
		Name:     "my-window",
		Type:     window.WindowTumblingCount,
		Size:     100,
		Function: window.FuncSum,
	}
	w := window.NewTumblingCountWindow(cfg)
	assert.Equal(t, cfg, w.Config())
}

// ─── Enterprise: Deduplication ───────────────────────────────────────────────

func TestTumblingCount_Deduplication(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 3, Function: window.FuncSum,
	})
	base := time.Now()
	// Add msg-1 twice → second should be silently ignored
	e1 := window.WindowEvent{Value: 10, Timestamp: base, MessageID: "msg-1"}
	e2 := window.WindowEvent{Value: 10, Timestamp: base.Add(time.Second), MessageID: "msg-1"} // duplicate
	e3 := window.WindowEvent{Value: 20, Timestamp: base.Add(2 * time.Second), MessageID: "msg-2"}
	e4 := window.WindowEvent{Value: 30, Timestamp: base.Add(3 * time.Second), MessageID: "msg-3"}

	_, closed, _, err := w.Add(e1)
	require.NoError(t, err)
	assert.False(t, closed)

	// Duplicate — must be silently ignored (window should NOT close yet)
	_, closed, _, err = w.Add(e2)
	require.NoError(t, err)
	assert.False(t, closed, "duplicate must not advance window")

	_, closed, _, err = w.Add(e3)
	require.NoError(t, err)
	assert.False(t, closed)

	// Third distinct event closes the window (size=3)
	r, closed, _, err := w.Add(e4)
	require.NoError(t, err)
	assert.True(t, closed)
	require.NotNil(t, r)
	// Only 3 distinct messages counted: 10+20+30=60
	assert.Equal(t, 60.0, r.Value)
	assert.Equal(t, int64(3), r.Count)
}

// ─── Enterprise: Overflow – drop_oldest ──────────────────────────────────────

func TestTumblingCount_Overflow_DropOldest(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 5, Function: window.FuncSum,
		MaxBufferSize:  3,
		OverflowPolicy: window.OverflowDropOldest,
	})
	base := time.Now()
	// Fill buffer to MaxBufferSize
	for i, v := range []float64{10, 20, 30} {
		_, _, _, err := w.Add(window.WindowEvent{Value: v, Timestamp: base.Add(time.Duration(i) * time.Second)})
		require.NoError(t, err)
	}
	// 4th event: buffer full (3), drop_oldest evicts 10 → buffer=[20, 30, 40]
	_, _, _, err := w.Add(window.WindowEvent{Value: 40, Timestamp: base.Add(3 * time.Second)})
	require.NoError(t, err)

	snap := w.Snapshot()
	assert.Equal(t, int64(3), snap.BufferSize)
	assert.Equal(t, int64(1), snap.MessagesDropped)
}

// ─── Enterprise: Overflow – drop_newest ──────────────────────────────────────

func TestTumblingCount_Overflow_DropNewest(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 5, Function: window.FuncSum,
		MaxBufferSize:  2,
		OverflowPolicy: window.OverflowDropNewest,
	})
	base := time.Now()
	for i, v := range []float64{10, 20} {
		_, _, _, err := w.Add(window.WindowEvent{Value: v, Timestamp: base.Add(time.Duration(i) * time.Second)})
		require.NoError(t, err)
	}
	// 3rd event must be dropped silently (buffer stays at 2)
	r, closed, _, err := w.Add(window.WindowEvent{Value: 99, Timestamp: base.Add(2 * time.Second)})
	require.NoError(t, err)
	assert.False(t, closed)
	assert.Nil(t, r)

	snap := w.Snapshot()
	assert.Equal(t, int64(2), snap.BufferSize)
	assert.Equal(t, int64(1), snap.MessagesDropped)
}

// ─── Enterprise: Overflow – error ────────────────────────────────────────────

func TestTumblingCount_Overflow_Error(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 5, Function: window.FuncSum,
		MaxBufferSize:  2,
		OverflowPolicy: window.OverflowErrorStop,
	})
	base := time.Now()
	for i, v := range []float64{10, 20} {
		_, _, _, err := w.Add(window.WindowEvent{Value: v, Timestamp: base.Add(time.Duration(i) * time.Second)})
		require.NoError(t, err)
	}
	// 3rd event must return an error
	_, _, _, err := w.Add(window.WindowEvent{Value: 99, Timestamp: base.Add(2 * time.Second)})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "buffer full")
}

// ─── Enterprise: Late events ──────────────────────────────────────────────────

func TestTumblingTime_LateEvent_RejectAll(t *testing.T) {
	// AllowedLateness=0 → any event older than watermark is rejected as late
	w := window.NewTumblingTimeWindow(window.WindowConfig{
		Type: window.WindowTumblingTime, Size: 5000, Function: window.FuncSum,
		AllowedLateness: 0,
	})
	base := time.Now()
	// Advance watermark to base+10s
	_, _, _, err := w.Add(window.WindowEvent{Value: 100, Timestamp: base.Add(10 * time.Second)})
	require.NoError(t, err)
	// Late event (older than watermark) → must be returned as LateEvent
	_, _, late, err := w.Add(window.WindowEvent{Value: 5, Timestamp: base.Add(2 * time.Second)})
	require.NoError(t, err)
	require.NotNil(t, late, "event should have been classified as late")
	assert.Contains(t, late.Reason, "watermark")
}

func TestTumblingTime_LateEvent_WithinTolerance(t *testing.T) {
	// AllowedLateness=5000ms → late event within tolerance must be accepted
	w := window.NewTumblingTimeWindow(window.WindowConfig{
		Type: window.WindowTumblingTime, Size: 10000, Function: window.FuncSum,
		AllowedLateness: 5000,
	})
	base := time.Now()
	// Advance watermark
	_, _, _, _ = w.Add(window.WindowEvent{Value: 100, Timestamp: base.Add(10 * time.Second)})
	// Event 3s behind watermark → within 5s tolerance, must be accepted (late=nil)
	_, _, late, err := w.Add(window.WindowEvent{Value: 5, Timestamp: base.Add(7 * time.Second)})
	require.NoError(t, err)
	assert.Nil(t, late, "event within AllowedLateness must be accepted")

	snap := w.Snapshot()
	assert.Equal(t, int64(1), snap.MessagesLate) // counted but accepted
}

// ─── Enterprise: Snapshot ────────────────────────────────────────────────────

func TestTumblingCount_Snapshot(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 3, Function: window.FuncSum,
	})
	base := time.Now()
	w.Add(window.WindowEvent{Value: 1, Timestamp: base})
	w.Add(window.WindowEvent{Value: 2, Timestamp: base.Add(time.Second)})

	snap := w.Snapshot()
	assert.Equal(t, int64(2), snap.BufferSize)
	assert.Equal(t, int64(2), snap.MessagesIn)
	assert.False(t, snap.LastEventAt.IsZero())
}

// ─── Enterprise: Idle timeout ────────────────────────────────────────────────

func TestTumblingCount_CheckIdle_NotYet(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 3, Function: window.FuncSum,
		IdleTimeoutMs: 60000, // 60s — won't expire in test
	})
	w.Add(window.WindowEvent{Value: 1, Timestamp: time.Now()})
	r, ok := w.CheckIdle()
	assert.False(t, ok)
	assert.Nil(t, r)
}

func TestTumblingCount_CheckIdle_Expires(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 100, Function: window.FuncSum,
		IdleTimeoutMs: 1, // 1ms — will always expire
	})
	w.Add(window.WindowEvent{Value: 42, Timestamp: time.Now()})
	time.Sleep(5 * time.Millisecond)
	r, ok := w.CheckIdle()
	assert.True(t, ok, "window should have expired after idle timeout")
	require.NotNil(t, r)
	assert.Equal(t, 42.0, r.Value)
}

// ─── Enterprise: WindowError implements error ─────────────────────────────────

func TestWindowError_ErrorMessage(t *testing.T) {
	we := &window.WindowError{
		Event:  window.WindowEvent{Value: 1, Key: "k1"},
		Cause:  assert.AnError,
		Window: "test-win",
	}
	assert.Contains(t, we.Error(), "test-win")
	assert.Contains(t, we.Error(), "k1")
}

// ─── Negative tests ───────────────────────────────────────────────────────────

// TestTumblingCount_Overflow_Error_BufferUnchanged verifies that when
// overflow=error the buffer is not modified and the error contains "buffer full".
func TestTumblingCount_Overflow_Error_BufferUnchanged(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 5, Function: window.FuncSum,
		MaxBufferSize: 2, OverflowPolicy: window.OverflowErrorStop,
	})
	base := time.Now()
	_, _, _, err := w.Add(event(1, base))
	require.NoError(t, err)
	_, _, _, err = w.Add(event(2, base.Add(time.Second)))
	require.NoError(t, err)

	snap := w.Snapshot()
	assert.Equal(t, int64(2), snap.BufferSize, "buffer must be full at this point")

	_, _, _, err = w.Add(event(3, base.Add(2*time.Second)))
	require.Error(t, err, "third event must be rejected")
	assert.Contains(t, err.Error(), "buffer full")

	// Buffer must remain unchanged after the rejected event.
	snap2 := w.Snapshot()
	assert.Equal(t, int64(2), snap2.BufferSize, "buffer must not grow after overflow error")
}

// TestTumblingTime_LateEvent_Watermark_ExactEqual_NotLate verifies that an event
// with a timestamp equal to (not before) the current watermark is never classified
// as late, even with AllowedLateness=0.
func TestTumblingTime_LateEvent_Watermark_ExactEqual_NotLate(t *testing.T) {
	w := window.NewTumblingTimeWindow(window.WindowConfig{
		Type: window.WindowTumblingTime, Size: 5000, Function: window.FuncSum,
		AllowedLateness: 0,
	})
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// Advance watermark to base+1s.
	_, _, late, err := w.Add(event(1, base.Add(time.Second)))
	require.NoError(t, err)
	assert.Nil(t, late)

	// Event with timestamp == watermark — .Before() returns false so it is NOT late.
	_, _, late2, err2 := w.Add(event(2, base.Add(time.Second)))
	require.NoError(t, err2)
	assert.Nil(t, late2, "event at watermark boundary must not be classified as late")
}

// TestTumblingCount_CheckIdle_EmptyBuffer_ReturnsFalse verifies that CheckIdle
// never emits a partial result when the buffer is empty, regardless of the elapsed time.
func TestTumblingCount_CheckIdle_EmptyBuffer_ReturnsFalse(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type: window.WindowTumblingCount, Size: 5, Function: window.FuncSum,
		IdleTimeoutMs: 1,
	})
	time.Sleep(5 * time.Millisecond) // let the idle timeout expire
	r, ok := w.CheckIdle()
	assert.Nil(t, r, "no result expected from an empty window")
	assert.False(t, ok, "empty buffer must not trigger an idle-close")
}

// TestTumblingTime_Overflow_Error verifies that the TumblingTimeWindow also
// enforces overflow=error correctly (not just the count-based variant).
func TestTumblingTime_Overflow_Error(t *testing.T) {
	w := window.NewTumblingTimeWindow(window.WindowConfig{
		Type: window.WindowTumblingTime, Size: 5000, Function: window.FuncSum,
		MaxBufferSize: 1, OverflowPolicy: window.OverflowErrorStop,
	})
	base := time.Now()
	_, _, _, err := w.Add(event(1, base))
	require.NoError(t, err)

	_, _, _, err = w.Add(event(2, base.Add(time.Second)))
	require.Error(t, err, "second event must be rejected once MaxBufferSize=1 is reached")
	assert.Contains(t, err.Error(), "buffer full")
}

// TestSlidingCount_Deduplication verifies that a duplicate MessageID within the
// sliding-count window is silently discarded.
func TestSlidingCount_Deduplication(t *testing.T) {
	w := window.NewSlidingCountWindow(window.WindowConfig{
		Type: window.WindowSlidingCount, Size: 5, Function: window.FuncSum,
	})
	base := time.Now()
	r, closed, _, err := w.Add(window.WindowEvent{Value: 10, Timestamp: base, MessageID: "A"})
	require.NoError(t, err)
	assert.True(t, closed)
	assert.Equal(t, 10.0, r.Value)

	// Duplicate MessageID — must be silently swallowed.
	r2, closed2, _, err2 := w.Add(window.WindowEvent{Value: 10, Timestamp: base.Add(time.Second), MessageID: "A"})
	require.NoError(t, err2)
	assert.False(t, closed2, "duplicate must produce no result")
	assert.Nil(t, r2)

	// A new MessageID is accepted and contributes to the running total.
	r3, _, _, err3 := w.Add(window.WindowEvent{Value: 20, Timestamp: base.Add(2 * time.Second), MessageID: "B"})
	require.NoError(t, err3)
	require.NotNil(t, r3)
	assert.Equal(t, 30.0, r3.Value, "A(10) + B(20) = 30")
}

// TestSlidingCount_Dedup_ReleasedAfterEviction verifies that once an event has
// been evicted from the sliding-count buffer, its MessageID is removed from the
// dedup set — the same ID should therefore be accepted on a subsequent Add.
func TestSlidingCount_Dedup_ReleasedAfterEviction(t *testing.T) {
	// Window size = 3: buffer holds the last 3 events.
	w := window.NewSlidingCountWindow(window.WindowConfig{
		Type: window.WindowSlidingCount, Size: 3, Function: window.FuncSum,
	})
	base := time.Now()
	addOK := func(id string, val float64) {
		t.Helper()
		r, closed, _, err := w.Add(window.WindowEvent{Value: val, Timestamp: base, MessageID: id})
		require.NoError(t, err)
		assert.True(t, closed)
		require.NotNil(t, r)
	}
	addOK("A", 1) // buffer: [A]
	addOK("B", 2) // buffer: [A, B]
	addOK("C", 3) // buffer: [A, B, C]
	addOK("D", 4) // buffer: [B, C, D] — A evicted; A's MessageID must be removed from seen

	// "A" was evicted → its MessageID must now be accepted again.
	r, closed, _, err := w.Add(window.WindowEvent{Value: 10, Timestamp: base, MessageID: "A"})
	require.NoError(t, err)
	assert.True(t, closed)
	require.NotNil(t, r)
	// Last 3 events in buffer: C(3), D(4), A(10) → sum = 17
	assert.Equal(t, 17.0, r.Value, "A must be re-accepted after eviction (dedup released)")
}

// TestSlidingTime_Deduplication verifies that a duplicate MessageID within the
// sliding-time window is silently discarded.
func TestSlidingTime_Deduplication(t *testing.T) {
	w := window.NewSlidingTimeWindow(window.WindowConfig{
		Type: window.WindowSlidingTime, Size: 3000, Function: window.FuncSum,
	})
	base := time.Now()
	r, closed, _, err := w.Add(window.WindowEvent{Value: 10, Timestamp: base, MessageID: "A"})
	require.NoError(t, err)
	assert.True(t, closed)
	assert.Equal(t, 10.0, r.Value)

	// Duplicate within the active window boundary — must be swallowed.
	r2, closed2, _, err2 := w.Add(window.WindowEvent{Value: 10, Timestamp: base.Add(time.Second), MessageID: "A"})
	require.NoError(t, err2)
	assert.False(t, closed2)
	assert.Nil(t, r2)

	// Different ID contributes to the total.
	r3, _, _, err3 := w.Add(window.WindowEvent{Value: 20, Timestamp: base.Add(2 * time.Second), MessageID: "B"})
	require.NoError(t, err3)
	require.NotNil(t, r3)
	assert.Equal(t, 30.0, r3.Value, "A(10) + B(20) = 30")
}

// TestSlidingTime_Dedup_ReleasedAfterEviction verifies that once an event has
// aged out of the sliding-time window, its MessageID is removed from the dedup
// set — the same ID should therefore be accepted on a subsequent Add.
func TestSlidingTime_Dedup_ReleasedAfterEviction(t *testing.T) {
	// Window size = 3000ms.
	w := window.NewSlidingTimeWindow(window.WindowConfig{
		Type: window.WindowSlidingTime, Size: 3000, Function: window.FuncSum,
	})
	epoch := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// T=0: A enters. T=1000: B enters.
	_, _, _, err := w.Add(window.WindowEvent{Value: 10, Timestamp: epoch, MessageID: "A"})
	require.NoError(t, err)
	_, _, _, err = w.Add(window.WindowEvent{Value: 20, Timestamp: epoch.Add(time.Second), MessageID: "B"})
	require.NoError(t, err)

	// T=3500ms: cutoff = T=500ms → A(T=0) is evicted; B(T=1000) remains.
	_, _, _, err = w.Add(window.WindowEvent{Value: 5, Timestamp: epoch.Add(3500 * time.Millisecond), MessageID: "C"})
	require.NoError(t, err)

	// "A" was evicted → must be accepted again at T=4000ms.
	r, closed, _, err := w.Add(window.WindowEvent{Value: 99, Timestamp: epoch.Add(4000 * time.Millisecond), MessageID: "A"})
	require.NoError(t, err)
	require.True(t, closed)
	require.NotNil(t, r)
	// Cutoff at T=4000ms with 3000ms window = T=1000ms.
	// B(T=1000) is at the boundary (not before) → stays.
	// Buffer: B(1000) + C(3500) + A(4000) → values 20+5+99 = 124
	assert.Equal(t, 124.0, r.Value, "A must be re-accepted after time-based eviction")
}

// ─── Bug regression: idle timer must not be reset by duplicates ───────────────

// TestTumblingCount_IdleTimeout_DuplicatesDoNotResetTimer verifies that a window
// which receives only re-delivered duplicate events (same MessageID) still fires
// its idle timeout. Previously, lastEventAt was updated before the dedup check,
// so duplicates silently prevented the window from ever being idle-closed.
func TestTumblingCount_IdleTimeout_DuplicatesDoNotResetTimer(t *testing.T) {
	w := window.NewTumblingCountWindow(window.WindowConfig{
		Type:          window.WindowTumblingCount,
		Size:          100, // large — will not close by count
		Function:      window.FuncSum,
		IdleTimeoutMs: 30,
	})
	// Seed one real event to start the idle clock.
	w.Add(window.WindowEvent{Value: 42, Timestamp: time.Now(), MessageID: "seed"})

	// Flood with duplicates — must NOT reset the idle timer.
	time.Sleep(10 * time.Millisecond)
	for i := 0; i < 10; i++ {
		w.Add(window.WindowEvent{Value: 42, Timestamp: time.Now(), MessageID: "seed"})
	}

	// After the idle timeout the window must close and emit the partial result.
	time.Sleep(40 * time.Millisecond)
	r, ok := w.CheckIdle()
	assert.True(t, ok, "idle-close must fire even when only duplicates were received after the seed event")
	require.NotNil(t, r)
	assert.Equal(t, 42.0, r.Value)
}

// TestTumblingTime_IdleTimeout_DuplicatesDoNotResetTimer is the same check for
// the time-based tumbling window.
func TestTumblingTime_IdleTimeout_DuplicatesDoNotResetTimer(t *testing.T) {
	w := window.NewTumblingTimeWindow(window.WindowConfig{
		Type:          window.WindowTumblingTime,
		Size:          60000, // 60 s — will not close by time in this test
		Function:      window.FuncSum,
		IdleTimeoutMs: 30,
	})
	base := time.Now()
	w.Add(window.WindowEvent{Value: 10, Timestamp: base, MessageID: "seed"})

	time.Sleep(10 * time.Millisecond)
	for i := 0; i < 10; i++ {
		w.Add(window.WindowEvent{Value: 10, Timestamp: base, MessageID: "seed"})
	}

	time.Sleep(40 * time.Millisecond)
	r, ok := w.CheckIdle()
	assert.True(t, ok, "idle-close must fire even when only duplicates were received after the seed event")
	require.NotNil(t, r)
	assert.Equal(t, 10.0, r.Value)
}

// ─── Bug regression: SlidingTimeWindow OverflowDropOldest must clean up seen ─

// TestSlidingTime_Overflow_DropOldest_MessageIDCleanup verifies that when the
// overflow policy is drop_oldest, the evicted event's MessageID is removed from
// the dedup set so it can be re-used. Previously, the evicted event was removed
// from events but its ID stayed in seen permanently.
func TestSlidingTime_Overflow_DropOldest_MessageIDCleanup(t *testing.T) {
	w := window.NewSlidingTimeWindow(window.WindowConfig{
		Type:           window.WindowSlidingTime,
		Size:           60000, // large — no time-based eviction in this test
		Function:       window.FuncSum,
		MaxBufferSize:  2,
		OverflowPolicy: window.OverflowDropOldest,
	})
	base := time.Now()
	// Fill buffer: [msg-1, msg-2]
	w.Add(window.WindowEvent{Value: 1, Timestamp: base, MessageID: "msg-1"})
	w.Add(window.WindowEvent{Value: 2, Timestamp: base.Add(time.Second), MessageID: "msg-2"})

	// msg-3 overflows: evicts msg-1 → buffer=[msg-2, msg-3]. msg-1 must leave seen.
	w.Add(window.WindowEvent{Value: 3, Timestamp: base.Add(2 * time.Second), MessageID: "msg-3"})

	// msg-1 must now be accepted (not blocked as a duplicate).
	// It overflows msg-2 → buffer=[msg-3, msg-1(10)].
	r, _, _, err := w.Add(window.WindowEvent{Value: 10, Timestamp: base.Add(3 * time.Second), MessageID: "msg-1"})
	require.NoError(t, err)
	require.NotNil(t, r, "msg-1 must be accepted after being evicted by overflow")
	assert.Equal(t, 13.0, r.Value, "sum(msg-3=3, msg-1=10) = 13")
}

// ─── Bug regression: sliding window persist must preserve seen map ────────────

// TestSlidingTime_SaveLoad_PreservesSeenMap verifies that after state is saved
// and restored into a new SlidingTimeWindow, previously-seen MessageIDs are
// remembered so at-least-once re-deliveries are still suppressed.
func TestSlidingTime_SaveLoad_PreservesSeenMap(t *testing.T) {
	w1 := window.NewSlidingTimeWindow(window.WindowConfig{
		Type: window.WindowSlidingTime, Size: 60000, Function: window.FuncSum,
	})
	base := time.Now()
	w1.Add(window.WindowEvent{Value: 5, Timestamp: base, MessageID: "seen-id"})

	// Save and restore into a fresh window instance.
	state := w1.SaveState()
	w2 := window.NewSlidingTimeWindow(window.WindowConfig{
		Type: window.WindowSlidingTime, Size: 60000, Function: window.FuncSum,
	})
	w2.LoadState(state)

	// "seen-id" must still be blocked — dedup state survived the round-trip.
	r, closed, _, err := w2.Add(window.WindowEvent{Value: 99, Timestamp: base.Add(time.Second), MessageID: "seen-id"})
	require.NoError(t, err)
	assert.False(t, closed, "duplicate should be suppressed after state restore")
	assert.Nil(t, r, "no result for a suppressed duplicate")

	// A fresh ID must pass through normally.
	r2, closed2, _, err2 := w2.Add(window.WindowEvent{Value: 20, Timestamp: base.Add(2 * time.Second), MessageID: "new-id"})
	require.NoError(t, err2)
	assert.True(t, closed2)
	require.NotNil(t, r2)
	assert.Equal(t, 25.0, r2.Value, "restored event(5) + new-id(20) = 25")
}

// TestSlidingCount_SaveLoad_PreservesSeenMap is the same round-trip check for
// the count-based sliding window.
func TestSlidingCount_SaveLoad_PreservesSeenMap(t *testing.T) {
	w1 := window.NewSlidingCountWindow(window.WindowConfig{
		Type: window.WindowSlidingCount, Size: 5, Function: window.FuncSum,
	})
	base := time.Now()
	w1.Add(window.WindowEvent{Value: 7, Timestamp: base, MessageID: "seen-id"})

	state := w1.SaveState()
	w2 := window.NewSlidingCountWindow(window.WindowConfig{
		Type: window.WindowSlidingCount, Size: 5, Function: window.FuncSum,
	})
	w2.LoadState(state)

	// Duplicate must be blocked.
	r, closed, _, err := w2.Add(window.WindowEvent{Value: 99, Timestamp: base.Add(time.Second), MessageID: "seen-id"})
	require.NoError(t, err)
	assert.False(t, closed, "duplicate should be suppressed after state restore")
	assert.Nil(t, r)

	// Fresh ID must be accepted.
	r2, closed2, _, err2 := w2.Add(window.WindowEvent{Value: 3, Timestamp: base.Add(2 * time.Second), MessageID: "new-id"})
	require.NoError(t, err2)
	assert.True(t, closed2)
	require.NotNil(t, r2)
	assert.Equal(t, 10.0, r2.Value, "restored event(7) + new-id(3) = 10")
}
