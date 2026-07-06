// Package vectordbconnector implements the Flogo connection.Manager for Redis Stack.
package vectordbconnector

import (
	"context"
	"fmt"
	"sync"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-redis"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
)

var logger = log.ChildLogger(log.RootLogger(), "redis-connector")

var factory = &RedisFactory{}
var sealOnce sync.Once

func init() {
	if err := connection.RegisterManagerFactory(factory); err != nil {
		panic(fmt.Sprintf("redis: failed to register connection manager factory: %v", err))
	}
}

// Settings holds the Redis connector properties shown in the Flogo UI.
type Settings struct {
	Name           string `md:"name,required"`
	Host           string `md:"host,required"`
	Port           int    `md:"port"`
	Password       string `md:"password"`
	RedisDB        int    `md:"redisDB"`
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
		DBType:         "redis",
		Host:           s.Host,
		Port:           s.Port,
		Password:       s.Password,
		RedisDB:        s.RedisDB,
		TimeoutSeconds: s.TimeoutSeconds,
		MaxRetries:     s.MaxRetries,
		RetryBackoffMs: s.RetryBackoffMs,
	}
}

// RedisFactory implements connection.ManagerFactory.
type RedisFactory struct{}

func (*RedisFactory) Type() string { return "redis-connector" }

func (*RedisFactory) NewManager(settings map[string]interface{}) (connection.Manager, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(settings, s, true); err != nil {
		return nil, fmt.Errorf("redis connector: failed to map settings: %w", err)
	}
	if s.Host == "" {
		return nil, fmt.Errorf("redis connector: host is required")
	}
	connRef := s.Name
	if connRef == "" {
		connRef = fmt.Sprintf("redis-%s-%d", s.Host, s.Port)
	}
	client, err := vectordb.GetOrCreateClient(context.Background(), connRef, s.toConnectionConfig())
	if err != nil {
		return nil, fmt.Errorf("redis connector: failed to create client: %w", err)
	}
	logger.Infof("Redis connection established: name=%s host=%s:%d db=%d", connRef, s.Host, s.Port, s.RedisDB)
	sealOnce.Do(func() { vectordb.SealRegistry() })
	return &RedisConnection{name: connRef, client: client, settings: s}, nil
}

// RedisConnection implements connection.Manager.
type RedisConnection struct {
	name     string
	client   vectordb.VectorDBClient
	settings *Settings
}

func (c *RedisConnection) Type() string                        { return "redis-connector" }
func (c *RedisConnection) GetConnection() interface{}          { return c }
func (c *RedisConnection) ReleaseConnection(_ interface{})     {}
func (c *RedisConnection) GetClient() vectordb.VectorDBClient  { return c.client }
func (c *RedisConnection) GetName() string                     { return c.name }
func (c *RedisConnection) GetSettings() *Settings              { return c.settings }

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
	return fmt.Sprintf("Settings{Name:%q Host:%q Port:%d RedisDB:%d Password:%s EmbeddingProvider:%q EmbeddingAPIKey:%s}",
		s.Name, s.Host, s.Port, s.RedisDB, password, s.EmbeddingProvider, embeddingAPIKey)
}

// NewConnectionForTest constructs a RedisConnection from an existing client (tests only).
func NewConnectionForTest(name string, client vectordb.VectorDBClient, s *Settings) *RedisConnection {
	return &RedisConnection{name: name, client: client, settings: s}
}
