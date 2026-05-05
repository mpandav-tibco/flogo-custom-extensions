package vectordb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// normalizeDistanceMetric
// ---------------------------------------------------------------------------

func TestNormalizeDistanceMetric(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "cosine"},
		{"cosine", "cosine"},
		{"COSINE", "cosine"},
		{"Cosine", "cosine"},
		{"dot", "dot"},
		{"DOT", "dot"},
		{"euclidean", "euclidean"},
		{"EUCLIDEAN", "euclidean"},
		{"  cosine  ", "cosine"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeDistanceMetric(tc.input))
		})
	}
}

// ---------------------------------------------------------------------------
// validateCollectionConfig
// ---------------------------------------------------------------------------

func TestValidateCollectionConfig_Valid(t *testing.T) {
	cfg := CollectionConfig{Name: "test", Dimensions: 128, DistanceMetric: "cosine"}
	assert.NoError(t, validateCollectionConfig(cfg))
}

func TestValidateCollectionConfig_EmptyName(t *testing.T) {
	cfg := CollectionConfig{Name: "", Dimensions: 128}
	err := validateCollectionConfig(cfg)
	require.Error(t, err)
	var ve *VDBError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, ErrCodeInvalidCollectionName, ve.Code)
}

func TestValidateCollectionConfig_ZeroDimensions(t *testing.T) {
	cfg := CollectionConfig{Name: "test", Dimensions: 0}
	err := validateCollectionConfig(cfg)
	require.Error(t, err)
	var ve *VDBError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, ErrCodeInvalidDimensions, ve.Code)
}

func TestValidateCollectionConfig_InvalidMetric(t *testing.T) {
	cfg := CollectionConfig{Name: "test", Dimensions: 128, DistanceMetric: "manhattan"}
	err := validateCollectionConfig(cfg)
	require.Error(t, err)
	var ve *VDBError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, ErrCodeInvalidMetric, ve.Code)
}

// ---------------------------------------------------------------------------
// validateConnectionConfig (via NewClient invalid type)
// ---------------------------------------------------------------------------

func TestValidateConnectionConfig_NormalizesDBType(t *testing.T) {
	cfg := ConnectionConfig{DBType: "  QDRANT  ", Host: "localhost", Port: 6333}
	err := validateConnectionConfig(&cfg)
	assert.NoError(t, err)
	assert.Equal(t, "qdrant", cfg.DBType)
}

func TestValidateConnectionConfig_AppliesDefaultPort(t *testing.T) {
	cases := []struct {
		dbType   string
		wantPort int
	}{
		{"qdrant", 6333},
		{"weaviate", 8080},
		{"chroma", 8000},
		{"milvus", 19530},
	}
	for _, tc := range cases {
		t.Run(tc.dbType, func(t *testing.T) {
			cfg := ConnectionConfig{DBType: tc.dbType, Host: "localhost"}
			require.NoError(t, validateConnectionConfig(&cfg))
			assert.Equal(t, tc.wantPort, cfg.Port)
		})
	}
}

func TestValidateConnectionConfig_AppliesDefaultMaxRetries(t *testing.T) {
	cfg := ConnectionConfig{DBType: "qdrant", Host: "localhost"}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Equal(t, 3, cfg.MaxRetries)
}

func TestValidateConnectionConfig_MissingHost(t *testing.T) {
	cfg := ConnectionConfig{DBType: "qdrant"}
	err := validateConnectionConfig(&cfg)
	require.Error(t, err)
	var ve *VDBError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, ErrCodeMissingHost, ve.Code)
}

func TestValidateConnectionConfig_InvalidDBType(t *testing.T) {
	cfg := ConnectionConfig{DBType: "redis", Host: "localhost"}
	err := validateConnectionConfig(&cfg)
	require.Error(t, err)
	var ve *VDBError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, ErrCodeInvalidDBType, ve.Code)
}

// ---------------------------------------------------------------------------
// validateBatchSize
// ---------------------------------------------------------------------------

func TestValidateBatchSize_UnderLimit(t *testing.T) {
	assert.NoError(t, validateBatchSize(999, 1000, "Qdrant"))
	assert.NoError(t, validateBatchSize(1000, 1000, "Qdrant"))
}

