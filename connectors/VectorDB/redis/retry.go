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
// retrying. Structured VDBErrors with deterministic codes are never retried.
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
			base := time.Duration(backoffMs*(1<<uint(attempt))) * time.Millisecond
			if base > maxRetryDelay {
				base = maxRetryDelay
			}
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
