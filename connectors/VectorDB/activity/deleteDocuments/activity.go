package deleteDocuments

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
		return nil, fmt.Errorf("vectordb-delete: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-delete: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-delete: invalid connection type, expected *VectorDBConnection")
	}
	ctx.Logger().Infof("DeleteDocuments initialised: connection=%s provider=%s", conn.GetName(), conn.GetSettings().DBType)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("DeleteDocuments: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-delete: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-delete: collectionName is required")
	}
	if len(input.IDs) == 0 && len(input.Filters) == 0 {
		return false, fmt.Errorf("vectordb-delete: at least one of ids or filters must be provided")
	}

	l.Debugf("DeleteDocuments: collection=%s id_count=%d hasFilters=%v",
		collectionName, len(input.IDs), len(input.Filters) > 0)

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "deleteDocuments")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", collectionName)
		tc.SetTag("db.vectordb.id_count", len(input.IDs))
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	var (
		deletedCount int64
		opErr        error
		start        = time.Now()
	)

	if len(input.IDs) > 0 {
		l.Debugf("DeleteDocuments: deleting by ID list: %v", input.IDs)
		opErr = a.conn.GetClient().DeleteDocuments(opCtx, collectionName, input.IDs)
		if opErr == nil {
			deletedCount = int64(len(input.IDs))
		}
	} else {
		l.Debugf("DeleteDocuments: deleting by filter: %v", input.Filters)
		deletedCount, opErr = a.conn.GetClient().DeleteByFilter(opCtx, collectionName, input.Filters)
	}

	if opErr != nil {
		l.Errorf("DeleteDocuments: collection=%s error=%v", collectionName, opErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": opErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: opErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return false, fmt.Errorf("vectordb-delete: %w", opErr)
	}
	duration := time.Since(start)
	l.Debugf("DeleteDocuments: collection=%s deleted=%d duration=%s", collectionName, deletedCount, duration)
	if err := ctx.SetOutputObject(&Output{Success: true, DeletedCount: deletedCount, Duration: duration.String()}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
