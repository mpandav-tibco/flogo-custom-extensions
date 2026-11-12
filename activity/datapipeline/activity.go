package datapipeline

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/support/trace"
)

// Flogo seeds unmapped inputs from these struct values, so they must carry the defaults.
var activityMd = activity.ToMetadata(newInput(), &Output{})

func init() {
	_ = activity.Register(&Activity{}, New)
}

// Error codes surface in the flow's error handler as $error.code.
const (
	ErrCodeConfig     = "DATAPIPELINE-4001" // an input is missing or wrong
	ErrCodeRefused    = "DATAPIPELINE-4002" // the engine refused the run (token, unknown pipeline, bad request)
	ErrCodeEngine     = "DATAPIPELINE-4003" // the engine is unreachable or failing (retriable)
	ErrCodeRunFailed  = "DATAPIPELINE-4004" // the run ended failed or cancelled (failOnRunFailure)
	ErrCodeTimeout    = "DATAPIPELINE-4005" // the run did not end within waitTimeoutSeconds
	ErrCodeRunUnknown = "DATAPIPELINE-4006" // the engine no longer knows the run (it restarted)
)

// Activity runs a pipeline of a Flogo data pipeline engine through its REST
// API — the integration layer's way into the data layer: a flow says which
// pipeline runs, on what (params, inputPath), and how its run goes with the
// pipeline's other runs (overlap); the engine moves the data. It waits for
// the run's result, or returns once the engine has taken the run.
type Activity struct {
	http *http.Client
}

