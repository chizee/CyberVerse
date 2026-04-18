package livekit

import (
	"testing"
)

func TestAudioPCMSlicesForVideoFrames(t *testing.T) {
	pcm := make([]byte, 20) // 10 samples
	for i := range pcm {
		pcm[i] = byte(i + 1)
	}
	slices := audioPCMSlicesForVideoFrames(pcm, 4)
	if len(slices) != 4 {
		t.Fatalf("len=%d want 4", len(slices))
	}
	// 10 samples / 4 -> sample ranges [0,2), [2,5), [5,7), [7,10) -> bytes 4,6,4,6
	wantLens := []int{4, 6, 4, 6}
	for i, w := range wantLens {
		if len(slices[i]) != w {
			t.Fatalf("slice %d len=%d want %d", i, len(slices[i]), w)
		}
	}
}

func TestEncodeRGBChunkToVP8Samples(t *testing.T) {
	w, h, n, fps := 32, 32, 2, 25
	rgb := make([]byte, w*h*3*n)
	samples, err := encodeRGBChunkToVP8Samples(rgb, w, h, n, fps)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) < n {
		t.Fatalf("got %d samples want at least %d", len(samples), n)
	}
}
