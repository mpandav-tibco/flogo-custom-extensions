package upsertDocuments

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
var logger = log.ChildLogger(log.RootLogger(), "vectordb-upsert")

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
		return nil, fmt.Errorf("vectordb-upsert: invalid connection type")
	}
	logger.Infof("VectorDB UpsertDocuments initialised: conn=%s", conn.GetName())
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
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

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	if upsertErr := a.conn.GetClient().UpsertDocuments(opCtx, collectionName, docs); upsertErr != nil {
		logger.Errorf("vectordb-upsert: %v", upsertErr)
		_ = ctx.SetOutputObject(&Output{Success: false, Error: upsertErr.Error(), Duration: time.Since(start).String()})
		return true, nil
	}

	_ = ctx.SetOutputObject(&Output{Success: true, UpsertedCount: len(docs), Duration: time.Since(start).String()})
	return true, nil
}
