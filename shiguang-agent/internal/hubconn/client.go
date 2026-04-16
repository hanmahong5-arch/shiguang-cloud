// Package hubconn manages the Agent's gRPC connection to ShiguangHub.
//
// It maintains a long-lived bidirectional stream (AgentHub.ReportStream)
// with exponential backoff + random jitter for reconnects.
//
// Heartbeats are sent every 10s (+ jitter 0-3s) and include relay stats
// collected from the gate subsystem. Commands received from Hub (ban, kick,
// config update) are dispatched to registered handlers.
package hubconn

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/shiguang/shared/hubpb"
)

// RelayStatsFunc collects current relay snapshots from the gate subsystem.
type RelayStatsFunc func() []RelaySnapshot

// RelaySnapshot mirrors gate embed's RelaySnapshot — kept separate to avoid
// cross-package dependency cycles.
type RelaySnapshot struct {
	Name            string
	Active          int64
	Accepted        uint64
	Rejected        uint64
	UpstreamHealthy bool
}

// CommandHandler is invoked for each command received from Hub.
type CommandHandler func(cmd *pb.HubCommand)

// ConfigUpdateHandler is invoked when Hub pushes a config update.
// Receives the freshly-fetched AgentConfig (nil if fetch failed).
type ConfigUpdateHandler func(cfg *pb.AgentConfig, err error)

// Client manages the gRPC connection and heartbeat loop to Hub.
type Client struct {
	hubAddr    string
	agentToken string
	hostname   string

	statsFunc      RelayStatsFunc
	onCommand      CommandHandler
	onConfigUpdate ConfigUpdateHandler

	mu       sync.Mutex
	banCount int32 // updated externally
}

// NewClient creates a Hub connection client.
func NewClient(hubAddr, agentToken string, stats RelayStatsFunc, handler CommandHandler) *Client {
	hostname, _ := os.Hostname()
	return &Client{
		hubAddr:    hubAddr,
		agentToken: agentToken,
		hostname:   hostname,
		statsFunc:  stats,
		onCommand:  handler,
	}
}

// SetConfigUpdateHandler registers a callback for config update commands.
func (c *Client) SetConfigUpdateHandler(h ConfigUpdateHandler) {
	c.onConfigUpdate = h
}

// SetBanCount updates the ban count reported in heartbeats.
func (c *Client) SetBanCount(n int32) {
	c.mu.Lock()
	c.banCount = n
	c.mu.Unlock()
}

// Run connects to Hub and maintains the bidirectional stream with
// exponential backoff + jitter reconnect. Blocks until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		start := time.Now()
		err := c.runStream(ctx)
		if ctx.Err() != nil {
			return nil // clean shutdown
		}

		// If the stream was alive for >30s, the disconnect is not a
		// connection issue — reset backoff so the next attempt is fast.
		if time.Since(start) > 30*time.Second {
			backoff = 1 * time.Second
		}

		// Reconnect with exponential backoff + random jitter (0-5s)
		jitter := time.Duration(rand.Int63n(5000)) * time.Millisecond
		wait := backoff + jitter
		slog.Warn("stream lost, reconnecting", "err", err, "wait", wait)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}

		// Grow backoff for next failure, but cap it
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// runStream establishes a single gRPC stream session.
// Returns when the stream breaks or ctx is cancelled.
func (c *Client) runStream(ctx context.Context) error {
	// Dial with agent_token in metadata for auth
	conn, err := grpc.NewClient(c.hubAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.hubAddr, err)
	}
	defer conn.Close()

	client := pb.NewAgentHubClient(conn)

	// Attach agent_token as gRPC metadata
	md := metadata.Pairs("agent-token", c.agentToken)
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.ReportStream(streamCtx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}

	slog.Info("gRPC stream opened", "hub", c.hubAddr)

	// Reset backoff on successful connection (caller handles this)

	// Receive commands in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.recvLoop(stream)
	}()

	// Send heartbeats in the main loop
	ticker := c.newHeartbeatTicker()
	defer ticker.Stop()

	// Send initial heartbeat immediately
	if err := c.sendHeartbeat(stream); err != nil {
		return fmt.Errorf("initial heartbeat: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			_ = stream.CloseSend()
			return nil
		case err := <-errCh:
			return fmt.Errorf("recv: %w", err)
		case <-ticker.C:
			if err := c.sendHeartbeat(stream); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
		}
	}
}

// recvLoop reads commands from Hub and dispatches to the handler.
func (c *Client) recvLoop(stream pb.AgentHub_ReportStreamClient) error {
	for {
		cmd, err := stream.Recv()
		if err == io.EOF {
			return fmt.Errorf("server closed stream")
		}
		if err != nil {
			return err
		}
		if c.onCommand != nil {
			c.onCommand(cmd)
		}
	}
}

// sendHeartbeat sends a single heartbeat with current relay stats.
func (c *Client) sendHeartbeat(stream pb.AgentHub_ReportStreamClient) error {
	var routes []*pb.RouteSnapshot
	if c.statsFunc != nil {
		for _, s := range c.statsFunc() {
			routes = append(routes, &pb.RouteSnapshot{
				Name:     s.Name,
				Active:   s.Active,
				Accepted: s.Accepted,
				Rejected: s.Rejected,
			})
		}
	}

	c.mu.Lock()
	banCount := c.banCount
	c.mu.Unlock()

	return stream.Send(&pb.AgentReport{
		AgentToken: c.agentToken,
		Payload: &pb.AgentReport_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				Hostname: c.hostname,
				Version:  "2.0.0",
				Status:   "online",
				Routes:   routes,
				BanCount: banCount,
			},
		},
	})
}

// FetchConfig calls the Hub's FetchConfig unary RPC to pull the latest
// agent configuration. This is called on startup and when Hub pushes
// a ConfigUpdate command.
func (c *Client) FetchConfig(ctx context.Context) (*pb.AgentConfig, error) {
	conn, err := grpc.NewClient(c.hubAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	client := pb.NewAgentHubClient(conn)
	cfg, err := client.FetchConfig(ctx, &pb.ConfigRequest{
		AgentToken: c.agentToken,
	})
	if err != nil {
		return nil, fmt.Errorf("FetchConfig: %w", err)
	}
	return cfg, nil
}

// newHeartbeatTicker creates a ticker with 10s base + 0-3s jitter.
// Uses a channel-based approach to add per-tick jitter.
func (c *Client) newHeartbeatTicker() *jitterTicker {
	return newJitterTicker(10*time.Second, 3*time.Second)
}

// jitterTicker fires at base interval + random jitter each tick.
type jitterTicker struct {
	C      <-chan time.Time
	stopCh chan struct{}
}

func newJitterTicker(base, maxJitter time.Duration) *jitterTicker {
	ch := make(chan time.Time, 1)
	stop := make(chan struct{})
	go func() {
		for {
			jitter := time.Duration(rand.Int63n(int64(maxJitter)))
			select {
			case <-stop:
				return
			case <-time.After(base + jitter):
				select {
				case ch <- time.Now():
				default:
				}
			}
		}
	}()
	return &jitterTicker{C: ch, stopCh: stop}
}

func (t *jitterTicker) Stop() {
	close(t.stopCh)
}
