package orchestrator

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyberverse/server/internal/inference"
	pb "github.com/cyberverse/server/internal/pb"
	"github.com/cyberverse/server/internal/recording"
)

const offlineTTSTextSegmentSoftLimit = 120

type OfflineAudioInput struct {
	PCM16      []byte
	SampleRate int
}

type OfflineVideoGenerateInput struct {
	JobID       string
	CharacterID string
	ImageData   []byte
	ImageFormat string
	Text        string
	Audio       *OfflineAudioInput
	TTSConfig   inference.TTSConfig
	OutputPath  string
	CRF         int
	OnProgress  func(stage string, progress int, message string)
}

type OfflineVideoGenerateResult struct {
	VideoPath       string
	Width           int
	Height          int
	FPS             int
	FrameCount      int
	AudioSampleRate int
}

type OfflineAudioGenerateInput struct {
	Text       string
	Audio      *OfflineAudioInput
	TTSConfig  inference.TTSConfig
	OutputPath string
	OnProgress func(stage string, progress int, message string)
}

type OfflineAudioGenerateResult struct {
	AudioPath  string
	SampleRate int
}

func (o *Orchestrator) emitOfflineProgress(in OfflineVideoGenerateInput, stage string, progress int, message string) {
	if in.OnProgress != nil {
		in.OnProgress(stage, progress, message)
	}
}

func (o *Orchestrator) GenerateOfflineAudio(ctx context.Context, in OfflineAudioGenerateInput) (*OfflineAudioGenerateResult, error) {
	if o == nil || o.inference == nil {
		return nil, errors.New("inference service is not configured")
	}
	if strings.TrimSpace(in.OutputPath) == "" {
		return nil, errors.New("output path is required")
	}
	if strings.TrimSpace(in.Text) == "" && (in.Audio == nil || len(in.Audio.PCM16) == 0) {
		return nil, errors.New("text or audio input is required")
	}
	if in.OnProgress != nil {
		in.OnProgress("audio", 8, "Preparing driving audio")
	}
	_, pcm, sampleRate, err := o.offlineAudioChunks(ctx, OfflineVideoGenerateInput{
		Text:      in.Text,
		Audio:     in.Audio,
		TTSConfig: in.TTSConfig,
	})
	if err != nil {
		return nil, err
	}
	if len(pcm) == 0 {
		return nil, errors.New("no usable audio was produced")
	}
	if err := writePCM16MonoWAV(in.OutputPath, pcm, sampleRate); err != nil {
		return nil, err
	}
	return &OfflineAudioGenerateResult{AudioPath: in.OutputPath, SampleRate: sampleRate}, nil
}

