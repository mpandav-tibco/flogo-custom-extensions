package vectordb

import (
	"errors"
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
	cfg := ConnectionConfig{Host: "myhost", Port: 9999}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Equal(t, 9999, cfg.Port, "explicit port must be preserved")
}

func TestValidateConnectionConfig_DefaultTimeoutSecondsApplied(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost"}
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

func TestValidateConnectionConfig_DefaultMaxRetriesApplied(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost"}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Greater(t, cfg.MaxRetries, 0, "MaxRetries default must be > 0")
}

func TestValidateConnectionConfig_DefaultRetryBackoffMsApplied(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost"}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Greater(t, cfg.RetryBackoffMs, 0, "RetryBackoffMs default must be > 0")
}

func TestValidateConnectionConfig_ExplicitMaxRetriesPreserved(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost", MaxRetries: 5}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Equal(t, 5, cfg.MaxRetries, "explicit MaxRetries must be preserved")
}

func TestValidateConnectionConfig_ExplicitRetryBackoffMsPreserved(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost", RetryBackoffMs: 1000}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Equal(t, 1000, cfg.RetryBackoffMs, "explicit RetryBackoffMs must be preserved")
}

func TestValidateConnectionConfig_PortOutOfRange(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost", Port: 99999}
	err := validateConnectionConfig(&cfg)
	require.Error(t, err)
	vErr, ok := err.(*VDBError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeInvalidPort, vErr.Code)
}

func TestConnectionConfig_String_RedactsSensitiveFields(t *testing.T) {
	cfg := ConnectionConfig{
		Host:      "db.example.com",
		APIKey:    "secret-api-key",
		Password:  "hunter2",
		ClientKey: "-----BEGIN EC PRIVATE KEY-----",
	}
	s := cfg.String()
	assert.NotContains(t, s, "secret-api-key", "APIKey must be redacted")
	assert.NotContains(t, s, "hunter2", "Password must be redacted")
	assert.NotContains(t, s, "BEGIN EC PRIVATE KEY", "ClientKey must be redacted")
	assert.Contains(t, s, "db.example.com", "Host must remain visible")
	assert.Contains(t, s, "[redacted]", "redaction marker must appear")
}

// ---------------------------------------------------------------------------
// milvusEscapeLike — LIKE wildcard escaping
// ---------------------------------------------------------------------------

func TestMilvusEscapeLike_UnderscoreEscaped(t *testing.T) {
	// A common field name like "price_usd" must escape _ so it is not treated
	// as a single-char wildcard in the LIKE pattern.
	assert.Equal(t, `price\_usd`, milvusEscapeLike("price_usd"))
}

func TestMilvusEscapeLike_PercentEscaped(t *testing.T) {
	assert.Equal(t, `50\% off`, milvusEscapeLike("50% off"))
}

func TestMilvusEscapeLike_NoSpecialChars(t *testing.T) {
	assert.Equal(t, "plainvalue", milvusEscapeLike("plainvalue"))
}

func TestBuildMilvusExpr_NonSchemaStringWithUnderscore_EscapedInLike(t *testing.T) {
	// Field name "price_usd" → key part in LIKE must be "price\_usd"
	expr := buildMilvusExpr(map[string]interface{}{"price_usd": "hello_world"})
	assert.Contains(t, expr, "_metadata like")
	assert.Contains(t, expr, `price\_usd`)
	assert.Contains(t, expr, `hello\_world`)
}

func TestBuildMilvusExpr_NonSchemaStringWithPercent_EscapedInLike(t *testing.T) {
	// Value "50% off" → value part in LIKE must escape %
	expr := buildMilvusExpr(map[string]interface{}{"discount": "50% off"})
	assert.Contains(t, expr, "_metadata like")
	assert.Contains(t, expr, `50\% off`)
	// Must not contain a bare % wildcard for the value portion
	assert.NotContains(t, expr, `"50% off"`)
}
