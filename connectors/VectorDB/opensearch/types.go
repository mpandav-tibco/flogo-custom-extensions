package vectordb

import (
	"context"
	"fmt"
)

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
	// Name is the collection identifier (maps to an OpenSearch index name).
	Name string

	// Dimensions is the embedding vector size (e.g. 1536 for text-embedding-3-small).
	Dimensions int

	// DistanceMetric is the similarity function: "cosine", "dot", or "euclidean".
	// Defaults to "cosine" if empty.
	DistanceMetric string

	// OnDisk controls on-disk storage (not applicable for OpenSearch; reserved for interface compatibility).
	OnDisk bool

	// ReplicationFactor sets the replica count (not applicable for OpenSearch; reserved for interface compatibility).
	ReplicationFactor int
}

// SearchRequest encapsulates a vector similarity search.
type SearchRequest struct {
	// CollectionName is the target collection (OpenSearch index name).
	CollectionName string

	// QueryVector is the dense embedding of the search query.
	QueryVector []float64

	// TopK is the maximum number of results to return.
	TopK int

	// ScoreThreshold filters out results below this similarity score (0 = disabled).
	ScoreThreshold float64

	// Filters is a provider-agnostic metadata filter expressed as a map.
	// Supported operators per key: {"$eq", "$ne", "$gt", "$gte", "$lt", "$lte", "$in"}.
	Filters map[string]interface{}

	// WithVectors includes the stored vector in each SearchResult.
	WithVectors bool

	// SkipPayload suppresses payload/metadata in each SearchResult when true.
	SkipPayload bool
}

// HybridSearchRequest combines dense vector and BM25 full-text search (RRF fusion).
type HybridSearchRequest struct {
	CollectionName string

	// QueryText is the keyword/BM25 component of the search.
	QueryText string

	// QueryVector is the dense component of the search.
	QueryVector []float64

	TopK           int
	ScoreThreshold float64
	Filters        map[string]interface{}

	// Alpha weights the fusion: 0.0 = sparse/BM25 only, 1.0 = dense only, 0.5 = balanced.
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

// ConnectionConfig holds all settings needed to connect to an OpenSearch instance.
type ConnectionConfig struct {
	// DBType identifies this as an OpenSearch connection. Always "opensearch".
	DBType string

	// Host is the OpenSearch server hostname or IP. Default: "localhost".
	Host string

	// Port is the OpenSearch port. Default: 9200.
	Port int

	// Username is the optional basic auth username.
	Username string

	// Password is the optional basic auth password.
	Password string

	// UseTLS controls whether to use HTTPS.
	UseTLS bool

	// TLSInsecureSkipVerify disables TLS certificate verification (insecure).
	TLSInsecureSkipVerify bool

	// TimeoutSeconds is the per-operation timeout. Default: 30.
	TimeoutSeconds int

	// MaxRetries is the number of retry attempts on transient errors. Default: 3.
	MaxRetries int

	// RetryBackoffMs is the initial retry backoff in milliseconds. Default: 500.
	RetryBackoffMs int
}

// String returns a log-safe representation of ConnectionConfig with sensitive fields redacted.
func (c ConnectionConfig) String() string {
	password := ""
	if c.Password != "" {
		password = "[redacted]"
	}
	return fmt.Sprintf(
		"ConnectionConfig{DBType:%q Host:%q Port:%d Username:%q Password:%s UseTLS:%v}",
		c.DBType, c.Host, c.Port, c.Username, password, c.UseTLS,
	)
}

// VectorDBClient is the unified interface for the OpenSearch provider.
// All implementations must be safe for concurrent use by multiple goroutines.
type VectorDBClient interface {
	// --- Collection / Index Management ---

	// CreateCollection creates a new vector index with knn_vector mapping.
	CreateCollection(ctx context.Context, cfg CollectionConfig) error

	// DeleteCollection drops an index and all its data permanently.
	DeleteCollection(ctx context.Context, name string) error

	// ListCollections returns the names of all user-visible indexes.
	ListCollections(ctx context.Context) ([]string, error)

	// CollectionExists returns true if the named index exists.
	CollectionExists(ctx context.Context, name string) (bool, error)

	// --- Document Operations ---

	// UpsertDocuments inserts or updates documents (matched by ID) via _bulk API.
	UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error

	// GetDocument retrieves a single document by its ID via _doc/{id}.
	GetDocument(ctx context.Context, collectionName, id string) (*Document, error)

	// DeleteDocuments removes documents by their IDs via _bulk delete.
	DeleteDocuments(ctx context.Context, collectionName string, ids []string) error

	// DeleteByFilter removes all documents matching the metadata filter via _delete_by_query.
	// Returns the number of documents deleted, or -1 if the provider does not report a count.
	DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error)

	// ScrollDocuments paginates through all documents using from/size pagination.
	ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error)

	// CountDocuments returns the count of documents matching the filter (nil = all).
	CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error)

	// --- Search ---

	// VectorSearch performs dense k-NN similarity search using the knn_vector field.
	VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error)

	// HybridSearch combines dense vector search with BM25 full-text search using bool/should.
	HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error)

	// --- Lifecycle ---

	// HealthCheck verifies the OpenSearch cluster is reachable and responsive.
	HealthCheck(ctx context.Context) error

	// DBType returns "opensearch".
	DBType() string

	// Close releases all resources held by this client.
	Close() error
}
