// Package vectordbconnector implements the Flogo connection.Manager and
// connection.ManagerFactory for the multi-provider VectorDB connector.
//
// The factory is registered under the module ref at init time.  Flogo loads
// the connector settings from the flogo.json / connector.json "connection"
// field and calls NewManager to produce a VectorDBConnection.  Activities
// obtain the shared VectorDB client by calling:
//
//	conn := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
//	client := conn.GetClient()
package vectordbconnector

import (
	"context"
	"fmt"
	"sync"

	"github.com/mpandav-tibco/flogo-extensions/vectordb"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "vectordb-connector")

var factory = &VectorDBFactory{}

// sealOnce ensures SealRegistry is called exactly once when the first real
// production connection is established via NewManager.  Tests that bypass
// NewManager (using NewConnectionForTest) are unaffected.
var sealOnce sync.Once

func init() {
	if err := connection.RegisterManagerFactory(factory); err != nil {
		panic(fmt.Sprintf("vectordb: failed to register connection manager factory: %v", err))
	}
}

// ---------------------------------------------------------------------------
// Settings – maps to connector.json settings block
// ---------------------------------------------------------------------------

// Settings holds every property shown in the Flogo UI connector dialog.
// These are the only place where host / port / TLS config should live.
type Settings struct {
	// Name is the human-readable connection name and also serves as the
	// registry key for the underlying VectorDB client.
	Name string `md:"name,required"`

	// DBType selects the vector database provider.
	// Accepted values: qdrant | weaviate | chroma | milvus
	DBType string `md:"dbType,required"`

	// Host is the hostname or IP address of the VectorDB server.
	Host string `md:"host,required"`

	// Port is the REST / HTTP port (provider-specific default applies if 0).
	Port int `md:"port"`

	// APIKey is the authentication token / API key.
	APIKey string `md:"apiKey"`

	// UseTLS enables TLS for the connection.
	UseTLS bool `md:"useTLS"`

	// TimeoutSeconds is the per-operation deadline (default: 30).
	TimeoutSeconds int `md:"timeoutSeconds"`

	// MaxRetries is the maximum number of retries on transient errors (default: 3).
	MaxRetries int `md:"maxRetries"`

	// RetryBackoffMs is the backoff in milliseconds between retries (default: 500).
	RetryBackoffMs int `md:"retryBackoffMs"`

	// GRPCPort is the gRPC port used by Qdrant (default: 6334).
	GRPCPort int `md:"grpcPort"`

	// Scheme is the HTTP scheme used by Weaviate: "http" or "https" (default: "http").
	Scheme string `md:"scheme"`

	// Username is the username used by Milvus authentication.
	Username string `md:"username"`

	// Password is the password used by Milvus authentication.
	Password string `md:"password"`

	// DBName is the Milvus database name (default: "default").
	DBName string `md:"dbName"`

	// DefaultMetricType is the distance metric used for Milvus vector searches.
	// Accepted values: cosine (default), dot, euclidean. Only used when DBType="milvus".
	DefaultMetricType string `md:"defaultMetricType"`

	// TLSInsecureSkipVerify disables certificate verification (dev/self-signed only).
	TLSInsecureSkipVerify bool `md:"tlsInsecureSkipVerify"`

	// TLSServerName overrides the TLS SNI server name.
	TLSServerName string `md:"tlsServerName"`

	// CACert is the CA certificate path or PEM content for server verification.
	CACert string `md:"caCert"`

	// ClientCert is the client certificate path or PEM content for mTLS.
	ClientCert string `md:"clientCert"`

	// ClientKey is the client private key path or PEM content for mTLS.
	ClientKey string `md:"clientKey"`

	// ---------------------------------------------------------------------------
	// AI Embedding Provider (optional shared configuration)
	// ---------------------------------------------------------------------------

	// EnableEmbedding opts in to connector-level embedding configuration.
	// When true, activities (ragQuery, ingestDocuments) can inherit the
	// embedding provider, API key, and base URL from this connector instead
	// of repeating them in every activity setting. The API key is stored once
	// here (with appPropertySupport) so it never needs to appear in flogo.json.
	EnableEmbedding bool `md:"enableEmbedding"`

	// EmbeddingProvider is the AI embedding service to use.
	// Accepted values: OpenAI | Azure OpenAI | Cohere | Ollama | Custom.
	// Only used when EnableEmbedding is true.
	EmbeddingProvider string `md:"embeddingProvider"`

	// EmbeddingAPIKey is the API key / bearer token for the embedding service.
	// Use $property[MY_KEY] to inject from an app property at runtime so the
	// secret never appears in the flogo.json source file.
	// Only used when EnableEmbedding is true.
	EmbeddingAPIKey string `md:"embeddingAPIKey"`

	// EmbeddingBaseURL overrides the default endpoint for the embedding provider.
	// Azure OpenAI: full deployment URL. Ollama: http://localhost:11434. Custom: your endpoint.
	// Only used when EnableEmbedding is true.
	EmbeddingBaseURL string `md:"embeddingBaseURL"`
}

