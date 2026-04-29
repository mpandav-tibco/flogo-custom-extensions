// Package chromalocal is a no-CGo stub for github.com/amikos-tech/chroma-go-local.
// It satisfies the compile-time API surface required by github.com/amikos-tech/chroma-go/pkg/api/v2
// without pulling in ebitengine/purego → runtime/cgo.
//
// All functions that would start an embedded/in-process Chroma instance return
// errNotSupported. Only the HTTP client path (chromago.NewHTTPClient) is used in
// production; this stub is never called at runtime.
package chromalocal

import "errors"

var errNotSupported = errors.New("embedded Chroma not supported (chroma-go-local stub)")

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// ServerConfig holds configuration for the Chroma server process.
type ServerConfig struct {
	Port           int
	ListenAddress  string
	PersistPath    string
	AllowReset     bool
	SQLiteFilename string
	MaxPayloadSize int
	CORSOrigins    []string
	OTLPEndpoint   string
	OTLPService    string
	RawYAML        string
}

// ServerOption is a function that configures a ServerConfig.
type ServerOption func(*ServerConfig)

// DefaultServerConfig returns a ServerConfig with Chroma's default values.
func DefaultServerConfig() *ServerConfig { return &ServerConfig{Port: 8000} }

// WithPort sets the server port.
func WithPort(port int) ServerOption { return func(c *ServerConfig) { c.Port = port } }

// WithListenAddress sets the bind address.
func WithListenAddress(addr string) ServerOption {
	return func(c *ServerConfig) { c.ListenAddress = addr }
}

// WithPersistPath sets the persistence directory.
func WithPersistPath(path string) ServerOption {
	return func(c *ServerConfig) { c.PersistPath = path }
}

// WithAllowReset enables the /api/v1/reset endpoint.
func WithAllowReset(allow bool) ServerOption {
	return func(c *ServerConfig) { c.AllowReset = allow }
}

// WithSQLiteFilename sets the SQLite filename.
func WithSQLiteFilename(filename string) ServerOption {
	return func(c *ServerConfig) { c.SQLiteFilename = filename }
}

// WithMaxPayloadSize sets the maximum payload size in bytes.
func WithMaxPayloadSize(bytes int) ServerOption {
	return func(c *ServerConfig) { c.MaxPayloadSize = bytes }
}

// WithCORSAllowOrigins sets allowed CORS origins.
func WithCORSAllowOrigins(origins ...string) ServerOption {
	return func(c *ServerConfig) { c.CORSOrigins = origins }
}

// WithOpenTelemetry configures OTel export.
func WithOpenTelemetry(endpoint, serviceName string) ServerOption {
	return func(c *ServerConfig) { c.OTLPEndpoint = endpoint; c.OTLPService = serviceName }
}

// WithRawYAML sets a raw YAML config string.
func WithRawYAML(yaml string) ServerOption { return func(c *ServerConfig) { c.RawYAML = yaml } }

// StartServerConfig holds low-level start config for the server process.
type StartServerConfig struct {
	ConfigPath   string
	ConfigString string
}

// Server represents a running Chroma server process.
type Server struct{}

// NewServer creates and starts a Chroma server — not supported in this stub.
func NewServer(_ ...ServerOption) (*Server, error) { return nil, errNotSupported }

// StartServer starts a Chroma server from a low-level config — not supported.
func StartServer(_ StartServerConfig) (*Server, error) { return nil, errNotSupported }

// Port returns the server port. Always 0 on the stub.
func (s *Server) Port() int { return 0 }

// Address returns the bind address.
func (s *Server) Address() string { return "" }

// URL returns the server URL.
func (s *Server) URL() string { return "" }

// Stop stops the server.
func (s *Server) Stop() error { return errNotSupported }

// Close closes the server.
func (s *Server) Close() error { return errNotSupported }

// Init loads the native Chroma library from libPath — not supported.
func Init(_ string) error { return errNotSupported }

// VersionWithError returns the native library version — not supported.
func VersionWithError() (string, error) { return "", errNotSupported }

// ---------------------------------------------------------------------------
// Embedded (in-process) types
// ---------------------------------------------------------------------------

// EmbeddedOption configures an EmbeddedConfig.
type EmbeddedOption func(*EmbeddedConfig)

// EmbeddedConfig holds configuration for in-process Chroma.
type EmbeddedConfig struct {
	PersistPath    string
	SQLiteFilename string
	AllowReset     bool
	RawYAML        string
}

// DefaultEmbeddedConfig returns an EmbeddedConfig with default values.
func DefaultEmbeddedConfig() *EmbeddedConfig { return &EmbeddedConfig{} }

