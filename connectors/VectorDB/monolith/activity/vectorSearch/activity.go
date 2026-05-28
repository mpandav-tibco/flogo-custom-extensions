package vectorSearch

import (
	"context"
	"fmt"
	"time"

	"github.com/mpandav-tibco/flogo-extensions/vectordb"
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
		return nil, fmt.Errorf("vectordb-search: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-search: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-search: invalid connection type, expected *VectorDBConnection")
	}
	ctx.Logger().Infof("VectorSearch initialised: connection=%s provider=%s defaultTopK=%d",
		conn.GetName(), conn.GetSettings().DBType, s.DefaultTopK)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("VectorSearch: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-search: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-search: collectionName is required")
	}
	if len(input.QueryVector) == 0 {
		return false, fmt.Errorf("vectordb-search: queryVector must not be empty")
	}

	topK := input.TopK
	if topK <= 0 {
		topK = a.settings.DefaultTopK
	}
	if topK <= 0 {
		topK = 10
	}

	l.Debugf("VectorSearch: collection=%s dims=%d topK=%d scoreThreshold=%.3f hasFilters=%v",
		collectionName, len(input.QueryVector), topK, input.ScoreThreshold, len(input.Filters) > 0)

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "vectorSearch")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", collectionName)
		tc.SetTag("db.vectordb.top_k", topK)
		tc.SetTag("db.vectordb.vector_dims", len(input.QueryVector))
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	results, searchErr := a.conn.GetClient().VectorSearch(opCtx, vectordb.SearchRequest{
		CollectionName: collectionName,
		QueryVector:    input.QueryVector,
		TopK:           topK,
		ScoreThreshold: input.ScoreThreshold,
		Filters:        input.Filters,
		WithVectors:    input.WithVectors,
		SkipPayload:    input.SkipPayload,
	})
	if searchErr != nil {
		l.Errorf("VectorSearch: collection=%s error=%v", collectionName, searchErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": searchErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: searchErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return true, nil
	}

	duration := time.Since(start)
	l.Debugf("VectorSearch: collection=%s results=%d duration=%s", collectionName, len(results), duration)
	if tc != nil {
		tc.SetTag("db.vectordb.result_count", len(results))
	}
	if err := ctx.SetOutputObject(&Output{
		Success:    true,
		Results:    searchResultsToInterface(results),
		TotalCount: len(results),
		Duration:   duration.String(),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
