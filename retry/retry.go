// Package retry implements the exponential-backoff helper used by ingest
// clients in this module.
//
// Defaults: base 100ms, factor 2, max 5 attempts, max wait 30s. The helper
// is context-aware and never sleeps past ctx.Done().
package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// Policy controls exponential-backoff behavior. The zero value is invalid;
// use Default or build a Policy with the constructor pattern below.
type Policy struct {
	MaxAttempts int           // Total tries including the first; must be >= 1.
	Base        time.Duration // Initial backoff before retry #1.
	Factor      float64       // Multiplier per attempt; must be >= 1.
	MaxWait     time.Duration // Cap on per-iteration sleep.
	Jitter      bool          // Add up to 25% random jitter on each sleep.
}

// Default returns the policy specified by the OTel incident-layer plan.
func Default() Policy {
	return Policy{
		MaxAttempts: 5,
		Base:        100 * time.Millisecond,
		Factor:      2,
		MaxWait:     30 * time.Second,
		Jitter:      true,
	}
}

// validate ensures a Policy is well-formed.
func (p Policy) validate() error {
	if p.MaxAttempts < 1 {
		return errors.New("retry: MaxAttempts must be >= 1")
	}
	if p.Base <= 0 {
		return errors.New("retry: Base must be > 0")
	}
	if p.Factor < 1 {
		return errors.New("retry: Factor must be >= 1")
	}
	if p.MaxWait < p.Base {
		return errors.New("retry: MaxWait must be >= Base")
	}
	return nil
}

// IsRetryable signals whether an error returned by the body of Do should
// trigger another attempt. Bodies that classify themselves implement this
// interface; bodies that do not are retried by default.
type IsRetryable interface {
	error
	Retryable() bool
}

// Permanent wraps err so Do treats it as terminal. Useful for 4xx responses.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

type permanentError struct{ err error }

func (e *permanentError) Error() string   { return e.err.Error() }
func (e *permanentError) Unwrap() error   { return e.err }
func (e *permanentError) Retryable() bool { return false }

// Do runs body up to policy.MaxAttempts times, sleeping between attempts.
// It stops on success, on a non-retryable error, when ctx is cancelled, or
// when MaxAttempts is reached. The returned error wraps the last attempt.
func Do(ctx context.Context, policy Policy, body func(ctx context.Context) error) error {
	if err := policy.validate(); err != nil {
		return err
	}
	if body == nil {
		return errors.New("retry: nil body")
	}

	var last error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if last != nil {
				return fmt.Errorf("retry: ctx cancelled after %d attempts: %w (last error: %v)", attempt-1, ctxErr, last)
			}
			return ctxErr
		}

		err := body(ctx)
		if err == nil {
			return nil
		}
		last = err

		var classified IsRetryable
		if errors.As(err, &classified) && !classified.Retryable() {
			return err
		}

		if attempt == policy.MaxAttempts {
			break
		}

		wait := policy.delay(attempt)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("retry: ctx cancelled while waiting: %w (last error: %v)", ctx.Err(), last)
		case <-timer.C:
		}
	}

	return fmt.Errorf("retry: exhausted %d attempts: %w", policy.MaxAttempts, last)
}

// delay returns the backoff for the i-th attempt (1-indexed) with optional
// jitter. The result is clamped to MaxWait.
func (p Policy) delay(attempt int) time.Duration {
	d := float64(p.Base)
	for i := 1; i < attempt; i++ {
		d *= p.Factor
		if d > float64(p.MaxWait) {
			d = float64(p.MaxWait)
			break
		}
	}
	if p.Jitter {
		// Add 0..25% jitter to spread out concurrent retriers.
		j := d * 0.25 * rand.Float64()
		d += j
	}
	if d > float64(p.MaxWait) {
		d = float64(p.MaxWait)
	}
	return time.Duration(d)
}
