package deleteCollection

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
var logger = log.ChildLogger(log.RootLogger(), "vectordb-delete-col")

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings *Settings
	conn     *vectordbconnector.VectorDBConnection
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("vectordb-delete-col: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-delete-col: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-delete-col: invalid connection type")
	}
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-delete-col: %w", err)
	}
	if input.CollectionName == "" {
		return false, fmt.Errorf("vectordb-delete-col: collectionName is required")
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	if err := a.conn.GetClient().DeleteCollection(opCtx, input.CollectionName); err != nil {
		logger.Errorf("vectordb-delete-col: %v", err)
		_ = ctx.SetOutputObject(&Output{Success: false, Error: err.Error(), Duration: time.Since(start).String()})
		return true, nil
	}

	_ = ctx.SetOutputObject(&Output{Success: true, Duration: time.Since(start).String()})
	return true, nil
}
