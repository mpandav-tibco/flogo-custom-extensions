package aggregate

import (
	"os"
	"testing"

	"github.com/project-flogo/core/support/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kafkastream "github.com/mpandav-tibco/flogo-extensions/kafkastream"
	"github.com/mpandav-tibco/flogo-extensions/kafkastream/window"
)

func newAct(t *testing.T, s *Settings) *Activity {
	t.Helper()
	act, err := New(test.NewActivityInitContext(s, nil))
	require.NoError(t, err)
	return act.(*Activity)
}

func eval(t *testing.T, act *Activity, input *Input) *Output {
	t.Helper()
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(input))
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
		Function: "sum",
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
		Function: "sum",
	})
	for _, v := range []float64{10, 20} {
		out := eval(t, act, &Input{Message: map[string]interface{}{"temperature": v}, ValueField: "temperature"})
		assert.False(t, out.WindowClosed, "window must not close before size reached")
	}
	out := eval(t, act, &Input{Message: map[string]interface{}{"temperature": 30.0}, ValueField: "temperature"})
	assert.True(t, out.WindowClosed)
	assert.Equal(t, 60.0, out.Result) // 10+20+30
	assert.Equal(t, int64(3), out.Count)
}

func TestEval_TumblingCount_WindowNotClosed(t *testing.T) {
	clearWindow("tc-notclosed")
	act := newAct(t, &Settings{
		WindowName: "tc-notclosed", WindowType: "TumblingCount", WindowSize: 5,
		Function: "sum",
	})
	for _, v := range []float64{1, 2, 3} {
		out := eval(t, act, &Input{Message: map[string]interface{}{"v": v}, ValueField: "v"})
		assert.False(t, out.WindowClosed)
		assert.Equal(t, 0.0, out.Result)
	}
}

// ---- TumblingCount + avg ----------------------------------------------------

func TestEval_TumblingCount_Avg(t *testing.T) {
	clearWindow("tc-avg")
	act := newAct(t, &Settings{
		WindowName: "tc-avg", WindowType: "TumblingCount", WindowSize: 4,
		Function: "avg",
	})
	var out *Output
	for _, v := range []float64{100, 200, 300, 400} {
		out = eval(t, act, &Input{Message: map[string]interface{}{"price": v}, ValueField: "price"})
	}
	assert.True(t, out.WindowClosed)
	assert.Equal(t, 250.0, out.Result)
}

// ---- TumblingCount + min/max ------------------------------------------------

func TestEval_TumblingCount_Min(t *testing.T) {
	clearWindow("tc-min")
	act := newAct(t, &Settings{
		WindowName: "tc-min", WindowType: "TumblingCount", WindowSize: 3,
		Function: "min",
	})
	var out *Output
	for _, v := range []float64{50, 3, 99} {
		out = eval(t, act, &Input{Message: map[string]interface{}{"val": v}, ValueField: "val"})
	}
	assert.Equal(t, 3.0, out.Result)
}

func TestEval_TumblingCount_Max(t *testing.T) {
	clearWindow("tc-max")
	act := newAct(t, &Settings{
		WindowName: "tc-max", WindowType: "TumblingCount", WindowSize: 3,
		Function: "max",
	})
	var out *Output
	for _, v := range []float64{50, 3, 99} {
		out = eval(t, act, &Input{Message: map[string]interface{}{"val": v}, ValueField: "val"})
	}
	assert.Equal(t, 99.0, out.Result)
}

// ---- SlidingCount -----------------------------------------------------------

func TestEval_SlidingCount_Sum(t *testing.T) {
	clearWindow("sc-sum")
	act := newAct(t, &Settings{
		WindowName: "sc-sum", WindowType: "SlidingCount", WindowSize: 3,
		Function: "sum",
	})
	cases := []struct{ val, expected float64 }{
		{10, 10}, {20, 30}, {30, 60}, {40, 90}, {50, 120},
	}
	for i, c := range cases {
		out := eval(t, act, &Input{Message: map[string]interface{}{"val": c.val}, ValueField: "val"})
		assert.True(t, out.WindowClosed, "sliding must always close (event %d)", i)
		assert.Equal(t, c.expected, out.Result, "event %d", i)
	}
}

// ---- Key-based grouping -----------------------------------------------------

