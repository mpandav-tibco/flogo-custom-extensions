package vectordb

import (
	"context"
)

// NewClient creates the Pinecone VectorDBClient for the given ConnectionConfig.
// Called by GetOrCreateClient; callers normally use GetOrCreateClient instead.
func NewClient(ctx context.Context, cfg ConnectionConfig) (VectorDBClient, error) {
	if err := validateConnectionConfig(&cfg); err != nil {
		return nil, err
	}
	return newPineconeClient(cfg)
}

// validateConnectionConfig applies defaults and validates required fields.
func validateConnectionConfig(cfg *ConnectionConfig) error {
	cfg.DBType = "pinecone"
	// Local emulator (Scheme="http") does not require an API key; cloud does.
	if cfg.APIKey == "" && cfg.Scheme != "http" {
		return newError(ErrCodeAuthFailed, "APIKey is required", nil)
	}
	if cfg.Host == "" {
		cfg.Host = "api.pinecone.io"
	}
	if cfg.PineconeCloud == "" {
		cfg.PineconeCloud = "aws"
	}
	if cfg.PineconeRegion == "" {
		cfg.PineconeRegion = "us-east-1"
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
