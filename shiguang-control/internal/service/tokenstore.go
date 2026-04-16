// Package service provides the TokenStore for session token handoff.
//
// Token Handoff flow:
//   1. Launcher → POST /api/account/login → gets session_token (UUID)
//   2. Launcher writes token to temp file, starts game client
//   3. Game client's version.dll reads token, sends to auth server
//   4. Auth server → POST /api/token/validate → gets account name
//   5. Auth server creates game session for the validated account
//
// Tokens are single-use with a 5-minute TTL. The store is in-memory since
// Control runs on the same machine as the game server.
package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

const tokenTTL = 5 * time.Minute

// SessionToken represents a one-time auth token.
type SessionToken struct {
	Token   string
	Account string
	Server  string // "5.8" or "4.8"
	Created time.Time
}

const tokenPersistFile = ".sg-tokens.json"

// TokenStore manages session tokens in memory with optional file persistence.
// Tokens survive Control restarts via periodic flush to disk.
type TokenStore struct {
	mu     sync.Mutex
	tokens map[string]*SessionToken
}

// NewTokenStore creates a token store, loads persisted tokens, and starts
// background cleanup + persistence goroutine.
func NewTokenStore() *TokenStore {
	ts := &TokenStore{tokens: make(map[string]*SessionToken)}
	ts.loadFromDisk() // recover tokens from previous run
	go ts.maintenance()
	return ts
}

// Issue creates a new session token for the given account.
// Token is 12 random bytes → 24 hex characters. This ensures
// "SG-" prefix + 24 hex = 27 chars, which fits within the
// AION CM_LOGIN password field limit (32 bytes with -loginex).
func (s *TokenStore) Issue(account, server string) (string, error) {
	b := make([]byte, 12) // 12 bytes = 24 hex chars (96 bits of entropy)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)

	s.mu.Lock()
	s.tokens[token] = &SessionToken{
		Token:   token,
		Account: account,
		Server:  server,
		Created: time.Now(),
	}
	s.mu.Unlock()

	return token, nil
}

// Consume validates and consumes a token. Returns the account name and
// server line if valid, or an error if expired/consumed/not found.
// Tokens are single-use — consumed atomically on successful validation.
func (s *TokenStore) Consume(token string) (account, server string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.tokens[token]
	if !ok {
		return "", "", fmt.Errorf("token not found or already consumed")
	}

	if time.Since(st.Created) > tokenTTL {
		delete(s.tokens, token)
		return "", "", fmt.Errorf("token expired")
	}

	account = st.Account
	server = st.Server
	delete(s.tokens, token) // single-use
	return account, server, nil
}

// maintenance removes expired tokens and persists remaining ones every 10 seconds.
func (s *TokenStore) maintenance() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		changed := false
		for k, v := range s.tokens {
			if time.Since(v.Created) > tokenTTL {
				delete(s.tokens, k)
				changed = true
			}
		}
		if changed || len(s.tokens) > 0 {
			s.flushToDiskLocked()
		}
		s.mu.Unlock()
	}
}

// flushToDiskLocked writes current tokens to disk. Caller must hold s.mu.
func (s *TokenStore) flushToDiskLocked() {
	data, err := json.Marshal(s.tokens)
	if err != nil {
		return
	}
	// Atomic write: temp file + rename
	tmp := tokenPersistFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	os.Rename(tmp, tokenPersistFile)
}

// loadFromDisk recovers tokens persisted before the last shutdown.
func (s *TokenStore) loadFromDisk() {
	data, err := os.ReadFile(tokenPersistFile)
	if err != nil {
		return // no persisted tokens (first run or cleaned up)
	}
	var tokens map[string]*SessionToken
	if err := json.Unmarshal(data, &tokens); err != nil {
		return
	}
	// Only load tokens that haven't expired
	now := time.Now()
	for k, v := range tokens {
		if now.Sub(v.Created) <= tokenTTL {
			s.tokens[k] = v
		}
	}
	// Clean up the persist file after loading
	os.Remove(tokenPersistFile)
}
