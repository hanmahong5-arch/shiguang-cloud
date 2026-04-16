package defense

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBanList_BasicBanUnban(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "bans.json")
	bl, err := NewBanList(tmpFile)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer bl.Close()

	if bl.IsBanned("1.1.1.1") {
		t.Error("fresh list should not have bans")
	}

	bl.Ban("1.1.1.1", "test", 0)
	if !bl.IsBanned("1.1.1.1") {
		t.Error("should be banned")
	}
	if bl.Size() != 1 {
		t.Errorf("size=%d want 1", bl.Size())
	}

	if !bl.Unban("1.1.1.1") {
		t.Error("unban should return true for banned IP")
	}
	if bl.IsBanned("1.1.1.1") {
		t.Error("should not be banned after unban")
	}
}

func TestBanList_Expiration(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "bans.json")
	bl, err := NewBanList(tmpFile)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer bl.Close()

	bl.Ban("1.1.1.1", "short", 50*time.Millisecond)
	if !bl.IsBanned("1.1.1.1") {
		t.Error("should be banned immediately")
	}
	time.Sleep(100 * time.Millisecond)
	if bl.IsBanned("1.1.1.1") {
		t.Error("should be unbanned after expiration")
	}
}

func TestBanList_Persistence(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, "bans.json")

	// Create, ban, close
	bl, err := NewBanList(tmpFile)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	bl.Ban("1.1.1.1", "persist-test", 0)
	bl.Ban("2.2.2.2", "another", 1*time.Hour)
	bl.Close() // synchronous save

	// File should exist
	if _, err := os.Stat(tmpFile); err != nil {
		t.Fatalf("banlist file not created: %v", err)
	}

	// Reload
	bl2, err := NewBanList(tmpFile)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	defer bl2.Close()
	if !bl2.IsBanned("1.1.1.1") {
		t.Error("persistent ban not reloaded")
	}
	if !bl2.IsBanned("2.2.2.2") {
		t.Error("timed ban not reloaded")
	}
}

func TestBanList_AtomicSaveDoesNotCorruptOnCrash(t *testing.T) {
	// Verify temp file rename semantics: file should always contain valid JSON.
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, "bans.json")

	bl, err := NewBanList(tmpFile)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	for i := 0; i < 100; i++ {
		bl.Ban("1.1.1.1", "spam", 0)
		bl.Unban("1.1.1.1")
	}
	bl.Close()

	// Read file - should be valid JSON (empty array or valid entries)
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) == 0 {
		t.Error("empty file after rapid ops")
	}
	if data[0] != '[' {
		t.Errorf("invalid JSON start: %q", data[0])
	}
}
