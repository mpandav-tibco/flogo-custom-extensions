//go:build integration

// Integration tests for VectorDB providers.
// Run against live Docker containers:
//
//	docker run -d --name vdb-test-qdrant   -p 6333:6333 -p 6334:6334 qdrant/qdrant:latest
//	docker run -d --name vdb-test-chroma   -p 18000:8000 chromadb/chroma:latest
//	docker run -d --name vdb-test-weaviate -p 18080:8080 \
//	    -e AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED=true \
//	    -e DEFAULT_VECTORIZER_MODULE=none \
//	    -e CLUSTER_HOSTNAME=node1 cr.weaviate.io/semitechnologies/weaviate:latest
//	docker run -d --name vdb-test-milvus   -p 19530:19530 -p 9091:9091 \
//	    milvusdb/milvus:v2.4.0 milvus run standalone
//
// IMPORTANT — required env flags:
//
//	CGO_ENABLED=0   avoids C compiler requirements pulled in by chroma-go's
//	                pure-onnx/pure-tokenizers deps (not needed for HTTP ops).
//	GOTOOLCHAIN=auto allows go1.24.x toolchain to build the module which
//	                declares `go 1.25` (required by weaviate-go-client/v5).
//
// Execute (all providers):
//
//	GOFLAGS="-mod=mod" CGO_ENABLED=0 GOTOOLCHAIN=auto \
//	    go test -tags integration -v -timeout 120s ./connectors/VectorDB/
//
// Execute (single provider):
//
//	GOFLAGS="-mod=mod" CGO_ENABLED=0 GOTOOLCHAIN=auto go test -tags integration -v -timeout 120s -run TestQdrant   ./connectors/VectorDB/
//	GOFLAGS="-mod=mod" CGO_ENABLED=0 GOTOOLCHAIN=auto go test -tags integration -v -timeout 120s -run TestWeaviate ./connectors/VectorDB/
//	GOFLAGS="-mod=mod" CGO_ENABLED=0 GOTOOLCHAIN=auto go test -tags integration -v -timeout 120s -run TestChroma   ./connectors/VectorDB/
//	GOFLAGS="-mod=mod" CGO_ENABLED=0 GOTOOLCHAIN=auto go test -tags integration -v -timeout 120s -run TestMilvus   ./connectors/VectorDB/

package vectordb_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testDimensions = 4
	testTimeout    = 60 * time.Second
)

// makeVec returns a unit-length 4-D vector based on an angle in degrees.
// Vectors at nearby angles will have high cosine similarity.
func makeVec(angleDeg float64) []float64 {
	rad := angleDeg * math.Pi / 180
	v := []float64{
		math.Cos(rad),
		math.Sin(rad),
		math.Cos(rad * 2),
		math.Sin(rad * 2),
	}
	// normalise to unit length
	norm := 0.0
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] /= norm
	}
	return v
}

// listContains checks membership case-insensitively on the first character
// to accommodate Weaviate's PascalCase class names.
func listContains(cols []string, name string) bool {
	lower := strings.ToLower(name)
	for _, c := range cols {
		if strings.ToLower(c) == lower {
			return true
		}
	}
	return false
}

// suiteOptions controls provider-specific behaviours.
type suiteOptions struct {
	// skipDeleteByFilter: Weaviate DeleteByFilter is not yet implemented.
	skipDeleteByFilter bool
	// scoresAreDistance: some providers return distance (lower=better) instead of similarity.
	scoresAreDistance bool
	// collectionNameTransform: applied when listing (e.g. Weaviate PascalCases the name).
	collectionNameTransform func(string) string
	// countFiltersSupported: whether CountDocuments(ctx, col, nonNilFilter) works.
	// Weaviate returns ErrCodeNotImplemented for filtered counts.
	countFiltersSupported bool
}