// WithEmbeddedPersistPath sets the persistence directory.
func WithEmbeddedPersistPath(path string) EmbeddedOption {
	return func(c *EmbeddedConfig) { c.PersistPath = path }
}

// WithEmbeddedSQLiteFilename sets the SQLite filename.
func WithEmbeddedSQLiteFilename(filename string) EmbeddedOption {
	return func(c *EmbeddedConfig) { c.SQLiteFilename = filename }
}

// WithEmbeddedAllowReset enables the reset endpoint.
func WithEmbeddedAllowReset(allow bool) EmbeddedOption {
	return func(c *EmbeddedConfig) { c.AllowReset = allow }
}

// WithEmbeddedRawYAML sets a raw YAML config string.
func WithEmbeddedRawYAML(yaml string) EmbeddedOption {
	return func(c *EmbeddedConfig) { c.RawYAML = yaml }
}

// StartEmbeddedConfig holds low-level start config for embedded mode.
type StartEmbeddedConfig struct {
	ConfigPath   string
	ConfigString string
}

// Embedded represents an in-process Chroma instance — not supported.
type Embedded struct{}

// NewEmbedded starts in-process Chroma — not supported.
func NewEmbedded(_ ...EmbeddedOption) (*Embedded, error) { return nil, errNotSupported }

// StartEmbedded starts in-process Chroma from a low-level config — not supported.
func StartEmbedded(_ StartEmbeddedConfig) (*Embedded, error) { return nil, errNotSupported }

// ---------------------------------------------------------------------------
// Embedded request / response types
// (field names match github.com/amikos-tech/chroma-go-local@v0.3.3/embedded.go)
// ---------------------------------------------------------------------------

type EmbeddedCollection struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Tenant            string         `json:"tenant"`
	Database          string         `json:"database"`
	Metadata          map[string]any `json:"metadata"`
	ConfigurationJSON map[string]any `json:"configuration_json"`
	Schema            map[string]any `json:"schema"`
}

type EmbeddedDatabase struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Tenant string `json:"tenant"`
}

type EmbeddedTenant struct {
	Name         string  `json:"name"`
	ResourceName *string `json:"resource_name,omitempty"`
}

type EmbeddedHealthCheckResponse struct {
	IsExecutorReady  bool `json:"is_executor_ready"`
	IsLogClientReady bool `json:"is_log_client_ready"`
}

type EmbeddedIndexingStatusResponse struct {
	OpIndexingProgress float32 `json:"op_indexing_progress"`
	NumUnindexedOps    uint64  `json:"num_unindexed_ops"`
	NumIndexedOps      uint64  `json:"num_indexed_ops"`
	TotalOps           uint64  `json:"total_ops"`
}

type EmbeddedGetRecordsResponse struct {
	IDs        []string         `json:"ids"`
	Embeddings [][]float32      `json:"embeddings,omitempty"`
	Documents  []*string        `json:"documents,omitempty"`
	URIs       []*string        `json:"uris,omitempty"`
	Metadatas  []map[string]any `json:"metadatas,omitempty"`
	Include    []string         `json:"include,omitempty"`
}

type EmbeddedQueryResponse struct {
	IDs [][]string `json:"ids"`
}

type EmbeddedCreateTenantRequest struct {
	Name string `json:"name"`
}

type EmbeddedGetTenantRequest struct {
	Name string `json:"name"`
}

type EmbeddedUpdateTenantRequest struct {
	TenantID     string `json:"tenant_id"`
	ResourceName string `json:"resource_name"`
}

type EmbeddedCreateDatabaseRequest struct {
	Name     string `json:"name"`
	TenantID string `json:"tenant_id,omitempty"`
}

type EmbeddedListDatabasesRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	Limit    uint32 `json:"limit,omitempty"`
	Offset   uint32 `json:"offset,omitempty"`
}

type EmbeddedGetDatabaseRequest struct {
	Name     string `json:"name"`
	TenantID string `json:"tenant_id,omitempty"`
}

type EmbeddedDeleteDatabaseRequest struct {
	Name     string `json:"name"`
	TenantID string `json:"tenant_id,omitempty"`
}

type EmbeddedListCollectionsRequest struct {
	TenantID     string `json:"tenant_id,omitempty"`
	DatabaseName string `json:"database_name,omitempty"`
	Limit        uint32 `json:"limit,omitempty"`
	Offset       uint32 `json:"offset,omitempty"`
}

type EmbeddedGetCollectionRequest struct {
	Name         string `json:"name"`
	TenantID     string `json:"tenant_id,omitempty"`
	DatabaseName string `json:"database_name,omitempty"`
}

type EmbeddedCountCollectionsRequest struct {
	TenantID     string `json:"tenant_id,omitempty"`
	DatabaseName string `json:"database_name,omitempty"`
}

