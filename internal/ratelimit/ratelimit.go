// Package ratelimit provides a token-bucket io.Writer that caps outbound
// throughput.  It is used to throttle shard uploads so that CerclBackup does
// not saturate a TPE/PME customer's uplink during a backup run.
//
// The bucket refills continuously at the configured rate; bursts up to the
// full bucket capacity are allowed (one "burst" = one second of quota).
package ratelimit

import (
	"io"
	"sync"
	"time"
)

// Limiter is a token-bucket rate limiter.  Zero value is unlimited.
type Limiter struct {
	mu       sync.Mutex
	tokens   float64 // current tokens (bytes)
	capacity float64 // max tokens (= 1-second quota)
	rateNs   float64 // tokens per nanosecond
	last     time.Time
}

// NewLimiter creates a Limiter that allows bytesPerSec bytes per second.
// bytesPerSec <= 0 means unlimited.
func NewLimiter(bytesPerSec int) *Limiter {
	if bytesPerSec <= 0 {
		return &Limiter{} // unlimited
	}
	cap := float64(bytesPerSec)
	return &Limiter{
		tokens:   cap,
		capacity: cap,
		rateNs:   cap / 1e9,
		last:     time.Now(),
	}
}

// Unlimited reports whether this limiter imposes no limit.
func (l *Limiter) Unlimited() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.capacity == 0
}

// Wait blocks until n tokens are available, then consumes them.
func (l *Limiter) Wait(n int) {
	if n <= 0 {
		return
	}
	l.mu.Lock()
	if l.capacity == 0 {
		l.mu.Unlock()
		return
	}
	// Refill tokens since last call.
	now := time.Now()
	elapsed := float64(now.Sub(l.last).Nanoseconds())
	l.last = now
	l.tokens += elapsed * l.rateNs
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}
	need := float64(n)
	if l.tokens >= need {
		l.tokens -= need
		l.mu.Unlock()
		return
	}
	// Not enough tokens: compute wait time, release lock, sleep, re-acquire.
	deficit := need - l.tokens
	l.tokens = 0
	waitNs := time.Duration(deficit / l.rateNs)
	l.mu.Unlock()
	time.Sleep(waitNs)
	// Stamp l.last to now so the next Wait starts from post-sleep time,
	// preventing the sleep from being double-counted as refill credit.
	l.mu.Lock()
	l.last = time.Now()
	l.mu.Unlock()
}

// Writer wraps w and rate-limits writes through l.
// If l is nil or unlimited, writes pass through directly.
type Writer struct {
	w io.Writer
	l *Limiter
}

// NewWriter returns an io.Writer that throttles writes to l's rate.
func NewWriter(w io.Writer, l *Limiter) io.Writer {
	if l == nil || l.Unlimited() {
		return w
	}
	return &Writer{w: w, l: l}
}

func (rw *Writer) Write(p []byte) (int, error) {
	rw.l.Wait(len(p))
	return rw.w.Write(p)
}
