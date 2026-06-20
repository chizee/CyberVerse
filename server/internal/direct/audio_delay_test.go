package direct

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/mediapeer"
)

func testPCM(samples int, start int) []byte {
	pcm := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(start+i+1))
	}
	return pcm
}

func TestCurrentVideoBitrateKbpsUsesGCCBudget(t *testing.T) {
	t.Parallel()
	p := &DirectPeer{}

	if got := p.currentVideoBitrateKbps(); got != defaultDirectVideoBitrateKbps {
		t.Fatalf("default bitrate=%d want %d", got, defaultDirectVideoBitrateKbps)
	}

	p.targetBitrateBps.Store(675_000)
	if got := p.currentVideoBitrateKbps(); got != minDirectVideoBitrateKbps {
		t.Fatalf("low GCC bitrate=%d want %d", got, minDirectVideoBitrateKbps)
	}

	p.targetBitrateBps.Store(2_000_000)
	if got := p.currentVideoBitrateKbps(); got != 1300 {
		t.Fatalf("scaled bitrate=%d want 1300", got)
	}

	p.targetBitrateBps.Store(4_000_000)
	if got := p.currentVideoBitrateKbps(); got != maxDirectVideoBitrateKbps {
		t.Fatalf("capped bitrate=%d want %d", got, maxDirectVideoBitrateKbps)
	}
}

func TestApplyAudioDelayInsertsSilenceAndPreservesLength(t *testing.T) {
	t.Parallel()
	p := &DirectPeer{}

	const sampleRate = 1000
	pcm := testPCM(200, 0)
	_ = p.applyAudioDelay(1, pcm, sampleRate)
	p.HandleAVSyncFeedback(1, 200, 180, "video_late_audio_leads")

	out := p.applyAudioDelay(1, pcm, sampleRate)
	if len(out) != len(pcm) {
		t.Fatalf("len=%d want %d", len(out), len(pcm))
	}

	delayBytes := audioDelayPCMBytes(audioDelayStepMS, sampleRate)
	if !bytes.Equal(out[:delayBytes], make([]byte, delayBytes)) {
		t.Fatalf("first %d bytes should be silence", delayBytes)
	}
	if !bytes.Equal(out[delayBytes:], pcm[:len(pcm)-delayBytes]) {
		t.Fatalf("audio content was not shifted by %d bytes", delayBytes)
	}
}

func TestApplyAudioDelayCarriesDelayedTail(t *testing.T) {
	t.Parallel()
	p := &DirectPeer{}

	const sampleRate = 1000
	first := testPCM(200, 0)
	second := testPCM(200, 1000)
	_ = p.applyAudioDelay(1, first, sampleRate)
	p.HandleAVSyncFeedback(1, 200, 180, "video_late_audio_leads")

	firstOut := p.applyAudioDelay(1, first, sampleRate)
	_ = firstOut
	secondOut := p.applyAudioDelay(1, second, sampleRate)

	delayBytes := audioDelayPCMBytes(audioDelayStepMS, sampleRate)
	if !bytes.Equal(secondOut[:delayBytes], first[len(first)-delayBytes:]) {
		t.Fatalf("second output does not start with delayed tail")
	}
}

func TestApplyAudioDelayResetsOnEpochChange(t *testing.T) {
	t.Parallel()
	p := &DirectPeer{}

	const sampleRate = 1000
	first := testPCM(200, 0)
	second := testPCM(200, 1000)
	_ = p.applyAudioDelay(1, first, sampleRate)
	p.HandleAVSyncFeedback(1, 200, 180, "video_late_audio_leads")
	_ = p.applyAudioDelay(1, first, sampleRate)

	out := p.applyAudioDelay(2, second, sampleRate)
	if !bytes.Equal(out, second) {
		t.Fatalf("new epoch should reset audio delay")
	}
}

func TestHandleAVSyncFeedbackIgnoresStaleTurn(t *testing.T) {
	t.Parallel()
	p := &DirectPeer{}
	p.AdvancePlaybackEpoch(2)

	const sampleRate = 1000
	pcm := testPCM(200, 0)
	_ = p.applyAudioDelay(2, pcm, sampleRate)
	p.HandleAVSyncFeedback(1, 200, 180, "video_late_audio_leads")

	out := p.applyAudioDelay(2, pcm, sampleRate)
	if !bytes.Equal(out, pcm) {
		t.Fatalf("stale feedback should not delay current epoch audio")
	}
}

func TestHandleAVSyncFeedbackUsesJitterDeltaWithoutPresentationLag(t *testing.T) {
	t.Parallel()
	p := &DirectPeer{}

	const sampleRate = 1000
	pcm := testPCM(600, 0)
	_ = p.applyAudioDelay(1, pcm, sampleRate)
	p.HandleAVSyncFeedback(1, 0, 500, "video_late_audio_leads")

	out := p.applyAudioDelay(1, pcm, sampleRate)
	delayBytes := audioDelayPCMBytes(audioDelayStepMS, sampleRate)
	if !bytes.Equal(out[:delayBytes], make([]byte, delayBytes)) {
		t.Fatalf("expected jitter-only feedback to insert %d bytes of silence", delayBytes)
	}
}

