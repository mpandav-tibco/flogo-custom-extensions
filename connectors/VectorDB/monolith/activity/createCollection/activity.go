package createCollection

import (
	"context"
	"fmt"
	"strings"
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
		return nil, fmt.Errorf("vectordb-create-col: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-create-col: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-create-col: invalid connection type, expected *VectorDBConnection")
	}
	ctx.Logger().Infof("CreateCollection initialised: connection=%s provider=%s", conn.GetName(), conn.GetSettings().DBType)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("CreateCollection: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-create-col: %w", err)
	}
	if input.CollectionName == "" {
		return false, fmt.Errorf("vectordb-create-col: collectionName is required")
	}
	if input.Dimensions <= 0 {
		input.Dimensions = 1536 // default to OpenAI ada-002 / text-embedding-3-small
	}
	if input.DistanceMetric == "" {
		input.DistanceMetric = "cosine"
	}
	if input.ReplicationFactor <= 0 {
		input.ReplicationFactor = 1
	}

	l.Debugf("CreateCollection: name=%s dims=%d metric=%s onDisk=%v replicas=%d",
		input.CollectionName, input.Dimensions, input.DistanceMetric, input.OnDisk, input.ReplicationFactor)

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "createCollection")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", input.CollectionName)
		tc.SetTag("db.vectordb.dimensions", input.Dimensions)
		tc.SetTag("db.vectordb.metric", input.DistanceMetric)
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 60 // collection creation may take longer
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
		// Treat "already exists" as success — idempotent create
		errMsg := createErr.Error()
		if strings.Contains(errMsg, "already exists") || strings.Contains(errMsg, "VDB-COL-2002") {
			l.Infof("CreateCollection: collection=%s already exists, skipping", input.CollectionName)
			duration := time.Since(start)
			if err := ctx.SetOutputObject(&Output{Success: true, Duration: duration.String()}); err != nil {
				l.Errorf("SetOutputObject: %v", err)
			}
			return true, nil
		}
		l.Errorf("CreateCollection: name=%s error=%v", input.CollectionName, createErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": createErr.Error()})
		}
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
