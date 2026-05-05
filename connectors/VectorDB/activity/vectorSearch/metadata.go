package vectorSearch

import (
	"encoding/json"
	"fmt"

	"github.com/mpandav-tibco/flogo-extensions/vectordb"
	"github.com/project-flogo/core/support/connection"
)

type Settings struct {
	Connection        connection.Manager `md:"connection,required"`
	DefaultCollection string             `md:"defaultCollection"`
	DefaultTopK       int                `md:"defaultTopK"`
}

type Input struct {
	CollectionName string                 `md:"collectionName"`
	QueryVector    []float64              `md:"queryVector"`
	TopK           int                    `md:"topK"`
	ScoreThreshold float64                `md:"scoreThreshold"`
	Filters        map[string]interface{} `md:"filters"`
	WithVectors    bool                   `md:"withVectors"`
	SkipPayload    bool                   `md:"skipPayload"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"collectionName": i.CollectionName,
		"queryVector":    i.QueryVector,
		"topK":           i.TopK,
		"scoreThreshold": i.ScoreThreshold,
		"filters":        i.Filters,
		"withVectors":    i.WithVectors,
		"skipPayload":    i.SkipPayload,
	}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["collectionName"]; ok {
		i.CollectionName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["queryVector"]; ok {
		if arr, ok := val.([]interface{}); ok {
			i.QueryVector = make([]float64, len(arr))
			for j, f := range arr {
				if fv, ok := f.(float64); ok {
					i.QueryVector[j] = fv
				}
			}
		}
	}
	if val, ok := v["topK"]; ok {
		switch n := val.(type) {
		case int:
			i.TopK = n
		case float64:
			i.TopK = int(n)
		}
	}
	if val, ok := v["scoreThreshold"]; ok {
		if f, ok := val.(float64); ok {
			i.ScoreThreshold = f
		}
	}
	if val, ok := v["filters"]; ok {
		if m, ok := val.(map[string]interface{}); ok {
			i.Filters = m
		}
	}
	if val, ok := v["withVectors"]; ok {
		i.WithVectors, _ = val.(bool)
	}
	if val, ok := v["skipPayload"]; ok {
		i.SkipPayload, _ = val.(bool)
	}
	return nil
}

type Output struct {
	Success    bool          `md:"success"`
	Results    []interface{} `md:"results"`
	TotalCount int           `md:"totalCount"`
	Duration   string        `md:"duration"`
	Error      string        `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":    o.Success,
		"results":    o.Results,
		"totalCount": o.TotalCount,
		"duration":   o.Duration,
		"error":      o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	if val, ok := v["results"]; ok {
		if arr, ok := val.([]interface{}); ok {
			o.Results = arr
		}
	}
	return nil
}

func searchResultsToInterface(in []vectordb.SearchResult) []interface{} {
	out := make([]interface{}, len(in))
	for i, sr := range in {
		b, _ := json.Marshal(sr)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		out[i] = m
	}
	return out
}
