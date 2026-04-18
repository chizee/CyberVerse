package main

import (
	"flag"
	"math/rand"
	"log"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	lkmedia "github.com/livekit/server-sdk-go/v2/pkg/media"
	mediasdk "github.com/livekit/media-sdk"
	protoLogger "github.com/livekit/protocol/logger"
)

func main() {
	var (
		livekitURL          = flag.String("livekit_url", "", "LiveKit wss URL")
		livekitToken        = flag.String("livekit_token", "", "LiveKit access token for the simulated user")
		participantIdentity = flag.String("participant_identity", "user-sim", "Simulated participant identity")
		durationSeconds    = flag.Int("duration_s", 20, "How long to publish silence (seconds)")
		pcmAmplitude       = flag.Int("pcm_amplitude", 0, "PCM16 amplitude for simulated audio (0 = silence)")
		waveform            = flag.String("waveform", "noise", "Simulated waveform: noise|const")
		sampleRate          = flag.Int("sample_rate", 16000, "Audio sample rate")
		chunkMs             = flag.Int("chunk_ms", 20, "PCM chunk duration in ms")
	)
	flag.Parse()

	if *livekitURL == "" || *livekitToken == "" {
		log.Fatal("missing --livekit_url or --livekit_token")
	}

	// Seed PRNG for noise waveform.
	rand.Seed(time.Now().UnixNano())

	roomCallback := &lksdk.RoomCallback{}

	// Join the same room as the server bot, then publish PCM silence as the "user" audio track.
	room, err := lksdk.ConnectToRoomWithToken(
		*livekitURL,
		*livekitToken,
		roomCallback,
		lksdk.WithAutoSubscribe(false),
	)
	if err != nil {
		log.Fatalf("ConnectToRoomWithToken failed: %v", err)
	}
	defer room.Disconnect()

	track, err := lkmedia.NewPCMLocalTrack(*sampleRate, 1, protoLogger.GetLogger())
	if err != nil {
		log.Fatalf("NewPCMLocalTrack failed: %v", err)
	}

	if _, err := room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{
		Name:   "user-sim-audio",
		Stream: "user",
	}); err != nil {
		_ = track.Close()
		log.Fatalf("PublishTrack failed: %v", err)
	}

	// Track binding is done during room negotiation; PCMLocalTrack drops samples
	// until it's bound, so give a short grace period before writing.
	log.Printf("published silence track, warming up before writing (participant_identity=%s)", *participantIdentity)
	time.Sleep(2 * time.Second)

	samplesPerChunk := (*sampleRate * (*chunkMs)) / 1000
	if samplesPerChunk <= 0 {
		log.Fatalf("invalid samplesPerChunk=%d", samplesPerChunk)
	}

	chunk := make(mediasdk.PCM16Sample, samplesPerChunk)
	frameDur := time.Duration(*chunkMs) * time.Millisecond
	end := time.Now().Add(time.Duration(*durationSeconds) * time.Second)

	for time.Now().Before(end) {
		start := time.Now()

		// Generate chunk per-frame so VAD doesn't treat it as constant-value "silence".
		if *pcmAmplitude == 0 {
			// chunk is already zeroed
		} else {
			amp := int16(*pcmAmplitude)
			switch *waveform {
			case "const":
				for i := range chunk {
					chunk[i] = amp
				}
			default:
				// noise
				for i := range chunk {
					// Range: [-amp, +amp]
					chunk[i] = int16((int32(amp)*2*int32(rand.Intn(1000)))/1000) - amp
				}
			}
		}

		// WriteSample is real-time paced: one PCM chunk per chunkMs.
		if err := track.WriteSample(chunk); err != nil {
			log.Printf("WriteSample error: %v", err)
		}

		if elapsed := time.Since(start); elapsed < frameDur {
			time.Sleep(frameDur - elapsed)
		}
	}

	_ = track.Close()
	log.Printf("silence simulation done (duration_s=%d)", *durationSeconds)
}

