package datapipeline

import (
	"fmt"
	"time"

	"github.com/project-flogo/core/data/coerce"
)

// Modes: what the activity does once the engine has taken the run.
const (
	// ModeWait waits for the run's result (the default).
	ModeWait = "wait"
	// ModeStart returns as soon as the engine has taken the run: status
	// "running", or "queued" (overlap "queue").
	ModeStart = "start"
)

// Overlap policies, as the engine's run API reads them: what a run asked for
// does while a run of the pipeline is in flight.
const (
	OverlapAllow = "allow" // runs alongside it (the default)
	OverlapSkip  = "skip"  // not run: the activity's status is "skipped"
	OverlapQueue = "queue" // waits its turn, then runs
)

const (
	defaultWaitTimeoutSeconds  = 3600
	defaultPollIntervalSeconds = 2
)

// Input is what the activity is given per execution. Flogo seeds the inputs
// a flow does not map from newInput's values, so they carry the defaults.
type Input struct {
	// EngineURL is the base URL of the data pipeline engine's REST API, e.g.
	// http://orders-etl.data:9080.
	EngineURL string `md:"engineUrl"`
	// APIToken is the engine's DP_API_TOKEN (map from a secret app property);
	// a deployed engine runs pipelines only for a caller that has it.
	APIToken string `md:"apiToken"`
	// Pipeline names the pipeline: its name (daily-tx) or Flogo resource ref
	// (res://pipeline:daily-tx).
	Pipeline string `md:"pipeline"`
	// RunID names the run; empty: the activity makes one. A run id the engine
	// has in flight is not run twice: the activity follows that run.
	RunID string `md:"runId"`
	// Params fill the ${…} of the pipeline's source path.
	Params map[string]interface{} `md:"params"`
	// InputPath replaces the pipeline's source path for this run.
	InputPath string `md:"inputPath"`
	// Mode is ModeWait or ModeStart.
	Mode string `md:"mode"`
	// Overlap is OverlapAllow, OverlapSkip or OverlapQueue.
	Overlap string `md:"overlap"`
	// CallbackURL is where the engine posts the run's result as it ends.
	CallbackURL string `md:"callbackUrl"`
	// WaitTimeoutSeconds bounds the wait for the result (ModeWait).
	WaitTimeoutSeconds int `md:"waitTimeoutSeconds"`
	// PollIntervalSeconds is how often the activity asks how the run goes.
	PollIntervalSeconds int `md:"pollIntervalSeconds"`
	// FailOnRunFailure makes a run that ends failed or cancelled an error of
	// the activity, for the flow's error handler.
	FailOnRunFailure bool `md:"failOnRunFailure"`
}

func newInput() *Input {
	return &Input{
		Mode:                ModeWait,
		Overlap:             OverlapAllow,
		WaitTimeoutSeconds:  defaultWaitTimeoutSeconds,
		PollIntervalSeconds: defaultPollIntervalSeconds,
		FailOnRunFailure:    true,
	}
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"engineUrl":           i.EngineURL,
		"apiToken":            i.APIToken,
		"pipeline":            i.Pipeline,
		"runId":               i.RunID,
		"params":              i.Params,
		"inputPath":           i.InputPath,
		"mode":                i.Mode,
		"overlap":             i.Overlap,
		"callbackUrl":         i.CallbackURL,
		"waitTimeoutSeconds":  i.WaitTimeoutSeconds,
		"pollIntervalSeconds": i.PollIntervalSeconds,
		"failOnRunFailure":    i.FailOnRunFailure,
	}
}

