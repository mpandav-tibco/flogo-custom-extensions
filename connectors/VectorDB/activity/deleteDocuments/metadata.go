package deleteDocuments

import (
	"fmt"

	"github.com/project-flogo/core/support/connection"
)

type Settings struct {
	Connection        connection.Manager `md:"connection,required"`
	DefaultCollection string             `md:"defaultCollection"`
}

type Input struct {
	CollectionName string                 `md:"collectionName"`
	IDs            []string               `md:"ids"`
	Filters        map[string]interface{} `md:"filters"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"collectionName": i.CollectionName,
		"ids":            i.IDs,
		"filters":        i.Filters,
	}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["collectionName"]; ok {
		i.CollectionName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["ids"]; ok {
		if arr, ok := val.([]interface{}); ok {
			i.IDs = make([]string, len(arr))
			for j, id := range arr {
				i.IDs[j] = fmt.Sprintf("%v", id)
			}
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
	Success      bool   `md:"success"`
	DeletedCount int64  `md:"deletedCount"`
	Duration     string `md:"duration"`
	Error        string `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":      o.Success,
		"deletedCount": o.DeletedCount,
		"duration":     o.Duration,
		"error":        o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	return nil
}
