package direct

import "time"

func rtpGapThreshold(frameDur time.Duration) time.Duration {
	threshold := 2 * frameDur
	if threshold < 40*time.Millisecond {
		return 40 * time.Millisecond
	}
	return threshold
}

// rtpGapToSkip advances the RTP clock over real idle gaps between segments.
// Small gaps are left alone so normal scheduling jitter does not inflate media time.
func rtpGapToSkip(wallGap, frameDur time.Duration) time.Duration {
	if wallGap <= rtpGapThreshold(frameDur) {
		return 0
	}
	return wallGap
}
