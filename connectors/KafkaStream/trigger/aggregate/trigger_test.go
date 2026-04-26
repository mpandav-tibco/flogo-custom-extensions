package aggregate

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kafkastream "github.com/mpandav-tibco/flogo-extensions/kafkastream"
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

// ─── validateSettings — additional corner cases ──────────────────────────────

func TestValidateSettings_MissingWindowName(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v"}
	assert.ErrorContains(t, validateSettings(s), "windowName")
}

func TestValidateSettings_NegativeWindowSize(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: -3, Function: "sum", ValueField: "v"}
	assert.ErrorContains(t, validateSettings(s), "windowSize")
}

func TestValidateSettings_InvalidOnSchemaError(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v", OnSchemaError: "abort"}
	assert.ErrorContains(t, validateSettings(s), "onSchemaError")
}

func TestValidateSettings_ValidOnSchemaError_Skip(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v", OnSchemaError: "skip"}
	require.NoError(t, validateSettings(s))
}

func TestValidateSettings_ValidOnSchemaError_Retry(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v", OnSchemaError: "retry"}
	require.NoError(t, validateSettings(s))
}

func TestValidateSettings_NegativeMaxKeys(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v", MaxKeys: -1}
	assert.ErrorContains(t, validateSettings(s), "maxKeys")
}

func TestValidateSettings_NegativeIdleTimeoutMs(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v", IdleTimeoutMs: -1}
	assert.ErrorContains(t, validateSettings(s), "idleTimeoutMs")
}

func TestValidateSettings_ValidOverflowPolicies(t *testing.T) {
	for _, p := range []string{"", "drop_oldest", "drop_newest", "error"} {
		s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v", OverflowPolicy: p}
		require.NoError(t, validateSettings(s), "overflowPolicy %q should be valid", p)
	}
}

func TestValidateSettings_AllWindowTypes(t *testing.T) {
	for _, wt := range []string{"TumblingTime", "TumblingCount", "SlidingTime", "SlidingCount"} {
		s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: wt, WindowSize: 5, Function: "sum", ValueField: "v"}
		require.NoError(t, validateSettings(s), "windowType %q should be valid", wt)
	}
}

func TestValidateSettings_AllFunctions(t *testing.T) {
	for _, fn := range []string{"sum", "count", "avg", "min", "max"} {
		s := &Settings{Topic: "t", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: fn, ValueField: "v"}
		require.NoError(t, validateSettings(s), "function %q should be valid", fn)
	}
}

