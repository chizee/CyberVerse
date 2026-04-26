package direct

import (
	"context"
	"testing"
	"time"
)

func TestWaitConnectedReturnsTrueWhenConnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := &DirectPeer{
		connected:      make(chan struct{}),
		avPipelineCtx:  ctx,
		avPipelineCancel: cancel,
	}
	close(p.connected)

	if ok := p.waitConnected(20 * time.Millisecond); !ok {
		t.Fatal("expected waitConnected to succeed for connected peer")
	}
}

func TestWaitConnectedReturnsFalseWhenTimedOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := &DirectPeer{
		connected:      make(chan struct{}),
		avPipelineCtx:  ctx,
		avPipelineCancel: cancel,
	}

	start := time.Now()
	if ok := p.waitConnected(20 * time.Millisecond); ok {
		t.Fatal("expected waitConnected to time out for disconnected peer")
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("expected waitConnected to wait for timeout, returned after %v", elapsed)
	}
}
