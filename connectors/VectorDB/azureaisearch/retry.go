package vectordb

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

const maxRetryDelay = 30 * time.Second

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var vdbErr *VDBError
	if errors.As(err, &vdbErr) {
		switch vdbErr.Code {
		case ErrCodeConnectionFailed, ErrCodeConnectionTimeout, ErrCodeProviderError:
			return true
		default:
			return false
		}
	}
	return true
}

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
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return err
}
