package ingestDocuments

import (
	"fmt"

	"github.com/project-flogo/core/support/connection"
)

type Settings struct {
	Connection        connection.Manager `md:"connection,required"`
	DefaultCollection string             `md:"defaultCollection"`
	ChunkStrategy     string             `md:"chunkStrategy"`
	ChunkSize         int                `md:"chunkSize"`
	ChunkOverlap      int                `md:"chunkOverlap"`
	EmbeddingProvider string             `md:"embeddingProvider"`
	EmbeddingAPIKey   string             `md:"embeddingAPIKey"`
	EmbeddingBaseURL  string             `md:"embeddingBaseURL"`
	EmbeddingModel    string             `md:"embeddingModel"`
	BatchSize         int                `md:"batchSize"`
}

type Input struct {
	CollectionName string        `md:"collectionName"`
	Documents      []interface{} `md:"documents"`
	FileName       string        `md:"fileName"`
	FileContent    string        `md:"fileContent"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"collectionName": i.CollectionName,
		"documents":      i.Documents,
		"fileName":       i.FileName,
		"fileContent":    i.FileContent,
	}
}

func (i *Input) FromMap(v map[string]interface{}) error {
	if val, ok := v["collectionName"]; ok {
		i.CollectionName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["documents"]; ok {
		if arr, ok := val.([]interface{}); ok {
			i.Documents = arr
		}
	}
	if val, ok := v["fileName"]; ok {
		i.FileName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["fileContent"]; ok {
		i.FileContent = fmt.Sprintf("%v", val)
	}
	return nil
}

type Output struct {
	Success       bool          `md:"success"`
	IngestedCount int           `md:"ingestedCount"`
	IDs           []interface{} `md:"ids"`
	Duration      string        `md:"duration"`
	Error         string        `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":       o.Success,
		"ingestedCount": o.IngestedCount,
		"ids":           o.IDs,
		"duration":      o.Duration,
		"error":         o.Error,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"]; ok {
		o.Success, _ = val.(bool)
	}
	return nil
}
