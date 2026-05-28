package upsertDocuments

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
		return nil, fmt.Errorf("vectordb-upsert: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-upsert: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-upsert: invalid connection type, expected *VectorDBConnection")
	}
	ctx.Logger().Infof("UpsertDocuments initialised: connection=%s provider=%s", conn.GetName(), conn.GetSettings().DBType)
	if s.DefaultCollection != "" {
		ctx.Logger().Debugf("UpsertDocuments default collection: %s", s.DefaultCollection)
	}
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("UpsertDocuments: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-upsert: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-upsert: collectionName is required")
	}

	docs, err := input.ToDocuments()
	if err != nil {
		return false, fmt.Errorf("vectordb-upsert: %w", err)
	}
	l.Debugf("UpsertDocuments: collection=%s doc_count=%d", collectionName, len(docs))

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "upsertDocuments")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", collectionName)
		tc.SetTag("db.vectordb.doc_count", len(docs))
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	if upsertErr := a.conn.GetClient().UpsertDocuments(opCtx, collectionName, docs); upsertErr != nil {
		l.Errorf("UpsertDocuments: collection=%s error=%v", collectionName, upsertErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": upsertErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: upsertErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return true, nil
	}

	duration := time.Since(start)
	l.Debugf("UpsertDocuments: collection=%s upserted=%d duration=%s", collectionName, len(docs), duration)
	if err := ctx.SetOutputObject(&Output{Success: true, UpsertedCount: len(docs), Duration: duration.String()}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
