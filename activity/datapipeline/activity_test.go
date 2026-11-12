package datapipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/support/test"
	"github.com/project-flogo/core/support/trace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// engine is a fake data pipeline engine's REST API: it answers the run
// request with start, and each GET of the run with the next of runs (the
// last for those after).
type engine struct {
	*httptest.Server
	t     *testing.T
	start func(w http.ResponseWriter, body map[string]interface{})
	runs  []string // GET …/run/{runId} answers, as JSON, in turn
	gets  atomic.Int32

	mu       sync.Mutex
	posted   map[string]interface{}
	postPath string
	headers  http.Header
}

func newEngine(t *testing.T) *engine {
	t.Helper()
	e := &engine{t: t}
	e.start = func(w http.ResponseWriter, body map[string]interface{}) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"runId":%q,"status":"running","startedAt":"2026-11-12T20:00:00Z"}`, body["runId"])
	}
	e.Server = httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(e.Close)
	return e
}

func (e *engine) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/run"):
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		e.mu.Lock()
		e.posted, e.postPath, e.headers = body, r.URL.EscapedPath()+"?"+r.URL.RawQuery, r.Header.Clone()
		e.mu.Unlock()
		e.start(w, body)
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/run/"):
		i := int(e.gets.Add(1)) - 1
		if len(e.runs) == 0 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"not_found","message":"no run"}`))
			return
		}
		// The run asked for, under the id it was asked for by.
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		answer := strings.Replace(e.runs[min(i, len(e.runs)-1)], `"runId":"r1"`, `"runId":"`+id+`"`, 1)
		if strings.HasPrefix(answer, "HTTP ") { // "HTTP 503 {body}"
			var code int
			_, _ = fmt.Sscanf(answer, "HTTP %d", &code)
			w.WriteHeader(code)
			_, _ = w.Write([]byte(answer[len("HTTP 503 "):]))
			return
		}
		_, _ = w.Write([]byte(answer))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"route not found: ` + r.Method + ` ` + r.URL.Path + `"}`))
	}
}

func (e *engine) request() (map[string]interface{}, string, http.Header) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.posted, e.postPath, e.headers
}

// eval runs the activity with inputs, as a flow does.
func eval(t *testing.T, inputs map[string]interface{}) (*test.TestActivityContext, bool, error) {
	t.Helper()
	act, err := New(test.NewActivityInitContext(nil, nil))
	require.NoError(t, err)
	tc := test.NewActivityContext(act.Metadata())
	for k, v := range inputs {
		tc.SetInput(k, v)
	}
	done, err := act.Eval(tc)
	return tc, done, err
}

func codeOfErr(t *testing.T, err error) (string, bool) {
	t.Helper()
	var ae *activity.Error
	require.True(t, errors.As(err, &ae), "not an activity error: %v", err)
	return ae.Code(), ae.Retriable()
}

const completed = `{"runId":"r1","status":"completed","rowsProcessed":30,"rowsWritten":19,"rowsFailed":1,"rowsDropped":10,
	"durationMs":42,"startedAt":"2026-11-12T20:00:00Z","finishedAt":"2026-11-12T20:00:01Z",
	"stageTimings":[{"id":"source","durationMs":1.5}]}`

// The activity starts the run, follows it and returns its result — every
// field of it in result — sending the token and only the fields asked for.
func TestRunsThePipelineAndWaitsForItsResult(t *testing.T) {
	e := newEngine(t)
	e.runs = []string{`{"runId":"r1","status":"running"}`, completed}
	tc, done, err := eval(t, map[string]interface{}{
		"engineUrl": e.URL + "/", "apiToken": "tok", "pipeline": "res://pipeline:daily-tx",
		"params": map[string]interface{}{"date": "2026-11-12"}, "inputPath": "/in/a.csv",
		"pollIntervalSeconds": 1,
	})
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, "completed", tc.GetOutput("status"))
	assert.EqualValues(t, 19, tc.GetOutput("rowsWritten"))
	assert.EqualValues(t, 10, tc.GetOutput("rowsDropped"))
	assert.Equal(t, "2026-11-12T20:00:01Z", tc.GetOutput("finishedAt"))
	result, _ := tc.GetOutput("result").(map[string]interface{})
	assert.NotNil(t, result["stageTimings"], "result keeps every field of the run")

	body, path, h := e.request()
	assert.Equal(t, "/v1/pipelines/res:%2F%2Fpipeline:daily-tx/run?async=true", path)
	assert.Equal(t, "Bearer tok", h.Get("Authorization"))
	assert.Regexp(t, `^[0-9a-f]{16}$`, body["runId"])
	assert.Equal(t, tc.GetOutput("runId"), body["runId"])
	assert.Equal(t, map[string]interface{}{"date": "2026-11-12"}, body["params"])
	assert.Equal(t, "/in/a.csv", body["inputPath"])
	_, sentOverlap := body["overlap"]
	_, sentCallback := body["callbackUrl"]
	assert.False(t, sentOverlap || sentCallback, "an engine that predates them would refuse the fields: %v", body)
	assert.EqualValues(t, 2, e.gets.Load())
}

// mode "start" returns once the engine has taken the run, with what was
// asked of it.
func TestStartModeReturnsOnceTheEngineTookTheRun(t *testing.T) {
	e := newEngine(t)
	e.start = func(w http.ResponseWriter, body map[string]interface{}) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"runId":%q,"status":"queued","startedAt":"2026-11-12T20:00:00Z"}`, body["runId"])
	}
	tc, done, err := eval(t, map[string]interface{}{
		"engineUrl": e.URL, "pipeline": "daily-tx", "runId": "close-1", "mode": "start",
		"overlap": "queue", "callbackUrl": "http://flows:9999/hooks/done",
	})
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, "queued", tc.GetOutput("status"))
	assert.Equal(t, "close-1", tc.GetOutput("runId"))
	body, _, h := e.request()
	assert.Equal(t, "queue", body["overlap"])
	assert.Equal(t, "http://flows:9999/hooks/done", body["callbackUrl"])
	assert.Empty(t, h.Get("Authorization"))
	assert.EqualValues(t, 0, e.gets.Load(), "start mode does not follow the run")
}

