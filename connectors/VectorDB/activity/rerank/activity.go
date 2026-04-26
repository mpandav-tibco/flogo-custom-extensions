package rerank

import (
	"context"
	"fmt"
	"time"

	vectordbconnector "github.com/mpandav-tibco/flogo-extensions/vectordb/connector"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings *Settings
	conn     *vectordbconnector.VectorDBConnection
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
	var conn *vectordbconnector.VectorDBConnection
	if s.Connection != nil {
		conn, _ = s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	}
	if conn != nil {
		ctx.Logger().Infof("Rerank initialised: endpoint=%s model=%s connection=%s",
			s.RerankEndpoint, s.Model, conn.GetName())
	} else {
		ctx.Logger().Infof("Rerank initialised: endpoint=%s model=%s (standalone)",
			s.RerankEndpoint, s.Model)
	}
	return &Activity{settings: s, conn: conn}, nil
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
		return false, fmt.Errorf("vectordb-rerank: documents must not be empty")
	}

	topN := a.settings.TopN
	if topN <= 0 {
		topN = len(input.Documents)
	}

	l.Debugf("Rerank: query=%q doc_count=%d topN=%d model=%s endpoint=%s",
		input.Query, len(input.Documents), topN, a.settings.Model, a.settings.RerankEndpoint)

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("rerank.endpoint", a.settings.RerankEndpoint)
		tc.SetTag("rerank.model", a.settings.Model)
		tc.SetTag("rerank.doc_count", len(input.Documents))
		tc.SetTag("rerank.top_n", topN)
	}

	timeout := a.settings.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	ranked, rerankErr := callRerankAPI(opCtx, rerankAPIRequest{
		Endpoint:  a.settings.RerankEndpoint,
		APIKey:    a.settings.APIKey,
		Model:     a.settings.Model,
		Query:     input.Query,
		Documents: input.Documents,
		TopN:      topN,
	})
	if rerankErr != nil {
		l.Errorf("Rerank: error=%v", rerankErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": rerankErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: rerankErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return false, fmt.Errorf("vectordb-rerank: %w", rerankErr)
	}

	duration := time.Since(start)
	l.Debugf("Rerank: returned=%d duration=%s", len(ranked), duration)
	if err := ctx.SetOutputObject(&Output{
		Success:         true,
		RankedDocuments: ranked,
		TotalCount:      len(ranked),
		Duration:        duration.String(),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
