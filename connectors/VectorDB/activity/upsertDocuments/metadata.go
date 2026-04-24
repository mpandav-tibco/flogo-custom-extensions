package upsertDocuments

import (
	"encoding/json"
	"fmt"

	"github.com/milindpandav/flogo-extensions/vectordb"
	"github.com/project-flogo/core/support/connection"
)

// Settings – only the connection ref + optional flow-level defaults.
// All host/port/TLS config lives in the shared VectorDB Connector.
type Settings struct {
	Connection        connection.Manager `md:"connection,required"`
	DefaultCollection string             `md:"defaultCollection"`
}

// Input holds the runtime inputs for an upsert operation.
type Input struct {
	CollectionName string        `md:"collectionName"`
	Documents      []interface{} `md:"documents"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"collectionName": i.CollectionName,
		"documents":      i.Documents,
	}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["collectionName"]; ok {
		i.CollectionName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["documents"]; ok {
		if arr, ok := val.([]interface{}); ok {
			i.Documents = arr
		} else {
			return fmt.Errorf("vectordb-upsert: 'documents' must be an array")
		}
	}
	return nil
}

// ToDocuments converts the generic []interface{} input to []vectordb.Document.
func (i *Input) ToDocuments() ([]vectordb.Document, error) {
	if len(i.Documents) == 0 {
		return nil, fmt.Errorf("at least one document is required")
	}
	docs := make([]vectordb.Document, 0, len(i.Documents))
	for idx, raw := range i.Documents {
		var m map[string]interface{}
		switch v := raw.(type) {
		case map[string]interface{}:
			m = v
		default:
			b, err := json.Marshal(raw)
			if err != nil {
				return nil, fmt.Errorf("document[%d]: cannot marshal: %w", idx, err)
			}
			if err = json.Unmarshal(b, &m); err != nil {
				return nil, fmt.Errorf("document[%d]: cannot unmarshal: %w", idx, err)
			}
		}

		doc := vectordb.Document{Payload: make(map[string]interface{})}
		if id, ok := m["id"]; ok {
			doc.ID = fmt.Sprintf("%v", id)
		}
		if content, ok := m["content"]; ok {
			doc.Content = fmt.Sprintf("%v", content)
		}
		if vec, ok := m["vector"]; ok {
			switch vv := vec.(type) {
			case []interface{}:
				doc.Vector = make([]float64, len(vv))
				for j, f := range vv {
					switch fv := f.(type) {
					case float64:
						doc.Vector[j] = fv
					case float32:
						doc.Vector[j] = float64(fv)
					default:
						return nil, fmt.Errorf("document[%d].vector[%d]: unexpected type %T", idx, j, f)
					}
				}
			case []float64:
				doc.Vector = vv
			default:
				return nil, fmt.Errorf("document[%d].vector: unexpected type %T", idx, vec)
			}
		}
		for k, v := range m {
			if k != "id" && k != "content" && k != "vector" {
				doc.Payload[k] = v
			}
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// Output holds the activity result.
type Output struct {
	Success       bool   `md:"success"`
	UpsertedCount int    `md:"upsertedCount"`
	Duration      string `md:"duration"`
	Error         string `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":       o.Success,
		"upsertedCount": o.UpsertedCount,
		"duration":      o.Duration,
		"error":         o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	if val, ok := v["upsertedCount"]; ok {
		switch n := val.(type) {
		case int:
			o.UpsertedCount = n
		case float64:
			o.UpsertedCount = int(n)
		}
	}
	if val, ok := v["duration"]; ok {
		o.Duration = fmt.Sprintf("%v", val)
	}
	if val, ok := v["error"]; ok {
		o.Error = fmt.Sprintf("%v", val)
	}
	return nil
}
