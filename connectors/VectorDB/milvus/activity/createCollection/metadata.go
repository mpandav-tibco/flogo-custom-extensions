package createCollection

import (
	"fmt"

	"github.com/project-flogo/core/support/connection"
)

type Settings struct {
	Connection connection.Manager `md:"connection,required"`
}

type Input struct {
	CollectionName    string `md:"collectionName"`
	Dimensions        int    `md:"dimensions"`
	DistanceMetric    string `md:"distanceMetric"`
	OnDisk            bool   `md:"onDisk"`
	ReplicationFactor int    `md:"replicationFactor"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"collectionName":    i.CollectionName,
		"dimensions":        i.Dimensions,
		"distanceMetric":    i.DistanceMetric,
		"onDisk":            i.OnDisk,
		"replicationFactor": i.ReplicationFactor,
	}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["collectionName"]; ok {
		i.CollectionName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["dimensions"]; ok {
		switch n := val.(type) {
		case int:
			i.Dimensions = n
		case float64:
			i.Dimensions = int(n)
		}
	}
	if val, ok := v["distanceMetric"]; ok {
		i.DistanceMetric = fmt.Sprintf("%v", val)
	}
	if val, ok := v["onDisk"]; ok {
		i.OnDisk, _ = val.(bool)
	}
	if val, ok := v["replicationFactor"]; ok {
		switch n := val.(type) {
		case int:
			i.ReplicationFactor = n
		case float64:
			i.ReplicationFactor = int(n)
		}
	}
	return nil
}

type Output struct {
	Success  bool   `md:"success"`
	Duration string `md:"duration"`
	Error    string `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{"success": o.Success, "duration": o.Duration, "error": o.Error}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	return nil
}