type EmbeddedUpdateCollectionRequest struct {
	CollectionID string         `json:"collection_id"`
	NewName      string         `json:"new_name,omitempty"`
	NewMetadata  map[string]any `json:"new_metadata,omitempty"`
	DatabaseName string         `json:"database_name,omitempty"`
}

type EmbeddedDeleteCollectionRequest struct {
	Name         string `json:"name"`
	TenantID     string `json:"tenant_id,omitempty"`
	DatabaseName string `json:"database_name,omitempty"`
}

type EmbeddedForkCollectionRequest struct {
	SourceCollectionID   string `json:"source_collection_id"`
	TargetCollectionName string `json:"target_collection_name"`
	TenantID             string `json:"tenant_id,omitempty"`
	DatabaseName         string `json:"database_name,omitempty"`
}

type EmbeddedCreateCollectionRequest struct {
	Name          string         `json:"name"`
	TenantID      string         `json:"tenant_id,omitempty"`
	DatabaseName  string         `json:"database_name,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Configuration map[string]any `json:"configuration,omitempty"`
	Schema        map[string]any `json:"schema,omitempty"`
	GetOrCreate   bool           `json:"get_or_create,omitempty"`
}

type EmbeddedAddRequest struct {
	CollectionID string           `json:"collection_id"`
	IDs          []string         `json:"ids"`
	Embeddings   [][]float32      `json:"embeddings"`
	Documents    []string         `json:"documents,omitempty"`
	URIs         []string         `json:"uris,omitempty"`
	Metadatas    []map[string]any `json:"metadatas,omitempty"`
	TenantID     string           `json:"tenant_id,omitempty"`
	DatabaseName string           `json:"database_name,omitempty"`
}

type EmbeddedUpsertRecordsRequest struct {
	CollectionID string           `json:"collection_id"`
	IDs          []string         `json:"ids"`
	Embeddings   [][]float32      `json:"embeddings"`
	Documents    []string         `json:"documents,omitempty"`
	URIs         []string         `json:"uris,omitempty"`
	Metadatas    []map[string]any `json:"metadatas,omitempty"`
	TenantID     string           `json:"tenant_id,omitempty"`
	DatabaseName string           `json:"database_name,omitempty"`
}

type EmbeddedUpdateRecordsRequest struct {
	CollectionID string           `json:"collection_id"`
	IDs          []string         `json:"ids"`
	Embeddings   [][]float32      `json:"embeddings,omitempty"`
	Documents    []string         `json:"documents,omitempty"`
	URIs         []string         `json:"uris,omitempty"`
	Metadatas    []map[string]any `json:"metadatas,omitempty"`
	TenantID     string           `json:"tenant_id,omitempty"`
	DatabaseName string           `json:"database_name,omitempty"`
}

type EmbeddedDeleteRecordsRequest struct {
	CollectionID  string         `json:"collection_id"`
	IDs           []string       `json:"ids,omitempty"`
	Where         map[string]any `json:"where,omitempty"`
	WhereDocument map[string]any `json:"where_document,omitempty"`
	TenantID      string         `json:"tenant_id,omitempty"`
	DatabaseName  string         `json:"database_name,omitempty"`
}

type EmbeddedGetRecordsRequest struct {
	CollectionID  string         `json:"collection_id"`
	IDs           []string       `json:"ids,omitempty"`
	Where         map[string]any `json:"where,omitempty"`
	WhereDocument map[string]any `json:"where_document,omitempty"`
	Limit         uint32         `json:"limit,omitempty"`
	Offset        uint32         `json:"offset,omitempty"`
	Include       []string       `json:"include,omitempty"`
	TenantID      string         `json:"tenant_id,omitempty"`
	DatabaseName  string         `json:"database_name,omitempty"`
}

type EmbeddedQueryRequest struct {
	CollectionID    string         `json:"collection_id"`
	QueryEmbeddings [][]float32    `json:"query_embeddings"`
	NResults        uint32         `json:"n_results,omitempty"`
	IDs             []string       `json:"ids,omitempty"`
	Where           map[string]any `json:"where,omitempty"`
	WhereDocument   map[string]any `json:"where_document,omitempty"`
	Include         []string       `json:"include,omitempty"`
	TenantID        string         `json:"tenant_id,omitempty"`
	DatabaseName    string         `json:"database_name,omitempty"`
}

type EmbeddedIndexingStatusRequest struct {
	CollectionID string `json:"collection_id"`
	DatabaseName string `json:"database_name,omitempty"`
}

type EmbeddedCountRecordsRequest struct {
	CollectionID string `json:"collection_id"`
	TenantID     string `json:"tenant_id,omitempty"`
	DatabaseName string `json:"database_name,omitempty"`
}
