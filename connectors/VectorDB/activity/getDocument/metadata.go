package getDocument

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
	CollectionName string `md:"collectionName"`
	DocumentID     string `md:"documentId"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{"collectionName": i.CollectionName, "documentId": i.DocumentID}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["collectionName"]; ok {
		i.CollectionName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["documentId"]; ok {
		i.DocumentID = fmt.Sprintf("%v", val)
	}
	return nil
}

type Output struct {
	Success  bool                   `md:"success"`
	Document map[string]interface{} `md:"document"`
	Found    bool                   `md:"found"`
	Duration string                 `md:"duration"`
	Error    string                 `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":  o.Success,
		"document": o.Document,
		"found":    o.Found,
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

func docToMap(d *vectordb.Document) map[string]interface{} {
	if d == nil {
		return nil
	}
	b, _ := json.Marshal(d)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	return m
}
