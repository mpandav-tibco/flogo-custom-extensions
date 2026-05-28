// Package vectordbconnector implements the Flogo connection.Manager for Milvus.
package vectordbconnector

import (
	"context"
	"fmt"
	"sync"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-milvus"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "milvus-connector")

var factory = &MilvusFactory{}
var sealOnce sync.Once

func init() {
	if err := connection.RegisterManagerFactory(factory); err != nil {
		panic(fmt.Sprintf("milvus: failed to register connection manager factory: %v", err))
	}
}

// Settings holds the Milvus connector properties shown in the Flogo UI.
type Settings struct {
	Name           string `md:"name,required"`
	Host           string `md:"host,required"`
	Port           int    `md:"port"`
	APIKey         string `md:"apiKey"`
	UseTLS         bool   `md:"useTLS"`
	TimeoutSeconds int    `md:"timeoutSeconds"`
	MaxRetries     int    `md:"maxRetries"`
	RetryBackoffMs int    `md:"retryBackoffMs"`
	// Milvus-specific auth and database settings.
	Username          string `md:"username"`
	Password          string `md:"password"`
	DBName            string `md:"dbName"`
	DefaultMetricType string `md:"defaultMetricType"`

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
		DBType:                "milvus",
		Host:                  s.Host,
		Port:                  s.Port,
		APIKey:                s.APIKey,
		UseTLS:                s.UseTLS,
		TimeoutSeconds:        s.TimeoutSeconds,
		MaxRetries:            s.MaxRetries,
		RetryBackoffMs:        s.RetryBackoffMs,
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

// MilvusFactory implements connection.ManagerFactory.
type MilvusFactory struct{}

func (*MilvusFactory) Type() string { return "milvus-connector" }

func (*MilvusFactory) NewManager(settings map[string]interface{}) (connection.Manager, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(settings, s, true); err != nil {
		return nil, fmt.Errorf("milvus connector: failed to map settings: %w", err)
	}
	if s.Host == "" {
		return nil, fmt.Errorf("milvus connector: host is required")
	}
	connRef := s.Name
	if connRef == "" {
		connRef = fmt.Sprintf("milvus-%s-%d", s.Host, s.Port)
	}
	client, err := vectordb.GetOrCreateClient(context.Background(), connRef, s.toConnectionConfig())
	if err != nil {
		return nil, fmt.Errorf("milvus connector: failed to create client: %w", err)
	}
	logger.Infof("Milvus connection established: name=%s host=%s", connRef, s.Host)
	sealOnce.Do(func() { vectordb.SealRegistry() })
	return &MilvusConnection{name: connRef, client: client, settings: s}, nil
}

// MilvusConnection implements connection.Manager.
type MilvusConnection struct {
	name     string
	client   vectordb.VectorDBClient
	settings *Settings
}

func (c *MilvusConnection) Type() string                       { return "milvus-connector" }
func (c *MilvusConnection) GetConnection() interface{}         { return c }
func (c *MilvusConnection) ReleaseConnection(_ interface{})    {}
func (c *MilvusConnection) GetClient() vectordb.VectorDBClient { return c.client }
func (c *MilvusConnection) GetName() string                    { return c.name }
func (c *MilvusConnection) GetSettings() *Settings             { return c.settings }

// String returns a log-safe representation of Settings with sensitive fields redacted.
func (s *Settings) String() string {
	apiKey := ""
	if s.APIKey != "" {
		apiKey = "[redacted]"
	}
	password := ""
	if s.Password != "" {
		password = "[redacted]"
	}
	embeddingAPIKey := ""
	if s.EmbeddingAPIKey != "" {
		embeddingAPIKey = "[redacted]"
	}
	clientKey := ""
	if s.ClientKey != "" {
		clientKey = "[redacted]"
	}
	return fmt.Sprintf("Settings{Name:%q Host:%q Port:%d Username:%q APIKey:%s Password:%s EmbeddingProvider:%q EmbeddingAPIKey:%s ClientKey:%s}",
		s.Name, s.Host, s.Port, s.Username, apiKey, password, s.EmbeddingProvider, embeddingAPIKey, clientKey)
}

// NewConnectionForTest constructs a MilvusConnection from an existing client (tests only).
func NewConnectionForTest(name string, client vectordb.VectorDBClient, s *Settings) *MilvusConnection {
	return &MilvusConnection{name: name, client: client, settings: s}
}
