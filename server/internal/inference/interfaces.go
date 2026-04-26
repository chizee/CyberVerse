package inference

import (
	"context"

	pb "github.com/cyberverse/server/internal/pb"
)

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string
	Content string
}

// LLMConfig holds parameters for LLM generation.
type LLMConfig struct {
	Model       string
	Temperature float32
	MaxTokens   int32
}

// VoiceLLMSessionConfig holds per-session character config for VoiceLLM.
type VoiceLLMSessionConfig struct {
	SessionID      string
	SystemPrompt   string
	Voice          string // maps to voice_type / speaker
	BotName        string
	SpeakingStyle  string
	WelcomeMessage string
}

// InferenceService defines the interface for communicating with the Python
// inference layer. Using an interface allows tests to inject mocks.
type InferenceService interface {
	HealthCheck(ctx context.Context) error
	AvatarInfo(ctx context.Context) (*pb.AvatarInfo, error)

	// Avatar
	SetAvatar(ctx context.Context, sessionID string, imageData []byte, format string) error
	GenerateAvatarStream(ctx context.Context, audioCh <-chan *pb.AudioChunk) (<-chan *pb.VideoChunk, <-chan error)
	GenerateAvatar(ctx context.Context, audioChunks []*pb.AudioChunk) (<-chan *pb.VideoChunk, <-chan error)

	// LLM
	GenerateLLMStream(ctx context.Context, sessionID string, messages []ChatMessage, config LLMConfig) (<-chan *pb.LLMChunk, <-chan error)

	// TTS
	SynthesizeSpeechStream(ctx context.Context, textCh <-chan string) (<-chan *pb.AudioChunk, <-chan error)

	// ASR
	TranscribeStream(ctx context.Context, audioCh <-chan []byte) (<-chan *pb.TranscriptEvent, <-chan error)

	// VoiceLLM
	ConverseStream(ctx context.Context, audioCh <-chan []byte, config VoiceLLMSessionConfig) (<-chan *pb.VoiceLLMOutput, <-chan error)
	Interrupt(ctx context.Context, sessionID string) error

	Close() error
}
