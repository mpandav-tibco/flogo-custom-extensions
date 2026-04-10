package aggregate

import (
	"testing"

	"github.com/project-flogo/core/support/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kafkastream "github.com/milindpandav/flogo-extensions/kafka-stream"
	"github.com/milindpandav/flogo-extensions/kafka-stream/window"
)

func newAct(t *testing.T, s *Settings) *Activity {
	t.Helper()
	act, err := New(test.NewActivityInitContext(s, nil))
	require.NoError(t, err)
	return act.(*Activity)
}

func eval(t *testing.T, act *Activity, message map[string]interface{}) *Output {
	t.Helper()
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&Input{Message: message}))
	done, err := act.Eval(tc)
	require.NoError(t, err)
	assert.True(t, done)
	out := &Output{}
	require.NoError(t, tc.GetOutputObject(out))
	return out
}

func clearWindow(names ...string) {
	for _, n := range names {
		kafkastream.UnregisterWindowStore(n)
	}
}

// ---- Registration -----------------------------------------------------------

func TestRegister(t *testing.T) {
	clearWindow("reg-test")
	act, err := New(test.NewActivityInitContext(&Settings{
		WindowName: "reg-test", WindowType: "TumblingCount", WindowSize: 5,
		Function: "sum", ValueField: "v",
	}, nil))
	clearWindow("reg-test")
	require.NoError(t, err)
	assert.NotNil(t, act)
}

// ---- TumblingCount + sum ----------------------------------------------------

func TestEval_TumblingCount_Sum(t *testing.T) {
	clearWindow("tc-sum")
	act := newAct(t, &Settings{
		WindowName: "tc-sum", WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "temperature",
	})
	for _, v := range []float64{10, 20} {
		out := eval(t, act, map[string]interface{}{"temperature": v})
		assert.False(t, out.WindowClosed, "window must not close before size reached")
	}
	out := eval(t, act, map[string]interface{}{"temperature": 30.0})
	assert.True(t, out.WindowClosed)
	assert.Equal(t, 60.0, out.Result) // 10+20+30
	assert.Equal(t, int64(3), out.Count)
}

func TestEval_TumblingCount_WindowNotClosed(t *testing.T) {
	clearWindow("tc-notclosed")
	act := newAct(t, &Settings{
		WindowName: "tc-notclosed", WindowType: "TumblingCount", WindowSize: 5,
		Function: "sum", ValueField: "v",
	})
	for _, v := range []float64{1, 2, 3} {
		out := eval(t, act, map[string]interface{}{"v": v})
		assert.False(t, out.WindowClosed)
		assert.Equal(t, 0.0, out.Result)
	}
}

// ---- TumblingCount + avg ----------------------------------------------------

func TestEval_TumblingCount_Avg(t *testing.T) {
	clearWindow("tc-avg")
	act := newAct(t, &Settings{
		WindowName: "tc-avg", WindowType: "TumblingCount", WindowSize: 4,
		Function: "avg", ValueField: "price",
	})
	var out *Output
	for _, v := range []float64{100, 200, 300, 400} {
		out = eval(t, act, map[string]interface{}{"price": v})
	}
	assert.True(t, out.WindowClosed)
	assert.Equal(t, 250.0, out.Result)
}

// ---- TumblingCount + min/max ------------------------------------------------

func TestEval_TumblingCount_Min(t *testing.T) {
	clearWindow("tc-min")
	act := newAct(t, &Settings{
		WindowName: "tc-min", WindowType: "TumblingCount", WindowSize: 3,
		Function: "min", ValueField: "val",
	})
	var out *Output
	for _, v := range []float64{50, 3, 99} {
		out = eval(t, act, map[string]interface{}{"val": v})
	}
	assert.Equal(t, 3.0, out.Result)
}

