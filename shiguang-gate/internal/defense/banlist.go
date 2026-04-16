package defense

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// BanList manages a set of banned IPs with persistent storage. Writes are
// atomic (temp file + rename) to prevent corruption on crash during save.
type BanList struct {
	mu       sync.RWMutex
	banned   map[string]BanEntry
	file     string
	saveCh   chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
}

// BanEntry records why/when an IP was banned.
type BanEntry struct {
	IP        string    `json:"ip"`
	Reason    string    `json:"reason"`
	BannedAt  time.Time `json:"banned_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"` // zero = permanent
}

// NewBanList creates a ban list backed by file. If file exists, it's loaded
// synchronously. A background saver coalesces writes.
func NewBanList(file string) (*BanList, error) {
	bl := &BanList{
		banned: make(map[string]BanEntry),
		file:   file,
		saveCh: make(chan struct{}, 1),
		stopCh: make(chan struct{}),
	}
	if err := bl.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load banlist: %w", err)
	}
	go bl.saveLoop()
	return bl, nil
}

// IsBanned returns true if ip is currently banned (and not expired).
func (bl *BanList) IsBanned(ip string) bool {
	bl.mu.RLock()
	entry, ok := bl.banned[ip]
	bl.mu.RUnlock()
	if !ok {
		return false
	}
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		// Lazy expiration: remove and signal save.
		bl.mu.Lock()
		delete(bl.banned, ip)
		bl.mu.Unlock()
		bl.triggerSave()
		return false
	}
	return true
}

// Ban adds or updates a ban. If duration is zero, the ban is permanent.
func (bl *BanList) Ban(ip, reason string, duration time.Duration) {
	bl.mu.Lock()
	entry := BanEntry{
		IP:       ip,
		Reason:   reason,
		BannedAt: time.Now(),
	}
	if duration > 0 {
		entry.ExpiresAt = time.Now().Add(duration)
	}
	bl.banned[ip] = entry
	bl.mu.Unlock()
	bl.triggerSave()
}

// Unban removes an IP from the ban list. Returns true if it was banned.
func (bl *BanList) Unban(ip string) bool {
	bl.mu.Lock()
	_, existed := bl.banned[ip]
	if existed {
		delete(bl.banned, ip)
	}
	bl.mu.Unlock()
	if existed {
		bl.triggerSave()
	}
	return existed
}

// List returns a snapshot of all current bans.
func (bl *BanList) List() []BanEntry {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	out := make([]BanEntry, 0, len(bl.banned))
	for _, e := range bl.banned {
		out = append(out, e)
	}
	return out
}

// Size returns current ban count.
func (bl *BanList) Size() int {
	bl.mu.RLock()
	defer bl.mu.RUnlock()
	return len(bl.banned)
}

// Close stops the background saver and flushes any pending writes.
func (bl *BanList) Close() error {
	bl.stopOnce.Do(func() {
		close(bl.stopCh)
	})
	return bl.save() // one last synchronous save
}

func (bl *BanList) triggerSave() {
	select {
	case bl.saveCh <- struct{}{}:
	default: // already queued
	}
}

func (bl *BanList) saveLoop() {
	// Coalesce saves on 1-second granularity so bursts of ban/unban
	// don't hammer the disk.
	var pending bool
	t := time.NewTimer(time.Hour) // idle forever until first signal
	t.Stop()
	for {
		select {
		case <-bl.stopCh:
			return
		case <-bl.saveCh:
			if !pending {
				pending = true
				t.Reset(1 * time.Second)
			}
		case <-t.C:
			if pending {
				_ = bl.save()
				pending = false
			}
		}
	}
}

func (bl *BanList) save() error {
	bl.mu.RLock()
	entries := make([]BanEntry, 0, len(bl.banned))
	for _, e := range bl.banned {
		entries = append(entries, e)
	}
	bl.mu.RUnlock()

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: temp file in same dir, then rename.
	dir := filepath.Dir(bl.file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".banlist-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, bl.file)
}

func (bl *BanList) load() error {
	data, err := os.ReadFile(bl.file)
	if err != nil {
		return err
	}
	var entries []BanEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse banlist: %w", err)
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()
	for _, e := range entries {
		// Skip entries that have expired
		if !e.ExpiresAt.IsZero() && time.Now().After(e.ExpiresAt) {
			continue
		}
		bl.banned[e.IP] = e
	}
	return nil
}