// String returns a log-safe representation of Settings with sensitive fields redacted.
// Prevents APIKey, Password, ClientKey, and EmbeddingAPIKey from leaking into log files.
func (s *Settings) String() string {
	apiKey, password, clientKey, embKey := "", "", "", ""
	if s.APIKey != "" {
		apiKey = "[redacted]"
	}
	if s.Password != "" {
		password = "[redacted]"
	}
	if s.ClientKey != "" {
		clientKey = "[redacted]"
	}
	if s.EmbeddingAPIKey != "" {
		embKey = "[redacted]"
	}
	return fmt.Sprintf("Settings{Name:%q DBType:%q Host:%q Port:%d Username:%q APIKey:%s Password:%s ClientKey:%s EnableEmbedding:%v EmbeddingProvider:%q EmbeddingAPIKey:%s}",
		s.Name, s.DBType, s.Host, s.Port, s.Username, apiKey, password, clientKey, s.EnableEmbedding, s.EmbeddingProvider, embKey)
}

func (s *Settings) toConnectionConfig() vectordb.ConnectionConfig {
	return vectordb.ConnectionConfig{
		DBType:                s.DBType,
		Host:                  s.Host,
		Port:                  s.Port,
		APIKey:                s.APIKey,
		UseTLS:                s.UseTLS,
		TimeoutSeconds:        s.TimeoutSeconds,
		MaxRetries:            s.MaxRetries,
		RetryBackoffMs:        s.RetryBackoffMs,
		GRPCPort:              s.GRPCPort,
		Scheme:                s.Scheme,
		Username:              s.Username,
		Password:              s.Password,
		DBName:                s.DBName,
		DefaultMetricType:     s.DefaultMetricType,
		TLSInsecureSkipVerify: s.TLSInsecureSkipVerify,
		TLSServerName:         s.TLSServerName,
		CACert:                s.CACert,
		ClientCert:            s.ClientCert,
		ClientKey:             s.ClientKey,
	}
}

// ---------------------------------------------------------------------------
// VectorDBFactory – connection.ManagerFactory
// ---------------------------------------------------------------------------

// VectorDBFactory creates VectorDBConnection managers.
type VectorDBFactory struct{}

func (*VectorDBFactory) Type() string { return "vectordb" }

// NewManager is called by the Flogo engine when a connection is first used.
// It creates (or retrieves from the registry) a VectorDB client for the
// given settings.
func (*VectorDBFactory) NewManager(settings map[string]interface{}) (connection.Manager, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(settings, s, true); err != nil {
		return nil, fmt.Errorf("vectordb connector: failed to map settings: %w", err)
	}
	if s.DBType == "" {
		return nil, fmt.Errorf("vectordb connector: dbType is required")
	}
	if s.Host == "" {
		return nil, fmt.Errorf("vectordb connector: host is required")
	}

	// Use the connection name as registry key; fall back to a derived key.
	connRef := s.Name
	if connRef == "" {
		connRef = fmt.Sprintf("vectordb-%s-%s-%d", s.DBType, s.Host, s.Port)
	}

	client, err := vectordb.GetOrCreateClient(context.Background(), connRef, s.toConnectionConfig())
	if err != nil {
		return nil, fmt.Errorf("vectordb connector: failed to create client: %w", err)
	}

	logger.Infof("VectorDB connection established: name=%s provider=%s host=%s", connRef, s.DBType, s.Host)

	// Seal the registry on the first successful production connection.
	// This prevents ResetRegistry from being called accidentally in long-running
	// processes. Tests that use NewConnectionForTest bypass this path.
	sealOnce.Do(func() { vectordb.SealRegistry() })

	return &VectorDBConnection{name: connRef, client: client, settings: s}, nil
}

// ---------------------------------------------------------------------------
// VectorDBConnection – connection.Manager
// ---------------------------------------------------------------------------

// VectorDBConnection wraps a live VectorDB client and implements the
// project-flogo connection.Manager interface.  Activities should call
// GetConnection() to obtain this struct, then GetClient() to get the
// VectorDBClient for database operations.
type VectorDBConnection struct {
	name     string
	client   vectordb.VectorDBClient
	settings *Settings
}

// Type returns the connection type identifier.
func (c *VectorDBConnection) Type() string { return "vectordb" }

// GetConnection returns the VectorDBConnection itself so that activities can
// perform a type assertion: s.Connection.GetConnection().(*VectorDBConnection)
func (c *VectorDBConnection) GetConnection() interface{} { return c }

// ReleaseConnection is a no-op; pooling is handled internally by the registry.
func (c *VectorDBConnection) ReleaseConnection(_ interface{}) {}

// GetClient returns the live VectorDBClient for use in activities.
func (c *VectorDBConnection) GetClient() vectordb.VectorDBClient { return c.client }

// GetName returns the connection registry key / human-readable name.
func (c *VectorDBConnection) GetName() string { return c.name }

// GetSettings returns the raw connector settings.
func (c *VectorDBConnection) GetSettings() *Settings { return c.settings }

// NewConnectionForTest constructs a VectorDBConnection from an existing client.
// This is intended for unit tests only and must not be used in production code.
func NewConnectionForTest(name string, client vectordb.VectorDBClient, s *Settings) *VectorDBConnection {
	return &VectorDBConnection{name: name, client: client, settings: s}
}
