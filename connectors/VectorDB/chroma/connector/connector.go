// Package vectordbconnector implements the Flogo connection.Manager for Chroma.
package vectordbconnector

import (
	"context"
	"fmt"
	"sync"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-chroma"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "chroma-connector")

var factory = &ChromaFactory{}
var sealOnce sync.Once

func init() {
	if err := connection.RegisterManagerFactory(factory); err != nil {
		panic(fmt.Sprintf("chroma: failed to register connection manager factory: %v", err))
	}
}

// Settings holds the Chroma connector properties shown in the Flogo UI.
type Settings struct {
	Name           string `md:"name,required"`
	Host           string `md:"host,required"`
	Port           int    `md:"port"`
	APIKey         string `md:"apiKey"`
	UseTLS         bool   `md:"useTLS"`
	TimeoutSeconds int    `md:"timeoutSeconds"`
	MaxRetries     int    `md:"maxRetries"`
	RetryBackoffMs int    `md:"retryBackoffMs"`
	// TLS settings
	TLSInsecureSkipVerify bool   `md:"tlsInsecureSkipVerify"`
	TLSServerName         string `md:"tlsServerName"`
	CACert                string `md:"caCert"`
	ClientCert            string `md:"clientCert"`
	ClientKey             string `md:"clientKey"`

	// Embedding Provider (optional shared config)
	EnableEmbedding   bool   `md:"enableEmbedding"`
	EmbeddingProvider string `md:"embeddingProvider"`
	EmbeddingAPIKey   string `md:"embeddingAPIKey"`
	EmbeddingBaseURL  string `md:"embeddingBaseURL"`
}

func (s *Settings) toConnectionConfig() vectordb.ConnectionConfig {
	return vectordb.ConnectionConfig{
		DBType:                "chroma",
		Host:                  s.Host,
		Port:                  s.Port,
		APIKey:                s.APIKey,
		UseTLS:                s.UseTLS,
		TimeoutSeconds:        s.TimeoutSeconds,
		MaxRetries:            s.MaxRetries,
		RetryBackoffMs:        s.RetryBackoffMs,
		TLSInsecureSkipVerify: s.TLSInsecureSkipVerify,
		TLSServerName:         s.TLSServerName,
		CACert:                s.CACert,
		ClientCert:            s.ClientCert,
		ClientKey:             s.ClientKey,
	}
}

// ChromaFactory implements connection.ManagerFactory.
type ChromaFactory struct{}

func (*ChromaFactory) Type() string { return "chroma-connector" }

func (*ChromaFactory) NewManager(settings map[string]interface{}) (connection.Manager, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(settings, s, true); err != nil {
		return nil, fmt.Errorf("chroma connector: failed to map settings: %w", err)
	}
	if s.Host == "" {
		return nil, fmt.Errorf("chroma connector: host is required")
	}
	connRef := s.Name
	if connRef == "" {
		connRef = fmt.Sprintf("chroma-%s-%d", s.Host, s.Port)
	}
	client, err := vectordb.GetOrCreateClient(context.Background(), connRef, s.toConnectionConfig())
	if err != nil {
		return nil, fmt.Errorf("chroma connector: failed to create client: %w", err)
	}
	logger.Infof("Chroma connection established: name=%s host=%s", connRef, s.Host)
	sealOnce.Do(func() { vectordb.SealRegistry() })
	return &ChromaConnection{name: connRef, client: client, settings: s}, nil
}

// ChromaConnection implements connection.Manager.
type ChromaConnection struct {
	name     string
	client   vectordb.VectorDBClient
	settings *Settings
}

func (c *ChromaConnection) Type() string                       { return "chroma-connector" }
func (c *ChromaConnection) GetConnection() interface{}         { return c }
func (c *ChromaConnection) ReleaseConnection(_ interface{})    {}
func (c *ChromaConnection) GetClient() vectordb.VectorDBClient { return c.client }
func (c *ChromaConnection) GetName() string                    { return c.name }
func (c *ChromaConnection) GetSettings() *Settings             { return c.settings }

// String returns a log-safe representation of Settings with sensitive fields redacted.
func (s *Settings) String() string {
	apiKey := ""
	if s.APIKey != "" {
		apiKey = "[redacted]"
	}
	embeddingAPIKey := ""
	if s.EmbeddingAPIKey != "" {
		embeddingAPIKey = "[redacted]"
	}
	clientKey := ""
	if s.ClientKey != "" {
		clientKey = "[redacted]"
	}
	return fmt.Sprintf("Settings{Name:%q Host:%q Port:%d APIKey:%s EmbeddingProvider:%q EmbeddingAPIKey:%s ClientKey:%s}",
		s.Name, s.Host, s.Port, apiKey, s.EmbeddingProvider, embeddingAPIKey, clientKey)
}

// NewConnectionForTest constructs a ChromaConnection from an existing client (tests only).
func NewConnectionForTest(name string, client vectordb.VectorDBClient, s *Settings) *ChromaConnection {
	return &ChromaConnection{name: name, client: client, settings: s}
}
