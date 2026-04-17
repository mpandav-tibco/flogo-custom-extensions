package aggregate

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kafkastream "github.com/milindpandav/flogo-extensions/kafkastream"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newAggregateTrigger(s *Settings) *Trigger {
	return &Trigger{
		settings: s,
		logger:   log.RootLogger(),
	}
}

// clearWindow removes window store entries created during tests so that each
// test starts with a clean state.
func clearWindow(names ...string) {
	for _, n := range names {
		kafkastream.UnregisterWindowStore(n)
	}
}

// process is a convenience wrapper that feeds a payload directly through the
// trigger's window processing pipeline (no Kafka needed).
func process(t *testing.T, trig *Trigger, payload map[string]interface{}) (*Output, string, error) {
	t.Helper()
	return trig.processPayload(context.Background(), payload, "test-topic", 0, 0)
}

// ─── validateSettings ────────────────────────────────────────────────────────

func TestValidateSettings_Valid(t *testing.T) {
	s := &Settings{
		Topic:         "t",
		ConsumerGroup: "g",
		WindowName:    "win",
		WindowType:    "TumblingCount",
		WindowSize:    5,
		Function:      "sum",
		ValueField:    "value",
	}
	require.NoError(t, validateSettings(s))
}

func TestValidateSettings_MissingTopic(t *testing.T) {
	s := &Settings{ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v"}
	assert.ErrorContains(t, validateSettings(s), "topic")
}

func TestValidateSettings_MissingConsumerGroup(t *testing.T) {
	s := &Settings{Topic: "t", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v"}
	assert.ErrorContains(t, validateSettings(s), "consumerGroup")
}

func TestValidateSettings_UnsupportedWindowType(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "RollingBad", WindowSize: 5, Function: "sum", ValueField: "v"}
	assert.ErrorContains(t, validateSettings(s), "windowType")
}

func TestValidateSettings_UnsupportedFunction(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "median", ValueField: "v"}
	assert.ErrorContains(t, validateSettings(s), "function")
}

func TestValidateSettings_ZeroWindowSize(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 0, Function: "sum", ValueField: "v"}
	assert.ErrorContains(t, validateSettings(s), "windowSize")
}

func TestValidateSettings_MissingValueField(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum"}
	assert.ErrorContains(t, validateSettings(s), "valueField")
}

func TestValidateSettings_InvalidOverflowPolicy(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v", OverflowPolicy: "discard"}
	assert.ErrorContains(t, validateSettings(s), "overflowPolicy")
}

func TestValidateSettings_NegativePersistEveryN(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v", PersistEveryN: -1}
	assert.ErrorContains(t, validateSettings(s), "persistEveryN")
}

func TestValidateSettings_NegativeAllowedLateness(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v", AllowedLateness: -1}
	assert.ErrorContains(t, validateSettings(s), "allowedLateness")
}

func TestValidateSettings_NegativeMaxBufferSize(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v", MaxBufferSize: -1}
	assert.ErrorContains(t, validateSettings(s), "maxBufferSize")
}

// ─── extractEventTime ────────────────────────────────────────────────────────

func TestExtractEventTime_UnixMs_Int64(t *testing.T) {
	ts := int64(1_700_000_000_000) // a fixed Unix-ms timestamp
	got := extractEventTime(map[string]interface{}{"ts": ts}, "ts")
	assert.Equal(t, time.UnixMilli(ts), got)
}

func TestExtractEventTime_UnixMs_Float64(t *testing.T) {
	ts := float64(1_700_000_000_000)
	got := extractEventTime(map[string]interface{}{"ts": ts}, "ts")
	assert.Equal(t, time.UnixMilli(int64(ts)), got)
}

func TestExtractEventTime_RFC3339(t *testing.T) {
	s := "2024-01-15T12:00:00Z"
	got := extractEventTime(map[string]interface{}{"ts": s}, "ts")
	expected, _ := time.Parse(time.RFC3339, s)
	assert.Equal(t, expected, got)
}

func TestExtractEventTime_UnixMsString(t *testing.T) {
	ts := int64(1_700_000_000_000)
	got := extractEventTime(map[string]interface{}{"ts": fmt.Sprintf("%d", ts)}, "ts")
	assert.Equal(t, time.UnixMilli(ts), got)
}

