package hybridSearch

import (
	"context"
	"fmt"
	"time"

	"github.com/milindpandav/flogo-extensions/vectordb"
	vectordbconnector "github.com/milindpandav/flogo-extensions/vectordb/connector"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})
var logger = log.ChildLogger(log.RootLogger(), "vectordb-hybrid")

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings *Settings
	conn     *vectordbconnector.VectorDBConnection
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("vectordb-hybrid: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-hybrid: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-hybrid: invalid connection type")
	}
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-hybrid: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-hybrid: collectionName is required")
	}

	topK := input.TopK
	if topK <= 0 {
		topK = a.settings.DefaultTopK
	}
	if topK <= 0 {
		topK = 10
	}

	alpha := input.Alpha
	if alpha == 0 {
		alpha = 0.5
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	results, err := a.conn.GetClient().HybridSearch(opCtx, vectordb.HybridSearchRequest{
		CollectionName: collectionName,
		QueryText:      input.QueryText,
		QueryVector:    input.QueryVector,
		TopK:           topK,
		ScoreThreshold: input.ScoreThreshold,
		Alpha:          alpha,
		Filters:        input.Filters,
	})
	if err != nil {
		logger.Errorf("vectordb-hybrid: %v", err)
		_ = ctx.SetOutputObject(&Output{Success: false, Error: err.Error(), Duration: time.Since(start).String()})
		return true, nil
	}

	_ = ctx.SetOutputObject(&Output{
		Success:    true,
		Results:    searchResultsToInterface(results),
		TotalCount: len(results),
		Duration:   time.Since(start).String(),
	})
	return true, nil
}
