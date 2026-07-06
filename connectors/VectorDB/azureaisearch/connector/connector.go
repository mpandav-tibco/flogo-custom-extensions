// Package vectordbconnector implements the Flogo connection.Manager for Azure AI Search.
package vectordbconnector

import (
	"context"
	"fmt"
	"sync"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-azureaisearch"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "azureaisearch-connector")

var factory = &AzureAISearchFactory{}
var sealOnce sync.Once

func init() {
	if err := connection.RegisterManagerFactory(factory); err != nil {
		panic(fmt.Sprintf("azureaisearch: failed to register connection manager factory: %v", err))
	}
}

// Settings holds the Azure AI Search connector properties shown in the Flogo UI.
type Settings struct {
	Name           string `md:"name,required"`
	Endpoint       string `md:"endpoint,required"`
	APIKey         string `md:"apiKey"`
	APIVersion     string `md:"apiVersion"`
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
		DBType:         "azureaisearch",
		Endpoint:       s.Endpoint,
		APIKey:         s.APIKey,
		APIVersion:     s.APIVersion,
		TimeoutSeconds: s.TimeoutSeconds,
		MaxRetries:     s.MaxRetries,
		RetryBackoffMs: s.RetryBackoffMs,
	}
}

// AzureAISearchFactory implements connection.ManagerFactory.
type AzureAISearchFactory struct{}

func (*AzureAISearchFactory) Type() string { return "azureaisearch-connector" }

func (*AzureAISearchFactory) NewManager(settings map[string]interface{}) (connection.Manager, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(settings, s, true); err != nil {
		return nil, fmt.Errorf("azureaisearch connector: failed to map settings: %w", err)
	}
	if s.Endpoint == "" {
		return nil, fmt.Errorf("azureaisearch connector: endpoint is required")
	}
	connRef := s.Name
	if connRef == "" {
		connRef = fmt.Sprintf("azureaisearch-%s", s.Endpoint)
	}
	client, err := vectordb.GetOrCreateClient(context.Background(), connRef, s.toConnectionConfig())
	if err != nil {
		return nil, fmt.Errorf("azureaisearch connector: failed to create client: %w", err)
	}
	logger.Infof("Azure AI Search connection established: name=%s endpoint=%s", connRef, s.Endpoint)
	sealOnce.Do(func() { vectordb.SealRegistry() })
	return &AzureAISearchConnection{name: connRef, client: client, settings: s}, nil
}

// AzureAISearchConnection implements connection.Manager.
type AzureAISearchConnection struct {
	name     string
	client   vectordb.VectorDBClient
	settings *Settings
}

func (c *AzureAISearchConnection) Type() string                       { return "azureaisearch-connector" }
func (c *AzureAISearchConnection) GetConnection() interface{}         { return c }
func (c *AzureAISearchConnection) ReleaseConnection(_ interface{})    {}
func (c *AzureAISearchConnection) GetClient() vectordb.VectorDBClient { return c.client }
func (c *AzureAISearchConnection) GetName() string                    { return c.name }
func (c *AzureAISearchConnection) GetSettings() *Settings             { return c.settings }

func (s *Settings) String() string {
	apiKey := ""
	if s.APIKey != "" {
		apiKey = "[redacted]"
	}
	embeddingAPIKey := ""
	if s.EmbeddingAPIKey != "" {
		embeddingAPIKey = "[redacted]"
	}
	return fmt.Sprintf("Settings{Name:%q Endpoint:%q APIKey:%s EmbeddingProvider:%q EmbeddingAPIKey:%s}",
		s.Name, s.Endpoint, apiKey, s.EmbeddingProvider, embeddingAPIKey)
}

// NewConnectionForTest constructs an AzureAISearchConnection from an existing client (tests only).
func NewConnectionForTest(name string, client vectordb.VectorDBClient, s *Settings) *AzureAISearchConnection {
	return &AzureAISearchConnection{name: name, client: client, settings: s}
}
