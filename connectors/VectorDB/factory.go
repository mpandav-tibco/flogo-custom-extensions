package vectordb

import (
	"fmt"
	"strings"
)

// NewClient creates the appropriate VectorDBClient for the given ConnectionConfig.
// Called by GetOrCreateClient; callers normally use GetOrCreateClient instead.
func NewClient(cfg ConnectionConfig) (VectorDBClient, error) {
	if err := validateConnectionConfig(&cfg); err != nil {
		return nil, err
	}
	switch strings.ToLower(cfg.DBType) {
	case "qdrant":
		return newQdrantClient(cfg)
	case "weaviate":
		return newWeaviateClient(cfg)
	case "chroma":
		return newChromaClient(cfg)
	case "milvus":
		return newMilvusClient(cfg)
	default:
		return nil, newError(ErrCodeInvalidDBType,
			fmt.Sprintf("unsupported DBType %q; valid values: qdrant, weaviate, chroma, milvus", cfg.DBType), nil)
	}
}

// validateConnectionConfig applies defaults and validates required fields.
func validateConnectionConfig(cfg *ConnectionConfig) error {
	if cfg.DBType == "" {
		return newError(ErrCodeInvalidDBType, "", nil)
	}
	if cfg.Host == "" {
		return newError(ErrCodeMissingHost, "", nil)
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return newError(ErrCodeInvalidPort,
			fmt.Sprintf("port %d is out of range (1-65535)", cfg.Port), nil)
	}
	// Apply defaults
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoffMs <= 0 {
		cfg.RetryBackoffMs = 500
	}
	if cfg.Scheme == "" {
		cfg.Scheme = "http"
	}
	if cfg.DBName == "" {
		cfg.DBName = "default"
	}
	if cfg.GRPCPort <= 0 {
		cfg.GRPCPort = 6334
	}
	return nil
}