func TestExtractEventTime_EmptyField_WallClock(t *testing.T) {
	before := time.Now()
	got := extractEventTime(map[string]interface{}{}, "")
	assert.False(t, got.Before(before))
}

func TestExtractEventTime_MissingField_WallClock(t *testing.T) {
	before := time.Now()
	got := extractEventTime(map[string]interface{}{}, "ts")
	assert.False(t, got.Before(before))
}

func TestExtractEventTime_UnparsableValue_WallClock(t *testing.T) {
	before := time.Now()
	got := extractEventTime(map[string]interface{}{"ts": "not-a-timestamp"}, "ts")
	assert.False(t, got.Before(before))
}

// ─── processPayload — TumblingCount window ────────────────────────────────────

func TestProcessPayload_TumblingCount_Sum_WindowClose(t *testing.T) {
	wn := "tt-tc-sum"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 5,
		Function: "sum", ValueField: "value",
	})
	// Pre-register window store (Initialize() would do this normally).
	cfg := buildWindowConfig(trig.settings, wn)
	_, err := kafkastream.GetOrCreateWindowStore(cfg)
	require.NoError(t, err)

	readings := []float64{10, 20, 30, 40, 50}
	var lastOut *Output
	var lastEventType string
	for i, v := range readings {
		out, et, err := process(t, trig, map[string]interface{}{"value": v})
		require.NoError(t, err)
		if i < 4 {
			assert.Empty(t, et, "window should not close before 5th event")
		} else {
			lastOut = out
			lastEventType = et
		}
	}

	require.Equal(t, EventTypeWindowClose, lastEventType)
	require.NotNil(t, lastOut)
	assert.True(t, lastOut.WindowResult.WindowClosed)
	assert.Equal(t, 150.0, lastOut.WindowResult.Result)
	assert.Equal(t, int64(5), lastOut.WindowResult.Count)
	assert.Equal(t, wn, lastOut.WindowResult.WindowName)
}

func TestProcessPayload_TumblingCount_Avg_WindowClose(t *testing.T) {
	wn := "tt-tc-avg"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "avg", ValueField: "price",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, err := kafkastream.GetOrCreateWindowStore(cfg)
	require.NoError(t, err)

	for _, v := range []float64{100, 200, 300} {
		_, _, err := process(t, trig, map[string]interface{}{"price": v})
		require.NoError(t, err)
	}
	// Force 3rd message — window closes.
	// Last call will have returned the result already above, so let's redo:
	clearWindow(wn)
	cfg = buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	var last *Output
	for _, v := range []float64{100, 200, 300} {
		out, et, err := process(t, trig, map[string]interface{}{"price": v})
		require.NoError(t, err)
		if et == EventTypeWindowClose {
			last = out
		}
	}
	require.NotNil(t, last)
	assert.Equal(t, 200.0, last.WindowResult.Result)
}

func TestProcessPayload_TumblingCount_Min(t *testing.T) {
	wn := "tt-tc-min"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "min", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	var last *Output
	for _, v := range []float64{5, 3, 8} {
		out, et, err := process(t, trig, map[string]interface{}{"v": v})
		require.NoError(t, err)
		if et == EventTypeWindowClose {
			last = out
		}
	}
	require.NotNil(t, last)
	assert.Equal(t, 3.0, last.WindowResult.Result)
}

func TestProcessPayload_TumblingCount_Max(t *testing.T) {
	wn := "tt-tc-max"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "max", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	var last *Output
	for _, v := range []float64{5, 3, 8} {
		out, et, err := process(t, trig, map[string]interface{}{"v": v})
		require.NoError(t, err)
		if et == EventTypeWindowClose {
			last = out
		}
	}
	require.NotNil(t, last)
	assert.Equal(t, 8.0, last.WindowResult.Result)
}

func TestProcessPayload_TumblingCount_Count(t *testing.T) {
	wn := "tt-tc-count"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 4,
		Function: "count", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	var last *Output
	for _, v := range []float64{1, 2, 3, 4} {
		out, et, err := process(t, trig, map[string]interface{}{"v": v})
		require.NoError(t, err)
		if et == EventTypeWindowClose {
			last = out
		}
	}
	require.NotNil(t, last)
	assert.Equal(t, float64(4), last.WindowResult.Result)
	assert.Equal(t, int64(4), last.WindowResult.Count)
}

// ─── processPayload — SlidingCount window ────────────────────────────────────

