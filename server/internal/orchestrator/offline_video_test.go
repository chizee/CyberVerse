package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/cyberverse/server/internal/inference"
	pb "github.com/cyberverse/server/internal/pb"
)

func TestSplitOfflineTTSTextKeepsFlashTalkEnding(t *testing.T) {
	text := "事情的转机在 2 月份，机缘巧合之下我发现了一个开源数字人模型——FlashTalk。这是一个音频驱动的数字人模型，这个模型最吸引人的地方是它做到了比主流数字人模型更好的效果，同时还能够进行实时推理。但这是有代价的，想要做到实时推理，需要 5 块 H200 显卡。巧的是，我当时恰好有一个能借到 H200 显卡的朋友。于是乎我花了一段时间去研究这个模型，我逐渐意识到我的愿望说不定真的能实现。"

	segments := splitOfflineTTSText(text)
	if len(segments) < 2 {
		t.Fatalf("expected long text to be split, got %v", segments)
	}
	joined := strings.Join(segments, "")
	if joined != text {
		t.Fatalf("expected segments to preserve text, got %q", joined)
	}
	last := segments[len(segments)-1]
	if !strings.Contains(last, "我的愿望说不定真的能实现。") {
		t.Fatalf("expected final segment to keep ending, got %q", last)
	}
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			t.Fatalf("expected no empty segment, got %v", segments)
		}
		if len([]rune(segment)) > offlineTTSTextSegmentSoftLimit {
			t.Fatalf("segment exceeds soft limit: %d %q", len([]rune(segment)), segment)
		}
	}
}

func TestSplitOfflineTTSTextFallsBackToHardLimit(t *testing.T) {
	text := strings.Repeat("长", offlineTTSTextSegmentSoftLimit*2+7)

	segments := splitOfflineTTSText(text)
	if len(segments) != 3 {
		t.Fatalf("expected hard split into 3 segments, got %d: %v", len(segments), segments)
	}
	if strings.Join(segments, "") != text {
		t.Fatalf("expected hard split to preserve text")
	}
	for _, segment := range segments {
		if len([]rune(segment)) > offlineTTSTextSegmentSoftLimit {
			t.Fatalf("segment exceeds hard limit: %d", len([]rune(segment)))
		}
	}
}

func TestSynthesizeOfflineTextCallsTTSOncePerSegment(t *testing.T) {
	text := "事情的转机在 2 月份，机缘巧合之下我发现了一个开源数字人模型——FlashTalk。这是一个音频驱动的数字人模型，这个模型最吸引人的地方是它做到了比主流数字人模型更好的效果，同时还能够进行实时推理。但这是有代价的，想要做到实时推理，需要 5 块 H200 显卡。巧的是，我当时恰好有一个能借到 H200 显卡的朋友。于是乎我花了一段时间去研究这个模型，我逐渐意识到我的愿望说不定真的能实现。"
	segments := splitOfflineTTSText(text)
	stub := &offlineTextTTSInferenceStub{}
	o := &Orchestrator{inference: stub}

	chunks, pcm, sampleRate, err := o.synthesizeOfflineText(context.Background(), text, inference.TTSConfig{})
	if err != nil {
		t.Fatalf("synthesizeOfflineText returned error: %v", err)
	}
	if sampleRate != 16000 {
		t.Fatalf("expected sample rate 16000, got %d", sampleRate)
	}
	if len(stub.calls) != len(segments) {
		t.Fatalf("expected %d independent TTS calls, got %d: %v", len(segments), len(stub.calls), stub.calls)
	}
	for i, segment := range segments {
		if stub.calls[i] != segment {
			t.Fatalf("call %d text mismatch: got %q want %q", i, stub.calls[i], segment)
		}
	}
	if !strings.Contains(stub.calls[len(stub.calls)-1], "我的愿望说不定真的能实现。") {
		t.Fatalf("expected final TTS call to include ending, got %q", stub.calls[len(stub.calls)-1])
	}
	if len(chunks) != len(segments) {
		t.Fatalf("expected one audio chunk per segment, got %d", len(chunks))
	}
	if len(pcm) != len(segments)*2 {
		t.Fatalf("expected concatenated pcm bytes for every segment, got %d", len(pcm))
	}
	for i, chunk := range chunks {
		wantFinal := i == len(chunks)-1
		if chunk.IsFinal != wantFinal {
			t.Fatalf("chunk %d final flag mismatch: got %v want %v", i, chunk.IsFinal, wantFinal)
		}
	}
}

type offlineTextTTSInferenceStub struct {
	calls []string
}

func (f *offlineTextTTSInferenceStub) HealthCheck(context.Context) error {
	return nil
}

func (f *offlineTextTTSInferenceStub) AvatarInfo(context.Context) (*pb.AvatarInfo, error) {
	return nil, nil
}

func (f *offlineTextTTSInferenceStub) SetAvatar(context.Context, string, []byte, string) error {
	return nil
}

func (f *offlineTextTTSInferenceStub) GenerateAvatarStream(context.Context, <-chan *pb.AudioChunk) (<-chan *pb.VideoChunk, <-chan error) {
	return nil, nil
}

func (f *offlineTextTTSInferenceStub) GenerateAvatar(context.Context, []*pb.AudioChunk) (<-chan *pb.VideoChunk, <-chan error) {
	return nil, nil
}

func (f *offlineTextTTSInferenceStub) GenerateLLMStream(context.Context, string, []inference.ChatMessage, inference.LLMConfig) (<-chan *pb.LLMChunk, <-chan error) {
	return nil, nil
}

func (f *offlineTextTTSInferenceStub) SynthesizeSpeechStream(_ context.Context, textCh <-chan string, _ inference.TTSConfig) (<-chan *pb.AudioChunk, <-chan error) {
	audioCh := make(chan *pb.AudioChunk, 1)
	errCh := make(chan error)
	text := ""
	for chunk := range textCh {
		text += chunk
	}
	f.calls = append(f.calls, text)
	audioCh <- &pb.AudioChunk{
		Data:       []byte{byte(len([]rune(text))), 0},
		SampleRate: 16000,
		Channels:   1,
		Format:     "pcm_s16le",
		IsFinal:    true,
	}
	close(audioCh)
	close(errCh)
	return audioCh, errCh
}

func (f *offlineTextTTSInferenceStub) TranscribeStream(context.Context, <-chan []byte, inference.ASRConfig) (<-chan *pb.TranscriptEvent, <-chan error) {
	return nil, nil
}

func (f *offlineTextTTSInferenceStub) CheckVoice(context.Context, inference.VoiceLLMSessionConfig) (string, error) {
	return "", nil
}

func (f *offlineTextTTSInferenceStub) ConverseStream(context.Context, <-chan inference.VoiceLLMInputEvent, inference.VoiceLLMSessionConfig) (<-chan *pb.VoiceLLMOutput, <-chan error) {
	return nil, nil
}

func (f *offlineTextTTSInferenceStub) Interrupt(context.Context, string) error {
	return nil
}

func (f *offlineTextTTSInferenceStub) Close() error {
	return nil
}
