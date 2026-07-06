package createCollection

import (
	"context"
	"fmt"
	"strings"
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
		return nil, fmt.Errorf("vectordb-create-col: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-create-col: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.AzureAISearchConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-create-col: invalid connection type, expected *AzureAISearchConnection")
	}
	ctx.Logger().Infof("CreateCollection initialised: connection=%s provider=%s", conn.GetName(), "azureaisearch")
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-create-col: %w", err)
	}
	if input.CollectionName == "" {
		return false, fmt.Errorf("vectordb-create-col: collectionName is required")
	}
	if input.Dimensions <= 0 {
		input.Dimensions = 1536
	}
	if input.DistanceMetric == "" {
		input.DistanceMetric = "cosine"
	}
	if input.ReplicationFactor <= 0 {
		input.ReplicationFactor = 1
	}

	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "createCollection")
		tc.SetTag("db.vectordb.provider", "azureaisearch")
		tc.SetTag("db.vectordb.collection", input.CollectionName)
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	cfg := vectordb.CollectionConfig{
		Name:              input.CollectionName,
		Dimensions:        input.Dimensions,
		DistanceMetric:    input.DistanceMetric,
		OnDisk:            input.OnDisk,
		ReplicationFactor: input.ReplicationFactor,
	}
	if createErr := a.conn.GetClient().CreateCollection(opCtx, cfg); createErr != nil {
		errMsg := createErr.Error()
		if strings.Contains(errMsg, "already exists") || strings.Contains(errMsg, "VDB-COL-2002") {
			l.Infof("CreateCollection: collection=%s already exists, skipping", input.CollectionName)
			if err := ctx.SetOutputObject(&Output{Success: true, Duration: time.Since(start).String()}); err != nil {
				l.Errorf("SetOutputObject: %v", err)
			}
			return true, nil
		}
		l.Errorf("CreateCollection: name=%s error=%v", input.CollectionName, createErr)
		if err := ctx.SetOutputObject(&Output{Success: false, Error: createErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return true, nil
	}

	duration := time.Since(start)
	l.Infof("CreateCollection: created collection=%s dims=%d duration=%s", input.CollectionName, input.Dimensions, duration)
	if err := ctx.SetOutputObject(&Output{Success: true, Duration: duration.String()}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
