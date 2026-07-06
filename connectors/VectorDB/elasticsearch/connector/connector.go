// Package vectordbconnector implements the Flogo connection.Manager for Elasticsearch.
package vectordbconnector

import (
	"context"
	"fmt"
	"sync"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "elasticsearch-connector")

var factory = &ElasticsearchFactory{}
var sealOnce sync.Once

func init() {
	if err := connection.RegisterManagerFactory(factory); err != nil {
		panic(fmt.Sprintf("elasticsearch: failed to register connection manager factory: %v", err))
	}
}

// Settings holds the Elasticsearch connector properties shown in the Flogo UI.
type Settings struct {
	Name                  string `md:"name,required"`
	Host                  string `md:"host"`
	Port                  int    `md:"port"`
	Username              string `md:"username"`
	Password              string `md:"password"`
	APIKey                string `md:"apiKey"`
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
		DBType:                "elasticsearch",
		Host:                  s.Host,
		Port:                  s.Port,
		Username:              s.Username,
		Password:              s.Password,
		APIKey:                s.APIKey,
		UseTLS:                s.UseTLS,
		TLSInsecureSkipVerify: s.TLSInsecureSkipVerify,
		TimeoutSeconds:        s.TimeoutSeconds,
		MaxRetries:            s.MaxRetries,
		RetryBackoffMs:        s.RetryBackoffMs,
	}
}

// ElasticsearchFactory implements connection.ManagerFactory.
type ElasticsearchFactory struct{}

func (*ElasticsearchFactory) Type() string { return "elasticsearch-connector" }

func (*ElasticsearchFactory) NewManager(settings map[string]interface{}) (connection.Manager, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(settings, s, true); err != nil {
		return nil, fmt.Errorf("elasticsearch connector: failed to map settings: %w", err)
	}
	if s.Host == "" {
		s.Host = "localhost"
	}
	connRef := s.Name
	if connRef == "" {
		connRef = fmt.Sprintf("elasticsearch-%s-%d", s.Host, s.Port)
	}
	client, err := vectordb.GetOrCreateClient(context.Background(), connRef, s.toConnectionConfig())
	if err != nil {
		return nil, fmt.Errorf("elasticsearch connector: failed to create client: %w", err)
	}
	logger.Infof("elasticsearch connection established: name=%s host=%s", connRef, s.Host)
	sealOnce.Do(func() { vectordb.SealRegistry() })
	return &ElasticsearchConnection{name: connRef, client: client, settings: s}, nil
}

// ElasticsearchConnection implements connection.Manager.
type ElasticsearchConnection struct {
	name     string
	client   vectordb.VectorDBClient
	settings *Settings
}

func (c *ElasticsearchConnection) Type() string                       { return "elasticsearch-connector" }
func (c *ElasticsearchConnection) GetConnection() interface{}         { return c }
func (c *ElasticsearchConnection) ReleaseConnection(_ interface{})    {}
func (c *ElasticsearchConnection) GetClient() vectordb.VectorDBClient { return c.client }
func (c *ElasticsearchConnection) GetName() string                    { return c.name }
func (c *ElasticsearchConnection) GetSettings() *Settings             { return c.settings }

// String returns a log-safe representation of Settings with sensitive fields redacted.
func (s *Settings) String() string {
	password := ""
	if s.Password != "" {
		password = "[redacted]"
	}
	apiKey := ""
	if s.APIKey != "" {
		apiKey = "[redacted]"
	}
	embeddingAPIKey := ""
	if s.EmbeddingAPIKey != "" {
		embeddingAPIKey = "[redacted]"
	}
	return fmt.Sprintf("Settings{Name:%q Host:%q Port:%d Username:%q Password:%s APIKey:%s EmbeddingProvider:%q EmbeddingAPIKey:%s}",
		s.Name, s.Host, s.Port, s.Username, password, apiKey, s.EmbeddingProvider, embeddingAPIKey)
}

// NewConnectionForTest constructs an ElasticsearchConnection from an existing client (tests only).
func NewConnectionForTest(name string, client vectordb.VectorDBClient, s *Settings) *ElasticsearchConnection {
	return &ElasticsearchConnection{name: name, client: client, settings: s}
}
