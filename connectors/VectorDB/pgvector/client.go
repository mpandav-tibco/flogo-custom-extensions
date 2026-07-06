package vectordb

import "context"

// VectorDBClient is the unified interface for the pgvector provider.
// All implementations must be safe for concurrent use by multiple goroutines.
type VectorDBClient interface {
	// --- Collection / Index Management ---

	// CreateCollection creates a new vector collection (PostgreSQL table with pgvector extension).
	CreateCollection(ctx context.Context, cfg CollectionConfig) error

	// DeleteCollection drops a collection and all its data permanently.
	DeleteCollection(ctx context.Context, name string) error

	// ListCollections returns the names of all collections in the public schema.
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
	// Returns the number of documents deleted, or -1 if the provider does not
	// report a count. Callers must not treat -1 as an error.
	DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error)

	// ScrollDocuments paginates through all documents, optionally filtered.
	ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error)

	// CountDocuments returns the count of documents matching the filter (nil = all).
	CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error)

	// --- Search ---

	// VectorSearch performs dense-vector similarity search using pgvector operators.
	VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error)

	// HybridSearch combines dense vector search with PostgreSQL full-text search (BM25/tsvector).
	// Uses Reciprocal Rank Fusion (RRF) to blend results.
	HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error)

	// --- Lifecycle ---

	// HealthCheck verifies the PostgreSQL connection is reachable and responsive.
	HealthCheck(ctx context.Context) error

	// DBType returns "pgvector".
	DBType() string

	// Close releases all resources held by this client (closes the connection pool).
	Close() error
}