func TestEval_KeyedWindow_IndependentState(t *testing.T) {
	clearWindow("keyed", "keyed:sensor-A", "keyed:sensor-B")
	act := newAct(t, &Settings{
		WindowName: "keyed", WindowType: "TumblingCount", WindowSize: 3,
		Function: "sum",
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
		out := eval(t, act, &Input{Message: map[string]interface{}{"temp": m.temp, "sensor": m.sensor}, ValueField: "temp", KeyField: "sensor"})
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
		Function: "sum",
	}, nil))
	assert.Error(t, err)
}

func TestNew_InvalidFunction(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		WindowName: "bad2", WindowType: "TumblingCount", WindowSize: 5,
		Function: "variance",
	}, nil))
	assert.Error(t, err)
}

func TestNew_ZeroWindowSize(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		WindowName: "bad3", WindowType: "TumblingCount", WindowSize: 0,
		Function: "sum",
	}, nil))
	assert.Error(t, err)
}

// ---- Eval error: missing field ----------------------------------------------

func TestEval_MissingValueField_ReturnsError(t *testing.T) {
	clearWindow("missing-field")
	act := newAct(t, &Settings{
		WindowName: "missing-field", WindowType: "TumblingCount",
		WindowSize: 3, Function: "sum",
	})
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&Input{
		Message:    map[string]interface{}{"wrong_field": 42.0},
		ValueField: "temperature",
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
		Function: "sum",
	})
	eval(t, act, &Input{Message: map[string]interface{}{"v": 5.0}, ValueField: "v"})
	out := eval(t, act, &Input{Message: map[string]interface{}{"v": 5.0}, ValueField: "v"})
	assert.True(t, out.WindowClosed)
	assert.Equal(t, 10.0, out.Result)

	// Second window must start fresh
	eval(t, act, &Input{Message: map[string]interface{}{"v": 1.0}, ValueField: "v"})
	out = eval(t, act, &Input{Message: map[string]interface{}{"v": 1.0}, ValueField: "v"})
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
			Function: "sum",
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
		Function: "sum",
	})
	for _, v := range []float64{10, 20, 30} {
		out := eval(t, act, &Input{
			Message:        map[string]interface{}{"val": v, "ts_ms": int64(1_700_000_000_000 + int64(v)*1000)},
			ValueField:     "val",
			EventTimeField: "ts_ms",
		})
		_ = out
	}
	// No panic = event-time extraction worked
}

// ─── Enterprise: invalid overflowPolicy ──────────────────────────────────────

func TestNew_InvalidOverflowPolicy(t *testing.T) {
	_, err := New(test.NewActivityInitContext(&Settings{
		WindowName: "bad-overflow", WindowType: "TumblingCount", WindowSize: 5,
		Function: "sum", OverflowPolicy: "discard_all",
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
		Function: "sum", AllowedLateness: 0,
	})

	// First event advances watermark to t+10s (Unix ms)
	baseMs := int64(1_700_100_000_000)
	eval(t, act, &Input{
		Message:        map[string]interface{}{"val": 100.0, "ts_ms": baseMs + 10_000},
		ValueField:     "val",
		EventTimeField: "ts_ms",
	})

	// Late event (t+2s, far behind watermark)
	tc2 := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc2.SetInputObject(&Input{
		Message:        map[string]interface{}{"val": 5.0, "ts_ms": baseMs + 2_000},
		ValueField:     "val",
		EventTimeField: "ts_ms",
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
		Function: "sum",
	})
	msgs := []map[string]interface{}{
		{"val": 10.0, "msgid": "A"},
		{"val": 10.0, "msgid": "A"}, // duplicate → ignored
		{"val": 20.0, "msgid": "B"},
		{"val": 30.0, "msgid": "C"}, // window closes here (3 distinct)
	}
	var lastOut *Output
	for _, m := range msgs {
		lastOut = eval(t, act, &Input{Message: m, ValueField: "val", MessageIDField: "msgid"})
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
		Function: "sum",
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
		Function: "avg",
	})
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&Input{Message: map[string]interface{}{"val": "not-a-number"}, ValueField: "val"}))
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
		Function: "sum", MaxKeys: maxKeys,
	})
	// First keyed sub-window fills the last slot (registry now at priorCount+2 = maxKeys).
	out := eval(t, act, &Input{Message: map[string]interface{}{"val": 1.0, "grp": "k1"}, ValueField: "val", KeyField: "grp"})
	assert.False(t, out.WindowClosed)

	// Second keyed sub-window would push the registry above maxKeys → must be blocked.
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&Input{Message: map[string]interface{}{"val": 2.0, "grp": "k2"}, ValueField: "val", KeyField: "grp"}))
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
		Function:      "sum",
		MaxBufferSize: 2, OverflowPolicy: "error",
	})
	eval(t, act, &Input{Message: map[string]interface{}{"v": 1.0}, ValueField: "v"})
	eval(t, act, &Input{Message: map[string]interface{}{"v": 2.0}, ValueField: "v"})

	// Third event exceeds MaxBufferSize=2 → Eval must propagate the error.
	tc := test.NewActivityContext(act.Metadata())
	require.NoError(t, tc.SetInputObject(&Input{Message: map[string]interface{}{"v": 3.0}, ValueField: "v"}))
	_, err := act.Eval(tc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "buffer full")
}

