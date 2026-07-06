// Package vectordbconnector implements the Flogo connection.Manager for pgvector.
package vectordbconnector

import (
	"context"
	"fmt"
	"sync"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-pgvector"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "pgvector-connector")

var factory = &PgvectorFactory{}
var sealOnce sync.Once

func init() {
	if err := connection.RegisterManagerFactory(factory); err != nil {
		panic(fmt.Sprintf("pgvector: failed to register connection manager factory: %v", err))
	}
}

// Settings holds the pgvector connector properties shown in the Flogo UI.
type Settings struct {
	Name           string `md:"name,required"`
	Host           string `md:"host,required"`
	Port           int    `md:"port"`
	Username       string `md:"username"`
	Password       string `md:"password"`
	DBName         string `md:"dbName"`
	SSLMode        string `md:"sslMode"`
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
		DBType:         "pgvector",
		Host:           s.Host,
		Port:           s.Port,
		Username:       s.Username,
		Password:       s.Password,
		DBName:         s.DBName,
		SSLMode:        s.SSLMode,
		TimeoutSeconds: s.TimeoutSeconds,
		MaxRetries:     s.MaxRetries,
		RetryBackoffMs: s.RetryBackoffMs,
	}
}

// PgvectorFactory implements connection.ManagerFactory.
type PgvectorFactory struct{}

func (*PgvectorFactory) Type() string { return "pgvector-connector" }

func (*PgvectorFactory) NewManager(settings map[string]interface{}) (connection.Manager, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(settings, s, true); err != nil {
		return nil, fmt.Errorf("pgvector connector: failed to map settings: %w", err)
	}
	if s.Host == "" {
		return nil, fmt.Errorf("pgvector connector: host is required")
	}
	connRef := s.Name
	if connRef == "" {
		connRef = fmt.Sprintf("pgvector-%s-%d", s.Host, s.Port)
	}
	client, err := vectordb.GetOrCreateClient(context.Background(), connRef, s.toConnectionConfig())
	if err != nil {
		return nil, fmt.Errorf("pgvector connector: failed to create client: %w", err)
	}
	logger.Infof("pgvector connection established: name=%s host=%s", connRef, s.Host)
	sealOnce.Do(func() { vectordb.SealRegistry() })
	return &PgvectorConnection{name: connRef, client: client, settings: s}, nil
}

// PgvectorConnection implements connection.Manager.
type PgvectorConnection struct {
	name     string
	client   vectordb.VectorDBClient
	settings *Settings
}

func (c *PgvectorConnection) Type() string                          { return "pgvector-connector" }
func (c *PgvectorConnection) GetConnection() interface{}            { return c }
func (c *PgvectorConnection) ReleaseConnection(_ interface{})       {}
func (c *PgvectorConnection) GetClient() vectordb.VectorDBClient    { return c.client }
func (c *PgvectorConnection) GetName() string                       { return c.name }
func (c *PgvectorConnection) GetSettings() *Settings                { return c.settings }

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
	return fmt.Sprintf("Settings{Name:%q Host:%q Port:%d DBName:%q Username:%q Password:%s EmbeddingProvider:%q EmbeddingAPIKey:%s}",
		s.Name, s.Host, s.Port, s.DBName, s.Username, password, s.EmbeddingProvider, embeddingAPIKey)
}

// NewConnectionForTest constructs a PgvectorConnection from an existing client (tests only).
func NewConnectionForTest(name string, client vectordb.VectorDBClient, s *Settings) *PgvectorConnection {
	return &PgvectorConnection{name: name, client: client, settings: s}
}
