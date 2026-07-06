package ragQuery

import (
	"fmt"

	"github.com/project-flogo/core/support/connection"
)

type Settings struct {
	Connection        connection.Manager `md:"connection,required"`
	DefaultCollection string             `md:"defaultCollection"`
	DefaultTopK       int                `md:"defaultTopK"`
	ScoreThreshold    float64            `md:"scoreThreshold"`
	HybridAlpha       float64            `md:"hybridAlpha"`
	EmbeddingProvider string             `md:"embeddingProvider"`
	EmbeddingAPIKey   string             `md:"embeddingAPIKey"`
	EmbeddingBaseURL  string             `md:"embeddingBaseURL"`
	EmbeddingModel    string             `md:"embeddingModel"`
	LLMEndpoint       string             `md:"llmEndpoint"`
	LLMAPIKey         string             `md:"llmAPIKey"`
	LLMModel          string             `md:"llmModel"`
	LLMTimeoutSeconds int                `md:"llmTimeoutSeconds"`
}

type Input struct {
	CollectionName string                 `md:"collectionName"`
	Query          string                 `md:"query"`
	TopK           int                    `md:"topK"`
	Filters        map[string]interface{} `md:"filters"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"collectionName": i.CollectionName,
		"query":          i.Query,
		"topK":           i.TopK,
		"filters":        i.Filters,
	}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["collectionName"]; ok {
		i.CollectionName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["query"]; ok {
		i.Query = fmt.Sprintf("%v", val)
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

type Output struct {
	Success           bool          `md:"success"`
	Answer            string        `md:"answer"`
	Context           string        `md:"context"`
	SearchResults     []interface{} `md:"searchResults"`
	SearchResultCount int           `md:"searchResultCount"`
	Duration          string        `md:"duration"`
	Error             string        `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":           o.Success,
		"answer":            o.Answer,
		"context":           o.Context,
		"searchResults":     o.SearchResults,
		"searchResultCount": o.SearchResultCount,
		"duration":          o.Duration,
		"error":             o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	return nil
}
