package vectordb

import (
	"context"
	"fmt"
)

// NewClient creates the Qdrant VectorDBClient for the given ConnectionConfig.
// Called by GetOrCreateClient; callers normally use GetOrCreateClient instead.
func NewClient(ctx context.Context, cfg ConnectionConfig) (VectorDBClient, error) {
	if err := validateConnectionConfig(&cfg); err != nil {
		return nil, err
	}
	return newQdrantClient(cfg)
}

// validateConnectionConfig applies defaults and validates required fields.
func validateConnectionConfig(cfg *ConnectionConfig) error {
	if cfg.Host == "" {
		return newError(ErrCodeMissingHost, "", nil)
	}
	// Apply provider default port if not set.
	if cfg.Port <= 0 {
		cfg.Port = 6333
	}

	if cfg.GRPCPort <= 0 {
		cfg.GRPCPort = 6334
	}
	if cfg.GRPCPort > 65535 {
		return newError(ErrCodeInvalidPort, fmt.Sprintf("grpcPort %d out of range", cfg.GRPCPort), nil)
	}
	if cfg.Port > 65535 {
		return newError(ErrCodeInvalidPort, fmt.Sprintf("port %d out of range", cfg.Port), nil)
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoffMs == 0 {
		cfg.RetryBackoffMs = 500
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.TimeoutSeconds < 0 {
		return newError(ErrCodeInvalidTimeout, "", nil)
	}
	return nil
}
