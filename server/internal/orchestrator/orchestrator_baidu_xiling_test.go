package orchestrator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/character"
	"github.com/cyberverse/server/internal/inference"
	pb "github.com/cyberverse/server/internal/pb"
	"github.com/cyberverse/server/internal/ws"
)

type baiduXilingInferenceStub struct {
	mu          sync.Mutex
	llmCalls    int
	ttsCalls    int
	avatarCalls int
}

func (f *baiduXilingInferenceStub) HealthCheck(context.Context) error { return nil }

func (f *baiduXilingInferenceStub) AvatarInfo(context.Context) (*pb.AvatarInfo, error) {
	return nil, nil
}

func (f *baiduXilingInferenceStub) SetAvatar(context.Context, string, []byte, string) error {
	return nil
}

func (f *baiduXilingInferenceStub) GenerateAvatarStream(context.Context, <-chan *pb.AudioChunk) (<-chan *pb.VideoChunk, <-chan error) {
	f.mu.Lock()
	f.avatarCalls++
	f.mu.Unlock()
	videoCh := make(chan *pb.VideoChunk)
	errCh := make(chan error)
	close(videoCh)
	close(errCh)
	return videoCh, errCh
}

func (f *baiduXilingInferenceStub) GenerateAvatar(context.Context, []*pb.AudioChunk) (<-chan *pb.VideoChunk, <-chan error) {
	videoCh := make(chan *pb.VideoChunk)
	errCh := make(chan error)
	close(videoCh)
	close(errCh)
	return videoCh, errCh
}

func (f *baiduXilingInferenceStub) GenerateLLMStream(context.Context, string, []inference.ChatMessage, inference.LLMConfig) (<-chan *pb.LLMChunk, <-chan error) {
	f.mu.Lock()
	f.llmCalls++
	f.mu.Unlock()
	ch := make(chan *pb.LLMChunk, 2)
	errCh := make(chan error)
	ch <- &pb.LLMChunk{Token: "百度", AccumulatedText: "百度数字人回答完成。"}
	ch <- &pb.LLMChunk{Token: "数字人回答完成。", AccumulatedText: "百度数字人回答完成。", IsFinal: true}
	close(ch)
	close(errCh)
	return ch, errCh
}

func (f *baiduXilingInferenceStub) SynthesizeSpeechStream(context.Context, <-chan string, inference.TTSConfig) (<-chan *pb.AudioChunk, <-chan error) {
	f.mu.Lock()
	f.ttsCalls++
	f.mu.Unlock()
	ch := make(chan *pb.AudioChunk, 1)
	errCh := make(chan error)
	ch <- &pb.AudioChunk{
		Data:       []byte{0, 0, 1, 0},
		SampleRate: 16000,
		Channels:   1,
		Format:     "pcm",
	}
	close(ch)
	close(errCh)
	return ch, errCh
}

func (f *baiduXilingInferenceStub) TranscribeStream(context.Context, <-chan []byte, inference.ASRConfig) (<-chan *pb.TranscriptEvent, <-chan error) {
	ch := make(chan *pb.TranscriptEvent)
	errCh := make(chan error)
	close(ch)
	close(errCh)
	return ch, errCh
}

func (f *baiduXilingInferenceStub) CheckVoice(context.Context, inference.VoiceLLMSessionConfig) (string, error) {
	return "", nil
}

func (f *baiduXilingInferenceStub) ConverseStream(context.Context, <-chan inference.VoiceLLMInputEvent, inference.VoiceLLMSessionConfig) (<-chan *pb.VoiceLLMOutput, <-chan error) {
	ch := make(chan *pb.VoiceLLMOutput)
	errCh := make(chan error)
	close(ch)
	close(errCh)
	return ch, errCh
}

func (f *baiduXilingInferenceStub) Interrupt(context.Context, string) error { return nil }
func (f *baiduXilingInferenceStub) Close() error                            { return nil }

func (f *baiduXilingInferenceStub) calls() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.llmCalls, f.ttsCalls, f.avatarCalls
}

