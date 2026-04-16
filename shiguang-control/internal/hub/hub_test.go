package hub

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHub_BroadcastDelivers(t *testing.T) {
	h := NewHub()
	go h.Run()
	time.Sleep(10 * time.Millisecond)

	// Build fake client with a direct channel (bypass websocket.Conn)
	c := &Client{ID: "c1", Account: "alice", Server: "5.8", send: make(chan []byte, 4), hub: h}
	h.register <- c
	time.Sleep(10 * time.Millisecond)

	h.Broadcast("test", map[string]int{"x": 1})

	select {
	case msg := <-c.send:
		var env Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if env.Type != "test" {
			t.Errorf("type=%s", env.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("no message delivered")
	}
}

func TestHub_KickReturnsFalseForUnknown(t *testing.T) {
	h := NewHub()
	go h.Run()
	time.Sleep(10 * time.Millisecond)
	if h.Kick("nobody", "bye") {
		t.Error("kick should return false for unknown client")
	}
}

func TestHub_KickDelivers(t *testing.T) {
	h := NewHub()
	go h.Run()
	time.Sleep(10 * time.Millisecond)

	c := &Client{ID: "c2", Account: "b", Server: "4.8", send: make(chan []byte, 4), hub: h}
	h.register <- c
	time.Sleep(10 * time.Millisecond)

	if !h.Kick("c2", "admin-kick") {
		t.Error("kick should return true for known client")
	}
	select {
	case msg := <-c.send:
		var env Envelope
		json.Unmarshal(msg, &env)
		if env.Type != "kick" {
			t.Errorf("type=%s want kick", env.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("no kick delivered")
	}
}

func TestHub_ConnectedCount(t *testing.T) {
	h := NewHub()
	go h.Run()
	time.Sleep(10 * time.Millisecond)
	if h.ConnectedCount() != 0 {
		t.Errorf("initial count=%d", h.ConnectedCount())
	}
	for i := 0; i < 5; i++ {
		h.register <- &Client{ID: string(rune('a' + i)), send: make(chan []byte, 1)}
	}
	time.Sleep(20 * time.Millisecond)
	if h.ConnectedCount() != 5 {
		t.Errorf("count=%d want 5", h.ConnectedCount())
	}
}
