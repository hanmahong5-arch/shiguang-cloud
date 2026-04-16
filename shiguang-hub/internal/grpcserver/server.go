// Package grpcserver implements the Hub-side gRPC AgentHub service.
//
// Responsibilities:
//   - Accept bidirectional streams from Gate Agents (ReportStream)
//   - Validate agent_token against the tenant database
//   - Update gate_agents.last_seen on heartbeat
//   - Dispatch pending commands (ban/kick/config) to connected agents
//   - FetchConfig for one-shot agent config pull
//   - PushCommand for REST→gRPC bridge (operator dashboard)
//   - Connection rate limiting (token bucket, 100 new conn/s)
package grpcserver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"golang.org/x/time/rate"

	pb "github.com/shiguang/shared/hubpb"
	"github.com/shiguang/shared/tenant"
)

// Server is the gRPC AgentHub server.
type Server struct {
	pb.UnimplementedAgentHubServer

	repo    *tenant.Repo
	limiter *rate.Limiter // connection rate limiter (100 new conn/s)

	// Connected agents indexed by agent_key.
	mu      sync.RWMutex
	streams map[string]*agentStream
}

// agentStream tracks a single connected agent's stream.
type agentStream struct {
	tenantID string
	agentKey string
	stream   pb.AgentHub_ReportStreamServer
	cmdCh    chan *pb.HubCommand // buffered channel for pending commands
	lastSeen time.Time
}

// NewServer creates a gRPC server backed by the tenant repository.
func NewServer(repo *tenant.Repo) *Server {
	return &Server{
		repo:    repo,
		limiter: rate.NewLimiter(100, 200), // 100 conn/s, burst 200
		streams: make(map[string]*agentStream),
	}
}

// Start binds the gRPC server to the given address and returns it.
// The caller is responsible for calling GracefulStop() on shutdown.
func (s *Server) Start(bind string) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", bind)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", bind, err)
	}

	srv := grpc.NewServer(
		grpc.UnaryInterceptor(s.unaryRateLimitInterceptor),
		grpc.StreamInterceptor(s.streamRateLimitInterceptor),
	)
	pb.RegisterAgentHubServer(srv, s)

	go func() {
		slog.Info("gRPC listening", "bind", bind)
		if err := srv.Serve(lis); err != nil {
			slog.Error("gRPC serve failed", "err", err)
		}
	}()

	return srv, nil
}

// ConnectedAgents returns the number of currently connected agents.
func (s *Server) ConnectedAgents() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.streams)
}

// SendCommand pushes a command to a specific tenant's connected agent(s).
// Returns true if at least one agent received the command.
func (s *Server) SendCommand(tenantID string, cmd *pb.HubCommand) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sent := false
	for _, as := range s.streams {
		if as.tenantID == tenantID {
			select {
			case as.cmdCh <- cmd:
				sent = true
			default:
				slog.Warn("command buffer full", "agent", as.agentKey[:8])
			}
		}
	}
	return sent
}

// ── ReportStream (bidirectional) ────────────────────────────────────────

func (s *Server) ReportStream(stream pb.AgentHub_ReportStreamServer) error {
	// Extract agent-token from gRPC metadata
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	tokens := md.Get("agent-token")
	if len(tokens) == 0 || tokens[0] == "" {
		return status.Error(codes.Unauthenticated, "missing agent-token")
	}
	agentToken := tokens[0]

	// Validate agent_token against database
	agent, err := s.repo.GetGateAgentByKey(stream.Context(), agentToken)
	if err != nil {
		return status.Errorf(codes.PermissionDenied, "invalid agent token: %v", err)
	}

	// Register stream
	as := &agentStream{
		tenantID: agent.TenantID,
		agentKey: agent.AgentKey,
		stream:   stream,
		cmdCh:    make(chan *pb.HubCommand, 32),
		lastSeen: time.Now(),
	}

	s.mu.Lock()
	s.streams[agent.AgentKey] = as
	s.mu.Unlock()

	slog.Info("agent connected", "agent", agent.AgentKey[:8], "tenant", agent.TenantID[:8])

	defer func() {
		s.mu.Lock()
		delete(s.streams, agent.AgentKey)
		s.mu.Unlock()
		slog.Info("agent disconnected", "agent", agent.AgentKey[:8])
	}()

	// Command sender goroutine — sends pending commands from cmdCh to the stream
	go func() {
		for {
			select {
			case <-stream.Context().Done():
				return
			case cmd := <-as.cmdCh:
				if err := stream.Send(cmd); err != nil {
					slog.Error("send command failed", "agent", agent.AgentKey[:8], "err", err)
					return
				}
			}
		}
	}()

	// Receive loop — process heartbeats/metrics/events from agent
	for {
		report, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		switch p := report.Payload.(type) {
		case *pb.AgentReport_Heartbeat:
			s.handleHeartbeat(stream.Context(), agent, p.Heartbeat)
		case *pb.AgentReport_Metrics:
			s.handleMetrics(agent, p.Metrics)
		case *pb.AgentReport_Event:
			s.handleEvent(agent, p.Event)
		}
	}
}

