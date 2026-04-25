package vectordb

import (
	"fmt"
	"strings"
)

// Provider-specific batch size limits.
// These are the sole batch size gates — validateUpsertDocuments does not
// enforce a global limit because per-provider limits are always tighter.
// Qdrant recommends ≤1,000 points per HTTP request for typical latency;
// beyond ~1,000 × 1536-dim vectors the default 16 MB body limit is approached.
// Weaviate is more permissive but 2,000 objects is a safe ceiling.
// Chroma and Milvus are conservative at 1,000 per call.
const (
	maxUpsertBatchQdrant   = 1_000
	maxUpsertBatchWeaviate = 2_000
	maxUpsertBatchChroma   = 1_000
	maxUpsertBatchMilvus   = 1_000
)

// validateBatchSize enforces a per-provider batch limit after the generic
// validateUpsertDocuments check.  Returns ErrCodeBatchTooLarge if exceeded.
func validateBatchSize(n, limit int, provider string) error {
	if n > limit {
		return newError(ErrCodeBatchTooLarge,
			fmt.Sprintf("batch of %d documents exceeds the %s limit of %d; split into smaller batches", n, provider, limit), nil)
	}
	return nil
}

// normalizeDistanceMetric lowercases and trims the metric string, returning
// "cosine" for empty input.  Used internally so callers may pass any casing
// (e.g. "Cosine", "DOT") and receive the canonical form back.
func normalizeDistanceMetric(metric string) string {
	m := strings.ToLower(strings.TrimSpace(metric))
	if m == "" {
		return "cosine"
	}
	return m
}

// validateSearchRequest validates common search parameters.
func validateSearchRequest(collectionName string, queryVector []float64, topK int) error {
	if collectionName == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if len(queryVector) == 0 {
		return newError(ErrCodeInvalidQueryVector, "", nil)
	}
	if topK <= 0 {
		return newError(ErrCodeInvalidTopK, fmt.Sprintf("topK=%d must be > 0", topK), nil)
	}
	return nil
}

// validateUpsertDocuments validates documents before insertion.
// Dimension consistency is enforced: all vectors must have the same length.
func validateUpsertDocuments(docs []Document) error {
	if len(docs) == 0 {
		return newError(ErrCodeEmptyDocumentList, "", nil)
	}
	expectedDim := -1
	for i, d := range docs {
		if d.ID == "" {
			return newError(ErrCodeInvalidDocumentID, fmt.Sprintf("document[%d] has empty ID", i), nil)
		}
		if len(d.Vector) == 0 {
			return newError(ErrCodeInvalidVector, fmt.Sprintf("document[%d] (id=%s) has nil/empty vector", i, d.ID), nil)
		}
		if expectedDim == -1 {
			expectedDim = len(d.Vector)
		} else if len(d.Vector) != expectedDim {
			return newError(ErrCodeInvalidVector,
				fmt.Sprintf("document[%d] (id=%s) has vector length %d; expected %d (all vectors in a batch must have the same dimension)",
					i, d.ID, len(d.Vector), expectedDim), nil)
		}
	}
	return nil
}

// validateCollectionConfig validates a CollectionConfig before creation.
func validateCollectionConfig(cfg CollectionConfig) error {
	if cfg.Name == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if cfg.Dimensions <= 0 {
		return newError(ErrCodeInvalidDimensions, fmt.Sprintf("dimensions=%d must be > 0", cfg.Dimensions), nil)
	}
	// Normalise casing so callers may pass "Cosine", "DOT", etc.
	normMetric := normalizeDistanceMetric(cfg.DistanceMetric)
	switch normMetric {
	case "cosine", "dot", "euclidean":
		// valid
	default:
		return newError(ErrCodeInvalidMetric,
			fmt.Sprintf("DistanceMetric %q is invalid; use: cosine, dot, euclidean", cfg.DistanceMetric), nil)
	}
	return nil
}

// toFloat32Slice converts []float64 to []float32 for providers that require it.
func toFloat32Slice(v []float64) []float32 {
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = float32(f)
	}
	return out
}

// toFloat64Slice converts []float32 to []float64.
func toFloat64Slice(v []float32) []float64 {
	out := make([]float64, len(v))
	for i, f := range v {
		out[i] = float64(f)
	}
	return out
}
