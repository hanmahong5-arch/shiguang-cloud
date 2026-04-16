// Package control implements the launcher's HTTP + WebSocket client against
// shiguang-control. Responsibilities:
//   - POST /api/account/* for register/login/change/reset
//   - GET /api/launcher/config for the operator's hot-edit config
//   - Maintain a long-lived WebSocket to /ws with exponential-backoff reconnect
package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/shiguang/shared/tenant"
)

// Client is the stateful client used by Wails bindings.
type Client struct {
	baseURL string
	http    *http.Client

	mu       sync.Mutex
	account  string
	server   string
	wsConn   *websocket.Conn
	wsStop   chan struct{}
	wsOpened bool

	// eventHandler is invoked for every received WSS envelope (account must
	// be logged in). Typically set by the Wails App to emit runtime events.
	eventHandler func(envelopeType string, payload json.RawMessage)
}

// NewClient builds an unauthenticated client against the given control URL
// (e.g. "https://control.example.com:10443").
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// SetEventHandler installs the callback for WSS-delivered events.
// Must be called before ConnectWS.
func (c *Client) SetEventHandler(fn func(string, json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eventHandler = fn
}

// LoginResult is what /api/account/login returns.
type LoginResult struct {
	OK           bool   `json:"ok"`
	SessionToken string `json:"session_token"`
}

// LauncherConfig is an alias for the shared wire type.
type LauncherConfig = tenant.LauncherWireConfig

// ServerLine is an alias for the shared wire type.
type ServerLine = tenant.ServerLineInfo

// ---- REST methods ----

func (c *Client) post(ctx context.Context, path string, body any) ([]byte, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return raw, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

// Register creates an account on the chosen server line.
func (c *Client) Register(ctx context.Context, server, name, password, email string) error {
	_, err := c.post(ctx, "/api/account/register", map[string]string{
		"server": server, "name": name, "password": password, "email": email,
	})
	return err
}

// Login verifies credentials. On success, caches account + server and
// returns the session token for Token Handoff.
func (c *Client) Login(ctx context.Context, server, name, password string) (string, error) {
	raw, err := c.post(ctx, "/api/account/login", map[string]string{
		"server": server, "name": name, "password": password,
	})
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.account = name
	c.server = server
	c.mu.Unlock()

	var result LoginResult
	if e := json.Unmarshal(raw, &result); e != nil {
		return "", nil // non-fatal: token extraction failed
	}
	return result.SessionToken, nil
}

// ChangePassword updates password on the server line.
func (c *Client) ChangePassword(ctx context.Context, server, name, oldPw, newPw string) error {
	_, err := c.post(ctx, "/api/account/change_password", map[string]string{
		"server": server, "name": name, "old_password": oldPw, "new_password": newPw,
	})
	return err
}

// ResetPassword sends a reset request; returns the new plain-text password.
func (c *Client) ResetPassword(ctx context.Context, server, name, email string) (string, error) {
	raw, err := c.post(ctx, "/api/account/reset_password", map[string]string{
		"server": server, "name": name, "email": email,
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		OK          bool   `json:"ok"`
		NewPassword string `json:"new_password"`
	}
	if e := json.Unmarshal(raw, &resp); e != nil {
		return "", e
	}
	return resp.NewPassword, nil
}

// FetchLauncherConfig gets the hot-edited launcher config from control.
func (c *Client) FetchLauncherConfig(ctx context.Context) (*LauncherConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/launcher/config", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var cfg LauncherConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ---- WebSocket with reconnect ----

// ConnectWS starts a background goroutine that maintains a WebSocket
// connection with exponential backoff (1s → 30s). Must be called after
// successful Login(). Events arrive at the configured eventHandler.
func (c *Client) ConnectWS() error {
	c.mu.Lock()
	if c.account == "" || c.server == "" {
		c.mu.Unlock()
		return errors.New("must Login before ConnectWS")
	}
	if c.wsOpened {
		c.mu.Unlock()
		return nil
	}
	c.wsOpened = true
	c.wsStop = make(chan struct{})
	c.mu.Unlock()

	go c.wsLoop()
	return nil
}

// DisconnectWS closes the WebSocket loop cleanly.
func (c *Client) DisconnectWS() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wsStop != nil {
		close(c.wsStop)
		c.wsStop = nil
	}
	if c.wsConn != nil {
		c.wsConn.Close()
	}
	c.wsOpened = false
}

func (c *Client) wsLoop() {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-c.wsStop:
			return
		default:
		}

		if err := c.wsConnect(); err != nil {
			// Sleep and retry
			select {
			case <-c.wsStop:
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = 1 * time.Second // reset after successful connect

		// Read loop until error
		c.wsReadLoop()
	}
}

func (c *Client) wsConnect() error {
	c.mu.Lock()
	account, server, baseURL := c.account, c.server, c.baseURL
	handler := c.eventHandler
	c.mu.Unlock()
	_ = handler

	// ws(s)://host/ws?account=X&server=Y
	u, err := url.Parse(baseURL)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/ws"
	q := u.Query()
	q.Set("account", account)
	q.Set("server", server)
	u.RawQuery = q.Encode()

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.wsConn = conn
	c.mu.Unlock()
	return nil
}

func (c *Client) wsReadLoop() {
	defer func() {
		c.mu.Lock()
		if c.wsConn != nil {
			c.wsConn.Close()
			c.wsConn = nil
		}
		c.mu.Unlock()
	}()

	for {
		c.mu.Lock()
		conn := c.wsConn
		handler := c.eventHandler
		c.mu.Unlock()
		if conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var env struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		if handler != nil {
			handler(env.Type, env.Payload)
		}
	}
}