func TestValidateSettings_WhitespaceTopic(t *testing.T) {
	s := &Settings{Topic: "  ", ConsumerGroup: "g", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v"}
	assert.ErrorContains(t, validateSettings(s), "topic")
}

func TestValidateSettings_WhitespaceConsumerGroup(t *testing.T) {
	s := &Settings{Topic: "t", ConsumerGroup: "  ", WindowName: "w", WindowType: "TumblingCount", WindowSize: 5, Function: "sum", ValueField: "v"}
	assert.ErrorContains(t, validateSettings(s), "consumerGroup")
}

// ─── processPayload — consecutive windows ─────────────────────────────────────

func TestProcessPayload_TumblingCount_ConsecutiveWindows(t *testing.T) {
	wn := "tt-tc-consec"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	var results []float64
	for _, v := range []float64{10, 20, 30, 40, 50, 60} {
		out, et, err := process(t, trig, map[string]interface{}{"v": v})
		require.NoError(t, err)
		if et == EventTypeWindowClose {
			results = append(results, out.WindowResult.Result)
		}
	}
	assert.Equal(t, []float64{30, 70, 110}, results, "three consecutive windows should close with correct sums")
}

// ─── processPayload — single element window ───────────────────────────────────

func TestProcessPayload_TumblingCount_SingleElement(t *testing.T) {
	wn := "tt-tc-single"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 1,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	// Every message should close its own window.
	for _, v := range []float64{7, 14, 21} {
		out, et, err := process(t, trig, map[string]interface{}{"v": v})
		require.NoError(t, err)
		require.Equal(t, EventTypeWindowClose, et)
		assert.Equal(t, v, out.WindowResult.Result)
		assert.Equal(t, int64(1), out.WindowResult.Count)
	}
}

// ─── processPayload — zero value ──────────────────────────────────────────────

func TestProcessPayload_TumblingCount_ZeroValues(t *testing.T) {
	wn := "tt-tc-zero"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	var last *Output
	for _, v := range []float64{0, 0, 0} {
		out, et, err := process(t, trig, map[string]interface{}{"v": v})
		require.NoError(t, err)
		if et == EventTypeWindowClose {
			last = out
		}
	}
	require.NotNil(t, last)
	assert.Equal(t, 0.0, last.WindowResult.Result)
}

// ─── processPayload — negative values ─────────────────────────────────────────

func TestProcessPayload_TumblingCount_NegativeValues(t *testing.T) {
	wn := "tt-tc-neg"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	var last *Output
	for _, v := range []float64{-10, 5, -3} {
		out, et, err := process(t, trig, map[string]interface{}{"v": v})
		require.NoError(t, err)
		if et == EventTypeWindowClose {
			last = out
		}
	}
	require.NotNil(t, last)
	assert.Equal(t, -8.0, last.WindowResult.Result)
}

// ─── processPayload — very large values ───────────────────────────────────────

func TestProcessPayload_TumblingCount_LargeValues(t *testing.T) {
	wn := "tt-tc-large"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	var last *Output
	for _, v := range []float64{1e15, 2e15} {
		out, et, err := process(t, trig, map[string]interface{}{"v": v})
		require.NoError(t, err)
		if et == EventTypeWindowClose {
			last = out
		}
	}
	require.NotNil(t, last)
	assert.Equal(t, 3e15, last.WindowResult.Result)
}

// ─── processPayload — value field types ───────────────────────────────────────

func TestProcessPayload_ValueAsInt_CoercedToFloat(t *testing.T) {
	wn := "tt-tc-int"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	// Integer values should be coerced to float64.
	_, _, err1 := process(t, trig, map[string]interface{}{"v": 10})
	require.NoError(t, err1)
	out, et, err2 := process(t, trig, map[string]interface{}{"v": 20})
	require.NoError(t, err2)
	require.Equal(t, EventTypeWindowClose, et)
	assert.Equal(t, 30.0, out.WindowResult.Result)
}

func TestProcessPayload_ValueAsStringNumeric_CoercedToFloat(t *testing.T) {
	wn := "tt-tc-strnum"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	_, _, err1 := process(t, trig, map[string]interface{}{"v": "10.5"})
	require.NoError(t, err1)
	out, et, err2 := process(t, trig, map[string]interface{}{"v": "20.5"})
	require.NoError(t, err2)
	require.Equal(t, EventTypeWindowClose, et)
	assert.Equal(t, 31.0, out.WindowResult.Result)
}

func TestProcessPayload_ValueAsBool_CoercedToNumeric(t *testing.T) {
	wn := "tt-tc-bool"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	// Flogo's coerce.ToFloat64 converts bool to 0/1 — this is valid behavior.
	_, _, err1 := process(t, trig, map[string]interface{}{"v": true})
	require.NoError(t, err1)
	out, et, err2 := process(t, trig, map[string]interface{}{"v": false})
	require.NoError(t, err2)
	require.Equal(t, EventTypeWindowClose, et)
	assert.Equal(t, 1.0, out.WindowResult.Result, "true=1, false=0 → sum=1")
}

// ─── processPayload — empty payload ──────────────────────────────────────────

func TestProcessPayload_EmptyPayload_MissingField(t *testing.T) {
	wn := "tt-tc-empty"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	_, _, err := process(t, trig, map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ─── processPayload — mixed keys ──────────────────────────────────────────────

func TestProcessPayload_KeyedWindows_ManyKeys(t *testing.T) {
	baseWN := "tt-keyed-many"
	// Pre-clean all possible keyed windows.
	keys := []string{"k1", "k2", "k3", "k4", "k5"}
	toClean := []string{baseWN}
	for _, k := range keys {
		toClean = append(toClean, baseWN+":"+k)
	}
	clearWindow(toClean...)

	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: baseWN, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v", KeyField: "key",
	})

	closedWindows := map[string]float64{}
	for _, k := range keys {
		for _, v := range []float64{100, 200} {
			out, et, err := process(t, trig, map[string]interface{}{"v": v, "key": k})
			require.NoError(t, err)
			if et == EventTypeWindowClose && out != nil {
				closedWindows[out.WindowResult.Key] = out.WindowResult.Result
			}
		}
	}
	for _, k := range keys {
		assert.Equal(t, 300.0, closedWindows[k], "keyed window for %q should sum to 300", k)
	}
}

// ─── processPayload — SlidingCount min/max/avg ───────────────────────────────

func TestProcessPayload_SlidingCount_Avg(t *testing.T) {
	wn := "tt-sc-avg"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "SlidingCount", WindowSize: 3,
		Function: "avg", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	type step struct {
		value       float64
		expectedAvg float64
	}
	steps := []step{
		{10, 10.0}, // [10] → avg=10
		{20, 15.0}, // [10,20] → avg=15
		{30, 20.0}, // [10,20,30] → avg=20
		{40, 30.0}, // [20,30,40] → avg=30
	}
	for _, s := range steps {
		out, et, err := process(t, trig, map[string]interface{}{"v": s.value})
		require.NoError(t, err)
		require.Equal(t, EventTypeWindowClose, et)
		assert.Equal(t, s.expectedAvg, out.WindowResult.Result)
	}
}

func TestProcessPayload_SlidingCount_Min(t *testing.T) {
	wn := "tt-sc-min"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "SlidingCount", WindowSize: 3,
		Function: "min", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	type step struct {
		value       float64
		expectedMin float64
	}
	steps := []step{
		{30, 30},
		{10, 10},
		{20, 10},
		{50, 10}, // window: [10,20,50] → min=10
		{60, 20}, // window: [20,50,60] → min=20
	}
	for _, s := range steps {
		out, et, err := process(t, trig, map[string]interface{}{"v": s.value})
		require.NoError(t, err)
		require.Equal(t, EventTypeWindowClose, et)
		assert.Equal(t, s.expectedMin, out.WindowResult.Result, "after adding %.0f", s.value)
	}
}

// ─── processPayload — window resets after close ───────────────────────────────

func TestProcessPayload_TumblingCount_ResetsAfterClose(t *testing.T) {
	wn := "tt-tc-reset"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	// First window: 10+20=30
	process(t, trig, map[string]interface{}{"v": 10.0})
	out1, et1, _ := process(t, trig, map[string]interface{}{"v": 20.0})
	require.Equal(t, EventTypeWindowClose, et1)
	assert.Equal(t, 30.0, out1.WindowResult.Result)

	// Second window: 100+200=300 (NOT 330)
	process(t, trig, map[string]interface{}{"v": 100.0})
	out2, et2, _ := process(t, trig, map[string]interface{}{"v": 200.0})
	require.Equal(t, EventTypeWindowClose, et2)
	assert.Equal(t, 300.0, out2.WindowResult.Result, "second window should start fresh")
}

// ─── Output round-trip — late event ──────────────────────────────────────────

func TestOutput_RoundTrip_LateEvent(t *testing.T) {
	orig := &Output{
		WindowResult: WindowResult{
			WindowName: "win-late",
			Key:        "dev-X",
		},
		Source: Source{
			Topic:      "test-topic",
			Partition:  1,
			Offset:     42,
			LateEvent:  true,
			LateReason: "event arrived after watermark",
		},
	}
	restored := &Output{}
	require.NoError(t, restored.FromMap(orig.ToMap()))
	assert.True(t, restored.Source.LateEvent)
	assert.Equal(t, "event arrived after watermark", restored.Source.LateReason)
	assert.Equal(t, "win-late", restored.WindowResult.WindowName)
}

// ─── Output round-trip — zero values ─────────────────────────────────────────

func TestOutput_RoundTrip_ZeroValues(t *testing.T) {
	orig := &Output{
		WindowResult: WindowResult{
			Result: 0, Count: 0, WindowName: "", Key: "",
			WindowClosed: false, DroppedCount: 0, LateEventCount: 0,
		},
		Source: Source{
			Topic: "", Partition: 0, Offset: 0, LateEvent: false, LateReason: "",
		},
	}
	restored := &Output{}
	require.NoError(t, restored.FromMap(orig.ToMap()))
	assert.Equal(t, 0.0, restored.WindowResult.Result)
	assert.False(t, restored.WindowResult.WindowClosed)
	assert.False(t, restored.Source.LateEvent)
}

// ─── resolveBalanceStrategy ──────────────────────────────────────────────────

func TestResolveBalanceStrategy_Roundrobin(t *testing.T) {
	bs := resolveBalanceStrategy("roundrobin")
	assert.NotNil(t, bs)
}

func TestResolveBalanceStrategy_Sticky(t *testing.T) {
	bs := resolveBalanceStrategy("sticky")
	assert.NotNil(t, bs)
}

func TestResolveBalanceStrategy_Range(t *testing.T) {
	bs := resolveBalanceStrategy("range")
	assert.NotNil(t, bs)
}

func TestResolveBalanceStrategy_Empty_DefaultsRoundrobin(t *testing.T) {
	bs := resolveBalanceStrategy("")
	assert.NotNil(t, bs)
}

func TestResolveBalanceStrategy_CaseInsensitive(t *testing.T) {
	bs := resolveBalanceStrategy("STICKY")
	assert.NotNil(t, bs)
}

// ─── extractEventTime — additional corner cases ──────────────────────────────

func TestExtractEventTime_BoolValue_WallClock(t *testing.T) {
	before := time.Now()
	got := extractEventTime(map[string]interface{}{"ts": true}, "ts")
	assert.False(t, got.Before(before))
}

func TestExtractEventTime_NilValue_WallClock(t *testing.T) {
	before := time.Now()
	got := extractEventTime(map[string]interface{}{"ts": nil}, "ts")
	assert.False(t, got.Before(before))
}

func TestExtractEventTime_NegativeMs(t *testing.T) {
	// Negative Unix-ms is technically valid (before epoch).
	got := extractEventTime(map[string]interface{}{"ts": float64(-1000)}, "ts")
	assert.Equal(t, time.UnixMilli(-1000), got)
}

func TestExtractEventTime_ZeroMs(t *testing.T) {
	got := extractEventTime(map[string]interface{}{"ts": float64(0)}, "ts")
	assert.Equal(t, time.UnixMilli(0), got)
}

// ─── processPayload — keyed window without KeyField set ──────────────────────

func TestProcessPayload_NoKeyField_UsesGlobalWindow(t *testing.T) {
	wn := "tt-nokey"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
		// KeyField intentionally not set
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	// All messages go to the same global window.
	process(t, trig, map[string]interface{}{"v": 5.0, "device": "A"})
	out, et, err := process(t, trig, map[string]interface{}{"v": 10.0, "device": "B"})
	require.NoError(t, err)
	require.Equal(t, EventTypeWindowClose, et)
	assert.Equal(t, 15.0, out.WindowResult.Result)
	assert.Empty(t, out.WindowResult.Key, "no KeyField → key should be empty")
}

// ─── processPayload — keyField present but value missing ─────────────────────

func TestProcessPayload_KeyFieldMissing_EmptyKey(t *testing.T) {
	wn := "tt-keymiss"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v", KeyField: "device",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, _ = kafkastream.GetOrCreateWindowStore(cfg)

	// Payload missing the keyField → empty key → global window.
	process(t, trig, map[string]interface{}{"v": 1.0})
	out, et, err := process(t, trig, map[string]interface{}{"v": 2.0})
	require.NoError(t, err)
	require.Equal(t, EventTypeWindowClose, et)
	assert.Equal(t, 3.0, out.WindowResult.Result)
}

// ─── Late-event and TumblingTime window paths ─────────────────────────────────

// TestProcessPayload_TumblingTime_LateEvent_Fired verifies that when an event
// arrives with a timestamp older than (watermark − allowedLateness), processPayload
// returns EventTypeLateEvent and sets Source.LateEvent=true.
func TestProcessPayload_TumblingTime_LateEvent_Fired(t *testing.T) {
	wn := "tt-late-fire"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName:      wn,
		WindowType:      "TumblingTime",
		WindowSize:      60000, // 60 s window (large so we don't close it)
		Function:        "sum",
		ValueField:      "v",
		EventTimeField:  "ts",
		AllowedLateness: 0, // reject ALL late events
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, err := kafkastream.GetOrCreateWindowStore(cfg)
	require.NoError(t, err)

	now := time.Now()
	// First event advances watermark.
	_, _, err = trig.processPayload(
		context.Background(),
		map[string]interface{}{"v": 1.0, "ts": now.UnixMilli()},
		"t", 0, 0,
	)
	require.NoError(t, err)

	// Second event is 2 minutes in the past — older than watermark, allowedLateness=0.
	oldTs := now.Add(-2 * time.Minute)
	out, et, err := trig.processPayload(
		context.Background(),
		map[string]interface{}{"v": 99.0, "ts": oldTs.UnixMilli()},
		"t", 0, 1,
	)
	require.NoError(t, err)
	assert.Equal(t, EventTypeLateEvent, et)
	require.NotNil(t, out)
	assert.True(t, out.Source.LateEvent)
	assert.NotEmpty(t, out.Source.LateReason)
}

// TestProcessPayload_TumblingTime_WindowClose verifies a TumblingTime window
// closes when an event crosses the window boundary.
func TestProcessPayload_TumblingTime_WindowClose(t *testing.T) {
	wn := "tt-ttime-close"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName:     wn,
		WindowType:     "TumblingTime",
		WindowSize:     1000, // 1 s
		Function:       "sum",
		ValueField:     "v",
		EventTimeField: "ts",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, err := kafkastream.GetOrCreateWindowStore(cfg)
	require.NoError(t, err)

	base := time.Now()
	// First event opens the window.
	_, et, _ := trig.processPayload(
		context.Background(),
		map[string]interface{}{"v": 5.0, "ts": base.UnixMilli()},
		"t", 0, 0,
	)
	assert.Empty(t, et)

	// Third event 1.5 s later crosses the 1 s boundary — window closes.
	later := base.Add(1500 * time.Millisecond)
	out, et, err := trig.processPayload(
		context.Background(),
		map[string]interface{}{"v": 3.0, "ts": later.UnixMilli()},
		"t", 0, 1,
	)
	require.NoError(t, err)
	assert.Equal(t, EventTypeWindowClose, et)
	require.NotNil(t, out)
	assert.True(t, out.WindowResult.WindowClosed)
	// Only the first event was in the closed window; the trigger event seeds the next.
	assert.Equal(t, 5.0, out.WindowResult.Result)
}

// TestProcessPayload_Source_Fields_Populated verifies that Topic/Partition/Offset
// are correctly plumbed into the Output.Source struct.
func TestProcessPayload_Source_Fields_Populated(t *testing.T) {
	wn := "tt-src-fields"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "src-topic", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, err := kafkastream.GetOrCreateWindowStore(cfg)
	require.NoError(t, err)

	// First event — no output yet.
	trig.processPayload(context.Background(), map[string]interface{}{"v": 1.0}, "src-topic", 2, 100)
	// Second event — window closes.
	out, et, err := trig.processPayload(context.Background(), map[string]interface{}{"v": 1.0}, "src-topic", 2, 101)
	require.NoError(t, err)
	require.Equal(t, EventTypeWindowClose, et)
	assert.Equal(t, "src-topic", out.Source.Topic)
	assert.Equal(t, int64(2), out.Source.Partition)
	assert.Equal(t, int64(101), out.Source.Offset)
}

// TestProcessPayload_TumblingCount_MaxBuffer_DropNewest verifies that when
// MaxBufferSize is reached with drop_newest policy the incoming event is silently
// dropped (no error). The window eventually closes because drop_newest does NOT
// prevent the count from advancing — events are still "seen" in the count window.
// NOTE: with drop_newest the window closes when WindowSize events have been
// *accepted into the buffer*. Once the buffer is full no more events are accepted,
// so with WindowSize > MaxBufferSize the window would never close via drop_newest
// alone. Use WindowSize == MaxBufferSize to get a clean close.
func TestProcessPayload_TumblingCount_MaxBuffer_DropNewest(t *testing.T) {
	wn := "tt-drop-newest"
	clearWindow(wn)
	// WindowSize == MaxBufferSize: after 2 accepted events the window closes.
	// The third event hits drop_newest (no error) and is silently dropped.
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
		MaxBufferSize:  2,
		OverflowPolicy: "drop_newest",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, err := kafkastream.GetOrCreateWindowStore(cfg)
	require.NoError(t, err)

	// Two events fill the buffer and close the window.
	_, _, err = trig.processPayload(context.Background(), map[string]interface{}{"v": 10.0}, "t", 0, 0)
	require.NoError(t, err, "first event must succeed")

	out, et, err := trig.processPayload(context.Background(), map[string]interface{}{"v": 20.0}, "t", 0, 1)
	require.NoError(t, err, "second event must succeed")
	require.Equal(t, EventTypeWindowClose, et)
	require.NotNil(t, out)
	assert.Equal(t, 30.0, out.WindowResult.Result)

	// Third event on the freshly-reset window must also not return an error
	// even if drop_newest fires.
	_, _, err = trig.processPayload(context.Background(), map[string]interface{}{"v": 99.0}, "t", 0, 2)
	require.NoError(t, err, "drop_newest must NOT return an error even when buffer is full")
}

// TestProcessPayload_TumblingCount_MaxBuffer_Error verifies that the "error"
// overflow policy causes processPayload to return a non-nil error.
func TestProcessPayload_TumblingCount_MaxBuffer_Error(t *testing.T) {
	wn := "tt-overflow-err"
	clearWindow(wn)
	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 5,
		Function: "sum", ValueField: "v",
		MaxBufferSize:  2,
		OverflowPolicy: "error",
	})
	cfg := buildWindowConfig(trig.settings, wn)
	_, err := kafkastream.GetOrCreateWindowStore(cfg)
	require.NoError(t, err)

	// First two events fill the buffer.
	_, _, err = trig.processPayload(context.Background(), map[string]interface{}{"v": 1.0}, "t", 0, 0)
	require.NoError(t, err)
	_, _, err = trig.processPayload(context.Background(), map[string]interface{}{"v": 2.0}, "t", 0, 1)
	require.NoError(t, err)

	// Third event should fail with buffer-full error.
	_, _, err = trig.processPayload(context.Background(), map[string]interface{}{"v": 3.0}, "t", 0, 2)
	assert.Error(t, err, "overflow policy=error should return a non-nil error when buffer is full")
}

