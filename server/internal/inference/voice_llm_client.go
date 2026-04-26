package inference

import (
	"context"
	"io"

	pb "github.com/cyberverse/server/internal/pb"
)

// ConverseStream opens a bidirectional stream for voice-to-voice conversation.
// Sends a config message first, then streams user audio. Receives VoiceLLM output.
func (c *Client) ConverseStream(ctx context.Context, audioCh <-chan []byte, config VoiceLLMSessionConfig) (<-chan *pb.VoiceLLMOutput, <-chan error) {
	outputCh := make(chan *pb.VoiceLLMOutput, 8)
	errCh := make(chan error, 1)

	go func() {
		defer close(outputCh)
		defer close(errCh)

		stream, err := c.voiceLLM.Converse(ctx)
		if err != nil {
			errCh <- err
			return
		}

		// Send config message first
		err = stream.Send(&pb.VoiceLLMInput{
			Input: &pb.VoiceLLMInput_Config{
				Config: &pb.VoiceLLMConfig{
					SessionId:      config.SessionID,
					SystemPrompt:   config.SystemPrompt,
					Voice:          config.Voice,
					BotName:        config.BotName,
					SpeakingStyle:  config.SpeakingStyle,
					WelcomeMessage: config.WelcomeMessage,
				},
			},
		})
		if err != nil {
			errCh <- err
			return
		}

		// Sender goroutine: stream user audio
		sendDone := make(chan error, 1)
		go func() {
			defer func() { _ = stream.CloseSend() }()
			for {
				select {
				case <-ctx.Done():
					sendDone <- ctx.Err()
					return
				case data, ok := <-audioCh:
					if !ok {
						sendDone <- nil
						return
					}
					err := stream.Send(&pb.VoiceLLMInput{
						Input: &pb.VoiceLLMInput_Audio{
							Audio: &pb.AudioChunk{
								Data:       data,
								SampleRate: 16000,
								Channels:   1,
								Format:     "pcm_s16le",
							},
						},
					})
					if err != nil {
						sendDone <- err
						return
					}
				}
			}
		}()

		// Receiver loop
		for {
			output, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				errCh <- err
				return
			}
			select {
			case outputCh <- output:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}

		if err := <-sendDone; err != nil {
			errCh <- err
		}
	}()

	return outputCh, errCh
}

// Interrupt sends an interrupt request to stop the current VoiceLLM response.
func (c *Client) Interrupt(ctx context.Context, sessionID string) error {
	_, err := c.voiceLLM.Interrupt(ctx, &pb.InterruptRequest{
		SessionId: sessionID,
	})
	return err
}
