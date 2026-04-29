package getDocument

import (
	"context"
	"errors"
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
		return nil, fmt.Errorf("vectordb-get: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-get: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-get: invalid connection type, expected *VectorDBConnection")
	}
	ctx.Logger().Infof("GetDocument initialised: connection=%s provider=%s", conn.GetName(), conn.GetSettings().DBType)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("GetDocument: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-get: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-get: collectionName is required")
	}
	if input.DocumentID == "" {
		return false, fmt.Errorf("vectordb-get: documentId is required")
	}

	l.Debugf("GetDocument: collection=%s id=%s", collectionName, input.DocumentID)

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "getDocument")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", collectionName)
		tc.SetTag("db.vectordb.document_id", input.DocumentID)
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	doc, getErr := a.conn.GetClient().GetDocument(opCtx, collectionName, input.DocumentID)
	if getErr != nil {
		// Check if it is a "not found" error — that is not an execution error
		var vdbErr *vectordb.VDBError
		if errors.As(getErr, &vdbErr) && vdbErr.Code == vectordb.ErrCodeDocumentNotFound {
			l.Debugf("GetDocument: collection=%s id=%s NOT FOUND", collectionName, input.DocumentID)
			if err := ctx.SetOutputObject(&Output{Success: true, Found: false, Duration: time.Since(start).String()}); err != nil {
				l.Errorf("SetOutputObject: %v", err)
			}
			return true, nil
		}
		l.Errorf("GetDocument: collection=%s id=%s error=%v", collectionName, input.DocumentID, getErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": getErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: getErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return true, nil
	}

	duration := time.Since(start)
	l.Debugf("GetDocument: collection=%s id=%s found=true duration=%s", collectionName, input.DocumentID, duration)
	if err := ctx.SetOutputObject(&Output{
		Success:  true,
		Found:    true,
		Document: docToMap(doc),
		Duration: duration.String(),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
