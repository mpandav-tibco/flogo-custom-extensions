package scrollDocuments

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
var logger = log.ChildLogger(log.RootLogger(), "vectordb-scroll")

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings *Settings
	conn     *vectordbconnector.VectorDBConnection
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("vectordb-scroll: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-scroll: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-scroll: invalid connection type")
	}
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-scroll: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-scroll: collectionName is required")
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 100
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	result, err := a.conn.GetClient().ScrollDocuments(opCtx, vectordb.ScrollRequest{
		CollectionName: collectionName,
		Limit:          limit,
		Offset:         input.Offset,
		Filters:        input.Filters,
		WithVectors:    input.WithVectors,
	})
	if err != nil {
		logger.Errorf("vectordb-scroll: %v", err)
		_ = ctx.SetOutputObject(&Output{Success: false, Error: err.Error(), Duration: time.Since(start).String()})
		return true, nil
	}

	_ = ctx.SetOutputObject(&Output{
		Success:    true,
		Documents:  docsToInterface(result.Documents),
		NextOffset: result.NextOffset,
		Total:      result.Total,
		Duration:   time.Since(start).String(),
	})
	return true, nil
}