func TestValidateBatchSize_OverLimit(t *testing.T) {
	err := validateBatchSize(1001, 1000, "Qdrant")
	require.Error(t, err)
	var ve *VDBError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, ErrCodeBatchTooLarge, ve.Code)
	assert.Contains(t, ve.Message, "Qdrant")
}

// ---------------------------------------------------------------------------
// weaviateClassName
// ---------------------------------------------------------------------------

func TestWeaviateClassName(t *testing.T) {
	cases := []struct{ input, want string }{
		{"", ""},
		{"products", "Products"},
		{"Products", "Products"},
		{"my_collection", "My_collection"},
		{"a", "A"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, weaviateClassName(tc.input), "input=%q", tc.input)
	}
}

// ---------------------------------------------------------------------------
// qdrantResolveID
// ---------------------------------------------------------------------------

func TestQdrantResolveID_ValidUUID(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	pt := qdrantResolveID(id)
	require.NotNil(t, pt)
	assert.Equal(t, id, pt.GetUuid())
}

func TestQdrantResolveID_NumericID(t *testing.T) {
	pt := qdrantResolveID("42")
	require.NotNil(t, pt)
	assert.Equal(t, uint64(42), pt.GetNum())
}

func TestQdrantResolveID_ArbitraryString_Deterministic(t *testing.T) {
	pt1 := qdrantResolveID("my-custom-id")
	pt2 := qdrantResolveID("my-custom-id")
	pt3 := qdrantResolveID("different-id")
	assert.Equal(t, pt1.GetUuid(), pt2.GetUuid(), "same input must produce same UUID")
	assert.NotEqual(t, pt1.GetUuid(), pt3.GetUuid(), "different inputs must produce different UUIDs")
}

// ---------------------------------------------------------------------------
// weaviateResolveUUID
// ---------------------------------------------------------------------------

func TestWeaviateResolveUUID_ValidUUID(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	result := weaviateResolveUUID(id)
	assert.Equal(t, id, string(result))
}

func TestWeaviateResolveUUID_ArbitraryString_Deterministic(t *testing.T) {
	r1 := weaviateResolveUUID("doc-abc")
	r2 := weaviateResolveUUID("doc-abc")
	r3 := weaviateResolveUUID("doc-xyz")
	assert.Equal(t, r1, r2)
	assert.NotEqual(t, r1, r3)
}

func TestWeaviateResolveUUID_NoDeadCode_NumericDerivedAsUUID(t *testing.T) {
	// Weaviate does not support raw numeric IDs — numeric strings must also
	// produce a proper UUID (not return the raw number like qdrantResolveID).
	result := weaviateResolveUUID("12345")
	// Must be a valid UUID string (36 chars with hyphens), not "12345".
	assert.NotEqual(t, "12345", string(result))
	assert.Equal(t, 36, len(string(result)))
}

// ---------------------------------------------------------------------------
// buildMilvusExpr
// ---------------------------------------------------------------------------

func TestBuildMilvusExpr_Empty(t *testing.T) {
	assert.Equal(t, "", buildMilvusExpr(nil))
	assert.Equal(t, "", buildMilvusExpr(map[string]interface{}{}))
}

func TestBuildMilvusExpr_ScalarEquality(t *testing.T) {
	// Schema field: direct equality expression.
	expr := buildMilvusExpr(map[string]interface{}{"_id": "doc-1"})
	assert.Equal(t, `_id == "doc-1"`, expr)

	// Non-schema payload field: routed to _metadata LIKE substring match.
	expr2 := buildMilvusExpr(map[string]interface{}{"status": "active"})
	assert.Equal(t, `_metadata like '%"status":"active"%'`, expr2)
}