// A pipeline busy (overlap "skip") or suspended is not run: the status says
// skipped, and why — the flow goes on.
func TestABusyPipelineIsSkipped(t *testing.T) {
	for code, msg := range map[string]string{"busy": "daily-tx has run 9f3c in flight (overlap: skip)", "suspended": "the pipeline is suspended"} {
		e := newEngine(t)
		e.start = func(w http.ResponseWriter, _ map[string]interface{}) {
			w.WriteHeader(http.StatusConflict)
			_, _ = fmt.Fprintf(w, `{"code":%q,"message":%q}`, code, msg)
		}
		tc, done, err := eval(t, map[string]interface{}{"engineUrl": e.URL, "pipeline": "daily-tx", "overlap": "skip"})
		require.NoError(t, err, code)
		assert.True(t, done)
		assert.Equal(t, "skipped", tc.GetOutput("status"), code)
		assert.Equal(t, msg, tc.GetOutput("error"))
	}
}

// A retried execution whose run id is in flight follows that run instead of
// starting another.
func TestARunIDInFlightIsFollowed(t *testing.T) {
	e := newEngine(t)
	e.start = func(w http.ResponseWriter, _ map[string]interface{}) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"run_in_flight","message":"run r1 is already in flight"}`))
	}
	e.runs = []string{completed}
	tc, _, err := eval(t, map[string]interface{}{"engineUrl": e.URL, "pipeline": "daily-tx", "runId": "r1", "pollIntervalSeconds": 1})
	require.NoError(t, err)
	assert.Equal(t, "completed", tc.GetOutput("status"))
	assert.EqualValues(t, 30, tc.GetOutput("rowsProcessed"))
}

// A run that ends failed is the activity's error, for the flow's error
// handler — unless failOnRunFailure is false; the outputs say how it ended
// either way.
func TestAFailedRun(t *testing.T) {
	failed := `{"runId":"r1","status":"failed","rowsProcessed":8,"rowsFailed":8,"error":"no rows were written: 8 of 8 failed — amount: -5 < min(0)"}`
	e := newEngine(t)
	e.runs = []string{failed}
	tc, done, err := eval(t, map[string]interface{}{"engineUrl": e.URL, "pipeline": "p", "runId": "r1"})
	require.Error(t, err)
	assert.False(t, done)
	code, retriable := codeOfErr(t, err)
	assert.Equal(t, ErrCodeRunFailed, code)
	assert.False(t, retriable)
	assert.Contains(t, err.Error(), "p run r1 failed: no rows were written")
	assert.Equal(t, "failed", tc.GetOutput("status"))
	var ae *activity.Error
	require.True(t, errors.As(err, &ae))
	assert.Equal(t, "failed", ae.Data().(map[string]interface{})["status"], "$error.data holds the run")

	e2 := newEngine(t)
	e2.runs = []string{failed}
	tc, done, err = eval(t, map[string]interface{}{"engineUrl": e2.URL, "pipeline": "p", "runId": "r1", "failOnRunFailure": false})
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, "failed", tc.GetOutput("status"))
}

// The engine refusing the run is an error of its own; an engine unreachable
// or failing is one the flow may retry.
func TestTheEngineRefusingOrFailing(t *testing.T) {
	for _, c := range []struct {
		status    int
		body      string
		code      string
		retriable bool
	}{
		{http.StatusUnauthorized, `{"code":"unauthorized","message":"missing or invalid API credential"}`, ErrCodeRefused, false},
		{http.StatusForbidden, `{"code":"forbidden","message":"in ops mode, actions need a token"}`, ErrCodeRefused, false},
		{http.StatusNotFound, `{"code":"not_found","message":"no pipeline registered for ref: nope"}`, ErrCodeRefused, false},
		{http.StatusBadRequest, `{"code":"callback_not_allowed","message":"in ops mode a run's result is posted only under DP_API_CALLBACK_URLS"}`, ErrCodeRefused, false},
		{http.StatusInternalServerError, `oops`, ErrCodeEngine, true},
	} {
		e := newEngine(t)
		e.start = func(w http.ResponseWriter, _ map[string]interface{}) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}
		_, _, err := eval(t, map[string]interface{}{"engineUrl": e.URL, "pipeline": "nope"})
		require.Error(t, err)
		code, retriable := codeOfErr(t, err)
		assert.Equal(t, c.code, code, "HTTP %d", c.status)
		assert.Equal(t, c.retriable, retriable, "HTTP %d", c.status)
	}
	gone := newEngine(t)
	gone.Close()
	_, _, err := eval(t, map[string]interface{}{"engineUrl": gone.URL, "pipeline": "p"})
	code, retriable := codeOfErr(t, err)
	assert.Equal(t, ErrCodeEngine, code)
	assert.True(t, retriable)
}

// A run that outlasts waitTimeoutSeconds is an error that says so; the run
// goes on.
func TestTheWaitTimesOut(t *testing.T) {
	e := newEngine(t)
	e.runs = []string{`{"runId":"r1","status":"running","rowsWritten":5}`}
	start := time.Now()
	tc, _, err := eval(t, map[string]interface{}{"engineUrl": e.URL, "pipeline": "p", "runId": "r1", "waitTimeoutSeconds": 1, "pollIntervalSeconds": 1})
	require.Error(t, err)
	code, _ := codeOfErr(t, err)
	assert.Equal(t, ErrCodeTimeout, code)
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.Equal(t, "running", tc.GetOutput("status"))
	assert.EqualValues(t, 5, tc.GetOutput("rowsWritten"))
}

// A run the engine no longer knows (it restarted) is an error of its own.
func TestARunTheEngineForgot(t *testing.T) {
	e := newEngine(t)
	e.runs = []string{`HTTP 404 {"code":"not_found","message":"no run r1 of p, in flight or among the last 100 finished"}`}
	_, _, err := eval(t, map[string]interface{}{"engineUrl": e.URL, "pipeline": "p", "runId": "r1"})
	code, _ := codeOfErr(t, err)
	assert.Equal(t, ErrCodeRunUnknown, code)
}

// While the engine answers 5xx (restarting) the activity asks again.
func TestTheWaitRidesOutTheEngineFailing(t *testing.T) {
	e := newEngine(t)
	e.runs = []string{`HTTP 503 {"code":"unavailable","message":"stopping"}`, `HTTP 502 bad gateway`, completed}
	tc, _, err := eval(t, map[string]interface{}{"engineUrl": e.URL, "pipeline": "p", "runId": "r1", "pollIntervalSeconds": 1})
	require.NoError(t, err)
	assert.Equal(t, "completed", tc.GetOutput("status"))
	assert.EqualValues(t, 3, e.gets.Load())
}

// An engine that predates GET …/run/{runId} is followed through its status.
func TestAnEngineWithoutTheRunEndpoint(t *testing.T) {
	var polls atomic.Int32
	e := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"runId":"r1","status":"running"}`))
		case strings.HasSuffix(r.URL.Path, "/status"):
			status := "running"
			if polls.Add(1) > 1 {
				status = "completed"
			}
			_, _ = fmt.Fprintf(w, `{"ref":"p","runs":[{"runId":"r1","status":"partial"},{"runId":"r0","status":"completed"},{"runId":"r1","status":%q,"rowsWritten":7}]}`, status)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"not_found","message":"route not found: GET ` + r.URL.Path + `"}`))
		}
	}))
	defer e.Close()
	tc, _, err := eval(t, map[string]interface{}{"engineUrl": e.URL, "pipeline": "p", "runId": "r1", "pollIntervalSeconds": 1})
	require.NoError(t, err)
	assert.Equal(t, "completed", tc.GetOutput("status"))
	assert.EqualValues(t, 7, tc.GetOutput("rowsWritten"))
}

// Inputs a run cannot do without, and the choices, are checked first.
func TestConfigErrors(t *testing.T) {
	for _, in := range []map[string]interface{}{
		{"pipeline": "p"},
		{"engineUrl": "http://e:9080"},
		{"engineUrl": "e:9080", "pipeline": "p"},
		{"engineUrl": "http://e:9080", "pipeline": "p", "mode": "later"},
		{"engineUrl": "http://e:9080", "pipeline": "p", "overlap": "sometimes"},
	} {
		_, _, err := eval(t, in)
		require.Error(t, err, "%v", in)
		code, _ := codeOfErr(t, err)
		assert.Equal(t, ErrCodeConfig, code, "%v", in)
	}
}

// fakeTracer stands for Flogo's: it writes a W3C traceparent of the span it
// started for the run.
type fakeTracer struct{ started, finished atomic.Int32 }

type fakeSpan struct{}

func (fakeSpan) TraceObject() interface{}                           { return nil }
func (fakeSpan) SetTags(map[string]interface{}) bool                { return true }
func (fakeSpan) SetTag(string, interface{}) bool                    { return true }
func (fakeSpan) LogKV(map[string]interface{}) bool                  { return true }
func (fakeSpan) TraceID() string                                    { return "4bf92f3577b34da6a3ce929d0e0e4736" }
func (fakeSpan) SpanID() string                                     { return "00f067aa0ba902b7" }
func (f *fakeTracer) Name() string                                  { return "fake" }
func (f *fakeTracer) Start() error                                  { return nil }
func (f *fakeTracer) Stop() error                                   { return nil }
func (f *fakeTracer) FinishTrace(trace.TracingContext, error) error { f.finished.Add(1); return nil }
func (f *fakeTracer) Extract(trace.CarrierFormat, interface{}) (trace.TracingContext, error) {
	return nil, nil
}
func (f *fakeTracer) StartTrace(trace.Config, trace.TracingContext) (trace.TracingContext, error) {
	f.started.Add(1)
	return fakeSpan{}, nil
}
func (f *fakeTracer) Inject(tc trace.TracingContext, _ trace.CarrierFormat, carrier interface{}) error {
	h, ok := carrier.(http.Header)
	if !ok {
		return errors.New("not an http.Header")
	}
	h.Set("traceparent", "00-"+tc.TraceID()+"-"+tc.SpanID()+"-01")
	return nil
}

// With Flogo's tracing on, the run request carries the activity's span: the
// engine's run joins the flow's trace. (The tracer stays registered for the
// tests after this one: Flogo registers one per process.)
func TestTheRunJoinsTheFlowsTrace(t *testing.T) {
	ft := &fakeTracer{}
	require.NoError(t, trace.RegisterTracer(ft))
	e := newEngine(t)
	e.runs = []string{completed}
	_, _, err := eval(t, map[string]interface{}{"engineUrl": e.URL, "pipeline": "p", "runId": "r1"})
	require.NoError(t, err)
	_, _, h := e.request()
	assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", h.Get("traceparent"))
	assert.EqualValues(t, 1, ft.started.Load())
	assert.EqualValues(t, 1, ft.finished.Load())
}
