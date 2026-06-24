package ratelimit_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/ratelimit"
)

func TestUnlimitedPassthrough(t *testing.T) {
	l := ratelimit.NewLimiter(0)
	if !l.Unlimited() {
		t.Error("NewLimiter(0) should be unlimited")
	}
	var buf bytes.Buffer
	w := ratelimit.NewWriter(&buf, l)
	data := []byte("hello world")
	w.Write(data)
	if !bytes.Equal(buf.Bytes(), data) {
		t.Error("unlimited writer should pass data through unchanged")
	}
}

func TestThrottleSlowsWrites(t *testing.T) {
	// 10 KB/s limiter; write 30 KB.
	// The bucket starts full (10 KB burst), so the first 10 KB is free.
	// The remaining 20 KB must wait ~2s — total should be >=1.8s.
	const rate = 10 * 1024
	const payload = 30 * 1024
	l := ratelimit.NewLimiter(rate)

	var buf bytes.Buffer
	w := ratelimit.NewWriter(&buf, l)

	start := time.Now()
	chunk := make([]byte, 1024)
	for i := 0; i < payload/1024; i++ {
		w.Write(chunk)
	}
	elapsed := time.Since(start)

	if elapsed < 1800*time.Millisecond {
		t.Errorf("throttled write finished in %v — expected >=1.8s for 30KB at 10KB/s (1KB burst)", elapsed)
	}
	if buf.Len() != payload {
		t.Errorf("wrote %d bytes, want %d", buf.Len(), payload)
	}
}

func TestNilLimiterPassthrough(t *testing.T) {
	var buf bytes.Buffer
	w := ratelimit.NewWriter(&buf, nil)
	w.Write([]byte("pass"))
	if buf.String() != "pass" {
		t.Error("nil limiter should pass through")
	}
}

func TestWaitZeroBytes(t *testing.T) {
	l := ratelimit.NewLimiter(1024)
	// Should not block.
	done := make(chan struct{})
	go func() { l.Wait(0); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("Wait(0) blocked")
	}
}
