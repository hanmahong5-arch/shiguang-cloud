// health.go implements background TCP dial probing for relay upstreams.
//
// The prober periodically dials the upstream with a short timeout.
// After failThreshold consecutive failures, the upstream is marked
// unhealthy. A single success immediately marks it healthy again.
//
// Relay uses this to fast-reject new connections to dead upstreams
// instead of making each client wait for a full dial timeout.
package proxy

import (
	"context"
	"log"
	"net"
	"sync/atomic"
	"time"
)

const (
	probeInterval    = 15 * time.Second // check every 15 seconds
	probeTimeout     = 3 * time.Second  // TCP dial timeout per probe
	probeFailThresh  = 3                // consecutive failures before marking unhealthy
)

// upstreamProber runs a background health check against one upstream.
type upstreamProber struct {
	upstream  string
	routeName string
	healthy   atomic.Bool
	failures  atomic.Int32
}

// newUpstreamProber creates a prober for the given upstream address.
// Starts healthy (optimistic) — we assume the upstream is up until proven otherwise.
func newUpstreamProber(routeName, upstream string) *upstreamProber {
	p := &upstreamProber{
		upstream:  upstream,
		routeName: routeName,
	}
	p.healthy.Store(true) // optimistic start
	return p
}

// run starts the probe loop. Blocks until ctx is cancelled.
func (p *upstreamProber) run(ctx context.Context) {
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probe()
		}
	}
}

// probe performs a single TCP dial check.
func (p *upstreamProber) probe() {
	conn, err := net.DialTimeout("tcp", p.upstream, probeTimeout)
	if err != nil {
		count := p.failures.Add(1)
		if count >= probeFailThresh && p.healthy.CompareAndSwap(true, false) {
			log.Printf("[gate] upstream %s (%s) marked UNHEALTHY after %d failures",
				p.routeName, p.upstream, count)
		}
		return
	}
	conn.Close()

	// One success → immediately healthy
	if p.healthy.CompareAndSwap(false, true) {
		log.Printf("[gate] upstream %s (%s) recovered → HEALTHY", p.routeName, p.upstream)
	}
	p.failures.Store(0)
}

// isHealthy returns the current health status.
func (p *upstreamProber) isHealthy() bool {
	return p.healthy.Load()
}