func TestProcessPayload_SlidingCount_AlwaysEmits(t *testing.T) {
	wn := "tt-sc-emit"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "SlidingCount", WindowSize: 3,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	// Sliding windows emit on every message.
	for i, v := range []float64{10, 20, 30, 40} {
		out, et, err := process(t, trig, map[string]interface{}{"v": v})
		require.NoError(t, err, "message %d should not error", i)
		assert.Equal(t, EventTypeWindowClose, et, "sliding window should emit on every message")
		assert.NotNil(t, out)
		assert.True(t, out.WindowResult.WindowClosed)
	}
}

func TestProcessPayload_SlidingCount_RollingSum(t *testing.T) {
	wn := "tt-sc-roll"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "SlidingCount", WindowSize: 3,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	type step struct {
		value       float64
		expectedSum float64
	}
	steps := []step{
		{10, 10}, // window: [10]
		{20, 30}, // window: [10,20]
		{30, 60}, // window: [10,20,30]
		{40, 90}, // window: [20,30,40] — oldest evicted
	}
	for _, s := range steps {
		out, et, err := process(t, trig, map[string]interface{}{"v": s.value})
		require.NoError(t, err)
		require.Equal(t, EventTypeWindowClose, et)
		assert.Equal(t, s.expectedSum, out.WindowResult.Result)
	}
}

// ─── processPayload — Keyed windows ──────────────────────────────────────────

func TestProcessPayload_KeyedWindows_IndependentClose(t *testing.T) {
	baseWN := "tt-keyed"
	clearWindow(baseWN, baseWN+":dev-A", baseWN+":dev-B")
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: baseWN, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "value", KeyField: "device",
	})
	cfg := buildWindowConfig(trig.settings, baseWN)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	closedWindows := map[string]float64{}

	messages := []struct {
		device string
		value  float64
	}{
		{"dev-A", 10}, {"dev-B", 20},
		{"dev-A", 10}, // dev-A closes (sum=20)
		{"dev-B", 20}, // dev-B closes (sum=40)
	}
	for _, m := range messages {
		out, et, err := process(t, trig, map[string]interface{}{
			"value": m.value, "device": m.device,
		})
		require.NoError(t, err)
		if et == EventTypeWindowClose && out != nil {
			closedWindows[out.WindowResult.Key] = out.WindowResult.Result
		}
	}

	assert.Equal(t, 20.0, closedWindows["dev-A"], "dev-A window should sum to 20")
	assert.Equal(t, 40.0, closedWindows["dev-B"], "dev-B window should sum to 40")
}

// ─── processPayload — MissingValueField error ─────────────────────────────────

func TestProcessPayload_MissingValueField_Error(t *testing.T) {
	wn := "tt-missing-vf"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "price",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	_, _, err := process(t, trig, map[string]interface{}{"temp": 25.0}) // wrong field name
	require.Error(t, err)
	assert.Contains(t, err.Error(), "price")
}

func TestProcessPayload_NonNumericValueField_Error(t *testing.T) {
	wn := "tt-non-numeric"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "price",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	_, _, err := process(t, trig, map[string]interface{}{"price": "not-a-number"})
	require.Error(t, err)
}

// ─── processPayload — MessageID deduplication ─────────────────────────────────

func TestProcessPayload_MessageIDDedup_DuplicateIgnored(t *testing.T) {
	wn := "tt-msgid-dedup"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "v", MessageIDField: "msgid",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	// Send the same message ID twice — second should be deduplicated.
	_, _, err := process(t, trig, map[string]interface{}{"v": 10.0, "msgid": "msg-1"})
	require.NoError(t, err)
	_, _, err = process(t, trig, map[string]interface{}{"v": 10.0, "msgid": "msg-1"}) // duplicate
	require.NoError(t, err)
	_, _, err = process(t, trig, map[string]interface{}{"v": 10.0, "msgid": "msg-1"}) // duplicate

	require.NoError(t, err)

	// Add one more unique message to close the window (size=3, but only 1 accepted so far).
	// We need 2 more unique messages.
	_, _, _ = process(t, trig, map[string]interface{}{"v": 10.0, "msgid": "msg-2"})
	out, et, err := process(t, trig, map[string]interface{}{"v": 10.0, "msgid": "msg-3"})
	require.NoError(t, err)
	require.Equal(t, EventTypeWindowClose, et)
	// Only msg-1, msg-2, msg-3 are unique — sum should be 30, not 50.
	assert.Equal(t, 30.0, out.WindowResult.Result)
	assert.Equal(t, int64(3), out.WindowResult.Count)
}