func New(_ activity.InitContext) (activity.Activity, error) {
	return &Activity{http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (a *Activity) Metadata() *activity.Metadata {
	return activityMd
}

func (a *Activity) Eval(ctx activity.Context) (done bool, err error) {
	logger := ctx.Logger()
	in := newInput()
	if err := ctx.GetInputObject(in); err != nil {
		return false, activity.NewError(err.Error(), ErrCodeConfig, nil)
	}
	if err := validate(in); err != nil {
		return false, activity.NewError(err.Error(), ErrCodeConfig, nil)
	}
	c, err := newClient(a.http, in.EngineURL, in.APIToken)
	if err != nil {
		return false, activity.NewError(err.Error(), ErrCodeConfig, nil)
	}
	if in.RunID == "" {
		in.RunID = newRunID()
	}

	// The run is a child of the flow's span: the engine's run span joins the
	// trace through the request's traceparent (Flogo's tracer writes it).
	var tc trace.TracingContext
	if trace.Enabled() {
		tracer := trace.GetTracer()
		var terr error
		tc, terr = tracer.StartTrace(trace.Config{
			Operation: "DataPipeline.Run " + in.Pipeline,
			Tags: map[string]interface{}{
				"pipeline.name":       in.Pipeline,
				"pipeline.runId":      in.RunID,
				"datapipeline.engine": c.base.Redacted(),
			},
			Logger: logger,
		}, ctx.GetTracingContext())
		if terr != nil {
			logger.Warnf("Run Data Pipeline: the run's span did not start: %v", terr)
			tc = nil
		}
		defer func() {
			if tc != nil {
				_ = tracer.FinishTrace(tc, err)
			}
		}()
	}
	header := func(h http.Header) {
		if tc != nil {
			if ierr := trace.GetTracer().Inject(tc, trace.HTTPHeaders, h); ierr != nil {
				logger.Debugf("Run Data Pipeline: trace context not sent: %v", ierr)
			}
		}
	}

	goCtx := context.Background()
	if fctx, ok := ctx.(interface{ Context() context.Context }); ok && fctx.Context() != nil {
		goCtx = fctx.Context()
	}

	run, err := c.start(goCtx, in, header)
	switch code := codeOf(err); {
	case err == nil:
		logger.Infof("Run Data Pipeline: %s run %s %s", in.Pipeline, run.RunID, run.Status)
	case code == "run_in_flight":
		// A run of that id is in flight — this execution's own, started by
		// an attempt that did not hear back: follow it.
		logger.Infof("Run Data Pipeline: %s run %s is already in flight — following it", in.Pipeline, in.RunID)
		run = &Run{RunID: in.RunID, Status: "running"}
	case code == "busy" || code == "suspended":
		why := messageOf(err)
		logger.Infof("Run Data Pipeline: %s run %s skipped: %s", in.Pipeline, in.RunID, why)
		return true, setOutput(ctx, &Output{RunID: in.RunID, Status: "skipped", Error: why})
	case transient(err):
		return false, activity.NewRetriableError(fmt.Sprintf("Run Data Pipeline: the engine at %s did not take the run: %v", c.base.Redacted(), err), ErrCodeEngine, nil)
	default:
		return false, activity.NewError(fmt.Sprintf("Run Data Pipeline: the engine refused to run %s: %v", in.Pipeline, err), ErrCodeRefused, nil)
	}
	if in.Mode == ModeStart {
		return true, setOutput(ctx, outputOf(run))
	}

	final, err := c.wait(goCtx, in.Pipeline, in.RunID,
		time.Duration(in.WaitTimeoutSeconds)*time.Second, time.Duration(in.PollIntervalSeconds)*time.Second, logger.Warnf)
	if final == nil {
		final = run
	}
	out := outputOf(final)
	if oerr := setOutput(ctx, out); oerr != nil {
		return false, oerr
	}
	switch code := codeOf(err); {
	case err == nil:
	case errors.Is(err, errWaitTimedOut):
		return false, activity.NewError(fmt.Sprintf("Run Data Pipeline: %s run %s is still %s after %d s (waitTimeoutSeconds); it goes on",
			in.Pipeline, in.RunID, final.Status, in.WaitTimeoutSeconds), ErrCodeTimeout, out.ToMap())
	case code == "not_found":
		return false, activity.NewError(fmt.Sprintf("Run Data Pipeline: the engine no longer knows %s run %s (it keeps runs in memory: did it restart?): %v",
			in.Pipeline, in.RunID, err), ErrCodeRunUnknown, out.ToMap())
	case transient(err):
		return false, activity.NewError(fmt.Sprintf("Run Data Pipeline: %s run %s: the engine stopped answering: %v", in.Pipeline, in.RunID, err), ErrCodeEngine, out.ToMap())
	default:
		return false, activity.NewError(fmt.Sprintf("Run Data Pipeline: %s run %s: %v", in.Pipeline, in.RunID, err), ErrCodeRefused, out.ToMap())
	}
	logger.Infof("Run Data Pipeline: %s run %s %s — processed=%d written=%d failed=%d dropped=%d in %d ms",
		in.Pipeline, final.RunID, final.Status, final.RowsProcessed, final.RowsWritten, final.RowsFailed, final.RowsDropped, final.DurationMs)
	if in.FailOnRunFailure && (final.Status == "failed" || final.Status == "cancelled") {
		return false, activity.NewError(fmt.Sprintf("Run Data Pipeline: %s run %s %s: %s", in.Pipeline, final.RunID, final.Status, final.Error),
			ErrCodeRunFailed, out.ToMap())
	}
	return true, nil
}

func setOutput(ctx activity.Context, out *Output) error {
	return ctx.SetOutputObject(out)
}

// validate checks the inputs a run cannot do without, and the choices.
func validate(in *Input) error {
	in.Pipeline = strings.TrimSpace(in.Pipeline)
	switch {
	case strings.TrimSpace(in.EngineURL) == "":
		return errors.New("engineUrl is required: the engine API's URL, e.g. http://orders-etl:9080")
	case in.Pipeline == "":
		return errors.New("pipeline is required: its name, or its ref res://pipeline:<name>")
	}
	switch in.Mode = strings.ToLower(strings.TrimSpace(in.Mode)); in.Mode {
	case ModeWait, ModeStart:
	default:
		return fmt.Errorf("mode %q: %s or %s", in.Mode, ModeWait, ModeStart)
	}
	switch in.Overlap = strings.ToLower(strings.TrimSpace(in.Overlap)); in.Overlap {
	case OverlapAllow, OverlapSkip, OverlapQueue:
	default:
		return fmt.Errorf("overlap %q: %s, %s or %s", in.Overlap, OverlapAllow, OverlapSkip, OverlapQueue)
	}
	return nil
}

// newRunID is a random run id, as the engine makes them: 16 hex digits.
func newRunID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
