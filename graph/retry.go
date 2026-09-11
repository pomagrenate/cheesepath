package graph

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// RetryPolicy configures automated retry behavior for a node.
type RetryPolicy struct {
	MaxRetries      int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	RetryIf         func(err error) bool
}

// DefaultRetryPolicy returns a robust exponential backoff policy suitable for LLMs and APIs.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:      3,
		InitialInterval: 50 * time.Millisecond,
		MaxInterval:     2 * time.Second,
		Multiplier:      2.0,
		RetryIf:         nil, // Retry all non-nil errors by default
	}
}

// shouldRetry determines whether an error should trigger a retry attempt.
func (p *RetryPolicy) shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	if p.RetryIf != nil {
		return p.RetryIf(err)
	}
	return true
}

// nextDelay calculates the backoff duration with full jitter for attempt number (0-indexed).
func (p *RetryPolicy) nextDelay(attempt int) time.Duration {
	interval := p.InitialInterval
	if p.Multiplier > 1.0 && attempt > 0 {
		mult := 1.0
		for i := 0; i < attempt; i++ {
			mult *= p.Multiplier
		}
		interval = time.Duration(float64(interval) * mult)
	}
	if p.MaxInterval > 0 && interval > p.MaxInterval {
		interval = p.MaxInterval
	}
	// Full jitter: uniformly random between [interval/2, interval]
	half := interval / 2
	if half <= 0 {
		return interval
	}
	jitter := time.Duration(rand.Int63n(int64(half)))
	return half + jitter
}

// ExecuteWithRetry executes a NodeFunc with the configured RetryPolicy.
func ExecuteWithRetry[S any](
	ctx context.Context,
	policy *RetryPolicy,
	fn NodeFunc[S],
	state S,
) (S, error) {
	if policy == nil || policy.MaxRetries <= 0 {
		return fn(ctx, state)
	}

	var lastErr error
	var zero S

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}

		res, err := fn(ctx, state)
		if err == nil {
			return res, nil
		}

		lastErr = err
		if !policy.shouldRetry(err) || attempt == policy.MaxRetries {
			break
		}

		delay := policy.nextDelay(attempt)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return zero, ctx.Err()
		}
	}

	return zero, errors.Join(errors.New("retry policy exhausted"), lastErr)
}
