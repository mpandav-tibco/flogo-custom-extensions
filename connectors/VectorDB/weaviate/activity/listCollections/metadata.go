package listCollections

import (
	"github.com/project-flogo/core/support/connection"
)

type Settings struct {
	Connection connection.Manager `md:"connection,required"`
}

type Input struct{}

func (i *Input) ToMap() map[string]interface{}            { return map[string]interface{}{} }
func (i *Input) FromMap(_ map[string]interface{}) error   { return nil }

type Output struct {
	Success     bool          `md:"success"`
	Collections []interface{} `md:"collections"`
	TotalCount  int           `md:"totalCount"`
	Duration    string        `md:"duration"`
	Error       string        `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":     o.Success,
		"collections": o.Collections,
		"totalCount":  o.TotalCount,
		"duration":    o.Duration,
		"error":       o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	return nil
}
