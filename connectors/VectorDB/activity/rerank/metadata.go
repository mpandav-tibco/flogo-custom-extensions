package rerank

import (
	"fmt"
)

// Settings for the rerank activity. Rerank is HTTP-based and standalone —
// no VectorDB connection is required.
type Settings struct {
	RerankEndpoint string `md:"rerankEndpoint,required"`
	APIKey         string `md:"apiKey"`
	Model          string `md:"model"`
	TopN           int    `md:"topN"`
	TimeoutSeconds int    `md:"timeoutSeconds"`
}

type Input struct {
	Query     string        `md:"query"`
	Documents []interface{} `md:"documents"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{"query": i.Query, "documents": i.Documents}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["query"]; ok {
		i.Query = fmt.Sprintf("%v", val)
	}
	if val, ok := v["documents"]; ok {
		if arr, ok := val.([]interface{}); ok {
			i.Documents = arr
		}
	}
	return nil
}

type Output struct {
	Success         bool          `md:"success"`
	RankedDocuments []interface{} `md:"rankedDocuments"`
	TotalCount      int           `md:"totalCount"`
	Duration        string        `md:"duration"`
	Error           string        `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":         o.Success,
		"rankedDocuments": o.RankedDocuments,
		"totalCount":      o.TotalCount,
		"duration":        o.Duration,
		"error":           o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	return nil
}
