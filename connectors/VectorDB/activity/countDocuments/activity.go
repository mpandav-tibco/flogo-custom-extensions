package countDocuments

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
		return nil, fmt.Errorf("vectordb-count: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-count: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-count: invalid connection type, expected *VectorDBConnection")
	}
	ctx.Logger().Infof("CountDocuments initialised: connection=%s provider=%s", conn.GetName(), conn.GetSettings().DBType)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("CountDocuments: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-count: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-count: collectionName is required")
	}

	l.Debugf("CountDocuments: collection=%s hasFilters=%v", collectionName, len(input.Filters) > 0)

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "countDocuments")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", collectionName)
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	count, countErr := a.conn.GetClient().CountDocuments(opCtx, collectionName, input.Filters)
	if countErr != nil {
		l.Errorf("CountDocuments: collection=%s error=%v", collectionName, countErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": countErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: countErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return false, fmt.Errorf("vectordb-count: %w", countErr)
	}

	duration := time.Since(start)
	l.Debugf("CountDocuments: collection=%s count=%d duration=%s", collectionName, count, duration)
	if tc != nil {
		tc.SetTag("db.vectordb.count", count)
	}
	if err := ctx.SetOutputObject(&Output{Success: true, Count: count, Duration: duration.String()}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