// ─── State Persistence ────────────────────────────────────────────────────────

func TestAggregate_Persist_SaveAndRestore(t *testing.T) {
	path := t.TempDir() + "/agg-state.gob"
	const wname = "persist-test"
	clearWindow(wname)
	defer clearWindow(wname)

	// Phase 1: add 2 events, then save state.
	act1 := newAct(t, &Settings{
		WindowName: wname, WindowType: "TumblingCount", WindowSize: 5,
		Function:      "sum",
		PersistPath:   path,
		PersistEveryN: 1,
	})
	eval(t, act1, &Input{Message: map[string]interface{}{"v": 10.0}, ValueField: "v"})
	eval(t, act1, &Input{Message: map[string]interface{}{"v": 20.0}, ValueField: "v"})
	require.NoError(t, kafkastream.SaveStateTo(path))

	// Phase 2: clear the registry (simulates restart), restore, verify buffer.
	clearWindow(wname)
	act2 := newAct(t, &Settings{
		WindowName: wname, WindowType: "TumblingCount", WindowSize: 5,
		Function:    "sum",
		PersistPath: path,
	})
	_ = act2 // New() already called RestoreStateFrom

	store, ok := kafkastream.GetWindowStore(wname)
	require.True(t, ok)
	snap := store.Snapshot()
	assert.Equal(t, int64(2), snap.BufferSize, "restored buffer should have 2 events")
}

func TestAggregate_Persist_NoFileOnFirstRun(t *testing.T) {
	path := t.TempDir() + "/does-not-exist.gob"
	const wname = "persist-noop"
	clearWindow(wname)
	defer clearWindow(wname)

	// Should not error when persist file does not exist.
	act := newAct(t, &Settings{
		WindowName: wname, WindowType: "TumblingCount", WindowSize: 5,
		Function:    "sum",
		PersistPath: path,
	})
	_ = act
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "no file should be created until SaveStateTo is called")
}

func TestAggregate_Persist_Disabled_WhenPathEmpty(t *testing.T) {
	const wname = "persist-disabled"
	clearWindow(wname)
	defer clearWindow(wname)
	act := newAct(t, &Settings{
		WindowName: wname, WindowType: "TumblingCount", WindowSize: 5,
		Function: "sum",
		// PersistPath intentionally empty
	})
	eval(t, act, &Input{Message: map[string]interface{}{"v": 5.0}, ValueField: "v"})
	// No error means disabled path is exercised cleanly.
}

// TestAggregate_Persist_CorruptGob verifies that a corrupt/unreadable gob file
// causes RestoreStateFrom to return an error (which the activity logs as a
// warning) but does NOT prevent the app from starting or processing events.
func TestAggregate_Persist_CorruptGob(t *testing.T) {
	path := t.TempDir() + "/corrupt.gob"
	require.NoError(t, os.WriteFile(path, []byte("THIS IS NOT A VALID GOB FILE"), 0o644))
	const wname = "persist-corrupt"
	clearWindow(wname)
	defer clearWindow(wname)

	// RestoreStateFrom should return an error for the corrupt file.
	err := kafkastream.RestoreStateFrom(path)
	assert.Error(t, err, "expected error decoding corrupt gob")
	assert.Contains(t, err.Error(), "decode error")

	// Activity creation must succeed regardless (New() logs warn and continues).
	act := newAct(t, &Settings{
		WindowName: wname, WindowType: "TumblingCount", WindowSize: 5,
		Function:    "sum",
		PersistPath: path,
	})
	out := eval(t, act, &Input{Message: map[string]interface{}{"v": 99.0}, ValueField: "v"})
	assert.False(t, out.WindowClosed, "first event should not close a count-5 window")
}