func TestEval_TumblingCount_Max(t *testing.T) {
	clearWindow("tc-max")
	act := newAct(t, &Settings{
		WindowName: "tc-max", WindowType: "TumblingCount", WindowSize: 3,
		Function: "max", ValueField: "val",
	})
	var out *Output
	for _, v := range []float64{50, 3, 99} {
		out = eval(t, act, map[string]interface{}{"val": v})
	}
	assert.Equal(t, 99.0, out.Result)
}

// ---- SlidingCount -----------------------------------------------------------

func TestEval_SlidingCount_Sum(t *testing.T) {
	clearWindow("sc-sum")
	act := newAct(t, &Settings{
		WindowName: "sc-sum", WindowType: "SlidingCount", WindowSize: 3,
		Function: "sum", ValueField: "val",
	})
	cases := []struct{ val, expected float64 }{
		{10, 10}, {20, 30}, {30, 60}, {40, 90}, {50, 120},
	}
	for i, c := range cases {
		out := eval(t, act, map[string]interface{}{"val": c.val})
		assert.True(t, out.WindowClosed, "sliding must always close (event %d)", i)
		assert.Equal(t, c.expected, out.Result, "event %d", i)
	}
}

// ---- Key-based grouping -----------------------------------------------------

func TestEval_KeyedWindow_IndependentState(t *testing.T) {
	clearWindow("keyed", "keyed:sensor-A", "keyed:sensor-B")
	act := newAct(t, &Settings{
		WindowName: "keyed", WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "temp", KeyField: "sensor",
	})
	msgs := []struct {
		sensor string
		temp   float64
	}{
		{"sensor-A", 10}, {"sensor-B", 5},
		{"sensor-A", 20}, {"sensor-B", 5},
		{"sensor-A", 30},
		{"sensor-B", 5},
	}
	results := make(map[string]float64)
	for _, m := range msgs {
		out := eval(t, act, map[string]interface{}{"temp": m.temp, "sensor": m.sensor})
		if out.WindowClosed {
			results[out.Key] = out.Result
		}
	}
	assert.Equal(t, 60.0, results["sensor-A"])
	assert.Equal(t, 15.0, results["sensor-B"])
}

// ---- New() validation errors ------------------------------------------------

func TestNew_InvalidWindowType(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		WindowName: "bad", WindowType: "InvalidType", WindowSize: 5,
		Function: "sum", ValueField: "v",
	}, nil))
	assert.Error(t, err)
}

func TestNew_InvalidFunction(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		WindowName: "bad2", WindowType: "TumblingCount", WindowSize: 5,
		Function: "variance", ValueField: "v",
	}, nil))
	assert.Error(t, err)
}

func TestNew_ZeroWindowSize(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		WindowName: "bad3", WindowType: "TumblingCount", WindowSize: 0,
		Function: "sum", ValueField: "v",
	}, nil))
	assert.Error(t, err)
}

// ---- Eval error: missing field ----------------------------------------------

func TestEval_MissingValueField_ReturnsError(t *testing.T) {
	clearWindow("missing-field")
	act := newAct(t, &Settings{
		WindowName: "missing-field", WindowType: "TumblingCount",
		WindowSize: 3, Function: "sum", ValueField: "temperature",
	})
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&Input{
		Message: map[string]interface{}{"wrong_field": 42.0},
	}))
	done, err := act.Eval(tc)
	assert.False(t, done)
	assert.Error(t, err)
}

// ---- Window reuse: second window gets fresh state ---------------------------

func TestEval_SecondWindowFreshState(t *testing.T) {
	clearWindow("tc-reset")
	act := newAct(t, &Settings{
		WindowName: "tc-reset", WindowType: "TumblingCount", WindowSize: 2,
		Function: "sum", ValueField: "v",
	})
	eval(t, act, map[string]interface{}{"v": 5.0})
	out := eval(t, act, map[string]interface{}{"v": 5.0})
	assert.True(t, out.WindowClosed)
	assert.Equal(t, 10.0, out.Result)

	// Second window must start fresh
	eval(t, act, map[string]interface{}{"v": 1.0})
	out = eval(t, act, map[string]interface{}{"v": 1.0})
	assert.True(t, out.WindowClosed)
	assert.Equal(t, 2.0, out.Result, "second window must not accumulate from first")
}

