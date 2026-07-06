package vectordb

import "fmt"

// Document represents a vector database record: an embedding vector plus metadata.
type Document struct {
	// ID is the unique identifier.
	ID string `json:"id"`

	// Vector is the dense embedding (float64 for precision; converted per provider).
	// Required for upsert; may be nil on retrieval unless WithVectors=true.
	Vector []float64 `json:"vector,omitempty"`

	// Content is the source text that was embedded — stored for reference retrieval.
	Content string `json:"content,omitempty"`

	// Payload holds arbitrary key-value metadata (source, category, timestamps, etc.).
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// SearchResult is a ranked result from VectorSearch or HybridSearch.
type SearchResult struct {
	ID      string                 `json:"id"`
	Score   float64                `json:"score"`
	Payload map[string]interface{} `json:"payload,omitempty"`
	Content string                 `json:"content,omitempty"`
	// Vector is populated only when SearchRequest.WithVectors is true.
	Vector []float64 `json:"vector,omitempty"`
}

// CollectionConfig defines the schema for a new vector collection/index.
type CollectionConfig struct {
	// Name is the collection identifier (maps to a LanceDB table name).
	Name string

	// Dimensions is the embedding vector size (e.g. 1536 for text-embedding-3-small).
	Dimensions int

	// DistanceMetric is the similarity function: "cosine", "dot", or "euclidean".
	// Defaults to "cosine" if empty.
	DistanceMetric string

	// OnDisk controls on-disk storage (not applicable for LanceDB REST; reserved for interface compatibility).
	OnDisk bool

	// ReplicationFactor sets the replica count (not applicable for LanceDB REST; reserved for interface compatibility).
	ReplicationFactor int
}

// SearchRequest encapsulates a vector similarity search.
type SearchRequest struct {
	// CollectionName is the target collection (LanceDB table name).
	CollectionName string

	// QueryVector is the dense embedding of the search query.
	QueryVector []float64

	// TopK is the maximum number of results to return.
	TopK int

	// ScoreThreshold filters out results below this similarity score (0 = disabled).
	ScoreThreshold float64

	// Filters is a provider-agnostic metadata filter expressed as a map.
	Filters map[string]interface{}

	// WithVectors includes the stored vector in each SearchResult.
	WithVectors bool

	// SkipPayload suppresses payload/metadata in each SearchResult when true.
	SkipPayload bool
}

// HybridSearchRequest combines dense vector and full-text search (RRF fusion).
type HybridSearchRequest struct {
	CollectionName string

	// QueryText is the keyword component of the search.
	QueryText string

	// QueryVector is the dense component of the search.
	QueryVector []float64

	TopK           int
	ScoreThreshold float64
	Filters        map[string]interface{}

	// Alpha weights the fusion: 0.0 = sparse/FTS only, 1.0 = dense only, 0.5 = balanced.
	Alpha float64

	// SkipPayload suppresses payload/metadata in each SearchResult when true.
	SkipPayload bool
}

// ScrollRequest paginates through all documents in a collection.
type ScrollRequest struct {
	CollectionName string

	// Limit is the maximum number of documents to return per page.
	Limit int

	// Offset is a numeric string cursor returned by the previous page. Empty = first page.
	Offset string

	Filters     map[string]interface{}
	WithVectors bool
}

// ScrollResult holds one page of documents and the cursor for the next page.
type ScrollResult struct {
	Documents []Document

	// NextOffset is the cursor for the next page. Empty string means last page.
	NextOffset string

	// Total is the total count of matching documents across all pages.
	// -1 means the provider does not support a count in this operation.
	Total int64
}

// ConnectionConfig holds all settings needed to connect to a LanceDB server.
type ConnectionConfig struct {
	// DBType identifies this as a lancedb connection. Always "lancedb".
	DBType string

	// Host is the LanceDB server hostname or IP (e.g. "localhost" for self-hosted).
	Host string

	// Port is the LanceDB server port. Default: 8181 for self-hosted. 0 = omit (cloud).
	Port int

	// APIKey is required for LanceDB Cloud; optional for self-hosted.
	APIKey string

	// Region is the LanceDB Cloud region (e.g. "us-east-1"), empty for self-hosted.
	Region string

	// Scheme is "http" (self-hosted default) or "https" (cloud).
	Scheme string

	// TimeoutSeconds is the per-operation timeout. Default: 30.
	TimeoutSeconds int

	// MaxRetries is the number of retry attempts on transient errors. Default: 3.
	MaxRetries int

	// RetryBackoffMs is the initial retry backoff in milliseconds. Default: 500.
	RetryBackoffMs int
}

// String returns a log-safe representation of ConnectionConfig with sensitive fields redacted.
func (c ConnectionConfig) String() string {
	apiKey := ""
	if c.APIKey != "" {
		apiKey = "[redacted]"
	}
	return fmt.Sprintf(
		"ConnectionConfig{DBType:%q Host:%q Port:%d Scheme:%q Region:%q APIKey:%s}",
		c.DBType, c.Host, c.Port, c.Scheme, c.Region, apiKey,
	)
}
