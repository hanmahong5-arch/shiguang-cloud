package game

import (
	"strings"
	"testing"
)

func TestBuildArgs_Basic(t *testing.T) {
	cfg := StartConfig{
		GateIP:   "1.2.3.4",
		AuthPort: 2108,
	}
	got := buildArgs(cfg)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-ip 1.2.3.4") {
		t.Errorf("missing -ip: %v", got)
	}
	if !strings.Contains(joined, "-port 2108") {
		t.Errorf("missing -port: %v", got)
	}
}

func TestBuildArgs_WithExtra(t *testing.T) {
	cfg := StartConfig{
		GateIP:    "1.2.3.4",
		AuthPort:  2107,
		ExtraArgs: "-cc:5 -lang:chs -noauthgg",
	}
	got := buildArgs(cfg)
	if len(got) < 7 {
		t.Errorf("expected >=7 args, got %v", got)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-cc:5") {
		t.Errorf("missing -cc:5: %v", got)
	}
	if !strings.Contains(joined, "-lang:chs") {
		t.Errorf("missing -lang:chs: %v", got)
	}
}

func TestResolveExecutable_UnknownServer(t *testing.T) {
	_, err := resolveExecutable(StartConfig{ClientRoot: "C:/tmp", ServerID: "bogus"})
	if err == nil {
		t.Error("expected error for unknown server")
	}
}

func TestResolveExecutable_EmptyRoot(t *testing.T) {
	_, err := resolveExecutable(StartConfig{ServerID: "5.8"})
	if err == nil {
		t.Error("expected error for empty client root")
	}
}
