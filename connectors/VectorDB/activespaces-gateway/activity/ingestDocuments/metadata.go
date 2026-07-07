package ingestDocuments

import (
	"encoding/json"
	"fmt"

	"github.com/project-flogo/core/support/connection"
)

// Settings holds activity-level configuration set once at flow design time.
type Settings struct {
	// Connection is the VectorDB connector reference.
	Connection connection.Manager `md:"connection,required"`

	// UseConnectorEmbedding instructs the activity to inherit the embedding
	// provider, API key, and base URL from the VectorDB connector settings.
	// When true, only embeddingModel and embeddingDimensions need to be set
	// here. Set to false to supply per-activity embedding credentials instead.
	UseConnectorEmbedding bool `md:"useConnectorEmbedding"`

	// Embedding provider settings — same as createEmbeddings activity.
	// Leave blank when UseConnectorEmbedding=true (inherited from connector).
	EmbeddingProvider   string `md:"embeddingProvider"`
	EmbeddingAPIKey     string `md:"embeddingAPIKey"`
	EmbeddingBaseURL    string `md:"embeddingBaseURL"`
	EmbeddingModel      string `md:"embeddingModel"`
	EmbeddingDimensions int    `md:"embeddingDimensions"`

	// DefaultCollection is used when the input does not supply one.
	DefaultCollection string `md:"defaultCollection"`

	// ContentField is the key inside each document's payload that holds the
	// text to embed. Defaults to "text" if empty.
	ContentField string `md:"contentField"`

	// TimeoutSeconds caps the total operation (embed + upsert). Default 60.
	TimeoutSeconds int `md:"timeoutSeconds"`

	// EmbeddingBatchSize controls how many document texts are sent to the
	// embedding API in a single request. Default 100. Tune down for providers
	// with small payload or rate limits (e.g. 20 for free-tier OpenAI).
	EmbeddingBatchSize int `md:"embeddingBatchSize"`

	// ── Chunking ─────────────────────────────────────────────────────────────
	// EnableChunking, when true, splits each input document's text into smaller
	// segments before embedding. Removes the need for an upstream splitting step.
	// Sub-fields below are only used when EnableChunking=true.
	EnableChunking bool `md:"enableChunking"`

	// ChunkStrategy selects the splitting algorithm.
	// Allowed values: "fixed", "sentence", "paragraph", "heading".
	// Default: "paragraph".
	ChunkStrategy string `md:"chunkStrategy"`

	// ChunkSize is the target chunk length in characters.
	// Used by "fixed" (hard window) and "sentence" (soft accumulator).
	// Ignored by "paragraph" and "heading". Default: 1000.
	ChunkSize int `md:"chunkSize"`

	// ChunkOverlap is the number of characters shared between consecutive
	// chunks. Only meaningful for "fixed" strategy. Default: 200.
	ChunkOverlap int `md:"chunkOverlap"`
}

// Input holds the runtime inputs for an ingest operation.
type Input struct {
	// CollectionName overrides Settings.DefaultCollection at runtime.
	CollectionName string `md:"collectionName"`

	// Documents is an array of objects, each with at minimum a text field
	// (default key: "text"). Optional fields: "id", "metadata" (object).
	// Example: [{"id":"doc1","text":"hello world","metadata":{"source":"web"}}]
	Documents []interface{} `md:"documents"`

	// FileName and FileContent are the multipart REST upload inputs. Map:
	//   fileName    = =$flow.multipartFormData.filename
	//   fileContent = =$flow.multipartFormData.file[0]
	// The activity synthesises a file entry from them internally.
	FileName    string      `md:"fileName"`
	FileContent interface{} `md:"fileContent"`
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
	if val, ok := v["documents"]; ok && val != nil {
		if arr, ok := val.([]interface{}); ok {
			i.Documents = arr
		} else {
			return fmt.Errorf("vectordb-ingest: 'documents' must be an array")
		}
	}
	if val, ok := v["fileName"]; ok && val != nil {
		i.FileName = fmt.Sprintf("%v", val)
	}
	if val, ok := v["fileContent"]; ok && val != nil {
		i.FileContent = val
	}
	return nil
}

// RawDocument is the parsed representation of one input item.
type RawDocument struct {
	ID       string
	Text     string
	Metadata map[string]interface{}
}

// parseDocuments converts []interface{} input into typed RawDocument slice.
// Returns an empty slice (not an error) when raw is nil or empty — callers
// must ensure at least one source (documents or files) is provided.
func parseDocuments(raw []interface{}, contentField string) ([]RawDocument, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if contentField == "" {
		contentField = "text"
	}
	docs := make([]RawDocument, 0, len(raw))
	for idx, item := range raw {
		var m map[string]interface{}
		switch v := item.(type) {
		case map[string]interface{}:
			m = v
		default:
			b, err := json.Marshal(item)
			if err != nil {
				return nil, fmt.Errorf("document[%d]: cannot marshal: %w", idx, err)
			}
			if err = json.Unmarshal(b, &m); err != nil {
				return nil, fmt.Errorf("document[%d]: cannot unmarshal: %w", idx, err)
			}
		}

		text, _ := m[contentField].(string)
		if text == "" {
			return nil, fmt.Errorf("document[%d]: field %q is required and must be a non-empty string", idx, contentField)
		}

		doc := RawDocument{
			Text:     text,
			Metadata: make(map[string]interface{}),
		}
		if id, ok := m["id"]; ok {
			doc.ID = fmt.Sprintf("%v", id)
		}
		if meta, ok := m["metadata"]; ok {
			if mm, ok := meta.(map[string]interface{}); ok {
				doc.Metadata = mm
			}
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// Output holds the results returned by the activity.
type Output struct {
	Success       bool     `md:"success"`
	IngestedCount int      `md:"ingestedCount"`
	IDs           []string `md:"ids"`
	Dimensions    int      `md:"dimensions"`
	Duration      string   `md:"duration"`
	Error         string   `md:"error"`
	// SourceDocumentCount is the number of input documents before chunking.
	// Equal to IngestedCount when chunking is disabled.
	SourceDocumentCount int `md:"sourceDocumentCount"`
	// ChunksCreated is the total number of chunks stored in VectorDB.
	// Equal to IngestedCount when chunking is disabled.
	ChunksCreated int `md:"chunksCreated"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"success":             o.Success,
		"ingestedCount":       o.IngestedCount,
		"ids":                 o.IDs,
		"dimensions":          o.Dimensions,
		"duration":            o.Duration,
		"error":               o.Error,
		"sourceDocumentCount": o.SourceDocumentCount,
		"chunksCreated":       o.ChunksCreated,
	}
}

func (o *Output) FromMap(v map[string]interface{}) error {
	if val, ok := v["success"].(bool); ok {
		o.Success = val
	}
	if val, ok := v["ingestedCount"].(int); ok {
		o.IngestedCount = val
	}
	if val, ok := v["ids"].([]string); ok {
		o.IDs = val
	}
	if val, ok := v["dimensions"].(int); ok {
		o.Dimensions = val
	}
	if val, ok := v["duration"].(string); ok {
		o.Duration = val
	}
	if val, ok := v["error"].(string); ok {
		o.Error = val
	}
	if val, ok := v["sourceDocumentCount"].(int); ok {
		o.SourceDocumentCount = val
	}
	if val, ok := v["chunksCreated"].(int); ok {
		o.ChunksCreated = val
	}
	return nil
}
