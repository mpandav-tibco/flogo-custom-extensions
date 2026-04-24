package vectordb

import "fmt"

// validateSearchRequest validates common search parameters.
func validateSearchRequest(collectionName string, queryVector []float64, topK int) error {
	if collectionName == "" {
		return newError(ErrCodeCollectionNotFound, "collectionName must not be empty", nil)
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
func validateUpsertDocuments(docs []Document) error {
	if len(docs) == 0 {
		return newError(ErrCodeEmptyDocumentList, "", nil)
	}
	for i, d := range docs {
		if d.ID == "" {
			return newError(ErrCodeInvalidDocumentID, fmt.Sprintf("document[%d] has empty ID", i), nil)
		}
		if len(d.Vector) == 0 {
			return newError(ErrCodeInvalidVector, fmt.Sprintf("document[%d] (id=%s) has nil/empty vector", i, d.ID), nil)
		}
	}
	return nil
}

// validateCollectionConfig validates a CollectionConfig before creation.
func validateCollectionConfig(cfg CollectionConfig) error {
	if cfg.Name == "" {
		return newError(ErrCodeCollectionNotFound, "collection Name must not be empty", nil)
	}
	if cfg.Dimensions <= 0 {
		return newError(ErrCodeInvalidDimensions, fmt.Sprintf("dimensions=%d must be > 0", cfg.Dimensions), nil)
	}
	switch cfg.DistanceMetric {
	case "", "cosine", "dot", "euclidean":
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

// normalizeMetric converts our canonical metric name to provider-specific values.
func normalizeMetric(metric string) string {
	if metric == "" {
		return "cosine"
	}
	return metric
}
