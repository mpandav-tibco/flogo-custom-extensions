package vectordb

import "fmt"

const maxUpsertBatch = 1000

func validateCollectionConfig(cfg CollectionConfig) error {
	if cfg.Name == "" {
		return newError(ErrCodeInvalidCollectionName, "", nil)
	}
	if cfg.Dimensions <= 0 {
		return newError(ErrCodeInvalidDimensions, "", nil)
	}
	return nil
}

func validateUpsertDocuments(docs []Document) error {
	if len(docs) == 0 {
		return newError(ErrCodeEmptyDocumentList, "", nil)
	}
	for i, d := range docs {
		if d.ID == "" {
			return newError(ErrCodeInvalidDocumentID,
				fmt.Sprintf("document[%d].ID must not be empty", i), nil)
		}
		if len(d.Vector) == 0 {
			return newError(ErrCodeInvalidVector,
				fmt.Sprintf("document[%d].Vector must not be empty", i), nil)
		}
	}
	return nil
}

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

func validateBatchSize(n, max int, provider string) error {
	if n > max {
		return newError(ErrCodeBatchTooLarge,
			fmt.Sprintf("%s: batch size %d exceeds max %d", provider, n, max), nil)
	}
	return nil
}
