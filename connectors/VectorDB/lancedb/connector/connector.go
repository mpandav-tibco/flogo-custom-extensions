// Package vectordbconnector implements the Flogo connection.Manager for LanceDB.
package vectordbconnector

import (
	"context"
	"fmt"
	"sync"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-lancedb"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "lancedb-connector")

var factory = &LanceDBFactory{}
var sealOnce sync.Once

func init() {
	if err := connection.RegisterManagerFactory(factory); err != nil {
		panic(fmt.Sprintf("lancedb: failed to register connection manager factory: %v", err))
	}
}

// Settings holds the LanceDB connector properties shown in the Flogo UI.
type Settings struct {
	Name           string `md:"name,required"`
	Scheme         string `md:"scheme"`
	Host           string `md:"host"`
	Port           int    `md:"port"`
	APIKey         string `md:"apiKey"`
	Region         string `md:"region"`
	TimeoutSeconds int    `md:"timeoutSeconds"`
	MaxRetries     int    `md:"maxRetries"`
	RetryBackoffMs int    `md:"retryBackoffMs"`

	// Embedding Provider (optional shared config)
	EnableEmbedding   bool   `md:"enableEmbedding"`
	EmbeddingProvider string `md:"embeddingProvider"`
	EmbeddingAPIKey   string `md:"embeddingAPIKey"`
	EmbeddingBaseURL  string `md:"embeddingBaseURL"`
}

func (s *Settings) toConnectionConfig() vectordb.ConnectionConfig {
	return vectordb.ConnectionConfig{
		DBType:         "lancedb",
		Host:           s.Host,
		Port:           s.Port,
		APIKey:         s.APIKey,
		Region:         s.Region,
		Scheme:         s.Scheme,
		TimeoutSeconds: s.TimeoutSeconds,
		MaxRetries:     s.MaxRetries,
		RetryBackoffMs: s.RetryBackoffMs,
	}
}

// LanceDBFactory implements connection.ManagerFactory.
type LanceDBFactory struct{}

func (*LanceDBFactory) Type() string { return "lancedb-connector" }

func (*LanceDBFactory) NewManager(settings map[string]interface{}) (connection.Manager, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(settings, s, true); err != nil {
		return nil, fmt.Errorf("lancedb connector: failed to map settings: %w", err)
	}
	if s.Host == "" {
		s.Host = "localhost"
	}
	if s.Port == 0 && s.Scheme != "https" {
		s.Port = 8181
	}
	connRef := s.Name
	if connRef == "" {
		connRef = fmt.Sprintf("lancedb-%s-%d", s.Host, s.Port)
	}
	client, err := vectordb.GetOrCreateClient(context.Background(), connRef, s.toConnectionConfig())
	if err != nil {
		return nil, fmt.Errorf("lancedb connector: failed to create client: %w", err)
	}
	logger.Infof("LanceDB connection established: name=%s host=%s", connRef, s.Host)
	sealOnce.Do(func() { vectordb.SealRegistry() })
	return &LanceDBConnection{name: connRef, client: client, settings: s}, nil
}

// LanceDBConnection implements connection.Manager.
type LanceDBConnection struct {
	name     string
	client   vectordb.VectorDBClient
	settings *Settings
}

func (c *LanceDBConnection) Type() string                        { return "lancedb-connector" }
func (c *LanceDBConnection) GetConnection() interface{}          { return c }
func (c *LanceDBConnection) ReleaseConnection(_ interface{})     {}
func (c *LanceDBConnection) GetClient() vectordb.VectorDBClient  { return c.client }
func (c *LanceDBConnection) GetName() string                     { return c.name }
func (c *LanceDBConnection) GetSettings() *Settings              { return c.settings }

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
	return fmt.Sprintf("Settings{Name:%q Host:%q Port:%d Scheme:%q Region:%q APIKey:%s EmbeddingProvider:%q EmbeddingAPIKey:%s}",
		s.Name, s.Host, s.Port, s.Scheme, s.Region, apiKey, s.EmbeddingProvider, embeddingAPIKey)
}

// NewConnectionForTest constructs a LanceDBConnection from an existing client (tests only).
func NewConnectionForTest(name string, client vectordb.VectorDBClient, s *Settings) *LanceDBConnection {
	return &LanceDBConnection{name: name, client: client, settings: s}
}
