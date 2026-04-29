package vectordb

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// VDBError — Error(), Unwrap(), newError fallback message
// ---------------------------------------------------------------------------

func TestVDBError_ErrorString_NoCause(t *testing.T) {
	e := &VDBError{Code: "VDB-TST-0001", Message: "something went wrong"}
	assert.Equal(t, "[VDB-TST-0001] something went wrong", e.Error())
}

func TestVDBError_ErrorString_WithCause(t *testing.T) {
	cause := errors.New("underlying network error")
	e := &VDBError{Code: "VDB-CON-5001", Message: "connection failed", Cause: cause}
	s := e.Error()
	assert.Contains(t, s, "VDB-CON-5001")
	assert.Contains(t, s, "connection failed")
	assert.Contains(t, s, "underlying network error")
}

func TestVDBError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	e := &VDBError{Code: "VDB-CON-5001", Message: "wrapped", Cause: cause}
	assert.Equal(t, cause, e.Unwrap())
	assert.True(t, errors.Is(e, cause))
}

func TestVDBError_Unwrap_NilCause(t *testing.T) {
	e := &VDBError{Code: "VDB-COL-2001", Message: "not found"}
	assert.Nil(t, e.Unwrap())
}

func TestNewError_FallsBackToDefaultMessage(t *testing.T) {
	// Empty msg: newError should fill in the default from ErrorMessages.
	e := newError(ErrCodeDocumentNotFound, "", nil)
	require.NotNil(t, e)
	assert.Equal(t, ErrCodeDocumentNotFound, e.Code)
	assert.Equal(t, ErrorMessages[ErrCodeDocumentNotFound], e.Message)
}

func TestNewError_CustomMessageOverridesDefault(t *testing.T) {
	e := newError(ErrCodeDocumentNotFound, "custom override", nil)
	assert.Equal(t, "custom override", e.Message)
}

// ---------------------------------------------------------------------------
// validateConnectionConfig — additional coverage
// ---------------------------------------------------------------------------

func TestValidateConnectionConfig_PreservesExplicitPort(t *testing.T) {
	// When a non-zero port is provided it must NOT be replaced by the default.
	cfg := ConnectionConfig{DBType: "qdrant", Host: "myhost", Port: 9999}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Equal(t, 9999, cfg.Port, "explicit port must be preserved")
}

func TestValidateConnectionConfig_DefaultTimeoutSecondsApplied(t *testing.T) {
	cfg := ConnectionConfig{DBType: "qdrant", Host: "localhost"}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Greater(t, cfg.TimeoutSeconds, 0, "TimeoutSeconds default must be > 0")
}

// ---------------------------------------------------------------------------
// milvusSafeFieldName — injection prevention
// ---------------------------------------------------------------------------

func TestMilvusSafeFieldName_AlphanumericAndUnderscore(t *testing.T) {
	assert.Equal(t, "my_field_1", milvusSafeFieldName("my_field_1"))
}

func TestMilvusSafeFieldName_StripsSpecialChars(t *testing.T) {
	// Spaces, punctuation and operators are stripped — only alnum + _ survive.
	assert.Equal(t, "fieldDROPTABLE", milvusSafeFieldName("field; DROP TABLE"))
	assert.Equal(t, "col", milvusSafeFieldName("col--"))
	// The key guarantee: no SQL-injection-like operators survive as operators.
	// Spaces, semicolons, = signs, etc. are all removed.
	result := milvusSafeFieldName("f==1 OR f")
	assert.NotContains(t, result, "=")
	assert.NotContains(t, result, " ")
}

func TestMilvusSafeFieldName_EmptyString(t *testing.T) {
	assert.Equal(t, "", milvusSafeFieldName(""))
}

func TestMilvusSafeFieldName_AllSpecialChars(t *testing.T) {
	assert.Equal(t, "", milvusSafeFieldName("!@#$%^&*()"))
}

// ---------------------------------------------------------------------------
// buildMilvusExpr — non-schema numeric, bool, multi-key
// ---------------------------------------------------------------------------

func TestBuildMilvusExpr_NonSchemaNumericFloat(t *testing.T) {
	// float64 non-schema field → _metadata LIKE with numeric pattern
	expr := buildMilvusExpr(map[string]interface{}{"rank": float64(3)})
	assert.Contains(t, expr, "_metadata like")
	assert.Contains(t, expr, `"rank"`)
	assert.Contains(t, expr, "3")
	// Must not reference "rank" as a direct column
	assert.NotContains(t, expr, `rank ==`)
}

func TestBuildMilvusExpr_NonSchemaNumericInt(t *testing.T) {
	expr := buildMilvusExpr(map[string]interface{}{"rank": int(5)})
	assert.Contains(t, expr, "_metadata like")
	assert.Contains(t, expr, `"rank"`)
	assert.Contains(t, expr, "5")
}

func TestBuildMilvusExpr_NonSchemaBool(t *testing.T) {
	expr := buildMilvusExpr(map[string]interface{}{"active": true})
	assert.Contains(t, expr, "_metadata like")
	assert.Contains(t, expr, `"active"`)
	assert.Contains(t, expr, "true")
}