func TestBaiduXilingTextTurnUsesTTSAndAudioDataPipeline(t *testing.T) {
	root := t.TempDir()
	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{
		Name:          "Baidu Xiling",
		AvatarBackend: character.AvatarBackendBaiduXiling,
		BaiduXiling:   &character.BaiduXiling{FigureID: "figure-1"},
		VoiceType:     "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	sessionMgr := NewSessionManager(4)
	t.Cleanup(sessionMgr.Stop)
	session, err := sessionMgr.Create("session-baidu-xiling", ModeStandard, char.ID)
	if err != nil {
		t.Fatal(err)
	}
	inf := &baiduXilingInferenceStub{}
	hub := ws.NewHub()
	client := &ws.Client{SessionID: session.ID, Send: make(chan []byte, 20)}
	hub.Register(client)
	orch := New(inf, hub, sessionMgr, nil, charStore)
	if err := orch.HandleTextInput(context.Background(), session.ID, "请说一句话"); err != nil {
		t.Fatal(err)
	}
	session.WaitPipelineDone(2 * time.Second)

	llmCalls, ttsCalls, avatarCalls := inf.calls()
	if llmCalls != 1 {
		t.Fatalf("expected one LLM call, got %d", llmCalls)
	}
	if ttsCalls != 1 {
		t.Fatalf("expected Baidu Xiling to reuse local TTS, got %d calls", ttsCalls)
	}
	if avatarCalls != 0 {
		t.Fatalf("expected Baidu Xiling to skip avatar stream, got %d calls", avatarCalls)
	}
	audioEvent := readBaiduXilingAudioEvent(t, client.Send)
	if audioEvent["request_id"] == "" {
		t.Fatalf("expected request_id in Baidu Xiling audio event, got %+v", audioEvent)
	}
	if audioEvent["audio"] == "" {
		t.Fatalf("expected base64 audio in Baidu Xiling audio event, got %+v", audioEvent)
	}
	if audioEvent["first"] != true || audioEvent["last"] != true {
		t.Fatalf("expected single audio chunk to be first and last, got %+v", audioEvent)
	}
	if got := session.GetState(); got != StateListening {
		t.Fatalf("expected session to return to listening, got %s", got)
	}
	history := session.HistorySnapshot()
	if len(history) != 2 {
		t.Fatalf("expected user and assistant messages, got %+v", history)
	}
	if history[0].Role != "user" || history[0].Content != "请说一句话" {
		t.Fatalf("unexpected user message: %+v", history[0])
	}
	if history[1].Role != "assistant" || history[1].Content != "百度数字人回答完成。" {
		t.Fatalf("unexpected assistant message: %+v", history[1])
	}
}

func TestBaiduXilingOmniTurnUsesVoiceLLMAudioDataPipeline(t *testing.T) {
	root := t.TempDir()
	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{
		Name:          "Baidu Xiling Omni",
		AvatarBackend: character.AvatarBackendBaiduXiling,
		BaiduXiling:   &character.BaiduXiling{FigureID: "figure-1"},
		VoiceType:     "Momo",
	})
	if err != nil {
		t.Fatal(err)
	}

	sessionMgr := NewSessionManager(4)
	t.Cleanup(sessionMgr.Stop)
	session, err := sessionMgr.Create("session-baidu-xiling-omni", ModeOmni, char.ID)
	if err != nil {
		t.Fatal(err)
	}

	inf := newVoiceRecordingInferenceStub()
	hub := ws.NewHub()
	client := &ws.Client{SessionID: session.ID, Send: make(chan []byte, 20)}
	hub.Register(client)
	orch := New(inf, hub, sessionMgr, nil, charStore)

	inputCh := make(chan inference.VoiceLLMInputEvent)
	close(inputCh)
	pipelineSeq := session.MarkPipelineRunning()
	go orch.runVoiceLLMPipelineWithConfig(
		context.Background(),
		session,
		session.ID,
		inputCh,
		pipelineSeq,
		0,
		inference.VoiceLLMSessionConfig{SessionID: session.ID},
		false,
	)

	select {
	case <-inf.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for voice stream start")
	}

	inf.outputs <- &pb.VoiceLLMOutput{
		UserTranscript: "你好",
		QuestionId:     "q1",
		ReplyId:        "r1",
	}
	inf.outputs <- &pb.VoiceLLMOutput{
		Transcript: "你好，我是百度数字人。",
		Audio: &pb.AudioChunk{
			Data:       make([]byte, 96000),
			SampleRate: 24000,
			Channels:   1,
			Format:     "pcm",
		},
		IsFinal:    true,
		QuestionId: "q1",
		ReplyId:    "r1",
	}
	close(inf.outputs)
	close(inf.errs)

	firstAudioEvent := readBaiduXilingAudioEvent(t, client.Send)
	secondAudioEvent := readBaiduXilingAudioEvent(t, client.Send)
	if firstAudioEvent["first"] != true || firstAudioEvent["last"] != false {
		t.Fatalf("expected first Baidu audio chunk to open stream, got %+v", firstAudioEvent)
	}
	if secondAudioEvent["first"] != false || secondAudioEvent["last"] != true {
		t.Fatalf("expected second Baidu audio chunk to close stream, got %+v", secondAudioEvent)
	}
	firstPCM := decodeBaiduXilingAudioEvent(t, firstAudioEvent)
	secondPCM := decodeBaiduXilingAudioEvent(t, secondAudioEvent)
	if len(firstPCM) > baiduXilingAudioMaxPCMBytes || len(secondPCM) > baiduXilingAudioMaxPCMBytes {
		t.Fatalf("Baidu audio chunks exceed max PCM bytes: first=%d second=%d", len(firstPCM), len(secondPCM))
	}
	if got := len(firstPCM) + len(secondPCM); got != 64000 {
		t.Fatalf("expected 24kHz input to be resampled to 64,000 bytes at 16kHz, got %d", got)
	}
	select {
	case <-inf.avatarStarted:
		t.Fatal("GenerateAvatarStream should not be called for Baidu Xiling omni output")
	default:
	}

	session.WaitPipelineDone(time.Second)
	if got := session.GetState(); got != StateListening {
		t.Fatalf("expected session to return to listening, got %s", got)
	}
	history := session.HistorySnapshot()
	if len(history) != 2 || history[0].Role != "user" || history[1].Role != "assistant" {
		t.Fatalf("expected user and assistant messages to be saved, got %+v", history)
	}
}

