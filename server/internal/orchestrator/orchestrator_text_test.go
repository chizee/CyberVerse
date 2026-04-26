package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/inference"
	pb "github.com/cyberverse/server/internal/pb"
	"github.com/cyberverse/server/internal/ws"
)

type ctxCapturingInference struct {
	llmCtxErrCh chan error
}

func (f *ctxCapturingInference) HealthCheck(context.Context) error { return nil }

func (f *ctxCapturingInference) AvatarInfo(context.Context) (*pb.AvatarInfo, error) {
	return &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25}, nil
}

func (f *ctxCapturingInference) SetAvatar(context.Context, string, []byte, string) error { return nil }

func (f *ctxCapturingInference) GenerateAvatarStream(context.Context, <-chan *pb.AudioChunk) (<-chan *pb.VideoChunk, <-chan error) {
	videoCh := make(chan *pb.VideoChunk)
	errCh := make(chan error)
	close(videoCh)
	close(errCh)
	return videoCh, errCh
}

func (f *ctxCapturingInference) GenerateAvatar(context.Context, []*pb.AudioChunk) (<-chan *pb.VideoChunk, <-chan error) {
	videoCh := make(chan *pb.VideoChunk)
	errCh := make(chan error)
	close(videoCh)
	close(errCh)
	return videoCh, errCh
}

func (f *ctxCapturingInference) GenerateLLMStream(ctx context.Context, _ string, _ []inference.ChatMessage, _ inference.LLMConfig) (<-chan *pb.LLMChunk, <-chan error) {
	select {
	case f.llmCtxErrCh <- ctx.Err():
	default:
	}
	ch := make(chan *pb.LLMChunk)
	errCh := make(chan error)
	close(ch)
	close(errCh)
	return ch, errCh
}

func (f *ctxCapturingInference) SynthesizeSpeechStream(context.Context, <-chan string) (<-chan *pb.AudioChunk, <-chan error) {
	ch := make(chan *pb.AudioChunk)
	errCh := make(chan error)
	close(ch)
	close(errCh)
	return ch, errCh
}

func (f *ctxCapturingInference) TranscribeStream(context.Context, <-chan []byte) (<-chan *pb.TranscriptEvent, <-chan error) {
	ch := make(chan *pb.TranscriptEvent)
	errCh := make(chan error)
	close(ch)
	close(errCh)
	return ch, errCh
}

func (f *ctxCapturingInference) ConverseStream(context.Context, <-chan []byte, inference.VoiceLLMSessionConfig) (<-chan *pb.VoiceLLMOutput, <-chan error) {
	ch := make(chan *pb.VoiceLLMOutput)
	errCh := make(chan error)
	close(ch)
	close(errCh)
	return ch, errCh
}

func (f *ctxCapturingInference) Interrupt(context.Context, string) error { return nil }
func (f *ctxCapturingInference) Close() error                            { return nil }

func TestHandleTextInputDetachesFromRequestContext(t *testing.T) {
	mgr := NewSessionManager(1)
	defer mgr.Stop()

	inf := &ctxCapturingInference{
		llmCtxErrCh: make(chan error, 1),
	}
	orch := New(inf, ws.NewHub(), mgr, nil, nil)

	session, err := mgr.Create("s1", ModeStandard, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	reqCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := orch.HandleTextInput(reqCtx, session.ID, "hello"); err != nil {
		t.Fatalf("HandleTextInput returned error: %v", err)
	}

	select {
	case ctxErr := <-inf.llmCtxErrCh:
		if ctxErr != nil {
			t.Fatalf("expected detached pipeline context, got canceled context: %v", ctxErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for LLM call")
	}

	session.WaitPipelineDone(2 * time.Second)
}