// ─── processPayload — EventType constants ────────────────────────────────────

func TestEventTypeConstants(t *testing.T) {
	assert.Equal(t, "windowClose", EventTypeWindowClose)
	assert.Equal(t, "lateEvent", EventTypeLateEvent)
	assert.Equal(t, "all", EventTypeAll)
}

// ─── Output ToMap / FromMap round-trip ───────────────────────────────────────

func TestOutput_RoundTrip(t *testing.T) {
	orig := &Output{
		WindowResult: WindowResult{
			Result:         42.5,
			Count:          10,
			WindowName:     "win-1",
			Key:            "dev-A",
			WindowClosed:   true,
			DroppedCount:   2,
			LateEventCount: 1,
		},
		Source: Source{
			LateEvent:  false,
			LateReason: "",
			Topic:      "test-topic",
			Partition:  3,
			Offset:     99,
		},
	}
	restored := &Output{}
	require.NoError(t, restored.FromMap(orig.ToMap()))
	assert.Equal(t, orig.WindowResult.Result, restored.WindowResult.Result)
	assert.Equal(t, orig.WindowResult.Count, restored.WindowResult.Count)
	assert.Equal(t, orig.WindowResult.WindowName, restored.WindowResult.WindowName)
	assert.Equal(t, orig.WindowResult.Key, restored.WindowResult.Key)
	assert.Equal(t, orig.WindowResult.WindowClosed, restored.WindowResult.WindowClosed)
	assert.Equal(t, orig.WindowResult.DroppedCount, restored.WindowResult.DroppedCount)
	assert.Equal(t, orig.WindowResult.LateEventCount, restored.WindowResult.LateEventCount)
	assert.Equal(t, orig.Source.Topic, restored.Source.Topic)
	assert.Equal(t, orig.Source.Partition, restored.Source.Partition)
	assert.Equal(t, orig.Source.Offset, restored.Source.Offset)
}

// ─── buildWindowConfig ────────────────────────────────────────────────────────

func TestBuildWindowConfig_MapsAllFields(t *testing.T) {
	s := &Settings{
		WindowName:      "win",
		WindowType:      "TumblingTime",
		WindowSize:      5000,
		Function:        "avg",
		EventTimeField:  "ts",
		AllowedLateness: 1000,
		MaxBufferSize:   500,
		OverflowPolicy:  "drop_newest",
		IdleTimeoutMs:   30000,
		MaxKeys:         100,
	}
	cfg := buildWindowConfig(s, "win")
	assert.Equal(t, "win", cfg.Name)
	assert.Equal(t, int64(5000), cfg.Size)
	assert.Equal(t, "ts", cfg.EventTimeField)
	assert.Equal(t, int64(1000), cfg.AllowedLateness)
	assert.Equal(t, int64(500), cfg.MaxBufferSize)
	assert.Equal(t, int64(30000), cfg.IdleTimeoutMs)
	assert.Equal(t, int64(100), cfg.MaxKeys)
}

// ─── HandlerSettings event type defaulting ───────────────────────────────────

func TestHandlerEventType_DefaultsToWindowClose(t *testing.T) {
	hs := &HandlerSettings{} // EventType empty
	et := hs.EventType
	if et == "" {
		et = EventTypeWindowClose
	}
	assert.Equal(t, EventTypeWindowClose, et)
}

func TestHandlerEventType_All(t *testing.T) {
	hs := &HandlerSettings{EventType: EventTypeAll}
	assert.Equal(t, EventTypeAll, hs.EventType)
}

// ─── TumblingTime window (wall-clock — just validate no error) ────────────────

func TestProcessPayload_TumblingTime_AccumulatesWithoutClose(t *testing.T) {
	wn := "tt-ttime-nocl"
	clearWindow(wn)
	// Large window (1 hour) — will never close in a test.
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingTime", WindowSize: 3_600_000,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	for _, v := range []float64{1, 2, 3, 4, 5} {
		out, et, err := process(t, trig, map[string]interface{}{"v": v})
		require.NoError(t, err)
		assert.Empty(t, et, "1-hour window should not close in test")
		assert.Nil(t, out)
	}
}