// FromMap reads the mapped inputs over the defaults; an empty string or a
// zero number leaves the default.
func (i *Input) FromMap(values map[string]interface{}) error {
	strs := map[string]*string{
		"engineUrl":   &i.EngineURL,
		"apiToken":    &i.APIToken,
		"pipeline":    &i.Pipeline,
		"runId":       &i.RunID,
		"inputPath":   &i.InputPath,
		"mode":        &i.Mode,
		"overlap":     &i.Overlap,
		"callbackUrl": &i.CallbackURL,
	}
	for k, p := range strs {
		if v, ok := values[k]; ok && v != nil {
			s, err := coerce.ToString(v)
			if err != nil {
				return fmt.Errorf("input %s: %w", k, err)
			}
			if s != "" {
				*p = s
			}
		}
	}
	ints := map[string]*int{
		"waitTimeoutSeconds":  &i.WaitTimeoutSeconds,
		"pollIntervalSeconds": &i.PollIntervalSeconds,
	}
	for k, p := range ints {
		if v, ok := values[k]; ok && v != nil {
			n, err := coerce.ToInt(v)
			if err != nil {
				return fmt.Errorf("input %s: %w", k, err)
			}
			if n > 0 {
				*p = n
			}
		}
	}
	if v, ok := values["failOnRunFailure"]; ok && v != nil {
		b, err := coerce.ToBool(v)
		if err != nil {
			return fmt.Errorf("input failOnRunFailure: %w", err)
		}
		i.FailOnRunFailure = b
	}
	if v, ok := values["params"]; ok && v != nil {
		obj, err := coerce.ToObject(v)
		if err != nil {
			return fmt.Errorf("input params: %w", err)
		}
		i.Params = obj
	}
	return nil
}

// Output is the run as the activity leaves it: its result once it has ended
// (ModeWait), or as the engine took it (ModeStart).
type Output struct {
	// RunID is the run's id.
	RunID string `md:"runId"`
	// Status is the run's: queued, running, completed, partial, failed or
	// cancelled — or skipped, when the engine did not run it (overlap
	// "skip" while the pipeline ran, a suspended pipeline).
	Status        string `md:"status"`
	RowsProcessed int64  `md:"rowsProcessed"`
	RowsWritten   int64  `md:"rowsWritten"`
	RowsFailed    int64  `md:"rowsFailed"`
	RowsDropped   int64  `md:"rowsDropped"`
	DurationMs    int64  `md:"durationMs"`
	// Error says why a run failed, was cancelled or skipped — for a partial
	// run, the first reason rows failed.
	Error      string `md:"error"`
	StartedAt  string `md:"startedAt"`
	FinishedAt string `md:"finishedAt"`
	// Result is the run as the engine gives it, every field of it
	// (validationReport, stageTimings, …).
	Result map[string]interface{} `md:"result"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"runId":         o.RunID,
		"status":        o.Status,
		"rowsProcessed": o.RowsProcessed,
		"rowsWritten":   o.RowsWritten,
		"rowsFailed":    o.RowsFailed,
		"rowsDropped":   o.RowsDropped,
		"durationMs":    o.DurationMs,
		"error":         o.Error,
		"startedAt":     o.StartedAt,
		"finishedAt":    o.FinishedAt,
		"result":        o.Result,
	}
}

func (o *Output) FromMap(values map[string]interface{}) error {
	o.RunID, _ = coerce.ToString(values["runId"])
	o.Status, _ = coerce.ToString(values["status"])
	o.RowsProcessed, _ = coerce.ToInt64(values["rowsProcessed"])
	o.RowsWritten, _ = coerce.ToInt64(values["rowsWritten"])
	o.RowsFailed, _ = coerce.ToInt64(values["rowsFailed"])
	o.RowsDropped, _ = coerce.ToInt64(values["rowsDropped"])
	o.DurationMs, _ = coerce.ToInt64(values["durationMs"])
	o.Error, _ = coerce.ToString(values["error"])
	o.StartedAt, _ = coerce.ToString(values["startedAt"])
	o.FinishedAt, _ = coerce.ToString(values["finishedAt"])
	o.Result, _ = coerce.ToObject(values["result"])
	return nil
}

// outputOf is run as the activity's output.
func outputOf(run *Run) *Output {
	return &Output{
		RunID:         run.RunID,
		Status:        run.Status,
		RowsProcessed: run.RowsProcessed,
		RowsWritten:   run.RowsWritten,
		RowsFailed:    run.RowsFailed,
		RowsDropped:   run.RowsDropped,
		DurationMs:    run.DurationMs,
		Error:         run.Error,
		StartedAt:     timeText(run.StartedAt),
		FinishedAt:    timeText(run.FinishedAt),
		Result:        run.Raw,
	}
}

// timeText is t as RFC 3339, "" for the zero time.
func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
