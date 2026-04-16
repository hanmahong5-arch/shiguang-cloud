package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Route describes one listen→upstream mapping (e.g. "5.8 Auth": :2108 → 127.0.0.1:2108).
type Route struct {
	Name          string        // human-readable, used in logs
	Listen        string        // "host:port" to bind
	Upstream      string        // "host:port" to dial for each new connection
	ProxyProtocol bool          // if true, write PROXY v2 header before relay
	DialTimeout   time.Duration // upstream dial timeout (default 5s)
	IdleTimeout   time.Duration // max inactivity before closing (0 = disabled)
}

// ConnHook is an optional per-connection callback invoked right after accept
// but before dialing upstream. Returning a non-nil error closes the connection
// without dialing upstream (used for IP bans, rate limits, etc.).
type ConnHook func(clientAddr *net.TCPAddr) error

// Relay is a single listener running for one Route. It accepts inbound TCP
// connections, optionally runs pre-dial hooks (bans/ratelimit), dials upstream,
// writes a PROXY v2 header, then bidirectionally copies bytes until either
// side closes.
type Relay struct {
	route  Route
	hook   ConnHook
	prober *upstreamProber // background upstream health checker

	listener net.Listener
	wg       sync.WaitGroup

	// Observability counters.
	mu           sync.Mutex
	connAccepted uint64
	connRejected uint64
	connActive   int64
}

// NewRelay constructs a relay for the given route. hook may be nil.
func NewRelay(route Route, hook ConnHook) *Relay {
	if route.DialTimeout == 0 {
		route.DialTimeout = 5 * time.Second
	}
	return &Relay{
		route:  route,
		hook:   hook,
		prober: newUpstreamProber(route.Name, route.Upstream),
	}
}

// Start begins listening and accepts connections until ctx is cancelled or
// Stop is called. It returns as soon as the listener is bound; actual accept
// loop runs in a goroutine. Call Wait() after shutdown to drain in-flight.
func (r *Relay) Start(ctx context.Context) error {
	l, err := net.Listen("tcp", r.route.Listen)
	if err != nil {
		return fmt.Errorf("relay %s: listen %s: %w", r.route.Name, r.route.Listen, err)
	}
	r.listener = l

	r.wg.Add(1)
	go r.acceptLoop(ctx)

	// Start background upstream health probing
	go r.prober.run(ctx)

	return nil
}

// Stop closes the listener; in-flight connections are NOT forcefully terminated
// — they continue until the remote or upstream closes naturally. Use Wait() to
// block until everything drains. For emergency shutdown, cancel the context
// passed to Start which will cause goroutines to close their sockets.
func (r *Relay) Stop() {
	if r.listener != nil {
		_ = r.listener.Close()
	}
}

// Wait blocks until all accept/copy goroutines have returned. Must be called
// after Stop() or context cancellation — otherwise it blocks forever.
func (r *Relay) Wait() { r.wg.Wait() }

// Stats returns a snapshot of counters for the admin API.
func (r *Relay) Stats() (accepted, rejected uint64, active int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connAccepted, r.connRejected, r.connActive
}

// UpstreamHealthy returns whether the upstream is reachable.
func (r *Relay) UpstreamHealthy() bool { return r.prober.isHealthy() }

// Name returns the route's human-readable name.
func (r *Relay) Name() string { return r.route.Name }

func (r *Relay) acceptLoop(ctx context.Context) {
	defer r.wg.Done()

	for {
		conn, err := r.listener.Accept()
		if err != nil {
			// listener closed is the normal shutdown path
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// transient error: backoff briefly and retry (avoids busy loop
			// on file descriptor exhaustion)
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}

		r.wg.Add(1)
		go r.handleConn(ctx, conn)
	}
}

func (r *Relay) handleConn(parentCtx context.Context, client net.Conn) {
	defer r.wg.Done()
	defer client.Close()

	clientTCP := TCPAddrFromAddr(client.RemoteAddr())
	if clientTCP == nil {
		return
	}

	// Pre-dial hook for bans/rate limiting.
	if r.hook != nil {
		if err := r.hook(clientTCP); err != nil {
			r.bump(&r.connRejected)
			return
		}
	}

	// Fast-reject if upstream is known to be down — saves the client
	// from waiting for a full dial timeout on a dead server.
	if !r.prober.isHealthy() {
		r.bump(&r.connRejected)
		return
	}

	r.bump(&r.connAccepted)
	r.incActive(+1)
	defer r.incActive(-1)

	// Dial upstream with timeout.
	dialer := net.Dialer{Timeout: r.route.DialTimeout}
	upstream, err := dialer.DialContext(parentCtx, "tcp", r.route.Upstream)
	if err != nil {
		return
	}
	defer upstream.Close()

	// Write PROXY v2 header to upstream BEFORE any payload bytes.
	// The downstream server's Session layer MUST read this header first
	// (before sending its S_INIT handshake) to populate realIp.
	if r.route.ProxyProtocol {
		dstTCP := TCPAddrFromAddr(upstream.LocalAddr())
		if dstTCP == nil {
			return
		}
		header, herr := BuildV2Header(clientTCP, dstTCP)
		if herr != nil {
			return
		}
		if _, werr := upstream.Write(header); werr != nil {
			return
		}
	}

	// Bidirectional copy with context cancellation.
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	copyDone := make(chan struct{}, 2)

	go func() {
		_, _ = io.Copy(upstream, client)
		// half-close: signal upstream we won't write anymore, let it drain
		if tc, ok := upstream.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		copyDone <- struct{}{}
	}()

	go func() {
		_, _ = io.Copy(client, upstream)
		if tc, ok := client.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		copyDone <- struct{}{}
	}()

	// Wait for either direction to close, or context cancellation.
	select {
	case <-ctx.Done():
		// force close both sides
	case <-copyDone:
		// one side closed; wait briefly for the other (or ctx)
		select {
		case <-copyDone:
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

// bump is a mutex-protected counter increment for u64 stats.
func (r *Relay) bump(counter *uint64) {
	r.mu.Lock()
	*counter++
	r.mu.Unlock()
}

func (r *Relay) incActive(delta int64) {
	r.mu.Lock()
	r.connActive += delta
	r.mu.Unlock()
}