// ---- All window types valid -------------------------------------------------

func TestAllWindowTypes_Valid(t *testing.T) {
	for _, wt := range []window.WindowType{
		window.WindowTumblingTime, window.WindowTumblingCount,
		window.WindowSlidingTime, window.WindowSlidingCount,
	} {
		n := "compile-" + string(wt)
		clearWindow(n)
		act, err := New(test.NewActivityInitContext(&Settings{
			WindowName: n, WindowType: string(wt), WindowSize: 1000,
			Function: "sum", ValueField: "v",
		}, nil))
		clearWindow(n)
		require.NoError(t, err, "window type %s should be valid", wt)
		assert.NotNil(t, act)
	}
}

// ─── Enterprise: event-time field ────────────────────────────────────────────

func TestEval_EventTimeField_UnixMs(t *testing.T) {
	// Send a Unix-ms timestamp in the message; window should use it.
	clearWindow("et-unix")
	act := newAct(t, &Settings{
		WindowName: "et-unix", WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "val", EventTimeField: "ts_ms",
	})
	for _, v := range []float64{10, 20, 30} {
		out := eval(t, act, map[string]interface{}{
			"val":   v,
			"ts_ms": int64(1_700_000_000_000 + int64(v)*1000),
		})
		_ = out
	}
	// No panic = event-time extraction worked
}

// ─── Enterprise: invalid overflowPolicy ──────────────────────────────────────

func TestNew_InvalidOverflowPolicy(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		WindowName: "bad-overflow", WindowType: "TumblingCount", WindowSize: 5,
		Function: "sum", ValueField: "v", OverflowPolicy: "discard_all",
	}, nil))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "overflowPolicy")
}

// ─── Enterprise: late event output ───────────────────────────────────────────

func TestEval_LateEvent_RoutedToDLQ(t *testing.T) {
	// AllowedLateness=0, so any event behind watermark must set lateEvent=true.
	clearWindow("late-dlq")
	act := newAct(t, &Settings{
		WindowName: "late-dlq", WindowType: "TumblingTime", WindowSize: 10000,
		Function: "sum", ValueField: "val", EventTimeField: "ts_ms",
		AllowedLateness: 0,
	})

	// First event advances watermark to t+10s (Unix ms)
	baseMs := int64(1_700_100_000_000)
	eval(t, act, map[string]interface{}{
		"val":   100.0,
		"ts_ms": baseMs + 10_000,
	})

	// Late event (t+2s, far behind watermark)
	tc2 := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc2.SetInputObject(&Input{
		Message: map[string]interface{}{
			"val":   5.0,
			"ts_ms": baseMs + 2_000,
		},
	}))
	done, err := act.Eval(tc2)
	require.NoError(t, err)
	assert.True(t, done)
	out := &Output{}
	require.NoError(t, tc2.GetOutputObject(out))
	assert.True(t, out.LateEvent, "event behind watermark must be flagged as late")
	assert.NotEmpty(t, out.LateReason)
}

// ─── Enterprise: deduplication via MessageIDField ────────────────────────────

func TestEval_MessageID_Deduplication(t *testing.T) {
	clearWindow("dedup-act")
	act := newAct(t, &Settings{
		WindowName: "dedup-act", WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum", ValueField: "val", MessageIDField: "msgid",
	})
	msgs := []map[string]interface{}{
		{"val": 10.0, "msgid": "A"},
		{"val": 10.0, "msgid": "A"}, // duplicate → ignored
		{"val": 20.0, "msgid": "B"},
		{"val": 30.0, "msgid": "C"}, // window closes here (3 distinct)
	}
	var lastOut *Output
	for _, m := range msgs {
		lastOut = eval(t, act, m)
	}
	assert.True(t, lastOut.WindowClosed)
	// Only 3 distinct events: 10+20+30=60
	assert.Equal(t, 60.0, lastOut.Result)
	assert.Equal(t, int64(3), lastOut.Count)
}

