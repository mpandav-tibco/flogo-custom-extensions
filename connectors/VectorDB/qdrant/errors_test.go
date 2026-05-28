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
		ClientKey: "-----BEGIN EC PRIVATE KEY-----",
	}
	s := cfg.String()
	assert.NotContains(t, s, "secret-api-key", "APIKey must be redacted")
	assert.NotContains(t, s, "BEGIN EC PRIVATE KEY", "ClientKey must be redacted")
	assert.Contains(t, s, "db.example.com", "Host must remain visible")
	assert.Contains(t, s, "[redacted]", "redaction marker must appear")
}

func TestValidateConnectionConfig_GRPCPortOutOfRange(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost", GRPCPort: 99999}
	err := validateConnectionConfig(&cfg)
	require.Error(t, err)
	vErr, ok := err.(*VDBError)
	require.True(t, ok)
	assert.Equal(t, ErrCodeInvalidPort, vErr.Code)
	assert.Contains(t, vErr.Message, "grpcPort")
}

func TestValidateConnectionConfig_GRPCPortDefaultApplied(t *testing.T) {
	cfg := ConnectionConfig{Host: "localhost"}
	require.NoError(t, validateConnectionConfig(&cfg))
	assert.Equal(t, 6334, cfg.GRPCPort, "GRPCPort default must be 6334")
}
