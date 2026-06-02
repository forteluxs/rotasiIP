// Package ratelimiter provides a simple minimum-interval rate limiter.
package ratelimiter

import (
	"sync"
	"time"
)

// Limiter enforces a minimum interval between calls to Wait.
// Safe for concurrent use.
type Limiter struct {
	minInterval time.Duration
	last        time.Time
	mu          sync.Mutex
	disabled    bool
}

// NewFromRPS creates a Limiter allowing at most rps requests per second.
// If rps <= 0 the limiter is disabled (Wait is a no-op).
func NewFromRPS(rps float64) *Limiter {
	if rps <= 0 {
		return &Limiter{disabled: true}
	}
	return &Limiter{minInterval: time.Duration(float64(time.Second) / rps)}
}

// NewFromDelay creates a Limiter that enforces a fixed delay between calls.
// If d <= 0 the limiter is disabled.
func NewFromDelay(d time.Duration) *Limiter {
	if d <= 0 {
		return &Limiter{disabled: true}
	}
	return &Limiter{minInterval: d}
}

// Wait blocks until the minimum interval since the last call has elapsed.
func (l *Limiter) Wait() {
	if l.disabled {
		return
	}

	l.mu.Lock()

	if !l.last.IsZero() {
		if wait := l.minInterval - time.Since(l.last); wait > 0 {
			l.mu.Unlock()
			time.Sleep(wait)
			l.mu.Lock()
		}
	}

	l.last = time.Now()
	l.mu.Unlock()
}
