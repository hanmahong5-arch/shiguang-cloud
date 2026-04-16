// Package defense implements Layer-7 protections for the TCP gate:
//   - Per-IP connection rate limiting (token bucket)
//   - Per-IP concurrent connection cap
//   - IP blacklist (ban list) with atomic persistence
//
// NOTE: This is NOT SYN flood protection. Go's net.Accept() only returns
// fully-established TCP connections; SYN floods exhaust the kernel's
// half-connection queue BEFORE user-space sees anything. Real L4 DDoS
// protection must be provided by the upstream CDN / iptables synproxy.
package defense

import (
	"sync"
	"time"
)

// RateLimiter enforces a per-IP token-bucket rate limit on accepted connections
// plus a hard cap on concurrent active connections per IP.
//
// Token bucket: each IP has its own bucket that fills at rate R tokens/sec up
// to a max of B tokens. Every AllowConnect() consumes 1 token. If the bucket
// is empty, the connection is rejected.
//
// Concurrent cap: independent of the rate limit, each IP is limited to
// MaxConcurrent simultaneous connections. Release() must be called when a
// connection ends so the counter drops.
type RateLimiter struct {
	mu sync.Mutex

	// config (set once at construction)
	rate          float64       // tokens per second
	burst         float64       // max tokens in bucket
	maxConcurrent int           // 0 = unlimited
	idleEvict     time.Duration // remove state older than this

	// per-IP state
	buckets map[string]*ipState
}

type ipState struct {
	tokens     float64
	lastRefill time.Time
	active     int
	lastSeen   time.Time
}

// NewRateLimiter constructs a limiter. rate is tokens/sec, burst is bucket
// capacity, maxConcurrent is the per-IP concurrent cap (0 = unlimited).
func NewRateLimiter(rate, burst float64, maxConcurrent int) *RateLimiter {
	return &RateLimiter{
		rate:          rate,
		burst:         burst,
		maxConcurrent: maxConcurrent,
		idleEvict:     10 * time.Minute,
		buckets:       make(map[string]*ipState),
	}
}

// AllowConnect returns nil if the IP may connect, or an error describing
// which check failed (rate exceeded / concurrent cap reached).
// On success the caller MUST call Release(ip) when the connection ends.
func (rl *RateLimiter) AllowConnect(ip string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	st, ok := rl.buckets[ip]
	if !ok {
		st = &ipState{tokens: rl.burst, lastRefill: now}
		rl.buckets[ip] = st
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(st.lastRefill).Seconds()
	st.tokens += elapsed * rl.rate
	if st.tokens > rl.burst {
		st.tokens = rl.burst
	}
	st.lastRefill = now
	st.lastSeen = now

	// Rate limit check
	if st.tokens < 1.0 {
		return ErrRateExceeded
	}

	// Concurrent cap check
	if rl.maxConcurrent > 0 && st.active >= rl.maxConcurrent {
		return ErrTooManyConcurrent
	}

	// Allow: consume token and increment active count
	st.tokens -= 1.0
	st.active++
	return nil
}

// Release decrements the active counter for an IP. Must be called exactly
// once per successful AllowConnect.
func (rl *RateLimiter) Release(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if st, ok := rl.buckets[ip]; ok && st.active > 0 {
		st.active--
	}
}

// Evict removes per-IP state for IPs unseen longer than idleEvict. Should be
// called periodically from a sweeper goroutine to bound memory.
func (rl *RateLimiter) Evict() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.idleEvict)
	removed := 0
	for ip, st := range rl.buckets {
		if st.active == 0 && st.lastSeen.Before(cutoff) {
			delete(rl.buckets, ip)
			removed++
		}
	}
	return removed
}

// Size returns the number of per-IP entries currently tracked.
func (rl *RateLimiter) Size() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.buckets)
}

// Sentinel errors returned by AllowConnect.
var (
	ErrRateExceeded      = RateError("rate limit exceeded")
	ErrTooManyConcurrent = RateError("too many concurrent connections")
)

// RateError is a simple typed error for classification.
type RateError string

func (e RateError) Error() string { return string(e) }
