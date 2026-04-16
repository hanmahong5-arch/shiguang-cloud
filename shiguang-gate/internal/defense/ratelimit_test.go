package defense

import (
	"errors"
	"testing"
	"time"
)

func TestRateLimiter_BurstThenRejects(t *testing.T) {
	rl := NewRateLimiter(1.0, 3.0, 0) // 1/sec, burst 3, no concurrent cap
	ip := "1.2.3.4"

	// 3 burst allowed
	for i := 0; i < 3; i++ {
		if err := rl.AllowConnect(ip); err != nil {
			t.Errorf("burst conn %d rejected: %v", i, err)
		}
		rl.Release(ip)
	}
	// 4th should be rejected (no refill time elapsed)
	err := rl.AllowConnect(ip)
	if !errors.Is(err, ErrRateExceeded) {
		t.Errorf("expected rate exceeded, got %v", err)
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(10.0, 2.0, 0) // 10/sec, burst 2
	ip := "1.2.3.4"

	rl.AllowConnect(ip)
	rl.AllowConnect(ip)
	rl.Release(ip)
	rl.Release(ip)
	// Bucket empty
	if err := rl.AllowConnect(ip); err == nil {
		t.Error("expected reject immediately after burst")
	}
	rl.Release(ip) // noop since it wasn't granted

	// Wait for refill (150ms = ~1.5 tokens at 10/sec)
	time.Sleep(150 * time.Millisecond)
	if err := rl.AllowConnect(ip); err != nil {
		t.Errorf("expected allow after refill, got %v", err)
	}
}

func TestRateLimiter_ConcurrentCap(t *testing.T) {
	rl := NewRateLimiter(100.0, 100.0, 2) // high rate, concurrent cap 2
	ip := "1.2.3.4"

	if err := rl.AllowConnect(ip); err != nil {
		t.Fatalf("1st should allow: %v", err)
	}
	if err := rl.AllowConnect(ip); err != nil {
		t.Fatalf("2nd should allow: %v", err)
	}
	if err := rl.AllowConnect(ip); !errors.Is(err, ErrTooManyConcurrent) {
		t.Errorf("3rd should hit concurrent cap, got %v", err)
	}
	rl.Release(ip)
	// Now 3rd (after release) should allow
	if err := rl.AllowConnect(ip); err != nil {
		t.Errorf("after release should allow: %v", err)
	}
}

func TestRateLimiter_IsolatedByIP(t *testing.T) {
	rl := NewRateLimiter(1.0, 1.0, 0)
	// A exhausts its bucket
	rl.AllowConnect("1.1.1.1")
	rl.Release("1.1.1.1")
	if err := rl.AllowConnect("1.1.1.1"); err == nil {
		t.Error("A should be rejected")
	}
	// B untouched
	if err := rl.AllowConnect("2.2.2.2"); err != nil {
		t.Errorf("B unrelated should allow: %v", err)
	}
}

func TestRateLimiter_Evict(t *testing.T) {
	rl := NewRateLimiter(1.0, 1.0, 0)
	rl.idleEvict = 10 * time.Millisecond
	rl.AllowConnect("1.1.1.1")
	rl.Release("1.1.1.1")
	if rl.Size() != 1 {
		t.Errorf("size=%d want 1", rl.Size())
	}
	time.Sleep(20 * time.Millisecond)
	removed := rl.Evict()
	if removed != 1 {
		t.Errorf("evicted=%d want 1", removed)
	}
	if rl.Size() != 0 {
		t.Errorf("size after evict=%d want 0", rl.Size())
	}
}