// TestProcessPayload_MaxKeys_Exceeded_Error verifies that when MaxKeys is reached,
// a new unique key is rejected with an error.
// NOTE: MaxKeys is enforced against the global registry size. The test uses a
// high MaxKeys value relative to the registry so that only the dedicated keyed
// sub-windows for this test hit the cap.
func TestProcessPayload_MaxKeys_Exceeded_Error(t *testing.T) {
	wn := "tt-maxkeys-ex"
	// Register the base window name and two keyed sub-window names that will be
	// created by processPayload, then clean them up so the registry is empty for
	// the controlled names.
	clearWindow(wn, wn+":A", wn+":B", wn+":C")
	t.Cleanup(func() { clearWindow(wn, wn+":A", wn+":B", wn+":C") })

	// Count how many windows are currently in the registry (from other tests).
	snaps := kafkastream.ListSnapshots()
	baseCount := int64(len(snaps))
	maxKeys := baseCount + 2 // allow exactly 2 more (for "A" and "B")

	trig := newAggregateTrigger(&Settings{
		Topic: "t", ConsumerGroup: "g",
		WindowName: wn, WindowType: "TumblingCount", WindowSize: 10,
		Function: "sum", ValueField: "v", KeyField: "device",
		MaxKeys: maxKeys,
	})

	// Two keys should succeed.
	_, _, err := trig.processPayload(context.Background(), map[string]interface{}{"v": 1.0, "device": "A"}, "t", 0, 0)
	require.NoError(t, err, "first key should succeed")
	_, _, err = trig.processPayload(context.Background(), map[string]interface{}{"v": 2.0, "device": "B"}, "t", 0, 1)
	require.NoError(t, err, "second key should succeed")

	// Third unique key should fail.
	_, _, err = trig.processPayload(context.Background(), map[string]interface{}{"v": 3.0, "device": "C"}, "t", 0, 2)
	assert.Error(t, err, "3rd key when MaxKeys is reached should be rejected")
}

// TestOutput_RoundTrip_Source verifies FromMap round-trips Source sub-fields
// including LateEvent / LateReason.
func TestOutput_RoundTrip_Source(t *testing.T) {
	orig := &Output{
		WindowResult: WindowResult{
			Result: 42.0, Count: 3, WindowName: "w", Key: "k",
			WindowClosed: true, DroppedCount: 1, LateEventCount: 2,
		},
		Source: Source{
			Topic:      "my-topic",
			Partition:  1,
			Offset:     99,
			LateEvent:  true,
			LateReason: "event older than watermark - allowedLateness",
		},
	}
	m := orig.ToMap()
	var restored Output
	require.NoError(t, restored.FromMap(m))
	assert.Equal(t, orig.Source.Topic, restored.Source.Topic)
	assert.Equal(t, orig.Source.LateEvent, restored.Source.LateEvent)
	assert.Equal(t, orig.Source.LateReason, restored.Source.LateReason)
	assert.Equal(t, orig.WindowResult.DroppedCount, restored.WindowResult.DroppedCount)
	assert.Equal(t, orig.WindowResult.LateEventCount, restored.WindowResult.LateEventCount)
}
