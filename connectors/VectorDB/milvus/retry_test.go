package vectordb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithRetry_SuccessOnFirstAttempt verifies no retry when op succeeds immediately.
func TestWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), 3, 1, func() error {
		calls++
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, calls, "should succeed on first attempt, no retries")
}

// TestWithRetry_AllAttemptsExhausted verifies that the last error is returned
// after maxRetries+1 total attempts.
func TestWithRetry_AllAttemptsExhausted(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), 2, 1, func() error {
		calls++
		return errors.New("transient failure")
	})
	assert.Error(t, err)
	assert.Equal(t, 3, calls, "should make maxRetries+1 total attempts (0, 1, 2)")
}

// TestWithRetry_SuccessOnSecondAttempt verifies op is retried after a transient failure.
func TestWithRetry_SuccessOnSecondAttempt(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), 3, 1, func() error {
		calls++
		if calls < 2 {
			return errors.New("transient")
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 2, calls, "should succeed on second attempt")
}

// TestWithRetry_NonRetryableVDBError verifies that a non-retryable VDBError is
// returned immediately without consuming any retry budget.
func TestWithRetry_NonRetryableVDBError(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), 5, 1, func() error {
		calls++
		return newError(ErrCodeAuthFailed, "invalid API key", nil)
	})
	require.Error(t, err)
	assert.Equal(t, 1, calls, "non-retryable error must not be retried")

	var ve *VDBError
	require.True(t, errors.As(err, &ve))
	assert.Equal(t, ErrCodeAuthFailed, ve.Code)
}

// TestWithRetry_RetryableVDBError verifies that ErrCodeProviderError IS retried.
func TestWithRetry_RetryableVDBError(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), 2, 1, func() error {
		calls++
		return newError(ErrCodeProviderError, "upstream 503", nil)
	})
	assert.Error(t, err)
	assert.Equal(t, 3, calls, "ErrCodeProviderError should be retried maxRetries+1 times")
}

// TestWithRetry_ContextAlreadyCancelled verifies that a pre-cancelled context
// does not prevent the first attempt but stops retries immediately.
func TestWithRetry_ContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling withRetry

	calls := 0
	err := withRetry(ctx, 3, 100, func() error {
		calls++
		return errors.New("transient")
	})
	// First attempt always runs; context is checked during the sleep before retry.
	assert.Error(t, err)
	assert.Equal(t, 1, calls, "only the first attempt should run when ctx is pre-cancelled")
}

// TestWithRetry_ContextCancelledDuringSleep verifies that context cancellation
// during the exponential back-off sleep causes an immediate exit without
// completing the full sleep duration.
func TestWithRetry_ContextCancelledDuringSleep(t *testing.T) {
	// backoffMs=200 means first sleep would be 200 ms; ctx times out in 20 ms.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	calls := 0
	start := time.Now()
	err := withRetry(ctx, 5, 200, func() error {
		calls++
		return errors.New("transient") // always fails → triggers sleep
	})
	elapsed := time.Since(start)

	assert.Error(t, err)
	// Should bail well before the first full 200 ms sleep
	assert.Less(t, elapsed, 150*time.Millisecond,
		"withRetry should not sleep full back-off when ctx is cancelled")
	assert.Equal(t, 1, calls,
		"only one attempt should run before context is cancelled during sleep")
}

// TestWithRetry_ZeroMaxRetries verifies that maxRetries=0 means a single attempt.
func TestWithRetry_ZeroMaxRetries(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), 0, 1, func() error {
		calls++
		return errors.New("fail")
	})
	assert.Error(t, err)
	assert.Equal(t, 1, calls, "maxRetries=0 means exactly one attempt")
}

// ---------------------------------------------------------------------------
// isRetryable
// ---------------------------------------------------------------------------

func TestIsRetryable_NilError(t *testing.T) {
	assert.False(t, isRetryable(nil), "nil is never retryable")
}

func TestIsRetryable_RawError(t *testing.T) {
	assert.True(t, isRetryable(errors.New("unexpected EOF")),
		"raw non-VDB errors are treated as transient")
}

func TestIsRetryable_Table(t *testing.T) {
	cases := []struct {
		code     string
		expected bool
		label    string
	}{
		{ErrCodeProviderError, true, "provider 5xx — transient"},
		{ErrCodeConnectionFailed, true, "connection failed — transient"},
		{ErrCodeConnectionTimeout, true, "connection timeout — transient"},
		// All deterministic codes must return false
		{ErrCodeAuthFailed, false, "auth failure — deterministic"},
		{ErrCodeInvalidDBType, false, "config error — deterministic"},
		{ErrCodeMissingHost, false, "missing host — deterministic"},
		{ErrCodeInvalidPort, false, "invalid port — deterministic"},
		{ErrCodeInvalidTimeout, false, "invalid timeout — deterministic"},
		{ErrCodeCollectionNotFound, false, "collection not found — deterministic"},
		{ErrCodeCollectionExists, false, "collection exists — deterministic"},
		{ErrCodeInvalidDimensions, false, "invalid dimensions — deterministic"},
		{ErrCodeInvalidMetric, false, "invalid metric — deterministic"},
		{ErrCodeDocumentNotFound, false, "doc not found — deterministic"},
		{ErrCodeInvalidVector, false, "invalid vector — deterministic"},
		{ErrCodeEmptyDocumentList, false, "empty docs — deterministic"},
		{ErrCodeInvalidDocumentID, false, "empty ID — deterministic"},
		{ErrCodeBatchTooLarge, false, "batch too large — deterministic"},
		{ErrCodeInvalidQueryVector, false, "empty query vector — deterministic"},
		{ErrCodeInvalidTopK, false, "invalid topK — deterministic"},
		{ErrCodeInvalidAlpha, false, "invalid alpha — deterministic"},
		{ErrCodeHybridNotSupported, false, "hybrid not supported — deterministic"},
		{ErrCodeClientNotFound, false, "client not found — deterministic"},
		{ErrCodeClientExists, false, "client exists — deterministic"},
		{ErrCodeNotImplemented, false, "not implemented — deterministic"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			err := newError(tc.code, "test", nil)
			assert.Equal(t, tc.expected, isRetryable(err), "isRetryable(%s)", tc.code)
		})
	}
}
