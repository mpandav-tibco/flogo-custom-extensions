package deleteDocuments

import (
	"context"
	"fmt"
	"time"

	vectordbconnector "github.com/milindpandav/flogo-extensions/vectordb/connector"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})
var logger = log.ChildLogger(log.RootLogger(), "vectordb-delete-docs")

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings *Settings
	conn     *vectordbconnector.VectorDBConnection
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("vectordb-delete-docs: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-delete-docs: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-delete-docs: invalid connection type")
	}
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-delete-docs: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-delete-docs: collectionName is required")
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	var deletedCount int64

	if len(input.IDs) > 0 {
		if err := a.conn.GetClient().DeleteDocuments(opCtx, collectionName, input.IDs); err != nil {
			logger.Errorf("vectordb-delete-docs: DeleteDocuments: %v", err)
			_ = ctx.SetOutputObject(&Output{Success: false, Error: err.Error(), Duration: time.Since(start).String()})
			return true, nil
		}
		deletedCount = int64(len(input.IDs))
	} else if len(input.Filters) > 0 {
		count, err := a.conn.GetClient().DeleteByFilter(opCtx, collectionName, input.Filters)
		if err != nil {
			logger.Errorf("vectordb-delete-docs: DeleteByFilter: %v", err)
			_ = ctx.SetOutputObject(&Output{Success: false, Error: err.Error(), Duration: time.Since(start).String()})
			return true, nil
		}
		deletedCount = count
	} else {
		return false, fmt.Errorf("vectordb-delete-docs: either ids or filters must be provided")
	}

	_ = ctx.SetOutputObject(&Output{Success: true, DeletedCount: deletedCount, Duration: time.Since(start).String()})
	return true, nil
}
