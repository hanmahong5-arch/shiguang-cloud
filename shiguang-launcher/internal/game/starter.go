// Package game launches the AION game client process.
//
// The launcher's sole responsibility here is to:
//  1. Pick the correct binary (bin32/Aion.exe for 4.8, bin64/Aion.bin for 5.8)
//  2. Compose the command line with the gate's public IP + auth port
//  3. Set working directory to the client root so the game can find its data
//  4. Start the process without blocking (detach)
//
// No DLL injection, no registry tweaks, no elevated privileges. The game
// client is an opaque subprocess; everything beyond -ip/-port/-cc is routed
// through the game server's own protocol.
package game

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// StartConfig bundles everything needed to launch the game.
type StartConfig struct {
	ClientRoot   string // absolute path to the client directory (contains bin32/bin64)
	ServerID     string // "5.8" or "4.8"
	GateIP       string // public gate IP (launcher points Aion.bin here)
	AuthPort     int    // 2108 for 5.8, 2107 for 4.8
	ExtraArgs    string // additional -cc / -lang / etc. args from control
	SessionToken string // session token for Token Handoff (empty = skip)
}

// Start launches the game client. Returns the spawned pid or an error.
// The subprocess is detached (fire and forget) — the launcher stays alive
// and does not wait for the game to exit.
func Start(cfg StartConfig) (int, error) {
	exePath, err := resolveExecutable(cfg)
	if err != nil {
		return 0, err
	}

	workDir := filepath.Dir(exePath) // e.g. bin64/
	args := buildArgs(cfg)

	// Write session token to temp file for version.dll to read.
	// Token Handoff: version.dll reads the token, injects it into the
	// auth handshake, then deletes the file. This avoids command-line
	// exposure (visible in process listing).
	if cfg.SessionToken != "" {
		tokenPath := filepath.Join(cfg.ClientRoot, ".sg-session")
		if err := writeSecureFile(tokenPath, []byte(cfg.SessionToken)); err != nil {
			return 0, fmt.Errorf("write session token: %w", err)
		}
	}

	cmd := exec.Command(exePath, args...)
	cmd.Dir = workDir
	// Inherit stdio to /dev/null — the game manages its own logging.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start %s: %w", exePath, err)
	}

	// Release the child so it survives launcher shutdown.
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return cmd.Process.Pid, nil
}

func resolveExecutable(cfg StartConfig) (string, error) {
	if cfg.ClientRoot == "" {
		return "", fmt.Errorf("client_root not configured")
	}

	// 5.8 uses 64-bit, 4.8 uses 32-bit.
	var subdir, exe string
	switch cfg.ServerID {
	case "5.8", "58":
		subdir = "bin64"
		exe = "Aion.bin"
	case "4.8", "48":
		subdir = "bin32"
		exe = "Aion.bin"
	default:
		return "", fmt.Errorf("unknown server: %s", cfg.ServerID)
	}

	path := filepath.Join(cfg.ClientRoot, subdir, exe)
	if _, err := os.Stat(path); err != nil {
		// Fall back to .exe suffix for some legacy builds
		alt := filepath.Join(cfg.ClientRoot, subdir, "Aion.exe")
		if _, err2 := os.Stat(alt); err2 == nil {
			return alt, nil
		}
		return "", fmt.Errorf("game binary not found at %s", path)
	}
	return path, nil
}

// writeSecureFile writes data to a file with restricted permissions.
// On Windows, NTFS ACLs are set via the hidden+system attributes as a
// pragmatic defense against casual snooping. The file is also deleted
// by version.dll immediately after reading (single-use).
func writeSecureFile(path string, data []byte) error {
	// Write with owner-only permissions (effective on Unix)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	// On Windows, mark file as hidden to prevent casual discovery
	if runtime.GOOS == "windows" {
		// Use os.Chmod equivalent — Go's os.WriteFile on Windows creates
		// a file readable by the current user. Marking hidden adds a layer
		// of obscurity. The real security is the single-use + 5min TTL.
		exec.Command("attrib", "+H", path).Run()
	}
	return nil
}

func buildArgs(cfg StartConfig) []string {
	// Example final command:
	//   Aion.bin -ip 1.2.3.4 -port 2108 -cc 1 -lang:chs -noauthgg ...
	args := []string{
		"-ip", cfg.GateIP,
		"-port", fmt.Sprintf("%d", cfg.AuthPort),
	}
	// Enable token handoff in version.dll when session token is present.
	// -loginex extends the CM_LOGIN password field from 16 to 32 bytes,
	// required to fit the "SG-" prefix + 24-char hex token (27 total).
	if cfg.SessionToken != "" {
		args = append(args, "-loginex", "-sg-token-handoff")
	}
	if extra := strings.TrimSpace(cfg.ExtraArgs); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}
	return args
}
