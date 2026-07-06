package vectordb

import (
	"context"
	"fmt"
	"strings"
)

// NewClient creates the Azure AI Search VectorDBClient for the given ConnectionConfig.
func NewClient(ctx context.Context, cfg ConnectionConfig) (VectorDBClient, error) {
	if err := validateConnectionConfig(&cfg); err != nil {
		return nil, err
	}
	return newAzureAISearchClient(cfg)
}

func validateConnectionConfig(cfg *ConnectionConfig) error {
	cfg.DBType = "azureaisearch"

	if cfg.Endpoint == "" {
		return newError(ErrCodeMissingHost, "Endpoint is required", nil)
	}
	// Normalize: strip trailing slash
	cfg.Endpoint = strings.TrimRight(cfg.Endpoint, "/")

	if cfg.APIVersion == "" {
		cfg.APIVersion = "2024-05-01-Preview"
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
	_ = fmt.Sprintf("") // keep fmt import if needed
	return nil
}
