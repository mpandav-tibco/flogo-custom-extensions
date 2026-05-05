package vectordb

import (
	"context"
	"fmt"
	"strings"
)

// NewClient creates the appropriate VectorDBClient for the given ConnectionConfig.
// Called by GetOrCreateClient; callers normally use GetOrCreateClient instead.
// The context is used only during connection establishment (Milvus gRPC dial);
// pass context.Background() when no deadline is required.
func NewClient(ctx context.Context, cfg ConnectionConfig) (VectorDBClient, error) {
	if err := validateConnectionConfig(&cfg); err != nil {
		return nil, err
	}
	// cfg.DBType is already normalised to lowercase by validateConnectionConfig.
	switch cfg.DBType {
	case "qdrant":
		return newQdrantClient(cfg)
	case "weaviate":
		return newWeaviateClient(cfg)
	case "chroma":
		return newChromaClient(cfg)
	case "milvus":
		return newMilvusClient(ctx, cfg)
	default:
		return nil, newError(ErrCodeInvalidDBType,
			fmt.Sprintf("unsupported DBType %q; valid values: qdrant, weaviate, chroma, milvus", cfg.DBType), nil)
	}
}

// validateConnectionConfig applies defaults and validates required fields.
func validateConnectionConfig(cfg *ConnectionConfig) error {
	// Normalise DBType early so all subsequent switch statements and comparisons
	// are case-insensitive regardless of what the caller passed in.
	cfg.DBType = strings.ToLower(strings.TrimSpace(cfg.DBType))
	if cfg.DBType == "" {
		return newError(ErrCodeInvalidDBType, "", nil)
	}
	// Validate the DBType before checking anything else so that callers with an
	// invalid type receive the correct error code even when Host is also empty.
	switch cfg.DBType {
	case "qdrant", "weaviate", "chroma", "milvus":
		// valid
	default:
		return newError(ErrCodeInvalidDBType,
			fmt.Sprintf("unsupported DBType %q; supported values: qdrant, weaviate, chroma, milvus", cfg.DBType), nil)
	}
	if cfg.Host == "" {
		return newError(ErrCodeMissingHost, "", nil)
	}

	// Apply port defaults before range validation so that Port=0 (UI default
	// meaning "use provider default") maps to the correct value.
	if cfg.Port <= 0 {
		switch cfg.DBType {
		case "qdrant":
			cfg.Port = 6333
		case "weaviate":
			cfg.Port = 8080
		case "chroma":
			cfg.Port = 8000
		case "milvus":
			cfg.Port = 19530
		default:
			cfg.Port = 0
		}
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return newError(ErrCodeInvalidPort,
			fmt.Sprintf("port %d is out of range (1-65535)", cfg.Port), nil)
	}

	// Apply remaining defaults
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoffMs <= 0 {
		cfg.RetryBackoffMs = 500
	}
	if cfg.Scheme == "" {
		cfg.Scheme = "http"
	}
	// Apply provider-specific defaults.
	switch strings.ToLower(cfg.DBType) {
	case "qdrant":
		// Qdrant exposes a gRPC port alongside HTTP; default to 6334.
		if cfg.GRPCPort <= 0 {
			cfg.GRPCPort = 6334
		}
		// DBName and DefaultMetricType are Milvus-only. Username is used by Milvus
		// only (Qdrant uses APIKey for authentication).
		if cfg.Username != "" || cfg.DBName != "" || cfg.DefaultMetricType != "" {
			logger.Warnf("validateConnectionConfig: Username, DBName, DefaultMetricType are Milvus-only fields and are ignored for DBType=%q; use APIKey for Qdrant Cloud authentication", cfg.DBType)
		}
	case "weaviate":
		// Warn when Qdrant-only fields are set.
		if cfg.GRPCPort > 0 {
			logger.Warnf("validateConnectionConfig: GRPCPort is a Qdrant-only field and is ignored for DBType=%q", cfg.DBType)
		}
		// Note: Username/Password are valid for Weaviate OIDC auth in addition to Milvus;
		// only DBName and DefaultMetricType are truly Milvus-only.
		if cfg.DBName != "" || cfg.DefaultMetricType != "" {
			logger.Warnf("validateConnectionConfig: DBName and DefaultMetricType are Milvus-only fields and are ignored for DBType=%q", cfg.DBType)
		}
	case "chroma":
		if cfg.GRPCPort > 0 {
			logger.Warnf("validateConnectionConfig: GRPCPort is a Qdrant-only field and is ignored for DBType=%q", cfg.DBType)
		}
		if cfg.DBName != "" || cfg.DefaultMetricType != "" {
			logger.Warnf("validateConnectionConfig: DBName and DefaultMetricType are Milvus-only fields and are ignored for DBType=%q", cfg.DBType)
		}
	case "milvus":
		// Milvus uses a database name (default "default").
		if cfg.DBName == "" {
			cfg.DBName = "default"
		}
		// Normalize the search metric type (defaults to "cosine").
		cfg.DefaultMetricType = normalizeDistanceMetric(cfg.DefaultMetricType)
		if cfg.GRPCPort > 0 {
			logger.Warnf("validateConnectionConfig: GRPCPort is a Qdrant-only field and is ignored for DBType=%q", cfg.DBType)
		}
	}
	return nil
}
