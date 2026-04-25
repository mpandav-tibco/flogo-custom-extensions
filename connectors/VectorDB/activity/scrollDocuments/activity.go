package scrollDocuments

import (
	"context"
	"fmt"
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
		return nil, fmt.Errorf("vectordb-scroll: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-scroll: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-scroll: invalid connection type, expected *VectorDBConnection")
	}
	ctx.Logger().Infof("ScrollDocuments initialised: connection=%s provider=%s", conn.GetName(), conn.GetSettings().DBType)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("ScrollDocuments: starting eval")

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

	l.Debugf("ScrollDocuments: collection=%s limit=%d offset=%q hasFilters=%v withVectors=%v",
		collectionName, limit, input.Offset, len(input.Filters) > 0, input.WithVectors)

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "scrollDocuments")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", collectionName)
		tc.SetTag("db.vectordb.limit", limit)
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
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": scrollErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: scrollErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return false, fmt.Errorf("vectordb-scroll: %w", scrollErr)
	}

	duration := time.Since(start)
	l.Debugf("ScrollDocuments: collection=%s returned=%d nextOffset=%q duration=%s",
		collectionName, len(result.Documents), result.NextOffset, duration)

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