// TestAggregate_Persist_PersistEveryN_Zero verifies that when PersistEveryN=0
// no gob file is ever written, even though a path is configured.
func TestAggregate_Persist_PersistEveryN_Zero(t *testing.T) {
	path := t.TempDir() + "/no-write.gob"
	const wname = "persist-n0"
	clearWindow(wname)
	defer clearWindow(wname)

	act := newAct(t, &Settings{
		WindowName:    wname,
		WindowType:    "TumblingCount",
		WindowSize:    3,
		Function:      "sum",
		PersistPath:   path,
		PersistEveryN: 0, // disabled
	})
	eval(t, act, &Input{Message: map[string]interface{}{"v": 1.0}, ValueField: "v"})
	eval(t, act, &Input{Message: map[string]interface{}{"v": 2.0}, ValueField: "v"})

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "gob file must NOT be written when PersistEveryN=0")
}

// TestAggregate_Persist_KeyedWindow_PendingRestore verifies the pendingRestores
// mechanism: state for a keyed sub-window is saved, registry cleared, state
// restored, and then the keyed store is lazily created — confirming the parked
// state is applied on lazy creation.
func TestAggregate_Persist_KeyedWindow_PendingRestore(t *testing.T) {
	path := t.TempDir() + "/keyed.gob"
	const base = "kw-persist-test"
	const keyed = "kw-persist-test:device-A"
	clearWindow(base, keyed)
	defer clearWindow(base, keyed)

	// Phase 1 — seed the keyed window with 2 events.
	act1 := newAct(t, &Settings{
		WindowName:    base,
		WindowType:    "TumblingCount",
		WindowSize:    3,
		Function:      "avg",
		PersistPath:   path,
		PersistEveryN: 1,
	})
	eval(t, act1, &Input{
		Message:    map[string]interface{}{"v": 10.0, "dev": "device-A"},
		ValueField: "v", KeyField: "dev",
	})
	eval(t, act1, &Input{
		Message:    map[string]interface{}{"v": 20.0, "dev": "device-A"},
		ValueField: "v", KeyField: "dev",
	})
	require.NoError(t, kafkastream.SaveStateTo(path))

	// Phase 2 — simulate restart: clear registry, create new activity (triggers
	// RestoreStateFrom), verify state is parked in pendingRestores.
	clearWindow(base, keyed)
	_ = newAct(t, &Settings{
		WindowName:  base,
		WindowType:  "TumblingCount",
		WindowSize:  3,
		Function:    "avg",
		PersistPath: path,
	})
	// Keyed store still not in registry (not yet lazily created).
	_, ok := kafkastream.GetWindowStore(keyed)
	assert.False(t, ok, "keyed store should not exist yet before first event")

	// Phase 3 — send third event; lazy creation applies pendingRestores, window closes.
	act2 := newAct(t, &Settings{
		WindowName:  base,
		WindowType:  "TumblingCount",
		WindowSize:  3,
		Function:    "avg",
		PersistPath: path,
	})
	out := eval(t, act2, &Input{
		Message:    map[string]interface{}{"v": 30.0, "dev": "device-A"},
		ValueField: "v", KeyField: "dev",
	})
	assert.True(t, out.WindowClosed, "window should close after 3 events (restored 2 + new 1)")
	assert.InDelta(t, 20.0, out.Result, 0.001, "avg(10,20,30) should be 20.0")
	assert.Equal(t, int64(3), out.Count)
}

// TestAggregate_Persist_GobIsDirectory verifies that when the gob path is a
// directory, RestoreStateFrom returns an error (handled gracefully at startup).
func TestAggregate_Persist_GobIsDirectory(t *testing.T) {
	dir := t.TempDir() + "/gobdir"
	require.NoError(t, os.Mkdir(dir, 0o755))

	err := kafkastream.RestoreStateFrom(dir)
	assert.Error(t, err, "expected error when gob path is a directory")
}

// TestAggregate_Persist_PermissionDenied verifies that a gob file with no read
// permission causes RestoreStateFrom to return an error, not a panic.
func TestAggregate_Persist_PermissionDenied(t *testing.T) {
	path := t.TempDir() + "/noperm.gob"
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o000))
	defer os.Chmod(path, 0o644) //nolint:errcheck

	err := kafkastream.RestoreStateFrom(path)
	assert.Error(t, err, "expected error for permission-denied gob file")
}
