// Package vectordbconnector implements the Flogo connection.Manager for Pinecone.
package vectordbconnector

import (
	"context"
	"fmt"
	"sync"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-pinecone"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "pinecone-connector")

var factory = &PineconeFactory{}
var sealOnce sync.Once

func init() {
	if err := connection.RegisterManagerFactory(factory); err != nil {
		panic(fmt.Sprintf("pinecone: failed to register connection manager factory: %v", err))
	}
}

// Settings holds the Pinecone connector properties shown in the Flogo UI.
type Settings struct {
	Name   string `md:"name,required"`
	Scheme string `md:"scheme"`
	Host   string `md:"host"`
	APIKey string `md:"apiKey"`

	PineconeCloud  string `md:"pineconeCloud"`
	PineconeRegion string `md:"pineconeRegion"`
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
		DBType:         "pinecone",
		Scheme:         s.Scheme,
		Host:           s.Host,
		APIKey:         s.APIKey,
		PineconeCloud:  s.PineconeCloud,
		PineconeRegion: s.PineconeRegion,
		TimeoutSeconds: s.TimeoutSeconds,
		MaxRetries:     s.MaxRetries,
		RetryBackoffMs: s.RetryBackoffMs,
	}
}

// PineconeFactory implements connection.ManagerFactory.
type PineconeFactory struct{}

func (*PineconeFactory) Type() string { return "pinecone-connector" }

func (*PineconeFactory) NewManager(settings map[string]interface{}) (connection.Manager, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(settings, s, true); err != nil {
		return nil, fmt.Errorf("pinecone connector: failed to map settings: %w", err)
	}
	// API key is required for cloud (https) but not for Pinecone Local (http).
	if s.APIKey == "" && s.Scheme != "http" {
		return nil, fmt.Errorf("pinecone connector: apiKey is required for cloud mode (set scheme=http for Pinecone Local)")
	}
	connRef := s.Name
	if connRef == "" {
		if s.Scheme == "http" {
			connRef = fmt.Sprintf("pinecone-local-%s", s.Host)
		} else {
			connRef = fmt.Sprintf("pinecone-%s-%s", s.PineconeCloud, s.PineconeRegion)
		}
	}
	client, err := vectordb.GetOrCreateClient(context.Background(), connRef, s.toConnectionConfig())
	if err != nil {
		return nil, fmt.Errorf("pinecone connector: failed to create client: %w", err)
	}
	logger.Infof("Pinecone connection established: name=%s cloud=%s region=%s", connRef, s.PineconeCloud, s.PineconeRegion)
	sealOnce.Do(func() { vectordb.SealRegistry() })
	return &PineconeConnection{name: connRef, client: client, settings: s}, nil
}

// PineconeConnection implements connection.Manager.
type PineconeConnection struct {
	name     string
	client   vectordb.VectorDBClient
	settings *Settings
}

func (c *PineconeConnection) Type() string                        { return "pinecone-connector" }
func (c *PineconeConnection) GetConnection() interface{}          { return c }
func (c *PineconeConnection) ReleaseConnection(_ interface{})     {}
func (c *PineconeConnection) GetClient() vectordb.VectorDBClient  { return c.client }
func (c *PineconeConnection) GetName() string                     { return c.name }
func (c *PineconeConnection) GetSettings() *Settings              { return c.settings }

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
	return fmt.Sprintf("Settings{Name:%q Scheme:%q Host:%q Cloud:%q Region:%q APIKey:%s EmbeddingProvider:%q EmbeddingAPIKey:%s}",
		s.Name, s.Scheme, s.Host, s.PineconeCloud, s.PineconeRegion, apiKey, s.EmbeddingProvider, embeddingAPIKey)
}

// NewConnectionForTest constructs a PineconeConnection from an existing client (tests only).
func NewConnectionForTest(name string, client vectordb.VectorDBClient, s *Settings) *PineconeConnection {
	return &PineconeConnection{name: name, client: client, settings: s}
}