func TestBuildMilvusExpr_StringInjectionPrevented(t *testing.T) {
	// Schema field with a double-quote injection attempt in the value.
	expr := buildMilvusExpr(map[string]interface{}{"_id": `foo" OR 1==1 OR _id == "bar`})
	// The double-quote in the value must be escaped.
	assert.Contains(t, expr, `\"`)
	assert.False(t, strings.Contains(expr, `foo" OR`), "raw injection must not appear unescaped")

	// Non-schema field: value is JSON-encoded inside the LIKE pattern,
	// so injection attempts are neutralised by json.Marshal quoting.
	expr2 := buildMilvusExpr(map[string]interface{}{"name": `foo" OR 1==1`})
	assert.Contains(t, expr2, `_metadata like`)
	assert.False(t, strings.Contains(expr2, `foo" OR`), "raw injection must not appear unescaped in LIKE pattern")
}

func TestBuildMilvusExpr_OperatorMap(t *testing.T) {
	// Operator maps on schema fields use direct expression (score is non-schema,
	// but operator maps fall through to direct expression regardless).
	expr := buildMilvusExpr(map[string]interface{}{"score": map[string]interface{}{"$gt": 0.5}})
	assert.Equal(t, "score > 0.5", expr)
}

func TestBuildMilvusExpr_InOperator(t *testing.T) {
	// $in operator also falls through to direct expression for non-schema fields.
	expr := buildMilvusExpr(map[string]interface{}{"tag": map[string]interface{}{"$in": []interface{}{"a", "b"}}})
	assert.Contains(t, expr, `tag in ["a", "b"]`)
}

// ---------------------------------------------------------------------------
// ConnectionConfig.String() — credential redaction
// ---------------------------------------------------------------------------

func TestConnectionConfigString_RedactsCredentials(t *testing.T) {
	cfg := ConnectionConfig{
		DBType:    "milvus",
		Host:      "db.example.com",
		Port:      19530,
		APIKey:    "super-secret-key",
		Password:  "hunter2",
		ClientKey: "-----BEGIN PRIVATE KEY-----\nMIIEvQ...",
	}
	s := cfg.String()
	assert.NotContains(t, s, "super-secret-key")
	assert.NotContains(t, s, "hunter2")
	assert.NotContains(t, s, "MIIEvQ")
	assert.Contains(t, s, "[redacted]")
	assert.Contains(t, s, "milvus")
	assert.Contains(t, s, "db.example.com")
}

func TestConnectionConfigString_EmptyCredentialsNotRedacted(t *testing.T) {
	cfg := ConnectionConfig{DBType: "qdrant", Host: "localhost"}
	s := cfg.String()
	// No credentials — [redacted] should not appear, no panic.
	assert.NotContains(t, s, "[redacted]")
}

// ---------------------------------------------------------------------------
// weaviateSearchFields
// ---------------------------------------------------------------------------

func TestWeaviateSearchFields_IncludesPayloadByDefault(t *testing.T) {
	fields := weaviateSearchFields(false, "_additional { id score }")
	require.Len(t, fields, 4)
	assert.Equal(t, "_additional { id score }", fields[0].Name)
	assert.Equal(t, "content", fields[1].Name)
	assert.Equal(t, "_docId", fields[2].Name)
	assert.Equal(t, "_metadata", fields[3].Name)
}

func TestWeaviateSearchFields_SkipPayload_OnlyScoreField(t *testing.T) {
	fields := weaviateSearchFields(true, "_additional { id score }")
	require.Len(t, fields, 1)
	assert.Equal(t, "_additional { id score }", fields[0].Name)
}

func TestWeaviateSearchFields_VectorSearch_ScoreField(t *testing.T) {
	fields := weaviateSearchFields(false, "_additional { id certainty distance }")
	require.Len(t, fields, 4)
	assert.Equal(t, "_additional { id certainty distance }", fields[0].Name)
}

func TestWeaviateSearchFields_SkipPayload_VectorSearch(t *testing.T) {
	fields := weaviateSearchFields(true, "_additional { id certainty distance }")
	require.Len(t, fields, 1)
	assert.Equal(t, "_additional { id certainty distance }", fields[0].Name)
}

// ---------------------------------------------------------------------------
// buildMilvusExpr — empty filter returns empty string (DeleteByFilter guard)
// ---------------------------------------------------------------------------

func TestBuildMilvusExpr_NilFilter_ReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", buildMilvusExpr(nil))
}

func TestBuildMilvusExpr_EmptyMap_ReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", buildMilvusExpr(map[string]interface{}{}))
}
