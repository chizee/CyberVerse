package orchestrator

import (
	"errors"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/ws"
	"github.com/cyberverse/server/internal/xunfeiavatar"
)

func TestHandleXunfeiAvatarAudioErrorStopsSessionAfterReconnectExhausted(t *testing.T) {
	sessionMgr := NewSessionManager(4)
	t.Cleanup(sessionMgr.Stop)
	session, err := sessionMgr.Create("session-xunfei-reconnect", ModeOmni, "")
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	sessionMgr.OnSessionEnd = func(*Session) {
		close(ended)
	}
	orch := New(nil, ws.NewHub(), sessionMgr, nil, nil)

	orch.handleXunfeiAvatarAudioError(session.ID, &xunfeiavatar.ReconnectExhaustedError{
		Attempts: 3,
		Err:      errors.New("dial failed"),
	})

	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for exhausted Xunfei reconnects to stop the session")
	}
	if got := sessionMgr.Count(); got != 0 {
		t.Fatalf("expected session to be removed after reconnect exhaustion, got %d", got)
	}
}
