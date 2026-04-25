package hybridSearch

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
		return nil, fmt.Errorf("vectordb-hybrid: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-hybrid: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-hybrid: invalid connection type, expected *VectorDBConnection")
	}
	ctx.Logger().Infof("HybridSearch initialised: connection=%s provider=%s defaultTopK=%d",
		conn.GetName(), conn.GetSettings().DBType, s.DefaultTopK)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("HybridSearch: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-hybrid: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-hybrid: collectionName is required")
	}
	if input.QueryText == "" && len(input.QueryVector) == 0 {
		return false, fmt.Errorf("vectordb-hybrid: at least one of queryText or queryVector must be provided")
	}

	topK := input.TopK
	if topK <= 0 {
		topK = a.settings.DefaultTopK
	}
	if topK <= 0 {
		topK = 10
	}

	// alpha is used as-is; descriptor default is 0.5.
	// 0.0 = pure sparse/BM25, 1.0 = pure dense vector, 0.5 = equal blend.
	alpha := input.Alpha

	l.Debugf("HybridSearch: collection=%s topK=%d alpha=%.2f hasText=%v hasDense=%v hasFilters=%v",
		collectionName, topK, alpha, input.QueryText != "", len(input.QueryVector) > 0, len(input.Filters) > 0)

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "hybridSearch")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", collectionName)
		tc.SetTag("db.vectordb.top_k", topK)
		tc.SetTag("db.vectordb.alpha", alpha)
	}

	timeout := a.conn.GetSettings().TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()
	provider := a.conn.GetSettings().DBType
	fallbackReason := ""
	// Chroma and Milvus have no native hybrid search — they fall back to dense vector search internally.
	// Weaviate supports native hybrid (BM25 + dense) and does NOT fall back.
	switch provider {
	case "chroma", "milvus":
		if input.QueryText != "" {
			fallbackReason = fmt.Sprintf("%s does not support native hybrid (sparse+dense) search; used dense vector search", provider)
		}
	}

	results, searchErr := a.conn.GetClient().HybridSearch(opCtx, vectordb.HybridSearchRequest{
		CollectionName: collectionName,
		QueryText:      input.QueryText,
		QueryVector:    input.QueryVector,
		TopK:           topK,
		ScoreThreshold: input.ScoreThreshold,
		Filters:        input.Filters,
		Alpha:          alpha,
		// SkipPayload defaults to false (zero value) = include payload.
	})
	if searchErr != nil {
		l.Errorf("HybridSearch: collection=%s error=%v", collectionName, searchErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": searchErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: searchErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return true, nil
	}

	duration := time.Since(start)
	l.Debugf("HybridSearch: collection=%s results=%d duration=%s", collectionName, len(results), duration)
	if tc != nil {
		tc.SetTag("db.vectordb.result_count", len(results))
	}
	if err := ctx.SetOutputObject(&Output{
		Success:        true,
		Results:        searchResultsToInterface(results),
		TotalCount:     len(results),
		Duration:       duration.String(),
		FallbackReason: fallbackReason,
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
