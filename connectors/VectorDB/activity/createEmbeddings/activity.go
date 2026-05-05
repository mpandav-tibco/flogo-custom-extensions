package createEmbeddings

import (
	"context"
	"fmt"
	"time"

	vectordbconnector "github.com/mpandav-tibco/flogo-extensions/vectordb/connector"
	vdbembed "github.com/mpandav-tibco/flogo-extensions/vectordb/embeddings"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() { _ = activity.Register(&Activity{}, New) }

type Activity struct {
	settings *Settings
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("vectordb-embed: %w", err)
	}

	// Resolve embedding credentials from connector when opted in.
	// Activity-level values (if set) always take precedence as an override.
	if s.UseConnectorEmbedding && s.Connection != nil {
		conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
		if !ok {
			return nil, fmt.Errorf("vectordb-embed: invalid connection type, expected *VectorDBConnection")
		}
		connSettings := conn.GetSettings()
		if !connSettings.EnableEmbedding {
			ctx.Logger().Warnf("CreateEmbeddings: useConnectorEmbedding=true but connector does not have enableEmbedding set — falling back to activity-level settings")
		} else {
			if s.Provider == "" {
				s.Provider = connSettings.EmbeddingProvider
			}
			if s.APIKey == "" {
				s.APIKey = connSettings.EmbeddingAPIKey
			}
			if s.BaseURL == "" {
				s.BaseURL = connSettings.EmbeddingBaseURL
			}
		}
	}

	if s.Provider == "" {
		s.Provider = string(vdbembed.ProviderOpenAI)
	}
	if s.Model == "" {
		return nil, fmt.Errorf("vectordb-embed: model is required")
	}
	if s.TimeoutSeconds <= 0 {
		s.TimeoutSeconds = 30
	}
	ctx.Logger().Infof("CreateEmbeddings initialised: provider=%s model=%s dims=%d",
		s.Provider, s.Model, s.Dimensions)
	return &Activity{settings: s}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("CreateEmbeddings: starting eval provider=%s model=%s", a.settings.Provider, a.settings.Model)

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-embed: %w", err)
	}

	// Build text list: prefer inputTexts (batch) over single inputText
	var texts []string
	if len(input.InputTexts) > 0 {
		for _, t := range input.InputTexts {
			texts = append(texts, fmt.Sprintf("%v", t))
		}
	} else if input.InputText != "" {
		texts = []string{input.InputText}
	}
	if len(texts) == 0 {
		return false, fmt.Errorf("vectordb-embed: inputText or inputTexts is required")
	}

	l.Debugf("CreateEmbeddings: embedding %d text(s)", len(texts))

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("ai.operation", "createEmbeddings")
		tc.SetTag("ai.provider", a.settings.Provider)
		tc.SetTag("ai.model", a.settings.Model)
		tc.SetTag("ai.input_count", len(texts))
	}

	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(a.settings.TimeoutSeconds)*time.Second)
	defer cancel()

	start := time.Now()
	result, embErr := vdbembed.CreateEmbeddings(opCtx, vdbembed.EmbeddingRequest{
		Provider:   vdbembed.EmbeddingProvider(a.settings.Provider),
		APIKey:     a.settings.APIKey,
		BaseURL:    a.settings.BaseURL,
		Model:      a.settings.Model,
		Texts:      texts,
		Dimensions: a.settings.Dimensions,
	})
	if embErr != nil {
		l.Errorf("CreateEmbeddings: provider=%s error=%v", a.settings.Provider, embErr)
		if tc != nil {
			tc.SetTag("error", true)
			tc.LogKV(map[string]interface{}{"event": "error", "message": embErr.Error()})
		}
		if err := ctx.SetOutputObject(&Output{Success: false, Error: embErr.Error(), Duration: time.Since(start).String()}); err != nil {
			l.Errorf("SetOutputObject: %v", err)
		}
		return true, nil
	}

	duration := time.Since(start)
	l.Debugf("CreateEmbeddings: got %d embeddings dims=%d tokens=%d duration=%s",
		len(result.Embeddings), result.Dimensions, result.TokensUsed, duration)
	if tc != nil {
		tc.SetTag("ai.dimensions", result.Dimensions)
		tc.SetTag("ai.tokens_used", result.TokensUsed)
		tc.SetTag("ai.embedding_count", len(result.Embeddings))
	}

	// Convert [][]float64 → []interface{}{[]interface{}{float64,...},...}
	allEmbs := make([]interface{}, len(result.Embeddings))
	for i, vec := range result.Embeddings {
		floatArr := make([]interface{}, len(vec))
		for j, f := range vec {
			floatArr[j] = f
		}
		allEmbs[i] = floatArr
	}

	var firstEmb []interface{}
	if len(allEmbs) > 0 {
		if arr, ok := allEmbs[0].([]interface{}); ok {
			firstEmb = arr
		}
	}

	if err := ctx.SetOutputObject(&Output{
		Success:    true,
		Embedding:  firstEmb,
		Embeddings: allEmbs,
		Dimensions: result.Dimensions,
		TokensUsed: result.TokensUsed,
		Duration:   duration.String(),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}
