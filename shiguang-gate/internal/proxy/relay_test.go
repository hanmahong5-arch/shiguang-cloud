package proxy

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// echoServer starts a tiny TCP echo server on a random local port for tests.
// Returns the addr string and a shutdown func.
func echoServer(t *testing.T) (addr string, shutdown func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	var wg sync.WaitGroup
	shutdown = func() {
		l.Close()
		wg.Wait()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()
	return l.Addr().String(), shutdown
}

// proxyV2AwareServer accepts connections, reads the 28-byte v4 PROXY v2 header
// first, stores the parsed source, then echoes remaining bytes.
func proxyV2AwareServer(t *testing.T) (addr string, getSrc func() *net.TCPAddr, shutdown func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pv2 listen: %v", err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var lastSrc *net.TCPAddr

	getSrc = func() *net.TCPAddr {
		mu.Lock()
		defer mu.Unlock()
		return lastSrc
	}
	shutdown = func() {
		l.Close()
		wg.Wait()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()

				// Read 28 bytes (fixed v4 header)
				header := make([]byte, 28)
				if _, err := io.ReadFull(c, header); err != nil {
					return
				}
				// Minimal parsing: bytes 16-20 = src IP, 24-26 = src port (BE)
				ip := net.IPv4(header[16], header[17], header[18], header[19])
				port := int(header[24])<<8 | int(header[25])
				mu.Lock()
				lastSrc = &net.TCPAddr{IP: ip, Port: port}
				mu.Unlock()

				io.Copy(c, c)
			}(conn)
		}
	}()
	return l.Addr().String(), getSrc, shutdown
}

func TestRelay_EchoRoundtrip(t *testing.T) {
	// upstream echo
	upstreamAddr, shutdown := echoServer(t)
	defer shutdown()

	// relay (no proxy protocol)
	route := Route{
		Name:          "test-echo",
		Listen:        "127.0.0.1:0", // random port
		Upstream:      upstreamAddr,
		ProxyProtocol: false,
	}
	r := NewRelay(route, nil)

	// Bind a fixed port since NewRelay's Start() uses the configured Listen.
	// Override by calling listen manually:
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	r.listener = l
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.wg.Add(1)
	go r.acceptLoop(ctx)

	relayAddr := l.Addr().String()
	defer r.Stop()

	// Connect client → relay → echo
	c, err := net.Dial("tcp", relayAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	payload := []byte("hello-aion-gate")
	if _, err := c.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(payload))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(payload) {
		t.Errorf("echo mismatch: got %q want %q", buf, payload)
	}

	accepted, rejected, active := r.Stats()
	if accepted == 0 {
		t.Errorf("expected accepted>0, got %d", accepted)
	}
	_ = rejected
	_ = active
}

func TestRelay_ProxyV2HeaderDelivered(t *testing.T) {
	// Upstream server that parses PROXY v2 header
	upstreamAddr, getSrc, shutdown := proxyV2AwareServer(t)
	defer shutdown()

	route := Route{
		Name:          "test-pv2",
		Listen:        "127.0.0.1:0",
		Upstream:      upstreamAddr,
		ProxyProtocol: true,
	}
	r := NewRelay(route, nil)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	r.listener = l
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.wg.Add(1)
	go r.acceptLoop(ctx)
	defer r.Stop()

	relayAddr := l.Addr().String()
	c, err := net.Dial("tcp", relayAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// Write a small payload so the upstream goroutine finishes reading
	// the header + starts echo.
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	// Wait a moment for the upstream goroutine to store lastSrc
	time.Sleep(50 * time.Millisecond)
	src := getSrc()
	if src == nil {
		t.Fatal("PROXY v2 header not received by upstream")
	}

	// The client's real address should match what upstream parsed
	clientLocalAddr := c.LocalAddr().(*net.TCPAddr)
	if !src.IP.Equal(clientLocalAddr.IP) && !(src.IP.IsLoopback() && clientLocalAddr.IP.IsLoopback()) {
		t.Errorf("parsed src IP %v != client IP %v", src.IP, clientLocalAddr.IP)
	}
	if src.Port != clientLocalAddr.Port {
		t.Errorf("parsed src port %d != client port %d", src.Port, clientLocalAddr.Port)
	}
}

func TestRelay_HookCanReject(t *testing.T) {
	upstreamAddr, shutdown := echoServer(t)
	defer shutdown()

	rejectHook := func(client *net.TCPAddr) error {
		return net.ErrClosed // any non-nil rejects
	}

	route := Route{
		Name:     "test-reject",
		Listen:   "127.0.0.1:0",
		Upstream: upstreamAddr,
	}
	r := NewRelay(route, rejectHook)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	r.listener = l
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.wg.Add(1)
	go r.acceptLoop(ctx)
	defer r.Stop()

	relayAddr := l.Addr().String()
	c, err := net.Dial("tcp", relayAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// The hook rejects, so the conn should be closed immediately.
	// A read should return EOF or an error quickly.
	c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	_, err = c.Read(buf)
	if err == nil {
		t.Error("expected read to fail after hook reject")
	}

	// Give time for counter update
	time.Sleep(50 * time.Millisecond)
	_, rejected, _ := r.Stats()
	if rejected == 0 {
		t.Errorf("expected rejected>0, got %d", rejected)
	}
}

// TestRelay_StartStop verifies the public Start/Stop lifecycle.
func TestRelay_StartStop(t *testing.T) {
	upstreamAddr, shutdown := echoServer(t)
	defer shutdown()

	// Pick a random free port
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	route := Route{
		Name:     "test-lifecycle",
		Listen:   "127.0.0.1:" + strconv.Itoa(port),
		Upstream: upstreamAddr,
	}
	r := NewRelay(route, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Quick sanity check that port is listening
	c, err := net.Dial("tcp", route.Listen)
	if err != nil {
		t.Fatalf("dial after start: %v", err)
	}
	c.Close()

	r.Stop()
	done := make(chan struct{})
	go func() {
		r.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Wait() did not return within 2s after Stop()")
	}
}