func (o *Orchestrator) GenerateOfflineVideo(ctx context.Context, in OfflineVideoGenerateInput) (*OfflineVideoGenerateResult, error) {
	if o == nil || o.inference == nil {
		return nil, errors.New("inference service is not configured")
	}
	if strings.TrimSpace(in.JobID) == "" {
		return nil, errors.New("offline video job id is required")
	}
	if len(in.ImageData) == 0 {
		return nil, errors.New("avatar image is required")
	}
	if strings.TrimSpace(in.OutputPath) == "" {
		return nil, errors.New("output path is required")
	}
	if strings.TrimSpace(in.Text) == "" && (in.Audio == nil || len(in.Audio.PCM16) == 0) {
		return nil, errors.New("text or audio input is required")
	}

	o.avatarMu.Lock()
	defer o.avatarMu.Unlock()

	o.emitOfflineProgress(in, "avatar", 8, "Preparing avatar image")
	if err := o.inference.SetAvatar(ctx, in.JobID, in.ImageData, in.ImageFormat); err != nil {
		return nil, err
	}

	o.emitOfflineProgress(in, "audio", 18, "Preparing driving audio")
	audioChunks, pcm, sampleRate, err := o.offlineAudioChunks(ctx, in)
	if err != nil {
		return nil, err
	}
	if len(audioChunks) == 0 || len(pcm) == 0 {
		return nil, errors.New("no usable audio was produced")
	}

	o.emitOfflineProgress(in, "video", 45, "Generating avatar frames")
	videoCh, errCh := o.inference.GenerateAvatar(ctx, audioChunks)
	var rgbChunks [][]byte
	width := 0
	height := 0
	fps := 0
	frameCount := 0
	for chunk := range videoCh {
		if chunk == nil || len(chunk.Data) == 0 {
			continue
		}
		if width == 0 {
			width = int(chunk.Width)
			height = int(chunk.Height)
			fps = int(chunk.Fps)
		}
		if int(chunk.Width) != width || int(chunk.Height) != height {
			return nil, fmt.Errorf("avatar output dimensions changed from %dx%d to %dx%d", width, height, chunk.Width, chunk.Height)
		}
		if int(chunk.Fps) > 0 {
			fps = int(chunk.Fps)
		}
		frameCount += int(chunk.NumFrames)
		rgb := append([]byte(nil), chunk.Data...)
		rgbChunks = append(rgbChunks, rgb)
	}
	if err := <-errCh; err != nil {
		return nil, err
	}
	if width <= 0 || height <= 0 || fps <= 0 || len(rgbChunks) == 0 {
		return nil, errors.New("avatar generation did not return video frames")
	}

	o.emitOfflineProgress(in, "encoding", 88, "Encoding MP4")
	if err := recording.EncodeRGB24ToMP4(in.OutputPath, width, height, fps, rgbChunks, pcm, sampleRate, in.CRF); err != nil {
		return nil, err
	}
	o.emitOfflineProgress(in, "completed", 100, "Offline video is ready")
	return &OfflineVideoGenerateResult{
		VideoPath:       in.OutputPath,
		Width:           width,
		Height:          height,
		FPS:             fps,
		FrameCount:      frameCount,
		AudioSampleRate: sampleRate,
	}, nil
}

func (o *Orchestrator) offlineAudioChunks(ctx context.Context, in OfflineVideoGenerateInput) ([]*pb.AudioChunk, []byte, int, error) {
	if strings.TrimSpace(in.Text) != "" {
		return o.synthesizeOfflineText(ctx, strings.TrimSpace(in.Text), in.TTSConfig)
	}
	sampleRate := in.Audio.SampleRate
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	chunk := &pb.AudioChunk{
		Data:       append([]byte(nil), in.Audio.PCM16...),
		SampleRate: int32(sampleRate),
		Channels:   1,
		Format:     "pcm_s16le",
		IsFinal:    true,
	}
	return []*pb.AudioChunk{chunk}, append([]byte(nil), in.Audio.PCM16...), sampleRate, nil
}

func (o *Orchestrator) synthesizeOfflineText(ctx context.Context, text string, cfg inference.TTSConfig) ([]*pb.AudioChunk, []byte, int, error) {
	segments := splitOfflineTTSText(text)
	chunks := make([]*pb.AudioChunk, 0)
	var pcm []byte
	sampleRate := 0
	for _, segment := range segments {
		textCh := make(chan string, 1)
		textCh <- segment
		close(textCh)

		audioCh, errCh := o.inference.SynthesizeSpeechStream(ctx, textCh, cfg)
		for chunk := range audioCh {
			if chunk == nil || len(chunk.Data) == 0 {
				continue
			}
			chunkCopy := *chunk
			chunkCopy.Data = append([]byte(nil), chunk.Data...)
			chunkCopy.IsFinal = false
			chunks = append(chunks, &chunkCopy)
			chunkPCM, sr, err := audioChunkPCM16Mono(chunk)
			if err != nil {
				return nil, nil, 0, err
			}
			if sampleRate == 0 {
				sampleRate = sr
			} else if sr != sampleRate {
				return nil, nil, 0, fmt.Errorf("tts sample rate changed from %d to %d", sampleRate, sr)
			}
			pcm = append(pcm, chunkPCM...)
		}
		if err := <-errCh; err != nil {
			return nil, nil, 0, err
		}
	}
	if len(chunks) > 0 {
		chunks[len(chunks)-1].IsFinal = true
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	return chunks, pcm, sampleRate, nil
}

func splitOfflineTTSText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	primary := splitOfflineTextByDelimiters(text, "。！？!?；;\n")
	segments := make([]string, 0, len(primary))
	for _, segment := range primary {
		segments = append(segments, splitOfflineTTSTextSegment(segment)...)
	}
	return segments
}