// handleHeartbeat updates gate_agents.last_seen and status.
func (s *Server) handleHeartbeat(ctx context.Context, agent *tenant.GateAgent, hb *pb.Heartbeat) {
	agent.Hostname = hb.Hostname
	agent.PublicIP = hb.PublicIp
	agent.Version = hb.Version
	agent.Status = hb.Status
	agent.LastSeen = time.Now()

	if err := s.repo.UpsertGateAgent(ctx, agent); err != nil {
		slog.Error("update agent heartbeat", "agent", agent.AgentKey[:8], "err", err)
	}
}

func (s *Server) handleMetrics(agent *tenant.GateAgent, batch *pb.MetricsBatch) {
	// Aggregate relay metrics into daily stats for dashboard graphs.
	// Each MetricPoint reports active connections — use peak for peak_online.
	var peakOnline int
	for _, p := range batch.Points {
		if v := int(p.Value); v > peakOnline {
			peakOnline = v
		}
	}
	if peakOnline > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.repo.IncrDailyStats(ctx, agent.TenantID, 0, 0, peakOnline); err != nil {
			slog.Error("daily stats update", "err", err)
		}
	}
	slog.Debug("metrics received", "agent", agent.AgentKey[:8], "points", len(batch.Points), "peak", peakOnline)
}

func (s *Server) handleEvent(agent *tenant.GateAgent, event *pb.PlayerEvent) {
	// Phase C-4: store events for dashboard activity feed
	slog.Debug("player event", "agent", agent.AgentKey[:8], "type", event.Type, "account", event.Account)
}

// ── FetchConfig (unary) ─────────────────────────────────────────────────

func (s *Server) FetchConfig(ctx context.Context, req *pb.ConfigRequest) (*pb.AgentConfig, error) {
	// Validate agent token
	agent, err := s.repo.GetGateAgentByKey(ctx, req.AgentToken)
	if err != nil {
		return nil, status.Errorf(codes.PermissionDenied, "invalid agent token")
	}

	// Load tenant info
	t, err := s.repo.GetTenant(ctx, agent.TenantID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "tenant lookup: %v", err)
	}

	// Load server lines
	lines, err := s.repo.ListServerLines(ctx, agent.TenantID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "server lines: %v", err)
	}

	// Load branding
	theme, _ := s.repo.GetTheme(ctx, agent.TenantID)

	// Build config response
	cfg := &pb.AgentConfig{
		TenantId:   t.ID,
		TenantSlug: t.Slug,
	}

	for _, l := range lines {
		cfg.Servers = append(cfg.Servers, &pb.ServerLineConfig{
			Id:       l.ID,
			Name:     l.Name,
			Version:  l.Version,
			AuthPort: int32(l.AuthPort),
			GamePort: int32(l.GamePort),
			ChatPort: int32(l.ChatPort),
			GameArgs: l.GameArgs,
		})
	}

	if theme != nil {
		cfg.Branding = &pb.BrandingConfig{
			ServerName:  theme.ServerName,
			LogoUrl:     theme.LogoURL,
			BgUrl:       theme.BgURL,
			AccentColor: theme.AccentColor,
			TextColor:   theme.TextColor,
		}
		cfg.PatchManifestUrl = theme.PatchURL
		cfg.NewsUrl = theme.NewsURL
	}

	return cfg, nil
}

// ── PushCommand (internal REST→gRPC bridge) ─────────────────────────────

func (s *Server) PushCommand(ctx context.Context, req *pb.PushCommandRequest) (*pb.PushCommandResponse, error) {
	if req.TenantId == "" || req.Command == nil {
		return &pb.PushCommandResponse{Delivered: false, Error: "tenant_id and command required"}, nil
	}

	delivered := s.SendCommand(req.TenantId, req.Command)
	resp := &pb.PushCommandResponse{Delivered: delivered}
	if !delivered {
		resp.Error = "no connected agents for tenant"
	}
	return resp, nil
}

// ── Rate limiting interceptors ──────────────────────────────────────────

// unaryRateLimitInterceptor limits new unary RPCs per second.
func (s *Server) unaryRateLimitInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	if !s.limiter.Allow() {
		return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}
	return handler(ctx, req)
}

// streamRateLimitInterceptor limits new stream connections per second.
func (s *Server) streamRateLimitInterceptor(
	srv any,
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	if !s.limiter.Allow() {
		return status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}
	return handler(srv, ss)
}