func TestResetRTPGapClockForEpochClearsCrossTurnGap(t *testing.T) {
	t.Parallel()
	p := &DirectPeer{}
	now := time.Now()
	p.lastPublishEpoch = 1
	p.lastVideoWriteTime = now
	p.lastAudioWriteTime = now

	p.resetRTPGapClockForEpoch(2)

	if p.lastPublishEpoch != 2 {
		t.Fatalf("lastPublishEpoch=%d want 2", p.lastPublishEpoch)
	}
	if !p.lastVideoWriteTime.IsZero() || !p.lastAudioWriteTime.IsZero() {
		t.Fatal("expected RTP write times to be reset across turns")
	}
}

func TestResetRTPGapClockForEpochKeepsSameTurnGap(t *testing.T) {
	t.Parallel()
	p := &DirectPeer{}
	now := time.Now()
	p.lastPublishEpoch = 1
	p.lastVideoWriteTime = now
	p.lastAudioWriteTime = now

	p.resetRTPGapClockForEpoch(1)

	if !p.lastVideoWriteTime.Equal(now) || !p.lastAudioWriteTime.Equal(now) {
		t.Fatal("expected RTP write times to be kept within the same turn")
	}
}

func TestPrepareMediaPathResetKeepsUserAudioChannelOpen(t *testing.T) {
	t.Parallel()
	p := NewDirectPeer("session-test", nil, nil, nil, nil)
	oldConnected := p.connected
	p.lastVideoWriteTime = time.Now()
	p.lastAudioWriteTime = time.Now()
	p.lastPublishEpoch = 7
	p.targetBitrateBps.Store(1_200_000)
	p.audioDelayTargetMS = 320
	p.audioDelayCurrentMS = 160
	p.audioDelayPCM = []byte{1, 2, 3}
	p.audioDelaySampleRate = 24000
	p.audioDelayEpoch = 7
	p.audioDelayLastFeedback = time.Now()
	p.opusEncoderSR = 24000

	oldPC := p.prepareMediaPathReset()
	if oldPC != nil {
		t.Fatalf("old pc=%v want nil", oldPC)
	}
	if p.connected == oldConnected {
		t.Fatalf("expected connected channel to be replaced")
	}
	if p.videoTrack != nil || p.audioTrack != nil {
		t.Fatalf("expected media tracks to be cleared")
	}
	if !p.lastVideoWriteTime.IsZero() || !p.lastAudioWriteTime.IsZero() {
		t.Fatalf("expected RTP write timestamps to be reset")
	}
	if p.lastPublishEpoch != 0 {
		t.Fatalf("expected publish epoch to be reset")
	}
	if got := p.targetBitrateBps.Load(); got != 0 {
		t.Fatalf("target bitrate=%d want 0", got)
	}
	if p.opusEncoder != nil || p.opusEncoderSR != 0 {
		t.Fatalf("expected opus encoder state to be reset")
	}
	if p.audioDelayTargetMS != 0 || p.audioDelayCurrentMS != 0 || len(p.audioDelayPCM) != 0 ||
		p.audioDelaySampleRate != 0 || p.audioDelayEpoch != 0 || !p.audioDelayLastFeedback.IsZero() {
		t.Fatalf("expected audio delay state to be reset")
	}

	select {
	case p.userAudioCh <- []byte{42}:
	default:
		t.Fatalf("user audio channel should remain open and writable")
	}
}

func TestSupersedableIdleStalesAfterSpeechEpochAdvances(t *testing.T) {
	t.Parallel()
	p := &DirectPeer{}

	idleBeforeSpeech := &mediapeer.RawAVSegment{Supersedable: true}
	if !p.prepareRawAVSegment(idleBeforeSpeech) {
		t.Fatal("expected idle segment before speech to be accepted")
	}
	if p.isRawAVSegmentStale(idleBeforeSpeech) {
		t.Fatal("idle segment should not be stale before speech is ready")
	}

	speech := &mediapeer.RawAVSegment{Epoch: 3}
	if !p.prepareRawAVSegment(speech) {
		t.Fatal("expected speech segment to be accepted")
	}
	if !p.isRawAVSegmentStale(idleBeforeSpeech) {
		t.Fatal("expected older idle segment to become stale after speech epoch advances")
	}

	idleAfterSpeech := &mediapeer.RawAVSegment{Supersedable: true}
	if !p.prepareRawAVSegment(idleAfterSpeech) {
		t.Fatal("expected idle segment after speech to be accepted")
	}
	if p.isRawAVSegmentStale(idleAfterSpeech) {
		t.Fatal("idle segment queued after the current speech epoch should remain publishable")
	}
}
