package listCollections

import (
	"context"
	"fmt"
	"time"

	vectordbconnector "github.com/mpandav-tibco/flogo-extensions/vectordb-azureaisearch/connector"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings *Settings
	conn     *vectordbconnector.AzureAISearchConnection
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("vectordb-list-col: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-list-col: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.AzureAISearchConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-list-col: invalid connection type, expected *AzureAISearchConnection")
	}
	ctx.Logger().Infof("ListCollections initialised: connection=%s provider=%s", conn.GetName(), "azureaisearch")
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-list-col: %w", err)
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	names, listErr := a.conn.GetClient().ListCollections(opCtx)
	if listErr != nil {
		l.Errorf("ListCollections: error=%v", listErr)
		if err := ctx.SetOutputObject(&Output{Success: false, Error: listErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return true, nil
	}

	duration := time.Since(start)
	out := make([]interface{}, len(names))
	for i, n := range names {
		out[i] = n
	}
	if err := ctx.SetOutputObject(&Output{
		Success:     true,
		Collections: out,
		TotalCount:  len(names),
		Duration:    duration.String(),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
