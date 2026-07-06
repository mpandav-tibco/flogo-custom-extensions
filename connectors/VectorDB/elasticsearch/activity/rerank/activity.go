package rerank

import (
	"fmt"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings *Settings
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("vectordb-rerank: %w", err)
	}
	if s.RerankEndpoint == "" {
		return nil, fmt.Errorf("vectordb-rerank: rerankEndpoint is required")
	}
	topN := s.TopN
	if topN <= 0 {
		topN = 5
	}
	ctx.Logger().Infof("Rerank initialised: endpoint=%s model=%s topN=%d", s.RerankEndpoint, s.Model, topN)
	return &Activity{settings: s}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("Rerank: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-rerank: %w", err)
	}
	if input.Query == "" {
		return false, fmt.Errorf("vectordb-rerank: query is required")
	}
	if len(input.Documents) == 0 {
		return false, fmt.Errorf("vectordb-rerank: documents is required")
	}

	topN := a.settings.TopN
	if topN <= 0 {
		topN = 5
	}
	if input.TopN > 0 {
		topN = input.TopN
	}

	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "rerank")
		tc.SetTag("db.vectordb.rerank.model", a.settings.Model)
		tc.SetTag("db.vectordb.rerank.input_count", len(input.Documents))
		tc.SetTag("db.vectordb.rerank.top_n", topN)
	}

	timeout := a.settings.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}

	start := time.Now()
	results, rerankErr := callRerankAPI(ctx.GoContext(), a.settings.RerankEndpoint, a.settings.APIKey,
		a.settings.Model, input.Query, input.Documents, topN, timeout)
	if rerankErr != nil {
		l.Errorf("Rerank: error=%v", rerankErr)
		if tc != nil {
			tc.SetTag("error", true)
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: rerankErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return true, nil
	}

	duration := time.Since(start)
	l.Debugf("Rerank: input=%d output=%d duration=%s", len(input.Documents), len(results), duration)
	if tc != nil {
		tc.SetTag("db.vectordb.rerank.output_count", len(results))
	}

	if err := ctx.SetOutputObject(&Output{
		Success:  true,
		Results:  results,
		Duration: duration.String(),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