func splitOfflineTTSTextSegment(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len([]rune(text)) <= offlineTTSTextSegmentSoftLimit {
		return []string{text}
	}
	parts := splitOfflineTextByDelimiters(text, "，,、：:")
	segments := make([]string, 0, len(parts))
	current := ""
	for _, part := range parts {
		if len([]rune(part)) > offlineTTSTextSegmentSoftLimit {
			if current != "" {
				segments = append(segments, current)
				current = ""
			}
			segments = append(segments, hardSplitOfflineTTSText(part)...)
			continue
		}
		if current == "" {
			current = part
			continue
		}
		if len([]rune(current))+len([]rune(part)) <= offlineTTSTextSegmentSoftLimit {
			current += part
			continue
		}
		segments = append(segments, current)
		current = part
	}
	if current != "" {
		segments = append(segments, current)
	}
	return segments
}

func splitOfflineTextByDelimiters(text string, delimiters string) []string {
	delimiterSet := make(map[rune]bool, len(delimiters))
	for _, delimiter := range delimiters {
		delimiterSet[delimiter] = true
	}
	var segments []string
	var builder strings.Builder
	for _, r := range text {
		builder.WriteRune(r)
		if delimiterSet[r] {
			if segment := strings.TrimSpace(builder.String()); segment != "" {
				segments = append(segments, segment)
			}
			builder.Reset()
		}
	}
	if segment := strings.TrimSpace(builder.String()); segment != "" {
		segments = append(segments, segment)
	}
	return segments
}

func hardSplitOfflineTTSText(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	segments := make([]string, 0, (len(runes)+offlineTTSTextSegmentSoftLimit-1)/offlineTTSTextSegmentSoftLimit)
	for len(runes) > 0 {
		take := offlineTTSTextSegmentSoftLimit
		if len(runes) < take {
			take = len(runes)
		}
		segments = append(segments, strings.TrimSpace(string(runes[:take])))
		runes = runes[take:]
	}
	return segments
}

func audioChunkPCM16Mono(chunk *pb.AudioChunk) ([]byte, int, error) {
	if chunk == nil {
		return nil, 0, errors.New("audio chunk is nil")
	}
	sampleRate := int(chunk.SampleRate)
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	format := strings.ToLower(strings.TrimSpace(chunk.Format))
	switch format {
	case "float32", "f32", "pcm_f32le":
		data := chunk.Data
		if len(data)%4 != 0 {
			data = data[:len(data)-(len(data)%4)]
		}
		out := make([]byte, len(data)/2)
		for i, j := 0, 0; i+4 <= len(data); i, j = i+4, j+2 {
			v := math.Float32frombits(binary.LittleEndian.Uint32(data[i : i+4]))
			if v > 1 {
				v = 1
			} else if v < -1 {
				v = -1
			}
			binary.LittleEndian.PutUint16(out[j:j+2], uint16(int16(math.Round(float64(v)*32767))))
		}
		return out, sampleRate, nil
	case "", "pcm_s16le", "s16le", "int16":
		data := chunk.Data
		if len(data)%2 != 0 {
			data = data[:len(data)-1]
		}
		return append([]byte(nil), data...), sampleRate, nil
	default:
		return nil, 0, fmt.Errorf("unsupported audio chunk format %q", chunk.Format)
	}
}

func writePCM16MonoWAV(path string, pcm []byte, sampleRate int) error {
	if len(pcm) == 0 {
		return errors.New("pcm audio is empty")
	}
	if len(pcm)%2 != 0 {
		pcm = pcm[:len(pcm)-1]
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	dataSize := uint32(len(pcm))
	byteRate := uint32(sampleRate * 2)
	var buf bytes.Buffer
	buf.Grow(44 + len(pcm))
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36)+dataSize)
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, byteRate)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, dataSize)
	buf.Write(pcm)
	return os.WriteFile(path, buf.Bytes(), 0644)
}
