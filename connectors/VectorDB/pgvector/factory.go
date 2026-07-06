package vectordb

import (
	"context"
	"fmt"
)

// NewClient creates the pgvector VectorDBClient for the given ConnectionConfig.
// Called by GetOrCreateClient; callers normally use GetOrCreateClient instead.
func NewClient(ctx context.Context, cfg ConnectionConfig) (VectorDBClient, error) {
	if err := validateConnectionConfig(&cfg); err != nil {
		return nil, err
	}
	return newPgvectorClient(ctx, cfg)
}

// validateConnectionConfig applies defaults and validates required fields.
func validateConnectionConfig(cfg *ConnectionConfig) error {
	cfg.DBType = "pgvector"

	if cfg.Host == "" {
		return newError(ErrCodeMissingHost, "", nil)
	}

	if cfg.Port <= 0 {
		cfg.Port = 5432
	}
	if cfg.Port > 65535 {
		return newError(ErrCodeInvalidPort, fmt.Sprintf("port %d out of range", cfg.Port), nil)
	}

	if cfg.DBName == "" {
		cfg.DBName = "postgres"
	}

	if cfg.SSLMode == "" {
		cfg.SSLMode = "disable"
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
