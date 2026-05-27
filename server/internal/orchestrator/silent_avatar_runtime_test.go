package orchestrator

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/pb"
)

func testPCM16(samples int, value int16) []byte {
	out := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(value))
	}
	return out
}

func TestSilentAvatarTimelineKeepsQueuedIdleBeforeSpeech(t *testing.T) {
	var timeline silentAvatarTimeline
	idleSamples := silentAvatarSampleRate / 10
	speechSamples := silentAvatarSampleRate / 10
	idlePlayback := make([]byte, idleSamples*2)
	speechPlayback := testPCM16(speechSamples, 1200)

	timeline.append(silentAvatarSpanIdle, 0, testPCM16(idleSamples, 100), idlePlayback, false)
	timeline.append(silentAvatarSpanSpeech, 7, speechPlayback, speechPlayback, false)

	pcm, meta := timeline.take(4, 20)
	if !meta.hasSpeech || meta.turnSeq != 7 {
		t.Fatalf("expected segment to include speech turn 7, got %+v", meta)
	}
	if len(pcm) != (idleSamples+speechSamples)*2 {
		t.Fatalf("expected 200ms pcm, got %d bytes", len(pcm))
	}
	if !bytes.Equal(pcm[:len(idlePlayback)], idlePlayback) {
		t.Fatal("expected queued idle playback to remain at segment head")
	}
	if !bytes.Equal(pcm[len(idlePlayback):], speechPlayback) {
		t.Fatal("expected speech playback to follow queued idle")
	}
}

func TestSilentAvatarTimelineLeadDropsAfterVideoTake(t *testing.T) {
	var timeline silentAvatarTimeline
	samples := silentAvatarSampleRate / 10
	pcm := testPCM16(samples, 500)
	timeline.append(silentAvatarSpanIdle, 0, pcm, make([]byte, len(pcm)), false)
	if got := timeline.leadSamples(); got != int64(samples) {
		t.Fatalf("expected lead %d, got %d", samples, got)
	}
	timeline.take(2, 20)
	if got := timeline.leadSamples(); got != 0 {
		t.Fatalf("expected lead to drain to zero, got %d", got)
	}
}

func TestSilentAvatarTimelineReportsFinalTurnWhenConsumed(t *testing.T) {
	var timeline silentAvatarTimeline
	samples := silentAvatarSampleRate / 10
	pcm := testPCM16(samples, 700)

	timeline.append(silentAvatarSpanSpeech, 7, pcm, pcm, true)

	_, meta := timeline.take(2, 20)
	if len(meta.finalTurnSeqs) != 1 || meta.finalTurnSeqs[0] != 7 {
		t.Fatalf("expected final turn 7, got %+v", meta.finalTurnSeqs)
	}
}

func TestSilentAvatarTimelineUsesNewestSpeechTurnInMixedSegment(t *testing.T) {
	var timeline silentAvatarTimeline
	samples := silentAvatarSampleRate / 10
	oldFinal := make([]byte, samples*2)
	newSpeech := testPCM16(samples, 900)

	timeline.append(silentAvatarSpanSpeech, 2, oldFinal, oldFinal, true)
	timeline.append(silentAvatarSpanSpeech, 3, newSpeech, newSpeech, false)

	_, meta := timeline.take(4, 20)
	if meta.turnSeq != 3 {
		t.Fatalf("expected newest speech turn 3, got %+v", meta)
	}
	if len(meta.finalTurnSeqs) != 1 || meta.finalTurnSeqs[0] != 2 {
		t.Fatalf("expected final turn 2, got %+v", meta.finalTurnSeqs)
	}
}

func TestSilentAvatarFinalItemDoesNotCloseContinuousModelStream(t *testing.T) {
	runtime := newSilentAvatarRuntime(t.Context(), nil, "session-test", time.Second)
	gotCh := make(chan *pb.AudioChunk, 1)
	go func() {
		gotCh <- <-runtime.modelAudioCh
	}()

	pcm := testPCM16(silentAvatarSampleRate/10, 300)
	ok := runtime.sendAudioItem(silentAvatarAudioItem{
		kind:        silentAvatarSpanSpeech,
		turnSeq:     8,
		modelPCM:    pcm,
		playbackPCM: pcm,
		isFinal:     true,
	})
	if !ok {
		t.Fatal("expected final item to be sent")
	}
	got := <-gotCh
	if got.GetIsFinal() {
		t.Fatal("expected continuous model stream chunk to keep is_final=false")
	}
}

func TestSilentAvatarMaxLeadUsesRealFrameDuration(t *testing.T) {
	info := &pb.AvatarInfo{
		OutputFps:      20,
		FramesPerChunk: 28,
		ChunkDurationS: 1.12,
	}
	got := silentAvatarMaxLead(info)
	want := 1400 * time.Millisecond
	if got != want {
		t.Fatalf("expected max lead %v, got %v", want, got)
	}
}

func TestSilentAvatarSubmitSpeechKeepsIdleBeforeFirstVideo(t *testing.T) {
	runtime := newSilentAvatarRuntime(t.Context(), nil, "session-test", time.Second)
	speech, err := runtime.beginSpeech(3, silentAvatarSpeechCallbacks{})
	if err != nil {
		t.Fatalf("begin speech: %v", err)
	}
	if !runtime.shouldSendIdle() {
		t.Fatal("expected idle filler to run before speech audio arrives")
	}

	chunk := &pb.AudioChunk{
		Data:       testPCM16(silentAvatarSampleRate/10, 900),
		SampleRate: silentAvatarSampleRate,
		Channels:   1,
		Format:     "pcm_s16le",
	}
	if err := runtime.submitSpeech(t.Context(), speech, chunk); err != nil {
		t.Fatalf("submit speech: %v", err)
	}
	if !runtime.shouldSendIdle() {
		t.Fatal("expected idle filler to continue before first speech video")
	}
}

func TestSilentAvatarFirstSpeechVideoStopsIdleFiller(t *testing.T) {
	runtime := newSilentAvatarRuntime(t.Context(), nil, "session-test", time.Second)
	speech, err := runtime.beginSpeech(4, silentAvatarSpeechCallbacks{})
	if err != nil {
		t.Fatalf("begin speech: %v", err)
	}
	runtime.markSpeechActive(speech)
	runtime.stateMu.Lock()
	speech.firstVideo = true
	runtime.stateMu.Unlock()
	if runtime.shouldSendIdle() {
		t.Fatal("expected active speech video to block idle filler")
	}
}

func TestSilentAvatarFinalSpeechAllowsIdleFiller(t *testing.T) {
	runtime := newSilentAvatarRuntime(t.Context(), nil, "session-test", time.Second)
	speech, err := runtime.beginSpeech(6, silentAvatarSpeechCallbacks{})
	if err != nil {
		t.Fatalf("begin speech: %v", err)
	}
	runtime.markSpeechActive(speech)
	runtime.stateMu.Lock()
	speech.firstVideo = true
	runtime.stateMu.Unlock()
	if runtime.shouldSendIdle() {
		t.Fatal("expected active speech to block idle filler")
	}

	runtime.stateMu.Lock()
	runtime.finalSpeech = speech
	runtime.stateMu.Unlock()
	if !runtime.shouldSendIdle() {
		t.Fatal("expected final speech tail to allow idle filler")
	}
}
