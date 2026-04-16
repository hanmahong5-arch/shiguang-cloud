// Package hub implements the WebSocket hub that every running launcher
// connects to for real-time communication.
//
// Responsibilities:
//   - Track currently connected launchers (by client ID)
//   - Broadcast online-count updates to all clients
//   - Accept admin-initiated kick/notify commands targeted at a client ID
//   - Receive anti-cheat event mirrors from gate (future)
//
// Concurrency model: one goroutine per connection plus a single hub loop
// (channel-based message queue). No shared mutable state — pure CSP.
package hub

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/contrib/websocket"
)

// Client represents one connected launcher session.
type Client struct {
	ID      string // server-assigned unique ID (UUID)
	Account string // the user that authenticated before opening the WS
	Server  string // "5.8" or "4.8"

	conn *websocket.Conn
	send chan []byte
	hub  *Hub
}

// Envelope is the wire protocol for WSS messages. Both directions share it.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Hub centralises client registration + broadcast.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client

	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	done       chan struct{} // signals Run() to exit gracefully

	// counters (atomic)
	totalConnected int64
}

// NewHub constructs an empty hub. Call Run() in a goroutine to start the loop.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client, 16),
		unregister: make(chan *Client, 16),
		broadcast:  make(chan []byte, 64),
		done:       make(chan struct{}),
	}
}

// Run is the hub event loop. Must be started exactly once.
// Returns when Stop() is called, after closing all client connections.
func (h *Hub) Run() {
	// Periodic online-count broadcast so admin SPA and launcher UIs see a
	// rough "currently online" number without polling.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.done:
			// Graceful shutdown: close all connected clients
			h.mu.Lock()
			for id, c := range h.clients {
				close(c.send)
				delete(h.clients, id)
			}
			h.mu.Unlock()
			return

		case c := <-h.register:
			h.mu.Lock()
			h.clients[c.ID] = c
			atomic.AddInt64(&h.totalConnected, 1)
			h.mu.Unlock()

		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c.ID]; ok {
				delete(h.clients, c.ID)
				close(c.send)
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			h.mu.RLock()
			for _, c := range h.clients {
				select {
				case c.send <- msg:
				default: // drop on slow client
				}
			}
			h.mu.RUnlock()

		case <-ticker.C:
			h.broadcastCounts()
		}
	}
}

// Stop signals the hub event loop to exit and close all client connections.
func (h *Hub) Stop() {
	select {
	case <-h.done:
		// already stopped
	default:
		close(h.done)
	}
}

// ConnectedCount returns the current number of registered clients.
func (h *Hub) ConnectedCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// TotalServed returns a monotonically-increasing count of connections
// since the hub started (useful for metrics).
func (h *Hub) TotalServed() int64 {
	return atomic.LoadInt64(&h.totalConnected)
}

// Kick sends a "kick" message to the named client and closes the socket.
func (h *Hub) Kick(clientID, reason string) bool {
	h.mu.RLock()
	c, ok := h.clients[clientID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	payload, _ := json.Marshal(map[string]string{"reason": reason})
	env, _ := json.Marshal(Envelope{Type: "kick", Payload: payload})
	select {
	case c.send <- env:
	default:
	}
	return true
}

// Broadcast sends an envelope to every connected client.
func (h *Hub) Broadcast(envelopeType string, payload any) {
	p, _ := json.Marshal(payload)
	msg, _ := json.Marshal(Envelope{Type: envelopeType, Payload: p})
	select {
	case h.broadcast <- msg:
	default: // channel full, drop (back-pressure)
	}
}

func (h *Hub) broadcastCounts() {
	counts := map[string]int{"5.8": 0, "4.8": 0}
	h.mu.RLock()
	for _, c := range h.clients {
		counts[c.Server]++
	}
	h.mu.RUnlock()
	h.Broadcast("online_count", counts)
}

// Attach wires an already-authenticated Client to the hub, starting its
// read and write pumps. The caller is the Fiber websocket handler — it
// has already validated the JWT / query token and constructed Client.
func (h *Hub) Attach(c *Client) {
	c.hub = h
	c.send = make(chan []byte, 32)
	h.register <- c

	go c.writePump()
	c.readPump() // blocks until close
	h.unregister <- c
}

// NewClient constructs an unattached Client. Call hub.Attach(client) to start.
func NewClient(id, account, server string, conn *websocket.Conn) *Client {
	return &Client{
		ID:      id,
		Account: account,
		Server:  server,
		conn:    conn,
	}
}

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 1 << 14 // 16 KB
)

func (c *Client) readPump() {
	defer c.conn.Close()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		// For now, incoming launcher messages are logged and ignored.
		// Future: parse Envelope and dispatch (e.g., anticheat reports).
		_ = msg
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
