package ws

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newTestClient(hub *Hub, sessionID string) *Client {
	return &Client{
		SessionID: sessionID,
		Conn:      &websocket.Conn{}, // nil-safe for non-network tests
		Send:      make(chan []byte, 64),
		hub:       hub,
	}
}

func TestHubRegisterUnregister(t *testing.T) {
	hub := NewHub()
	client := newTestClient(hub, "session-1")

	hub.Register(client)

	hub.mu.RLock()
	clients, ok := hub.clients["session-1"]
	hub.mu.RUnlock()

	if !ok || len(clients) != 1 {
		t.Fatal("expected 1 client registered for session-1")
	}

	hub.Unregister(client)

	hub.mu.RLock()
	_, ok = hub.clients["session-1"]
	hub.mu.RUnlock()

	if ok {
		t.Error("expected session-1 to be removed after last client unregisters")
	}
}

func TestHubBroadcast(t *testing.T) {
	hub := NewHub()
	c1 := newTestClient(hub, "s1")
	c2 := newTestClient(hub, "s1")
	c3 := newTestClient(hub, "s2") // different session

	hub.Register(c1)
	hub.Register(c2)
	hub.Register(c3)

	msg := []byte(`{"type":"test"}`)
	hub.Broadcast("s1", msg)

	// c1 and c2 should receive the message
	select {
	case received := <-c1.Send:
		if string(received) != string(msg) {
			t.Errorf("c1 got %s, want %s", received, msg)
		}
	case <-time.After(time.Second):
		t.Error("c1 did not receive message")
	}

	select {
	case received := <-c2.Send:
		if string(received) != string(msg) {
			t.Errorf("c2 got %s, want %s", received, msg)
		}
	case <-time.After(time.Second):
		t.Error("c2 did not receive message")
	}

	// c3 should not receive the message
	select {
	case <-c3.Send:
		t.Error("c3 should not receive message from different session")
	case <-time.After(50 * time.Millisecond):
		// expected
	}

	// Cleanup
	hub.Unregister(c1)
	hub.Unregister(c2)
	hub.Unregister(c3)
}

func TestHubBroadcastNoSession(t *testing.T) {
	hub := NewHub()
	// Should not panic
	hub.Broadcast("nonexistent", []byte("hello"))
}

func TestHubBroadcastJSON(t *testing.T) {
	hub := NewHub()
	c := newTestClient(hub, "s1")
	hub.Register(c)

	hub.BroadcastJSON("s1", map[string]string{"type": "test", "data": "hello"})

	select {
	case msg := <-c.Send:
		if len(msg) == 0 {
			t.Error("expected non-empty JSON message")
		}
	case <-time.After(time.Second):
		t.Error("did not receive JSON broadcast")
	}

	hub.Unregister(c)
}

func TestHubMultipleSessionsIsolated(t *testing.T) {
	hub := NewHub()
	c1 := newTestClient(hub, "session-a")
	c2 := newTestClient(hub, "session-b")

	hub.Register(c1)
	hub.Register(c2)

	hub.Broadcast("session-a", []byte("for-a"))

	select {
	case <-c1.Send:
		// expected
	case <-time.After(time.Second):
		t.Error("c1 should receive message")
	}

	select {
	case <-c2.Send:
		t.Error("c2 should not receive message for session-a")
	case <-time.After(50 * time.Millisecond):
		// expected
	}

	hub.Unregister(c1)
	hub.Unregister(c2)
}
