// Hub-Spoke REST protocol types for ShiguangCloud.
//
// gRPC protocol types (heartbeat, commands, config) are in shared/hubpb/
// (auto-generated from proto/hub.proto). This file only contains REST wire
// types used by the Hub's HTTP handlers for launcher-facing endpoints.
package tenant

// LauncherBootstrap is the response to GET /api/v1/public/{code}/bootstrap.
// This is the FIRST thing a launcher fetches after the player enters a
// tenant code. It carries everything the launcher needs to configure itself.
type LauncherBootstrap struct {
	TenantID    string        `json:"tenant_id"`
	TenantName  string        `json:"tenant_name"`
	Theme       LauncherTheme `json:"theme"`
	GateIP      string        `json:"gate_ip"`       // gate agent's public IP
	ServerLines []ServerLine  `json:"server_lines"`
}

// LauncherLoginResponse is returned after successful launcher login.
// The session_token is used for the Token Handoff flow to the game server.
type LauncherLoginResponse struct {
	OK           bool   `json:"ok"`
	SessionToken string `json:"session_token"`
	Account      string `json:"account"`
	ServerLineID string `json:"server_line_id"`
}