// ─── Negative tests ───────────────────────────────────────────────────────────

// TestNew_NegativeWindowSize_ReturnsError verifies that a non-positive windowSize
// is caught at construction time.
func TestNew_NegativeWindowSize_ReturnsError(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		WindowName: "neg-size", WindowType: "TumblingCount", WindowSize: -1,
		Function: "sum", ValueField: "v",
	}, nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "windowSize must be > 0")
}

// TestEval_ValueField_NonNumericString_ReturnsError verifies that a message field
// value that cannot be coerced to float64 causes Eval to return an error.
func TestEval_ValueField_NonNumericString_ReturnsError(t *testing.T) {
	clearWindow("non-numeric")
	defer clearWindow("non-numeric")
	act := newAct(t, &Settings{
		WindowName: "non-numeric", WindowType: "TumblingCount", WindowSize: 3,
		Function: "avg", ValueField: "val",
	})
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&Input{Message: map[string]interface{}{"val": "not-a-number"}}))
	_, err := act.Eval(tc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be coerced to float64")
}

// TestEval_MaxKeys_ExceedLimit_ReturnsError verifies that attempting to create
// more keyed sub-windows than MaxKeys allows causes Eval to return an error.
func TestEval_MaxKeys_ExceedLimit_ReturnsError(t *testing.T) {
	// The window registry is shared across all tests. Compute the current size so
	// we can set a MaxKeys ceiling that allows the base window plus exactly one
	// keyed sub-window, blocking any second keyed sub-window.
	//   priorCount+0 (before New): registry full at priorCount
	//   priorCount+1 (after New):  base window created  → still OK (< maxKeys)
	//   priorCount+2 (k1 created): first keyed window   → still OK (== maxKeys)
	//   priorCount+3 (k2 attempt): maxKeys exceeded     → BLOCKED
	priorCount := int64(len(kafkastream.ListSnapshots()))
	maxKeys := priorCount + 2

	defer clearWindow("maxkeys-dyn", "maxkeys-dyn:k1", "maxkeys-dyn:k2")
	act := newAct(t, &Settings{
		WindowName: "maxkeys-dyn", WindowType: "TumblingCount", WindowSize: 10,
		Function: "sum", ValueField: "val", KeyField: "grp", MaxKeys: maxKeys,
	})
	// First keyed sub-window fills the last slot (registry now at priorCount+2 = maxKeys).
	out := eval(t, act, map[string]interface{}{"val": 1.0, "grp": "k1"})
	assert.False(t, out.WindowClosed)

	// Second keyed sub-window would push the registry above maxKeys → must be blocked.
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&Input{Message: map[string]interface{}{"val": 2.0, "grp": "k2"}}))
	_, err := act.Eval(tc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max keyed windows")
}

// TestEval_OverflowError_PropagatesToEval verifies that when overflow=error the
// window returns an error on the third insert and that error surfaces from Eval.
func TestEval_OverflowError_PropagatesToEval(t *testing.T) {
	clearWindow("overflow-err")
	defer clearWindow("overflow-err")
	act := newAct(t, &Settings{
		WindowName: "overflow-err", WindowType: "TumblingCount", WindowSize: 5,
		Function: "sum", ValueField: "v",
		MaxBufferSize: 2, OverflowPolicy: "error",
	})
	eval(t, act, map[string]interface{}{"v": 1.0})
	eval(t, act, map[string]interface{}{"v": 2.0})

	// Third event exceeds MaxBufferSize=2 → Eval must propagate the error.
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&Input{Message: map[string]interface{}{"v": 3.0}}))
	_, err := act.Eval(tc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "buffer full")
}
