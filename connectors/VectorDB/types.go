package vectordb

import "fmt"

// Document represents a vector database record: an embedding vector plus metadata.
type Document struct {
	// ID is the unique identifier. Use UUIDs for Qdrant/Weaviate compatibility.
	// Chroma accepts any string. Milvus maps to the primary-key field.
	ID string `json:"id"`

	// Vector is the dense embedding (float64 for precision; converted per provider).
	// Required for upsert; may be nil on retrieval unless WithVectors=true.
	Vector []float64 `json:"vector,omitempty"`

	// Content is the source text that was embedded — stored for reference retrieval.
	Content string `json:"content,omitempty"`

	// Payload holds arbitrary key-value metadata (source, category, timestamps, etc.).
	// The following keys are reserved for internal connector use and must not be
	// set by callers — they will be silently overwritten:
	//   "_original_id", "_content"         (Qdrant)
	//   "_docId",       "_metadata"         (Weaviate, Chroma, Milvus)
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
	// Name is the collection identifier.
	Name string

	// Dimensions is the embedding vector size (e.g. 1536 for text-embedding-3-small).
	Dimensions int

	// DistanceMetric is the similarity function: "cosine", "dot", or "euclidean".
	// Defaults to "cosine" if empty.
	DistanceMetric string

	// OnDisk controls Qdrant's on-disk vector storage (reduces RAM usage).
	OnDisk bool

	// ReplicationFactor sets the replica count for Weaviate / Milvus clusters.
	// Defaults to 1.
	ReplicationFactor int
}

// SearchRequest encapsulates a vector similarity search.
type SearchRequest struct {
	// CollectionName is the target collection.
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
	// The zero value (false) is the safe default: payload and content are
	// included unless explicitly skipped. Set to true only for ranking-only
	// passes where only the ID and score are needed.
	SkipPayload bool
}

// HybridSearchRequest combines dense vector and sparse/keyword (BM25) search.
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
	// The zero value (false) is the safe default: payload is included.
	// Mirrors SearchRequest.SkipPayload for uniform behaviour across search operations.
	SkipPayload bool
}

// ScrollRequest paginates through all documents in a collection.
type ScrollRequest struct {
	CollectionName string

	// Limit is the maximum number of documents to return per page.
	Limit int

	// Offset is an opaque cursor returned by the previous page. Empty = first page.
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
	// -1 means the provider does not support a count in this operation; it is
	// NOT an error. Use CountDocuments for an authoritative count.
	Total int64
}

// ConnectionConfig holds all settings needed to connect to a VectorDB provider.
type ConnectionConfig struct {
	// DBType selects the provider: "qdrant", "weaviate", "chroma", or "milvus".
	DBType string

	// Host is the database server hostname or IP.
	Host string

	// Port is the primary connection port.
	// Defaults: Qdrant REST=6333, Qdrant gRPC=6334, Weaviate=8080, Chroma=8000, Milvus=19530.
	Port int

	// APIKey for cloud-hosted instances (Qdrant Cloud, Weaviate Cloud).
	APIKey string

	// UseTLS enables TLS/HTTPS for the connection.
	UseTLS bool

	// TimeoutSeconds is the per-operation timeout. Default: 30.
	TimeoutSeconds int

	// MaxRetries is the number of retry attempts on transient errors. Default: 3.
	MaxRetries int

	// RetryBackoffMs is the initial retry backoff in milliseconds. Default: 500.
	RetryBackoffMs int

	// --- Provider-specific settings ---

	// GRPCPort is the Qdrant gRPC port (default 6334). Only used when DBType="qdrant".
	GRPCPort int

	// Scheme is the HTTP scheme for Weaviate: "http" or "https". Default: "http".
	Scheme string

	// Username / Password are used for Milvus authentication.
	Username string
	Password string

	// DBName is the Milvus database name. Default: "default".
	DBName string

	// DefaultMetricType is the distance metric used for vector similarity in Milvus searches
	// ("cosine", "dot", "euclidean"). Defaults to "cosine". Only used when DBType="milvus".
	DefaultMetricType string

	// --- TLS certificate settings (only used when UseTLS=true) ---

	// TLSInsecureSkipVerify disables certificate verification. Use only for dev/self-signed certs.
	TLSInsecureSkipVerify bool

	// TLSServerName overrides the server name sent in the TLS SNI extension.
	TLSServerName string

	// CACert is the path to (or PEM content of) a CA certificate for server verification.
	CACert string

	// ClientCert is the path to (or PEM content of) the client certificate for mTLS.
	ClientCert string

	// ClientKey is the path to (or PEM content of) the client private key for mTLS.
	ClientKey string
}

// String returns a human-readable representation of the ConnectionConfig with
// all sensitive fields (APIKey, Password, ClientKey) replaced by "[redacted]".
// This prevents credentials from leaking into log files or error messages.
func (c ConnectionConfig) String() string {
	apiKey := ""
	if c.APIKey != "" {
		apiKey = "[redacted]"
	}
	password := ""
	if c.Password != "" {
		password = "[redacted]"
	}
	clientKey := ""
	if c.ClientKey != "" {
		clientKey = "[redacted]"
	}
	return fmt.Sprintf(
		"ConnectionConfig{DBType:%q Host:%q Port:%d Username:%q APIKey:%s Password:%s UseTLS:%v ClientKey:%s}",
		c.DBType, c.Host, c.Port, c.Username, apiKey, password, c.UseTLS, clientKey,
	)
}
