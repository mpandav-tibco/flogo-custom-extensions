package ragQuery

import (
	"fmt"

	"github.com/project-flogo/core/support/connection"
)

// Settings holds design-time activity configuration.
type Settings struct {
	Connection            connection.Manager `md:"connection,required"`
	UseConnectorEmbedding bool               `md:"useConnectorEmbedding"`
	EmbeddingProvider     string             `md:"embeddingProvider"`
	EmbeddingAPIKey       string             `md:"embeddingAPIKey"`
	EmbeddingBaseURL      string             `md:"embeddingBaseURL"`
	EmbeddingModel        string             `md:"embeddingModel,required"`
	EmbeddingDimensions   int                `md:"embeddingDimensions"`
	DefaultCollection     string             `md:"defaultCollection"`
	DefaultTopK           int                `md:"defaultTopK"`
	ScoreThreshold        float64            `md:"scoreThreshold"`
	ContentField          string             `md:"contentField"`
	ContextFormat         string             `md:"contextFormat"`
	TimeoutSeconds        int                `md:"timeoutSeconds"`
	UseHybridSearch       bool               `md:"useHybridSearch"`
	HybridAlpha           float64            `md:"hybridAlpha"`
	EnableLLMGenerate     bool               `md:"enableLLMGenerate"`
	LLMProvider           string             `md:"llmProvider"`
	LLMBaseURL            string             `md:"llmBaseURL"`
	LLMAPIKey             string             `md:"llmAPIKey"`
	LLMModel              string             `md:"llmModel"`
	SystemPrompt          string             `md:"systemPrompt"`
	MaxTokens             int                `md:"maxTokens"`
	Temperature           float64            `md:"temperature"`
}

func (s Settings) String() string {
	apiKey := ""
	if s.EmbeddingAPIKey != "" {
		apiKey = "[redacted]"
	}
	llmKey := ""
	if s.LLMAPIKey != "" {
		llmKey = "[redacted]"
	}
	return fmt.Sprintf(
		"ragQuery.Settings{provider:%q model:%q dims:%d topK:%d collection:%q hybrid:%v alpha:%.2f llmGenerate:%v llmProvider:%q llmModel:%q apiKey:%s llmApiKey:%s}",
		s.EmbeddingProvider, s.EmbeddingModel, s.EmbeddingDimensions,
		s.DefaultTopK, s.DefaultCollection, s.UseHybridSearch, s.HybridAlpha,
		s.EnableLLMGenerate, s.LLMProvider, s.LLMModel, apiKey, llmKey,
	)
}

// Input holds runtime data for the activity.
type Input struct {
	QueryText      string                 `md:"queryText"`
	CollectionName string                 `md:"collectionName"`
	TopK           int                    `md:"topK"`
	Filters        map[string]interface{} `md:"filters"`
	SystemPrompt   string                 `md:"systemPrompt"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"queryText":      i.QueryText,
		"collectionName": i.CollectionName,
		"topK":           i.TopK,
		"filters":        i.Filters,
		"systemPrompt":   i.SystemPrompt,
	}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["queryText"]; ok && val != nil {
		i.QueryText = fmt.Sprintf("%v", val)
	}
	if val, ok := v["collectionName"]; ok && val != nil {
		i.CollectionName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["topK"]; ok {
		switch n := val.(type) {
		case int:
			i.TopK = n
		case float64:
			i.TopK = int(n)
		}
	}
	if val, ok := v["filters"]; ok {
		if m, ok := val.(map[string]interface{}); ok {
			i.Filters = m
		}
	}
	if val, ok := v["systemPrompt"]; ok && val != nil {
		i.SystemPrompt = fmt.Sprintf("%v", val)
	}
	return nil
}

// Output holds the activity result.
type Output struct {
	Success          bool          `md:"success"`
	Answer           string        `md:"answer"`
	FormattedContext string        `md:"formattedContext"`
	SourceDocuments  []interface{} `md:"sourceDocuments"`
	QueryEmbedding   []interface{} `md:"queryEmbedding"`
	TotalFound       int           `md:"totalFound"`
	Duration         string        `md:"duration"`
	Error            string        `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":          o.Success,
		"answer":           o.Answer,
		"formattedContext": o.FormattedContext,
		"sourceDocuments":  o.SourceDocuments,
		"queryEmbedding":   o.QueryEmbedding,
		"totalFound":       o.TotalFound,
		"duration":         o.Duration,
		"error":            o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	return nil
}
