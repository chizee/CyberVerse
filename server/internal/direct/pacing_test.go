package direct

import (
	"testing"
	"time"

	"github.com/cyberverse/server/internal/mediapeer"
	"github.com/pion/webrtc/v4/pkg/media"
)

func TestAVSegmentWallDurationUsesExplicitSegmentDuration(t *testing.T) {
	t.Parallel()
	seg := &mediapeer.AVSegment{DurationMS: 1600, VP8Samples: make([]media.Sample, 10)}
	if got := avSegmentWallDuration(seg, 50*time.Millisecond, nil); got != 1600*time.Millisecond {
		t.Fatalf("duration=%v want 1600ms", got)
	}
}

func TestAVSegmentWallDurationFallsBackToLongestTrack(t *testing.T) {
	t.Parallel()
	seg := &mediapeer.AVSegment{VP8Samples: make([]media.Sample, 10)}
	audio := []media.Sample{
		{Duration: 20 * time.Millisecond},
		{Duration: 20 * time.Millisecond},
	}
	if got := avSegmentWallDuration(seg, 50*time.Millisecond, audio); got != 500*time.Millisecond {
		t.Fatalf("duration=%v want 500ms", got)
	}

	audio = make([]media.Sample, 40)
	for i := range audio {
		audio[i].Duration = 20 * time.Millisecond
	}
	if got := avSegmentWallDuration(seg, 50*time.Millisecond, audio); got != 800*time.Millisecond {
		t.Fatalf("duration=%v want 800ms", got)
	}
}
