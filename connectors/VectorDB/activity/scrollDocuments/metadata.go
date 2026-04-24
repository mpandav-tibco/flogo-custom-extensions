package scrollDocuments

import (
	"encoding/json"
	"fmt"

	"github.com/milindpandav/flogo-extensions/vectordb"
	"github.com/project-flogo/core/support/connection"
)

type Settings struct {
	Connection        connection.Manager `md:"connection,required"`
	DefaultCollection string             `md:"defaultCollection"`
}

type Input struct {
	CollectionName string                 `md:"collectionName"`
	Limit          int                    `md:"limit"`
	Offset         string                 `md:"offset"`
	Filters        map[string]interface{} `md:"filters"`
	WithVectors    bool                   `md:"withVectors"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"collectionName": i.CollectionName,
		"limit":          i.Limit,
		"offset":         i.Offset,
		"filters":        i.Filters,
		"withVectors":    i.WithVectors,
	}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["collectionName"]; ok {
		i.CollectionName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["limit"]; ok {
		switch n := val.(type) {
		case int:
			i.Limit = n
		case float64:
			i.Limit = int(n)
		}
	}
	if val, ok := v["offset"]; ok {
		i.Offset = fmt.Sprintf("%v", val)
	}
	if val, ok := v["filters"]; ok {
		if m, ok := val.(map[string]interface{}); ok {
			i.Filters = m
		}
	}
	if val, ok := v["withVectors"]; ok {
		i.WithVectors, _ = val.(bool)
	}
	return nil
}

type Output struct {
	Success    bool          `md:"success"`
	Documents  []interface{} `md:"documents"`
	NextOffset string        `md:"nextOffset"`
	Total      int64         `md:"total"`
	Duration   string        `md:"duration"`
	Error      string        `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":    o.Success,
		"documents":  o.Documents,
		"nextOffset": o.NextOffset,
		"total":      o.Total,
		"duration":   o.Duration,
		"error":      o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	return nil
}

func docsToInterface(in []vectordb.Document) []interface{} {
	out := make([]interface{}, len(in))
	for i, d := range in {
		b, _ := json.Marshal(d)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		out[i] = m
	}
	return out
}
