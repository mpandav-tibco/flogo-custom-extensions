package datapipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Run is a run as the engine's REST API gives it: the fields the activity
// reads, and all of them (Raw).
type Run struct {
	RunID         string    `json:"runId"`
	Status        string    `json:"status"`
	RowsProcessed int64     `json:"rowsProcessed"`
	RowsWritten   int64     `json:"rowsWritten"`
	RowsFailed    int64     `json:"rowsFailed"`
	RowsDropped   int64     `json:"rowsDropped"`
	DurationMs    int64     `json:"durationMs"`
	Error         string    `json:"error"`
	StartedAt     time.Time `json:"startedAt"`
	FinishedAt    time.Time `json:"finishedAt"`

	Raw map[string]interface{} `json:"-"`
}

// inFlight reports whether the run has not ended: running, or waiting its
// turn.
func (r *Run) inFlight() bool { return r.Status == "running" || r.Status == "queued" }

// apiError is the engine's answer to a request it did not do: its HTTP
// status, and the code and message of its body.
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("HTTP %d %s: %s", e.Status, e.Code, e.Message)
}

// codeOf is err's engine error code, "" when err is no answer of the engine.
func codeOf(err error) string {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

// messageOf is the engine's message in err, else err's text.
func messageOf(err error) string {
	var ae *apiError
	if errors.As(err, &ae) && ae.Message != "" {
		return ae.Message
	}
	return err.Error()
}

// transient reports whether err may pass: the engine unreachable, or
// answering 5xx (restarting, overloaded).
func transient(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Status >= 500
	}
	return err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// errNoRunEndpoint: the engine predates GET …/run/{runId}; its runs are read
// from …/status.
var errNoRunEndpoint = errors.New("the engine has no GET …/run/{runId}")

// client calls one engine's REST API.
type client struct {
	http  *http.Client
	base  *url.URL
	token string
}

func newClient(hc *http.Client, engineURL, token string) (*client, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(engineURL), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("engineUrl %q: the engine API's http(s) URL, e.g. http://orders-etl:9080", engineURL)
	}
	return &client{http: hc, base: u, token: token}, nil
}

// pipelinePath is the API path of pipeline (its name or ref), then rest.
func (c *client) pipelinePath(pipeline string, rest ...string) string {
	p := c.base.String() + "/v1/pipelines/" + url.PathEscape(pipeline)
	for _, r := range rest {
		p += "/" + url.PathEscape(r)
	}
	return p
}

// start asks the engine to run the pipeline (asynchronously: the engine
// answers as it takes the run), with header adding the caller's trace.
func (c *client) start(ctx context.Context, in *Input, header func(http.Header)) (*Run, error) {
	body := map[string]interface{}{"runId": in.RunID}
	if len(in.Params) > 0 {
		body["params"] = in.Params
	}
	if in.InputPath != "" {
		body["inputPath"] = in.InputPath
	}
	// Sent only when asked for: an engine that predates them refuses fields
	// it does not know.
	if in.Overlap != OverlapAllow {
		body["overlap"] = in.Overlap
	}
	if in.CallbackURL != "" {
		body["callbackUrl"] = in.CallbackURL
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("params: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.pipelinePath(in.Pipeline, "run")+"?async=true", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if header != nil {
		header(req.Header)
	}
	return c.do(req)
}

// get is the run runID of pipeline as the engine has it now.
func (c *client) get(ctx context.Context, pipeline, runID string) (*Run, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.pipelinePath(pipeline, "run", runID), nil)
	if err != nil {
		return nil, err
	}
	run, err := c.do(req)
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound && strings.HasPrefix(ae.Message, "route not found") {
		return nil, errNoRunEndpoint
	}
	return run, err
}

// fromStatus is the run runID of pipeline as an engine that predates GET
// …/run/{runId} lists it: the last of that id among its runs (those in flight
// come after the finished ones).
func (c *client) fromStatus(ctx context.Context, pipeline, runID string) (*Run, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.pipelinePath(pipeline, "status"), nil)
	if err != nil {
		return nil, err
	}
	raw, err := c.send(req)
	if err != nil {
		return nil, err
	}
	var st struct {
		Runs []json.RawMessage `json:"runs"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("the engine's status: %w", err)
	}
	for i := len(st.Runs) - 1; i >= 0; i-- {
		run, err := decodeRun(st.Runs[i])
		if err == nil && run.RunID == runID {
			return run, nil
		}
	}
	return nil, &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "no run " + runID + " among the pipeline's runs"}
}

// do sends req and decodes the run the engine answers with.
func (c *client) do(req *http.Request) (*Run, error) {
	raw, err := c.send(req)
	if err != nil {
		return nil, err
	}
	return decodeRun(raw)
}

// send sends req with the token, and returns the body of a 2xx answer, else
// the engine's error.
func (c *client) send(req *http.Request) ([]byte, error) {
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		ae := &apiError{Status: resp.StatusCode}
		var body struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &body) == nil && (body.Code != "" || body.Message != "") {
			ae.Code, ae.Message = body.Code, body.Message
		} else {
			ae.Message = strings.TrimSpace(string(raw))
		}
		return nil, ae
	}
	return raw, nil
}

func decodeRun(raw []byte) (*Run, error) {
	var run Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return nil, fmt.Errorf("the engine's run: %w", err)
	}
	if err := json.Unmarshal(raw, &run.Raw); err != nil {
		return nil, fmt.Errorf("the engine's run: %w", err)
	}
	return &run, nil
}

// errWaitTimedOut ends a wait the run outlasted.
var errWaitTimedOut = errors.New("the run did not end within waitTimeoutSeconds")

// wait asks how the run runID of pipeline goes every interval until it has
// ended, and returns it then — or, with an error, the run as last seen: the
// wait timed out (errWaitTimedOut), ctx ended, the engine no longer knows
// the run (404: it restarted, or the run fell out of its history) or refuses
// to say. While the engine is unreachable or answers 5xx it asks again.
func (c *client) wait(ctx context.Context, pipeline, runID string, timeout, interval time.Duration, warn func(string, ...interface{})) (*Run, error) {
	deadline := time.Now().Add(timeout)
	fromStatus := false
	var last *Run
	pause := min(500*time.Millisecond, interval) // a short run ends before the first interval
	for unreachable := 0; ; {
		var run *Run
		var err error
		if fromStatus {
			run, err = c.fromStatus(ctx, pipeline, runID)
		} else if run, err = c.get(ctx, pipeline, runID); errors.Is(err, errNoRunEndpoint) {
			fromStatus = true
			continue
		}
		switch {
		case err == nil:
			last, unreachable = run, 0
			if !run.inFlight() {
				return run, nil
			}
		case ctx.Err() != nil:
			return last, ctx.Err()
		case transient(err):
			if unreachable++; unreachable == 1 || unreachable%30 == 0 {
				warn("run %s: the engine did not say how the run goes (%v) — asking again", runID, err)
			}
		default:
			return last, err
		}
		if !time.Now().Before(deadline) {
			return last, errWaitTimedOut
		}
		t := time.NewTimer(min(pause, time.Until(deadline)))
		select {
		case <-ctx.Done():
			t.Stop()
			return last, ctx.Err()
		case <-t.C:
		}
		pause = interval
	}
}