// runProviderSuite executes the full CRUD + search lifecycle for a single provider.
func runProviderSuite(t *testing.T, cfg vectordb.ConnectionConfig, opts suiteOptions) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	client, err := vectordb.NewClient(context.Background(), cfg)
	require.NoError(t, err, "NewClient")
	defer client.Close()

	// unique collection name per run to avoid state leakage between test runs
	colName := fmt.Sprintf("integtest%d", time.Now().UnixMilli())

	// ---- HealthCheck ----
	t.Run("HealthCheck", func(t *testing.T) {
		err := client.HealthCheck(ctx)
		assert.NoError(t, err, "HealthCheck must succeed")
	})

	// ---- Input Validation (negative cases — pure Go, no DB state required) ----

	t.Run("Validate_UpsertDocuments_Nil", func(t *testing.T) {
		err := client.UpsertDocuments(ctx, colName, nil)
		require.Error(t, err, "nil document slice must return an error")
		var ve *vectordb.VDBError
		if errors.As(err, &ve) {
			assert.Equal(t, vectordb.ErrCodeEmptyDocumentList, ve.Code)
		}
	})

	t.Run("Validate_UpsertDocuments_EmptySlice", func(t *testing.T) {
		err := client.UpsertDocuments(ctx, colName, []vectordb.Document{})
		require.Error(t, err, "empty document slice must return an error")
		var ve *vectordb.VDBError
		if errors.As(err, &ve) {
			assert.Equal(t, vectordb.ErrCodeEmptyDocumentList, ve.Code)
		}
	})

	t.Run("Validate_UpsertDocuments_MissingID", func(t *testing.T) {
		err := client.UpsertDocuments(ctx, colName, []vectordb.Document{
			{ID: "", Vector: makeVec(0), Content: "no id"},
		})
		require.Error(t, err, "document with empty ID must return an error")
		var ve *vectordb.VDBError
		if errors.As(err, &ve) {
			assert.Equal(t, vectordb.ErrCodeInvalidDocumentID, ve.Code)
		}
	})

	t.Run("Validate_UpsertDocuments_MissingVector", func(t *testing.T) {
		err := client.UpsertDocuments(ctx, colName, []vectordb.Document{
			{ID: "bad-doc", Vector: nil, Content: "no vector"},
		})
		require.Error(t, err, "document with nil vector must return an error")
		var ve *vectordb.VDBError
		if errors.As(err, &ve) {
			assert.Equal(t, vectordb.ErrCodeInvalidVector, ve.Code)
		}
	})

	t.Run("Validate_VectorSearch_TopKZero", func(t *testing.T) {
		_, err := client.VectorSearch(ctx, vectordb.SearchRequest{
			CollectionName: colName,
			QueryVector:    makeVec(0),
			TopK:           0,
		})
		require.Error(t, err, "topK=0 must return an error")
		var ve *vectordb.VDBError
		if errors.As(err, &ve) {
			assert.Equal(t, vectordb.ErrCodeInvalidTopK, ve.Code)
		}
	})

	t.Run("Validate_VectorSearch_EmptyQueryVector", func(t *testing.T) {
		_, err := client.VectorSearch(ctx, vectordb.SearchRequest{
			CollectionName: colName,
			QueryVector:    nil,
			TopK:           3,
		})
		require.Error(t, err, "nil query vector must return an error")
		var ve *vectordb.VDBError
		if errors.As(err, &ve) {
			assert.Equal(t, vectordb.ErrCodeInvalidQueryVector, ve.Code)
		}
	})

	t.Run("Validate_CreateCollection_ZeroDimensions", func(t *testing.T) {
		err := client.CreateCollection(ctx, vectordb.CollectionConfig{
			Name:       colName + "_baddim",
			Dimensions: 0,
		})
		require.Error(t, err, "dimensions=0 must return an error")
		var ve *vectordb.VDBError
		if errors.As(err, &ve) {
			assert.Equal(t, vectordb.ErrCodeInvalidDimensions, ve.Code)
		}
	})

	t.Run("Validate_CreateCollection_InvalidMetric", func(t *testing.T) {
		err := client.CreateCollection(ctx, vectordb.CollectionConfig{
			Name:           colName + "_badmetric",
			Dimensions:     testDimensions,
			DistanceMetric: "manhattan",
		})
		require.Error(t, err, "unsupported metric must return an error")
		var ve *vectordb.VDBError
		if errors.As(err, &ve) {
			assert.Equal(t, vectordb.ErrCodeInvalidMetric, ve.Code)
		}
	})

	t.Run("Validate_DeleteByFilter_EmptyFilter", func(t *testing.T) {
		// Empty filter must be rejected before hitting the DB.
		_, err := client.DeleteByFilter(ctx, colName, map[string]interface{}{})
		require.Error(t, err, "empty filter map must return an error")
		// Any VDB error is acceptable; the key constraint is that it errors.
		var ve *vectordb.VDBError
		assert.True(t, errors.As(err, &ve), "error must be a VDBError")
	})

	// ---- CreateCollection ----
	t.Run("CreateCollection", func(t *testing.T) {
		err := client.CreateCollection(ctx, vectordb.CollectionConfig{
			Name:           colName,
			Dimensions:     testDimensions,
			DistanceMetric: "cosine",
		})
		require.NoError(t, err, "CreateCollection must succeed")
	})

	// ---- CollectionExists (positive) ----
	t.Run("CollectionExists_True", func(t *testing.T) {
		exists, err := client.CollectionExists(ctx, colName)
		assert.NoError(t, err)
		assert.True(t, exists, "collection should exist after creation")
	})

	// ---- ListCollections ----
	t.Run("ListCollections", func(t *testing.T) {
		cols, err := client.ListCollections(ctx)
		assert.NoError(t, err)
		assert.True(t, listContains(cols, colName),
			"expected collection %q in list %v", colName, cols)
	})

	// ---- UpsertDocuments ----
	// 5 documents spread across the 4-D unit sphere at 45° intervals.
	testDocs := []vectordb.Document{
		{
			ID:      "doc-1",
			Vector:  makeVec(0),
			Content: "The quick brown fox jumps over the lazy dog",
			Payload: map[string]interface{}{"category": "animals", "rank": 1},
		},
		{
			ID:      "doc-2",
			Vector:  makeVec(45),
			Content: "A lazy dog sleeps all day in the sun",
			Payload: map[string]interface{}{"category": "animals", "rank": 2},
		},
		{
			ID:      "doc-3",
			Vector:  makeVec(90),
			Content: "Machine learning is transforming every industry",
			Payload: map[string]interface{}{"category": "tech", "rank": 3},
		},
		{
			ID:      "doc-4",
			Vector:  makeVec(135),
			Content: "Deep neural networks achieve state-of-the-art results",
			Payload: map[string]interface{}{"category": "tech", "rank": 4},
		},
		{
			ID:      "doc-5",
			Vector:  makeVec(180),
			Content: "Flogo is a lightweight flow-based programming engine",
			Payload: map[string]interface{}{"category": "flogo", "rank": 5},
		},
	}

	t.Run("UpsertDocuments", func(t *testing.T) {
		err := client.UpsertDocuments(ctx, colName, testDocs)
		require.NoError(t, err, "UpsertDocuments must succeed")
		// Give the DB time to index
		time.Sleep(600 * time.Millisecond)
	})

	// ---- GetDocument ----
	t.Run("GetDocument", func(t *testing.T) {
		doc, err := client.GetDocument(ctx, colName, "doc-1")
		assert.NoError(t, err)
		require.NotNil(t, doc, "GetDocument should return a document")
		assert.Equal(t, "doc-1", doc.ID)
		// Content may vary by provider; at minimum it should be non-empty
		assert.NotEmpty(t, doc.Content, "document content should be populated")
	})

	// ---- GetDocument_NotFound ----
	t.Run("GetDocument_NotFound", func(t *testing.T) {
		_, err := client.GetDocument(ctx, colName, "nonexistent-id-xyz")
		assert.Error(t, err, "GetDocument for missing ID must return an error")
	})

	// ---- Payload_RoundTrip ----
	// Verify that arbitrary payload keys survive the JSON encode/decode cycle
	// through the _metadata field, so callers can rely on payload retrieval.
	t.Run("Payload_RoundTrip", func(t *testing.T) {
		doc, err := client.GetDocument(ctx, colName, "doc-2")
		require.NoError(t, err)
		require.NotNil(t, doc)
		require.NotNil(t, doc.Payload, "payload must not be nil after round-trip")
		assert.Equal(t, "animals", doc.Payload["category"],
			"string payload field must survive JSON round-trip")
		// Numeric values are decoded as float64 by json.Unmarshal
		if rank, ok := doc.Payload["rank"]; ok {
			assert.EqualValues(t, 2, rank, "numeric payload field must round-trip correctly")
		}
	})

	// ---- CountDocuments ----
	t.Run("CountDocuments", func(t *testing.T) {
		count, err := client.CountDocuments(ctx, colName, nil)
		assert.NoError(t, err)
		assert.EqualValues(t, 5, count, "count must equal 5 after 5 upserts")
	})

	// ---- CountDocuments_WithFilter ----
	// Count with a payload field filter — exercises the filter-expression path
	// independently of search and delete. Weaviate returns ErrCodeNotImplemented.
	t.Run("CountDocuments_WithFilter", func(t *testing.T) {
		if !opts.countFiltersSupported {
			t.Skip("CountDocuments with filters not supported by this provider")
		}
		count, err := client.CountDocuments(ctx, colName, map[string]interface{}{"category": "tech"})
		require.NoError(t, err)
		assert.EqualValues(t, 2, count,
			"exactly 2 docs have category=tech (doc-3 and doc-4)")
	})

	// ---- ScrollDocuments ----
	t.Run("ScrollDocuments", func(t *testing.T) {
		result, err := client.ScrollDocuments(ctx, vectordb.ScrollRequest{
			CollectionName: colName,
			Limit:          10,
		})
		assert.NoError(t, err)
		require.NotNil(t, result)
		assert.Len(t, result.Documents, 5, "scroll should return all 5 documents")
	})

	// ---- ScrollDocuments_Pagination ----
	t.Run("ScrollDocuments_Pagination", func(t *testing.T) {
		page1, err := client.ScrollDocuments(ctx, vectordb.ScrollRequest{
			CollectionName: colName,
			Limit:          3,
		})
		assert.NoError(t, err)
		require.NotNil(t, page1)
		assert.Len(t, page1.Documents, 3, "first page should have 3 docs")

		// fetch second page using offset
		if page1.NextOffset != "" {
			page2, err := client.ScrollDocuments(ctx, vectordb.ScrollRequest{
				CollectionName: colName,
				Limit:          3,
				Offset:         page1.NextOffset,
			})
			assert.NoError(t, err)
			require.NotNil(t, page2)
			assert.True(t, len(page2.Documents) >= 1, "second page should have at least 1 doc")
		}
	})

	// ---- ScrollDocuments_ExhaustedNextOffset ----
	// When the result set is smaller than the limit, NextOffset must be "".
	t.Run("ScrollDocuments_ExhaustedNextOffset", func(t *testing.T) {
		result, err := client.ScrollDocuments(ctx, vectordb.ScrollRequest{
			CollectionName: colName,
			Limit:          100, // larger than total docs
		})
		assert.NoError(t, err)
		require.NotNil(t, result)
		assert.Empty(t, result.NextOffset,
			"NextOffset must be empty string when all documents fit in one page")
	})

	// ---- ScrollDocuments_WithFilter ----
	// Scroll with a payload field filter — only documents matching the filter
	// should be returned (category=animals: doc-1 and doc-2).
	t.Run("ScrollDocuments_WithFilter", func(t *testing.T) {
		result, err := client.ScrollDocuments(ctx, vectordb.ScrollRequest{
			CollectionName: colName,
			Limit:          10,
			Filters:        map[string]interface{}{"category": "animals"},
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Len(t, result.Documents, 2,
			"filter category=animals should return exactly 2 documents (doc-1, doc-2)")
		for _, doc := range result.Documents {
			assert.True(t,
				doc.ID == "doc-1" || doc.ID == "doc-2",
				"only animal-category docs should appear, got %q", doc.ID)
		}
	})

	// ---- VectorSearch_TopK ----
	t.Run("VectorSearch_TopK", func(t *testing.T) {
		results, err := client.VectorSearch(ctx, vectordb.SearchRequest{
			CollectionName: colName,
			QueryVector:    makeVec(5), // very close to doc-1 (angle=0)
			TopK:           3,
		})
		assert.NoError(t, err)
		assert.Len(t, results, 3, "TopK=3 should return exactly 3 results")

		// doc-1 (angle=0) must be the closest to a query at angle=5
		if len(results) > 0 {
			assert.Equal(t, "doc-1", results[0].ID,
				"doc-1 should be the nearest neighbour for angle=5 query")
		}

		// All results must have a score
		for _, r := range results {
			assert.NotEmpty(t, r.ID)
		}
	})

	// ---- VectorSearch_ScoreThreshold ----
	t.Run("VectorSearch_ScoreThreshold", func(t *testing.T) {
		// Query identical to doc-3 (angle=90) with a high threshold.
		// Only doc-3 (score ≈ 1.0) should pass a 0.9 threshold.
		results, err := client.VectorSearch(ctx, vectordb.SearchRequest{
			CollectionName: colName,
			QueryVector:    makeVec(90),
			TopK:           5,
			ScoreThreshold: 0.9,
		})
		assert.NoError(t, err)
		// Expect at least 1 and at most 2 results (neighbours at 45° drop well below 0.9)
		if !opts.scoresAreDistance {
			assert.True(t, len(results) >= 1,
				"at least 1 result should exceed cosine similarity 0.9")
			assert.True(t, len(results) <= 2,
				"at most 2 results expected above 0.9 cosine similarity")
		}
	})

	// ---- VectorSearch_TopK1 ----
	t.Run("VectorSearch_TopK1", func(t *testing.T) {
		results, err := client.VectorSearch(ctx, vectordb.SearchRequest{
			CollectionName: colName,
			QueryVector:    makeVec(180), // identical to doc-5
			TopK:           1,
		})
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		if len(results) == 1 {
			assert.Equal(t, "doc-5", results[0].ID,
				"doc-5 must be the nearest neighbour for angle=180 query")
		}
	})

	// ---- VectorSearch_HighThreshold_NoResults (corner case) ----
	t.Run("VectorSearch_HighThreshold_NoResults", func(t *testing.T) {
		if opts.scoresAreDistance {
			t.Skip("score-threshold not applicable for distance-mode providers")
		}
		// A query between two stored angles with similarity threshold at 0.9999
		// should return zero results — no stored vector is that close to an
		// in-between query angle.
		results, err := client.VectorSearch(ctx, vectordb.SearchRequest{
			CollectionName: colName,
			QueryVector:    makeVec(22), // between doc-1 (0°) and doc-2 (45°)
			TopK:           5,
			ScoreThreshold: 0.9999,
		})
		assert.NoError(t, err, "high-threshold query must not error")
		assert.Empty(t, results, "no document should match a similarity threshold of 0.9999 for an off-angle query")
	})

	// ---- VectorSearch_WithFilter ----
	// Filter on a payload field: only documents matching category=tech should
	// be returned. Exercises the filter-expression path in each provider's
	// VectorSearch implementation (query near angle=90 which aligns with tech docs).
	t.Run("VectorSearch_WithFilter", func(t *testing.T) {
		results, err := client.VectorSearch(ctx, vectordb.SearchRequest{
			CollectionName: colName,
			QueryVector:    makeVec(90), // near doc-3 (90°) and doc-4 (135°) — both category=tech
			TopK:           5,
			Filters:        map[string]interface{}{"category": "tech"},
		})
		require.NoError(t, err, "VectorSearch with filter must not error")
		require.NotEmpty(t, results, "at least 1 tech doc must match")
		for _, r := range results {
			assert.True(t, r.ID == "doc-3" || r.ID == "doc-4",
				"filter category=tech must exclude non-tech docs, got %q", r.ID)
		}
	})

	// ---- HybridSearch ----
	t.Run("HybridSearch", func(t *testing.T) {
		results, err := client.HybridSearch(ctx, vectordb.HybridSearchRequest{
			CollectionName: colName,
			QueryVector:    makeVec(90), // near doc-3 and doc-4
			QueryText:      "neural networks",
			TopK:           3,
		})
		assert.NoError(t, err)
		assert.True(t, len(results) >= 1, "HybridSearch must return at least 1 result")
	})

	// ---- HybridSearch_AlphaVectorOnly ----
	// Alpha=1.0 means 100% vector weight — exercises the alpha parameter path
	// and verifies that providers don't mis-handle the boundary value.
	t.Run("HybridSearch_AlphaVectorOnly", func(t *testing.T) {
		results, err := client.HybridSearch(ctx, vectordb.HybridSearchRequest{
			CollectionName: colName,
			QueryVector:    makeVec(0), // identical to doc-1
			QueryText:      "fox dog",
			TopK:           3,
			Alpha:          1.0, // pure vector search
		})
		require.NoError(t, err, "HybridSearch with alpha=1.0 must not error")
		require.NotEmpty(t, results, "alpha=1.0 must return results")
		// doc-1 (angle=0) must be closest for a query at angle=0
		assert.Equal(t, "doc-1", results[0].ID,
			"doc-1 must be top result for alpha=1.0 query at angle=0")
	})

	// ---- UpsertDocuments_Update ----
	t.Run("UpsertDocuments_Update", func(t *testing.T) {
		// Re-upsert doc-1 with updated payload — should overwrite, not duplicate.
		updated := []vectordb.Document{
			{
				ID:      "doc-1",
				Vector:  makeVec(0),
				Content: "UPDATED: The quick brown fox jumps over the lazy dog",
				Payload: map[string]interface{}{"category": "animals", "rank": 1, "updated": true},
			},
		}
		err := client.UpsertDocuments(ctx, colName, updated)
		assert.NoError(t, err, "upsert update must succeed")
		time.Sleep(400 * time.Millisecond)

		// Count must still be 5 (not 6)
		count, err := client.CountDocuments(ctx, colName, nil)
		assert.NoError(t, err)
		assert.EqualValues(t, 5, count, "count must remain 5 after update upsert")
	})

	// ---- DeleteDocuments ----
	t.Run("DeleteDocuments", func(t *testing.T) {
		err := client.DeleteDocuments(ctx, colName, []string{"doc-5"})
		assert.NoError(t, err, "DeleteDocuments must succeed")
		time.Sleep(400 * time.Millisecond)

		count, err := client.CountDocuments(ctx, colName, nil)
		assert.NoError(t, err)
		assert.EqualValues(t, 4, count, "count must be 4 after deleting doc-5")
	})

	// ---- DeleteDocuments_AlreadyDeleted (idempotent) ----
	t.Run("DeleteDocuments_Idempotent", func(t *testing.T) {
		// Deleting an already-deleted or non-existent ID must not error
		err := client.DeleteDocuments(ctx, colName, []string{"doc-5"})
		assert.NoError(t, err, "deleting a non-existent doc must be idempotent")
	})

	// ---- DeleteDocuments_MultipleIDs ----
	// Delete doc-3 and doc-4 in a single call — exercises the multi-ID code
	// path (OR filter for Weaviate/Qdrant, _id in [...] for Milvus, where
	// clause for Chroma) and verifies the count drops atomically.
	t.Run("DeleteDocuments_MultipleIDs", func(t *testing.T) {
		err := client.DeleteDocuments(ctx, colName, []string{"doc-3", "doc-4"})
		assert.NoError(t, err, "multi-ID delete must succeed")
		time.Sleep(400 * time.Millisecond)

		count, err := client.CountDocuments(ctx, colName, nil)
		assert.NoError(t, err)
		assert.EqualValues(t, 2, count,
			"count must be 2 after deleting doc-3 and doc-4 (doc-5 was already deleted)")
	})

	// ---- DeleteByFilter ----
	t.Run("DeleteByFilter", func(t *testing.T) {
		if opts.skipDeleteByFilter {
			t.Skip("DeleteByFilter not implemented for this provider")
		}
		_, err := client.DeleteByFilter(ctx, colName, map[string]interface{}{"category": "flogo"})
		assert.NoError(t, err)
	})

	// ---- VectorSearch_AfterDelete ----
	t.Run("VectorSearch_AfterDelete", func(t *testing.T) {
		results, err := client.VectorSearch(ctx, vectordb.SearchRequest{
			CollectionName: colName,
			QueryVector:    makeVec(180), // was doc-5
			TopK:           5,
		})
		assert.NoError(t, err)
		// doc-5 must not appear in results
		for _, r := range results {
			assert.NotEqual(t, "doc-5", r.ID, "doc-5 was deleted, must not appear in results")
		}
	})

	// ---- CreateCollection_DuplicateError ----
	t.Run("CreateCollection_DuplicateError", func(t *testing.T) {
		err := client.CreateCollection(ctx, vectordb.CollectionConfig{
			Name:       colName,
			Dimensions: testDimensions,
		})
		assert.Error(t, err, "creating a duplicate collection must return an error")
	})

	// ---- DeleteCollection ----
	t.Run("DeleteCollection", func(t *testing.T) {
		err := client.DeleteCollection(ctx, colName)
		assert.NoError(t, err, "DeleteCollection must succeed")
	})

	// ---- CollectionExists (negative — after delete) ----
	t.Run("CollectionExists_False", func(t *testing.T) {
		exists, err := client.CollectionExists(ctx, colName)
		assert.NoError(t, err)
		assert.False(t, exists, "collection should not exist after deletion")
	})

	// ---- Post-delete negative cases ----

	t.Run("VectorSearch_NonExistentCollection", func(t *testing.T) {
		_, err := client.VectorSearch(ctx, vectordb.SearchRequest{
			CollectionName: colName, // already deleted above
			QueryVector:    makeVec(0),
			TopK:           3,
		})
		assert.Error(t, err, "searching a deleted collection must return an error")
	})

	t.Run("GetDocument_NonExistentCollection", func(t *testing.T) {
		_, err := client.GetDocument(ctx, colName, "any-id")
		assert.Error(t, err, "reading from a deleted collection must return an error")
	})

	t.Run("DeleteCollection_Idempotent", func(t *testing.T) {
		// Providers may return an error or silently succeed — we only care that
		// the call does not panic or block indefinitely.
		_ = client.DeleteCollection(ctx, colName)
	})
}

// ---------------------------------------------------------------------------
// Provider-specific test entry points
// ---------------------------------------------------------------------------

func TestQdrant_Integration(t *testing.T) {
	runProviderSuite(t, vectordb.ConnectionConfig{
		DBType:         "qdrant",
		Host:           "localhost",
		Port:           6333,
		GRPCPort:       6334,
		TimeoutSeconds: 30,
	}, suiteOptions{
		skipDeleteByFilter:    false, // Qdrant supports filter-based delete
		scoresAreDistance:     false, // Qdrant reports cosine similarity (higher = better)
		countFiltersSupported: true,
	})
}

func TestWeaviate_Integration(t *testing.T) {
	runProviderSuite(t, vectordb.ConnectionConfig{
		DBType:         "weaviate",
		Host:           "localhost",
		Port:           18080,
		Scheme:         "http",
		TimeoutSeconds: 30,
	}, suiteOptions{
		skipDeleteByFilter:    false, // Weaviate DeleteByFilter now implemented
		scoresAreDistance:     false, // Weaviate returns certainty (similarity-like)
		countFiltersSupported: false, // Weaviate CountDocuments with filters not yet implemented
	})
}

func TestChroma_Integration(t *testing.T) {
	runProviderSuite(t, vectordb.ConnectionConfig{
		DBType:         "chroma",
		Host:           "localhost",
		Port:           18000,
		Scheme:         "http",
		TimeoutSeconds: 30,
	}, suiteOptions{
		skipDeleteByFilter:    false, // Chroma supports filter delete via Where clause
		scoresAreDistance:     false, // score = 1 - cosine_distance (higher = better)
		countFiltersSupported: true,
	})
}

// TestMilvus_Integration runs the full provider test suite against a local Milvus instance.
//
// Start Milvus with:
//
//	docker run -d --name vdb-test-milvus -p 19530:19530 -p 9091:9091 \
//	    milvusdb/milvus:v2.4.0 milvus run standalone
func TestMilvus_Integration(t *testing.T) {
	runProviderSuite(t, vectordb.ConnectionConfig{
		DBType:         "milvus",
		Host:           "localhost",
		Port:           19530,
		TimeoutSeconds: 30,
	}, suiteOptions{
		skipDeleteByFilter:    false, // Milvus supports filter-based delete via expressions
		scoresAreDistance:     false, // Milvus COSINE metric returns similarity (higher = better)
		countFiltersSupported: true,
	})
}
