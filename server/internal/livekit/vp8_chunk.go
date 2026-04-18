package livekit

import (
	"github.com/cyberverse/server/internal/mediapeer"
)

// Type aliases for backward compatibility — all code referencing
// livekit.RawAVSegment / livekit.AVSegment continues to compile.
type RawAVSegment = mediapeer.RawAVSegment
type AVSegment = mediapeer.AVSegment

// Re-export encoding functions under their original names.
var EncodeRGBChunkToVP8Samples = mediapeer.EncodeRGBChunkToVP8Samples

// encodeRGBChunkToVP8Samples is the package-private alias used by Bot.
var encodeRGBChunkToVP8Samples = mediapeer.EncodeRGBChunkToVP8Samples

// audioPCMSlicesForVideoFrames splits 16-bit mono PCM into n contiguous slices.
func audioPCMSlicesForVideoFrames(pcm []byte, n int) [][]byte {
	if n <= 0 || len(pcm) == 0 {
		return nil
	}
	total := len(pcm) / 2
	out := make([][]byte, n)
	for i := 0; i < n; i++ {
		s0 := (total * i) / n
		s1 := (total * (i + 1)) / n
		out[i] = pcm[s0*2 : s1*2]
	}
	return out
}
