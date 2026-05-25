package direct

import (
	"testing"
	"time"
)

func TestRTPGapThresholdUsesFrameDuration(t *testing.T) {
	t.Parallel()
	frameDur := time.Second / 25
	if got := rtpGapThreshold(frameDur); got != 2*frameDur {
		t.Fatalf("expected %v, got %v", 2*frameDur, got)
	}
}

func TestRTPGapToSkip(t *testing.T) {
	t.Parallel()
	frameDur := 50 * time.Millisecond

	if got := rtpGapToSkip(90*time.Millisecond, frameDur); got != 0 {
		t.Fatalf("expected no skip below threshold, got %v", got)
	}
	if got := rtpGapToSkip(500*time.Millisecond, frameDur); got != 500*time.Millisecond {
		t.Fatalf("expected 500ms skip, got %v", got)
	}
	if got := rtpGapToSkip(36*time.Second, frameDur); got != 36*time.Second {
		t.Fatalf("expected full idle gap skip, got %v", got)
	}
}
