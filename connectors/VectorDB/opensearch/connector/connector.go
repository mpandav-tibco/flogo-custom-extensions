// Package vectordbconnector implements the Flogo connection.Manager for OpenSearch.
package vectordbconnector

import (
	"context"
	"fmt"
	"sync"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-opensearch"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "opensearch-connector")

var factory = &OpenSearchFactory{}
var sealOnce sync.Once

func init() {
	if err := connection.RegisterManagerFactory(factory); err != nil {
		panic(fmt.Sprintf("opensearch: failed to register connection manager factory: %v", err))
	}
}

// Settings holds the OpenSearch connector properties shown in the Flogo UI.
type Settings struct {
	Name                  string `md:"name,required"`
	Host                  string `md:"host"`
	Port                  int    `md:"port"`
	Username              string `md:"username"`
	Password              string `md:"password"`
	UseTLS                bool   `md:"useTLS"`
	TLSInsecureSkipVerify bool   `md:"tlsInsecureSkipVerify"`
	TimeoutSeconds        int    `md:"timeoutSeconds"`
	MaxRetries            int    `md:"maxRetries"`
	RetryBackoffMs        int    `md:"retryBackoffMs"`

	// Embedding Provider (optional shared config)
	EnableEmbedding   bool   `md:"enableEmbedding"`
	EmbeddingProvider string `md:"embeddingProvider"`
	EmbeddingAPIKey   string `md:"embeddingAPIKey"`
	EmbeddingBaseURL  string `md:"embeddingBaseURL"`
}

func (s *Settings) toConnectionConfig() vectordb.ConnectionConfig {
	return vectordb.ConnectionConfig{
		DBType:                "opensearch",
		Host:                  s.Host,
		Port:                  s.Port,
		Username:              s.Username,
		Password:              s.Password,
		UseTLS:                s.UseTLS,
		TLSInsecureSkipVerify: s.TLSInsecureSkipVerify,
		TimeoutSeconds:        s.TimeoutSeconds,
		MaxRetries:            s.MaxRetries,
		RetryBackoffMs:        s.RetryBackoffMs,
	}
}

// OpenSearchFactory implements connection.ManagerFactory.
type OpenSearchFactory struct{}

func (*OpenSearchFactory) Type() string { return "opensearch-connector" }

func (*OpenSearchFactory) NewManager(settings map[string]interface{}) (connection.Manager, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(settings, s, true); err != nil {
		return nil, fmt.Errorf("opensearch connector: failed to map settings: %w", err)
	}
	if s.Host == "" {
		s.Host = "localhost"
	}
	connRef := s.Name
	if connRef == "" {
		connRef = fmt.Sprintf("opensearch-%s-%d", s.Host, s.Port)
	}
	client, err := vectordb.GetOrCreateClient(context.Background(), connRef, s.toConnectionConfig())
	if err != nil {
		return nil, fmt.Errorf("opensearch connector: failed to create client: %w", err)
	}
	logger.Infof("opensearch connection established: name=%s host=%s", connRef, s.Host)
	sealOnce.Do(func() { vectordb.SealRegistry() })
	return &OpenSearchConnection{name: connRef, client: client, settings: s}, nil
}

// OpenSearchConnection implements connection.Manager.
type OpenSearchConnection struct {
	name     string
	client   vectordb.VectorDBClient
	settings *Settings
}

func (c *OpenSearchConnection) Type() string                          { return "opensearch-connector" }
func (c *OpenSearchConnection) GetConnection() interface{}            { return c }
func (c *OpenSearchConnection) ReleaseConnection(_ interface{})       {}
func (c *OpenSearchConnection) GetClient() vectordb.VectorDBClient    { return c.client }
func (c *OpenSearchConnection) GetName() string                       { return c.name }
func (c *OpenSearchConnection) GetSettings() *Settings                { return c.settings }

// String returns a log-safe representation of Settings with sensitive fields redacted.
func (s *Settings) String() string {
	password := ""
	if s.Password != "" {
		password = "[redacted]"
	}
	embeddingAPIKey := ""
	if s.EmbeddingAPIKey != "" {
		embeddingAPIKey = "[redacted]"
	}
	return fmt.Sprintf("Settings{Name:%q Host:%q Port:%d Username:%q Password:%s EmbeddingProvider:%q EmbeddingAPIKey:%s}",
		s.Name, s.Host, s.Port, s.Username, password, s.EmbeddingProvider, embeddingAPIKey)
}

// NewConnectionForTest constructs an OpenSearchConnection from an existing client (tests only).
func NewConnectionForTest(name string, client vectordb.VectorDBClient, s *Settings) *OpenSearchConnection {
	return &OpenSearchConnection{name: name, client: client, settings: s}
}