func readBaiduXilingAudioEvent(t *testing.T, ch <-chan []byte) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-ch:
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("invalid websocket JSON: %v", err)
			}
			if payload["type"] == "baidu_xiling_audio" {
				return payload
			}
		case <-deadline:
			t.Fatal("timed out waiting for Baidu Xiling audio event")
		}
	}
}

func decodeBaiduXilingAudioEvent(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	audio, ok := payload["audio"].(string)
	if !ok || audio == "" {
		t.Fatalf("expected base64 audio payload, got %+v", payload)
	}
	data, err := base64.StdEncoding.DecodeString(audio)
	if err != nil {
		t.Fatalf("invalid base64 audio payload: %v", err)
	}
	return data
}

func TestBaiduXilingStandardSessionHydratesDialogContext(t *testing.T) {
	root := t.TempDir()
	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{
		Name:          "Baidu Memory",
		AvatarBackend: character.AvatarBackendBaiduXiling,
		BaiduXiling:   &character.BaiduXiling{FigureID: "figure-1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	startedAt := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	if err := charStore.SaveConversation(char.ID, "previous-session", startedAt, startedAt.Add(time.Minute), []map[string]any{
		{"role": "user", "content": "记住我喜欢蓝色。", "timestamp": startedAt.Format(time.RFC3339)},
		{"role": "assistant", "content": "我记住了，你喜欢蓝色。", "timestamp": startedAt.Add(time.Second).Format(time.RFC3339)},
	}); err != nil {
		t.Fatal(err)
	}

	sessionMgr := NewSessionManager(4)
	t.Cleanup(sessionMgr.Stop)
	session, err := sessionMgr.Create("session-baidu-memory", ModeStandard, char.ID)
	if err != nil {
		t.Fatal(err)
	}
	orch := New(nil, nil, sessionMgr, nil, charStore)
	if err := orch.HydrateVoiceDialogContext(session); err != nil {
		t.Fatal(err)
	}

	context := session.DialogContextSnapshot()
	if len(context) != 2 {
		t.Fatalf("expected one previous user/assistant pair, got %+v", context)
	}
	if context[0].Role != "user" || context[0].Text != "记住我喜欢蓝色。" {
		t.Fatalf("unexpected previous user context: %+v", context[0])
	}
	if context[1].Role != "assistant" || context[1].Text != "我记住了，你喜欢蓝色。" {
		t.Fatalf("unexpected previous assistant context: %+v", context[1])
	}
}
