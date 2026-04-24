package vectordb

import "context"

// VectorDBClient is the unified interface for all supported vector database providers.
// All implementations must be safe for concurrent use by multiple goroutines.
//
// Supported providers: qdrant, weaviate, chroma, milvus
type VectorDBClient interface {
	// --- Collection / Index Management ---

	// CreateCollection creates a new vector collection/index.
	CreateCollection(ctx context.Context, cfg CollectionConfig) error

	// DeleteCollection drops a collection and all its data permanently.
	DeleteCollection(ctx context.Context, name string) error

	// ListCollections returns the names of all collections.
	ListCollections(ctx context.Context) ([]string, error)

	// CollectionExists returns true if the named collection exists.
	CollectionExists(ctx context.Context, name string) (bool, error)

	// --- Document Operations ---

	// UpsertDocuments inserts or updates documents (matched by ID).
	UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error

	// GetDocument retrieves a single document by its ID.
	GetDocument(ctx context.Context, collectionName, id string) (*Document, error)

	// DeleteDocuments removes documents by their IDs.
	DeleteDocuments(ctx context.Context, collectionName string, ids []string) error

	// DeleteByFilter removes all documents matching the metadata filter.
	// Returns the number of documents deleted.
	DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error)

	// ScrollDocuments paginates through all documents, optionally filtered.
	ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error)

	// CountDocuments returns the count of documents matching the filter (nil = all).
	CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error)

	// --- Search ---

	// VectorSearch performs dense-vector similarity search.
	VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error)

	// HybridSearch combines dense vector search with sparse/keyword (BM25) search.
	// Providers without native hybrid support fall back to VectorSearch.
	HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error)

	// --- Lifecycle ---

	// HealthCheck verifies the provider is reachable and responsive.
	HealthCheck(ctx context.Context) error

	// DBType returns the provider identifier: "qdrant", "weaviate", "chroma", or "milvus".
	DBType() string

	// Close releases all resources held by this client.
	Close() error
}
