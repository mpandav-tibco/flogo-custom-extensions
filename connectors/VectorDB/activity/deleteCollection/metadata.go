package deleteCollection

import (
	"fmt"

	"github.com/project-flogo/core/support/connection"
)

type Settings struct {
	Connection connection.Manager `md:"connection,required"`
}

type Input struct {
	CollectionName string `md:"collectionName"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{"collectionName": i.CollectionName}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["collectionName"]; ok {
		i.CollectionName = fmt.Sprintf("%v", val)
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
