package rerank

import (
	"fmt"

	_ "github.com/project-flogo/core/support/connection"
)

type Settings struct {
	RerankEndpoint string `md:"rerankEndpoint"`
	APIKey         string `md:"apiKey"`
	Model          string `md:"model"`
	TopN           int    `md:"topN"`
	TimeoutSeconds int    `md:"timeoutSeconds"`
}

type Input struct {
	Query     string        `md:"query"`
	Documents []interface{} `md:"documents"`
	TopN      int           `md:"topN"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"query":     i.Query,
		"documents": i.Documents,
		"topN":      i.TopN,
	}
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
	if val, ok := v["topN"]; ok {
		switch n := val.(type) {
		case int:
			i.TopN = n
		case float64:
			i.TopN = int(n)
		}
	}
	return nil
}

type Output struct {
	Success  bool          `md:"success"`
	Results  []interface{} `md:"results"`
	Duration string        `md:"duration"`
	Error    string        `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":  o.Success,
		"results":  o.Results,
		"duration": o.Duration,
		"error":    o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	return nil
}
