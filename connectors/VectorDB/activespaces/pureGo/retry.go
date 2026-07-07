package vectordb

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// maxRetryDelay caps exponential backoff so a high MaxRetries value never
// produces minute-long sleeps between attempts.
const maxRetryDelay = 30 * time.Second

// isRetryable reports whether err represents a transient failure that is worth
// retrying.  Structured VDBErrors with deterministic codes (auth failures,
// validation errors, not-found, not-implemented, etc.) are never retried.
// Only connection-level and generic provider errors are considered transient.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var vdbErr *VDBError
	if errors.As(err, &vdbErr) {
		switch vdbErr.Code {
		// Transient — worth retrying
		case ErrCodeConnectionFailed, ErrCodeConnectionTimeout, ErrCodeProviderError:
			return true
		// Everything else is deterministic — retrying will not change the outcome
		default:
			return false
		}
	}
	// Raw SDK / network errors without a VDB code are treated as transient.
	return true
}

// withRetry calls op up to maxRetries+1 times, using exponential back-off with
// ±25% jitter and a 30-second hard cap.
//
// backoffMs is the base delay in milliseconds; the delay doubles on each
// successive attempt, is capped at maxRetryDelay, then ±25% random jitter is
// applied so that concurrent callers experiencing the same transient outage do
// not all wake at the same instant (thundering-herd prevention).
//
// The context is checked between attempts: if it is already cancelled or times
// out during a sleep the function returns ctx.Err() immediately without
// further retries.  Non-retryable VDBErrors are returned on the first failure
// without sleeping.
func withRetry(ctx context.Context, maxRetries int, backoffMs int, op func() error) error {
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err = op(); err == nil {
			return nil
		}
		if !isRetryable(err) {
			return err
		}
		if attempt < maxRetries {
			// Exponential base, capped at maxRetryDelay.
			base := time.Duration(backoffMs*(1<<uint(attempt))) * time.Millisecond
			if base > maxRetryDelay {
				base = maxRetryDelay
			}
			// ±25% jitter: random value in [-base/4, +base/4].
			quarter := int64(base / 4)
			if quarter > 0 {
				base += time.Duration(rand.Int64N(quarter*2) - quarter)
			}
			if base < 0 {
				base = 0
			}
			select {
			case <-time.After(base):
				// proceed to next attempt
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return err
}