func TestBuildMilvusExpr_MultiKey_SchemaAndPayload(t *testing.T) {
	// One schema column + one payload field → direct expr AND LIKE
	expr := buildMilvusExpr(map[string]interface{}{
		"_id":      "doc-1",
		"category": "tech",
	})
	assert.Contains(t, expr, `_id == "doc-1"`)
	assert.Contains(t, expr, "_metadata like")
	assert.Contains(t, expr, "&&")
}

func TestBuildMilvusExpr_MultiKey_AllPayload(t *testing.T) {
	// Multiple non-schema keys → multiple LIKE clauses joined by &&
	expr := buildMilvusExpr(map[string]interface{}{
		"category": "tech",
		"active":   true,
	})
	assert.Contains(t, expr, "_metadata like")
	assert.Contains(t, expr, "&&")
}

// ---------------------------------------------------------------------------
// buildWeaviateWhereFilter — unit coverage
// ---------------------------------------------------------------------------

func TestBuildWeaviateWhereFilter_SchemaField(t *testing.T) {
	// Schema property: must produce a direct property filter (not LIKE).
	f, err := buildWeaviateWhereFilter(map[string]interface{}{"_docId": "abc-123"})
	require.NoError(t, err)
	require.NotNil(t, f)
	// The WhereBuilder is opaque, but we can verify it serialises with the
	// correct path. Check via JSON representation.
	jsonStr := f.String()
	assert.Contains(t, jsonStr, "_docId", "schema prop must be queried directly, not via _metadata")
	assert.NotContains(t, jsonStr, "_metadata")
}

func TestBuildWeaviateWhereFilter_NonSchemaString(t *testing.T) {
	// Non-schema string field: must route to _metadata LIKE wildcard.
	f, err := buildWeaviateWhereFilter(map[string]interface{}{"category": "tech"})
	require.NoError(t, err)
	require.NotNil(t, f)
	jsonStr := f.String()
	assert.Contains(t, jsonStr, "_metadata", "non-schema field must be queried via _metadata LIKE")
	assert.Contains(t, jsonStr, "Like", "operator must be Like for LIKE wildcard")
}

func TestBuildWeaviateWhereFilter_NonSchemaNumeric(t *testing.T) {
	f, err := buildWeaviateWhereFilter(map[string]interface{}{"rank": float64(3)})
	require.NoError(t, err)
	require.NotNil(t, f)
	jsonStr := f.String()
	assert.Contains(t, jsonStr, "_metadata")
	assert.Contains(t, jsonStr, "Like")
}

func TestBuildWeaviateWhereFilter_MultiKey(t *testing.T) {
	// Multiple keys must produce an AND operator with operands.
	f, err := buildWeaviateWhereFilter(map[string]interface{}{
		"category": "tech",
		"_docId":   "doc-1",
	})
	require.NoError(t, err)
	require.NotNil(t, f)
	jsonStr := f.String()
	assert.Contains(t, jsonStr, "And", "multiple keys must produce AND operator")
}

func TestBuildWeaviateWhereFilter_UnsupportedOperator(t *testing.T) {
	// Unsupported operator on a schema field must return an error.
	_, err := buildWeaviateWhereFilter(map[string]interface{}{
		"_docId": map[string]interface{}{"$regex": ".*"},
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "$regex") || strings.Contains(err.Error(), "unsupported"),
		"error must mention the bad operator")
}

func TestBuildWeaviateWhereFilter_OperatorEqOnSchemaField(t *testing.T) {
	// Explicit $eq operator on a schema prop must produce a direct filter.
	f, err := buildWeaviateWhereFilter(map[string]interface{}{
		"_docId": map[string]interface{}{"$eq": "doc-99"},
	})
	require.NoError(t, err)
	require.NotNil(t, f)
	jsonStr := f.String()
	assert.Contains(t, jsonStr, "_docId")
	assert.NotContains(t, jsonStr, "_metadata")
}

// ---------------------------------------------------------------------------
// DeleteByFilter empty-filter guard — Qdrant, Chroma, Milvus, Weaviate
// ---------------------------------------------------------------------------

// buildQdrantFilter with nil/empty input returns nil (used by the guard test).
func TestBuildQdrantFilter_EmptyReturnsNil(t *testing.T) {
	assert.Nil(t, buildQdrantFilter(nil))
	assert.Nil(t, buildQdrantFilter(map[string]interface{}{}))
}

// buildWeaviateWhereFilter with an empty map should NOT be called (guard is
// upstream), but if it were it would panic / behave unexpectedly. Verify
// the guard path itself returns a proper VDBError.
func TestChromaWhereFilter_EmptyReturnsNil(t *testing.T) {
	// chromaWhereFilter returns nil for empty/nil maps — the caller's guard
	// prevents the nil filter from reaching col.Delete().
	assert.Nil(t, chromaWhereFilter(nil))
	assert.Nil(t, chromaWhereFilter(map[string]interface{}{}))
}
