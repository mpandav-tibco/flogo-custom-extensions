package scrollDocuments

import (
	"context"
	"fmt"
	"time"

	"github.com/mpandav-tibco/flogo-extensions/vectordb-azureaisearch"
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
		return nil, fmt.Errorf("vectordb-scroll: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-scroll: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.AzureAISearchConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-scroll: invalid connection type, expected *AzureAISearchConnection")
	}
	ctx.Logger().Infof("ScrollDocuments initialised: connection=%s provider=%s", conn.GetName(), "azureaisearch")
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
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
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	result, scrollErr := a.conn.GetClient().ScrollDocuments(opCtx, vectordb.ScrollRequest{
		CollectionName: collectionName,
		Limit:          limit,
		Offset:         input.Offset,
		Filters:        input.Filters,
		WithVectors:    input.WithVectors,
	})
	if scrollErr != nil {
		l.Errorf("ScrollDocuments: collection=%s error=%v", collectionName, scrollErr)
		if err := ctx.SetOutputObject(&Output{Success: false, Error: scrollErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return true, nil
	}

	duration := time.Since(start)
	if err := ctx.SetOutputObject(&Output{
		Success:    true,
		Documents:  docsToInterface(result.Documents),
		NextOffset: result.NextOffset,
		Total:      result.Total,
		Duration:   duration.String(),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
