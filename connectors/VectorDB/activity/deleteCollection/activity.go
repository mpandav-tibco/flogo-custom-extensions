package deleteCollection

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
		return nil, fmt.Errorf("vectordb-drop-col: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-drop-col: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-drop-col: invalid connection type, expected *VectorDBConnection")
	}
	ctx.Logger().Infof("DeleteCollection initialised: connection=%s provider=%s", conn.GetName(), conn.GetSettings().DBType)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("DeleteCollection: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-drop-col: %w", err)
	}
	if input.CollectionName == "" {
		return false, fmt.Errorf("vectordb-drop-col: collectionName is required")
	}

	l.Warnf("DeleteCollection: PERMANENTLY deleting collection=%s from provider=%s",
		input.CollectionName, a.conn.GetSettings().DBType)

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "deleteCollection")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", input.CollectionName)
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	if dropErr := a.conn.GetClient().DeleteCollection(opCtx, input.CollectionName); dropErr != nil {
		l.Errorf("DeleteCollection: name=%s error=%v", input.CollectionName, dropErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": dropErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: dropErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return true, nil
	}

	duration := time.Since(start)
	l.Infof("DeleteCollection: dropped collection=%s duration=%s", input.CollectionName, duration)
	if err := ctx.SetOutputObject(&Output{Success: true, Duration: duration.String()}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
