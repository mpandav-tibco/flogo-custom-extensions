package vectordb

import "context"

// VectorDBClient is the unified interface for the Azure AI Search provider.
// All implementations must be safe for concurrent use by multiple goroutines.
type VectorDBClient interface {
	CreateCollection(ctx context.Context, cfg CollectionConfig) error
	DeleteCollection(ctx context.Context, name string) error
	ListCollections(ctx context.Context) ([]string, error)
	CollectionExists(ctx context.Context, name string) (bool, error)
	UpsertDocuments(ctx context.Context, collectionName string, docs []Document) error
	GetDocument(ctx context.Context, collectionName, id string) (*Document, error)
	DeleteDocuments(ctx context.Context, collectionName string, ids []string) error
	DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error)
	ScrollDocuments(ctx context.Context, req ScrollRequest) (*ScrollResult, error)
	CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error)
	VectorSearch(ctx context.Context, req SearchRequest) ([]SearchResult, error)
	HybridSearch(ctx context.Context, req HybridSearchRequest) ([]SearchResult, error)
	HealthCheck(ctx context.Context) error
	DBType() string
	Close() error
}
