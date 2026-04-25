package ragQuery

import (
	"fmt"

	"github.com/project-flogo/core/support/connection"
)

// Settings holds design-time activity configuration.
type Settings struct {
	Connection connection.Manager `md:"connection,required"`
	// UseConnectorEmbedding instructs the activity to inherit the embedding
	// provider, API key, and base URL from the VectorDB connector settings.
	// When true, only embeddingModel and embeddingDimensions need to be set
	// here. Set to false to supply per-activity embedding credentials instead.
	UseConnectorEmbedding bool    `md:"useConnectorEmbedding"`
	EmbeddingProvider     string  `md:"embeddingProvider"`
	EmbeddingAPIKey       string  `md:"embeddingAPIKey"`
	EmbeddingBaseURL      string  `md:"embeddingBaseURL"`
	EmbeddingModel        string  `md:"embeddingModel,required"`
	EmbeddingDimensions   int     `md:"embeddingDimensions"`
	DefaultCollection     string  `md:"defaultCollection"`
	DefaultTopK           int     `md:"defaultTopK"`
	ScoreThreshold        float64 `md:"scoreThreshold"`
	ContentField          string  `md:"contentField"`
	ContextFormat         string  `md:"contextFormat"`
	TimeoutSeconds        int     `md:"timeoutSeconds"`
	// UseHybridSearch enables Weaviate hybrid (BM25 + dense) search instead of
	// pure vector search. When true, queryText is used for the BM25 component
	// and queryVector for the dense component. Falls back to VectorSearch on
	// providers that do not support native hybrid (Chroma, Qdrant, Milvus).
	UseHybridSearch bool `md:"useHybridSearch"`
	// HybridAlpha controls the weighting between dense (1.0) and sparse/BM25 (0.0).
	// Only used when UseHybridSearch=true. Default: 0.5 (balanced fusion).
	HybridAlpha float64 `md:"hybridAlpha"`
}

// String returns a human-readable representation of Settings with sensitive
// fields (EmbeddingAPIKey) replaced by "[redacted]".
// Prevents API keys from leaking into Flogo logs or error messages.
func (s Settings) String() string {
	apiKey := ""
	if s.EmbeddingAPIKey != "" {
		apiKey = "[redacted]"
	}
	return fmt.Sprintf(
		"ragQuery.Settings{provider:%q model:%q dims:%d topK:%d collection:%q hybrid:%v alpha:%.2f apiKey:%s}",
		s.EmbeddingProvider, s.EmbeddingModel, s.EmbeddingDimensions,
		s.DefaultTopK, s.DefaultCollection, s.UseHybridSearch, s.HybridAlpha, apiKey,
	)
}

// Input holds runtime data for the activity.
type Input struct {
	QueryText      string                 `md:"queryText"`
	CollectionName string                 `md:"collectionName"`
	TopK           int                    `md:"topK"`
	Filters        map[string]interface{} `md:"filters"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"queryText":      i.QueryText,
		"collectionName": i.CollectionName,
		"topK":           i.TopK,
		"filters":        i.Filters,
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
	return nil
}

// Output holds the activity result.
type Output struct {
	Success          bool          `md:"success"`
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
