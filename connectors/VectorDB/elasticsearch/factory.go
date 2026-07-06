package vectordb

import (
	"context"
	"fmt"
)

func NewClient(ctx context.Context, cfg ConnectionConfig) (VectorDBClient, error) {
	if err := validateConnectionConfig(&cfg); err != nil {
		return nil, err
	}
	return newElasticsearchClient(cfg)
}

func validateConnectionConfig(cfg *ConnectionConfig) error {
	cfg.DBType = "elasticsearch"

	if cfg.Host == "" {
		cfg.Host = "localhost"
	}

	if cfg.Port <= 0 {
		cfg.Port = 9200
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
