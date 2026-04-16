// wire.go defines API/config wire types shared across modules.
//
// These are the "over-the-wire" representations of tenant entities that are
// exchanged between control, launcher, and agent. They are intentionally
// smaller than the full DB entities in model.go — only the fields that
// clients need to display or connect.

package tenant

// ServerLineInfo is the canonical wire type for a game server line as
// presented to launcher UIs and control APIs.
//
// This single definition replaces the three former copies:
//   - control/internal/config.ServerLine
//   - launcher/internal/control.ServerLine
//   - control/pkg/embed.ServerLineEmbed
//
// The full DB entity is ServerLine (model.go) with additional columns
// like TenantID, GamePort, ChatPort, Enabled, CreatedAt.
type ServerLineInfo struct {
	ID         string `json:"id"          yaml:"id"`          // "5.8" or "4.8"
	Name       string `json:"name"        yaml:"name"`        // display name
	AuthPort   int    `json:"auth_port"   yaml:"auth_port"`   // 2108 or 2107
	GameArgs   string `json:"game_args"   yaml:"game_args"`   // extra Aion.bin args
	ClientPath string `json:"client_path" yaml:"client_path"` // local client directory
}

// LauncherWireConfig carries operational knobs delivered to every launcher.
// Used by control's /api/launcher/config and the embed facade.
//
// This single definition replaces:
//   - control/internal/config.LauncherConfig
//   - launcher/internal/control.LauncherConfig
//   - control/pkg/embed.LauncherEmbedConfig
type LauncherWireConfig struct {
	PublicGateIP     string           `json:"public_gate_ip"     yaml:"public_gate_ip"`
	PatchManifestURL string           `json:"patch_manifest_url" yaml:"patch_manifest_url"`
	NewsURL          string           `json:"news_url"           yaml:"news_url"`
	Servers          []ServerLineInfo `json:"servers"             yaml:"servers"`
}
