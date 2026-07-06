package vectordb

import "fmt"

// Document represents a vector database record: an embedding vector plus metadata.
type Document struct {
	ID      string                 `json:"id"`
	Vector  []float64              `json:"vector,omitempty"`
	Content string                 `json:"content,omitempty"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// SearchResult is a ranked result from VectorSearch or HybridSearch.
type SearchResult struct {
	ID      string                 `json:"id"`
	Score   float64                `json:"score"`
	Payload map[string]interface{} `json:"payload,omitempty"`
	Content string                 `json:"content,omitempty"`
	Vector  []float64              `json:"vector,omitempty"`
}

// CollectionConfig defines the schema for a new vector collection/index.
type CollectionConfig struct {
	Name              string
	Dimensions        int
	DistanceMetric    string
	OnDisk            bool
	ReplicationFactor int
}

// SearchRequest encapsulates a vector similarity search.
type SearchRequest struct {
	CollectionName string
	QueryVector    []float64
	TopK           int
	ScoreThreshold float64
	Filters        map[string]interface{}
	WithVectors    bool
	SkipPayload    bool
}

// HybridSearchRequest combines dense vector and BM25 full-text search.
type HybridSearchRequest struct {
	CollectionName string
	QueryText      string
	QueryVector    []float64
	TopK           int
	ScoreThreshold float64
	Filters        map[string]interface{}
	Alpha          float64
	SkipPayload    bool
}

// ScrollRequest paginates through all documents in a collection.
type ScrollRequest struct {
	CollectionName string
	Limit          int
	Offset         string
	Filters        map[string]interface{}
	WithVectors    bool
}

// ScrollResult holds one page of documents and the cursor for the next page.
type ScrollResult struct {
	Documents  []Document
	NextOffset string
	Total      int64
}

// ConnectionConfig holds all settings needed to connect to Azure AI Search.
type ConnectionConfig struct {
	DBType         string
	Endpoint       string
	APIKey         string
	APIVersion     string
	TimeoutSeconds int
	MaxRetries     int
	RetryBackoffMs int
}

// String returns a log-safe representation of ConnectionConfig with sensitive fields redacted.
func (c ConnectionConfig) String() string {
	apiKey := ""
	if c.APIKey != "" {
		apiKey = "[redacted]"
	}
	return fmt.Sprintf(
		"ConnectionConfig{DBType:%q Endpoint:%q APIVersion:%q APIKey:%s}",
		c.DBType, c.Endpoint, c.APIVersion, apiKey,
	)
}
