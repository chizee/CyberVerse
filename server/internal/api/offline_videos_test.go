package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/character"
	"github.com/cyberverse/server/internal/inference"
	pb "github.com/cyberverse/server/internal/pb"
)

func TestLocalOfflineVideoTextUsesOfflineTTSPreferenceNotOmniVoice(t *testing.T) {
	inf := &fakeInferenceService{
		ttsConfigs: make(chan inference.TTSConfig, 1),
		ttsChunks: []*pb.AudioChunk{{
			Data:       []byte{0, 0, 1, 0},
			SampleRate: 16000,
			Channels:   1,
			Format:     "pcm_s16le",
		}},
	}
	r := newTestRouterWithInference(inf)
	char, err := r.charStore.Create(&character.Character{
		Name:          "Offline Voice",
		VoiceProvider: "qwen_omni",
		VoiceType:     "Tina",
		OfflineVideoTTS: &character.OfflineVideoTTS{
			Provider: "qwen",
			Model:    "cosyvoice-v3-flash",
			Voice:    "longanyang",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	addOfflineVideoAvatarImage(t, r.charStore, char.ID)

	req := newOfflineVideoMultipartRequest(t, char.ID, map[string]string{
		"input_type": "text",
		"text":       "hello from offline",
	})
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case cfg := <-inf.ttsConfigs:
		if cfg.Provider != "qwen" || cfg.Model != "cosyvoice-v3-flash" || cfg.Voice != "longanyang" {
			t.Fatalf("expected offline tts qwen/cosyvoice-v3-flash/longanyang, got %+v", cfg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tts config")
	}
}

func TestLocalOfflineVideoTextFormTTSOverridesPreference(t *testing.T) {
	r := newTestRouter()
	char := &character.Character{
		OfflineVideoTTS: &character.OfflineVideoTTS{
			Provider: "qwen",
			Voice:    "Momo",
		},
	}
	req := newOfflineVideoMultipartRequest(t, "character-id", map[string]string{
		"input_type":   "text",
		"text":         "hello",
		"tts_provider": "qwen",
		"tts_model":    "cosyvoice-v3.5-flash",
		"tts_voice":    "voice-clone-1",
	})

	cfg, err := r.offlineVideoTTSConfig(req, char, "text", "offline-job")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "qwen" || cfg.Model != "cosyvoice-v3.5-flash" || cfg.Voice != "voice-clone-1" {
		t.Fatalf("expected form tts qwen/cosyvoice-v3.5-flash/voice-clone-1, got %+v", cfg)
	}
}

func TestLocalOfflineVideoTextFallsBackToDefaultTTS(t *testing.T) {
	r := newTestRouter()
	req := newOfflineVideoMultipartRequest(t, "character-id", map[string]string{
		"input_type": "text",
		"text":       "hello",
	})

	cfg, err := r.offlineVideoTTSConfig(req, &character.Character{}, "text", "offline-job")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "qwen" || cfg.Voice != "Momo" {
		t.Fatalf("expected default qwen/Momo, got %+v", cfg)
	}
}

func TestLocalOfflineVideoAudioSkipsTTSProviderValidation(t *testing.T) {
	r := newTestRouter()
	req := newOfflineVideoMultipartRequest(t, "character-id", map[string]string{
		"input_type":   "audio",
		"tts_provider": "not-configured",
	})

	cfg, err := r.offlineVideoTTSConfig(req, &character.Character{}, "audio", "offline-job")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "" || cfg.Voice != "" || cfg.SessionID != "offline-job" {
		t.Fatalf("expected empty audio tts config with session id, got %+v", cfg)
	}
}

func TestBaiduXilingOfflineVideoOptionsUseBaiduTTSParams(t *testing.T) {
	t.Setenv("BAIDU_XILING_OFFLINE_TTS_PERSON", "")
	t.Setenv("BAIDU_XILING_OFFLINE_TTS_LAN", "")
	t.Setenv("BAIDU_XILING_OFFLINE_TTS_SPEED", "")
	t.Setenv("BAIDU_XILING_OFFLINE_TTS_VOLUME", "")
	t.Setenv("BAIDU_XILING_OFFLINE_TTS_PITCH", "")

	req := newOfflineVideoMultipartRequest(t, "character-id", map[string]string{
		"tts_provider": "qwen",
		"tts_voice":    "Momo",
		"tts_person":   "baidu-person-1",
		"tts_lan":      "English",
		"tts_speed":    "7",
		"tts_volume":   "8",
		"tts_pitch":    "9",
	})

	options := baiduXilingOfflineVideoOptionsFromRequest(req, &character.Character{
		BaiduXiling: &character.BaiduXiling{Width: 720, Height: 406},
		OfflineVideoTTS: &character.OfflineVideoTTS{
			Provider: "qwen",
			Voice:    "Momo",
		},
	})
	if options.TTSPerson != "baidu-person-1" ||
		options.TTSLan != "English" ||
		options.TTSSpeed != "7" ||
		options.TTSVolume != "8" ||
		options.TTSPitch != "9" {
		t.Fatalf("expected Baidu TTS params to be preserved, got %+v", options)
	}
}

func TestXunfeiOfflineVideoQueuesWithoutLocalAvatarImage(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_APP_ID", "")
	t.Setenv("XUNFEI_AVATAR_API_KEY", "")
	t.Setenv("XUNFEI_AVATAR_API_SECRET", "")

	r := newTestRouter()
	char, err := r.charStore.Create(&character.Character{
		Name:          "Xunfei Offline",
		AvatarBackend: character.AvatarBackendXunfei,
		Xunfei: &character.XunfeiAvatar{
			AvatarID: "avatar-1",
			SceneID:  "scene-1",
			VCN:      "vcn-1",
			Protocol: "xrtc",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := newOfflineVideoMultipartRequestWithAudio(t, char.ID, map[string]string{
		"input_type":        "audio",
		"audio_sample_rate": "16000",
	}, "input.pcm", []byte{0, 0, 1, 0})
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp offlineVideoJobResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Provider != character.AvatarBackendXunfei {
		t.Fatalf("expected Xunfei provider, got %#v", resp)
	}
	if resp.Width != 720 || resp.Height != 1280 || resp.FPS != 25 {
		t.Fatalf("expected normalized Xunfei output metadata, got %#v", resp)
	}
	deadline := time.After(2 * time.Second)
	for {
		job, _, err := r.readOfflineVideoJob(char.ID, resp.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "failed" {
			if job.Stage != "start" {
				t.Fatalf("expected missing credentials to fail in start stage, got %#v", job)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for Xunfei offline job to fail, latest job=%#v", job)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestXunfeiOfflineCharacterUsesRecordableProtocol(t *testing.T) {
	ch := &character.Character{
		AvatarBackend: character.AvatarBackendXunfei,
		Xunfei: &character.XunfeiAvatar{
			AvatarID: "avatar-1",
			SceneID:  "scene-1",
			Protocol: "webrtc",
			Width:    721,
			Height:   1281,
		},
	}
	got := xunfeiOfflineCharacter(ch)
	if got.Xunfei.Protocol != "flv" {
		t.Fatalf("expected web recording protocol to fall back to flv, got %+v", got.Xunfei)
	}
	if got.Xunfei.Width != 720 || got.Xunfei.Height != 1280 || got.Xunfei.FPS != 25 {
		t.Fatalf("expected normalized Xunfei dimensions, got %+v", got.Xunfei)
	}
	if ch.Xunfei.Protocol != "webrtc" {
		t.Fatalf("expected original character to remain unchanged, got %+v", ch.Xunfei)
	}

	t.Setenv("XUNFEI_AVATAR_OFFLINE_PROTOCOL", "rtmp")
	got = xunfeiOfflineCharacter(ch)
	if got.Xunfei.Protocol != "rtmp" {
		t.Fatalf("expected env override to use rtmp, got %+v", got.Xunfei)
	}
}

func TestWaitForXunfeiOfflineCaptureAllowsNoDataWhileRecorderRuns(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_OFFLINE_CAPTURE_WAIT_MS", "1")

	err := waitForXunfeiOfflineCapture(context.Background(), filepath.Join(t.TempDir(), "capture.flv"), make(chan error, 1))
	if err != nil {
		t.Fatalf("expected capture wait to allow recorder preroll, got %v", err)
	}
}

func TestWaitForXunfeiOfflineCaptureReturnsRecorderError(t *testing.T) {
	recordDone := make(chan error, 1)
	recordDone <- errors.New("record failed")

	err := waitForXunfeiOfflineCapture(context.Background(), filepath.Join(t.TempDir(), "capture.flv"), recordDone)
	if err == nil || err.Error() != "record failed" {
		t.Fatalf("expected recorder error, got %v", err)
	}
}

func TestXunfeiOfflineRemainingRenderDurationUsesPCMDurationAndDrainWait(t *testing.T) {
	pcm := make([]byte, xunfeiOfflineAudioSampleRate*2)
	got := xunfeiOfflineRemainingRenderDuration(pcm, time.Now())
	if got < 1900*time.Millisecond || got > 2100*time.Millisecond {
		t.Fatalf("expected about 2s render wait, got %s", got)
	}
}

func TestPrependXunfeiOfflinePrerollAddsSilenceBeforeAudio(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_OFFLINE_PREROLL_MS", "1000")

	pcm := []byte{1, 2, 3, 4}
	got := prependXunfeiOfflinePreroll(pcm)
	prerollBytes := xunfeiOfflineAudioSampleRate * 2
	if len(got) != prerollBytes+len(pcm) {
		t.Fatalf("expected %d bytes, got %d", prerollBytes+len(pcm), len(got))
	}
	for i, b := range got[:prerollBytes] {
		if b != 0 {
			t.Fatalf("expected preroll byte %d to be silent, got %d", i, b)
		}
	}
	if !bytes.Equal(got[prerollBytes:], pcm) {
		t.Fatalf("expected original pcm after preroll, got %v", got[prerollBytes:])
	}
}

func TestPrependXunfeiOfflinePrerollCanBeDisabled(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_OFFLINE_PREROLL_MS", "0")

	pcm := []byte{1, 2, 3, 4}
	got := prependXunfeiOfflinePreroll(pcm)
	if !bytes.Equal(got, pcm) {
		t.Fatalf("expected preroll to be disabled, got %v", got)
	}
}

func TestBuildXunfeiOfflineDrivingPCMAddsTailSilenceAfterAudio(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_OFFLINE_PREROLL_MS", "0")
	t.Setenv("XUNFEI_AVATAR_OFFLINE_TAIL_MS", "1000")

	pcm := []byte{1, 2, 3, 4}
	got := buildXunfeiOfflineDrivingPCM(pcm)
	tailBytes := xunfeiOfflineAudioSampleRate * 2
	if len(got) != len(pcm)+tailBytes {
		t.Fatalf("expected %d bytes, got %d", len(pcm)+tailBytes, len(got))
	}
	if !bytes.Equal(got[:len(pcm)], pcm) {
		t.Fatalf("expected original pcm before tail, got %v", got[:len(pcm)])
	}
	for i, b := range got[len(pcm):] {
		if b != 0 {
			t.Fatalf("expected tail byte %d to be silent, got %d", i, b)
		}
	}
}

func TestXunfeiOfflineOutputTargetDurationUsesRawAudioAndTail(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_OFFLINE_OUTPUT_TAIL_MS", "3000")

	pcm := make([]byte, xunfeiOfflineAudioSampleRate*2)
	got := xunfeiOfflineOutputTargetDuration(pcm)
	if got != 4*time.Second {
		t.Fatalf("expected 1s raw audio plus 3s tail, got %s", got)
	}
}

func TestParseXunfeiOfflineTrailingSilenceDetectsFinalSegment(t *testing.T) {
	output := `
[Parsed_silencedetect_0 @ 0x1] silence_start: 6.113437
[Parsed_silencedetect_0 @ 0x1] silence_end: 6.711875 | silence_duration: 0.598438
[Parsed_silencedetect_0 @ 0x1] silence_start: 46.287562
[Parsed_silencedetect_0 @ 0x1] silence_end: 69.056 | silence_duration: 22.768438
`
	got, ok, err := parseXunfeiOfflineTrailingSilence(output, 69056*time.Millisecond)
	if err != nil {
		t.Fatalf("expected trailing silence parse to succeed, got %v", err)
	}
	if !ok {
		t.Fatalf("expected trailing silence to be detected")
	}
	if got < 46280*time.Millisecond || got > 46300*time.Millisecond {
		t.Fatalf("expected trailing silence near 46.288s, got %s", got)
	}
}

func TestParseXunfeiOfflineTrailingSilenceIgnoresMiddleSilence(t *testing.T) {
	output := `
[Parsed_silencedetect_0 @ 0x1] silence_start: 6.113437
[Parsed_silencedetect_0 @ 0x1] silence_end: 6.711875 | silence_duration: 0.598438
`
	_, ok, err := parseXunfeiOfflineTrailingSilence(output, 20*time.Second)
	if err != nil {
		t.Fatalf("expected silence parse to succeed, got %v", err)
	}
	if ok {
		t.Fatalf("expected middle silence to be ignored")
	}
}

func TestIsRetryableXunfeiOfflineAttemptErrorOnlyRetriesDriveClose(t *testing.T) {
	if !isRetryableXunfeiOfflineAttemptError(xunfeiOfflineAttemptError{
		stage: "drive",
		err:   errors.New("websocket: close sent"),
	}) {
		t.Fatalf("expected drive websocket close to be retryable")
	}
	if isRetryableXunfeiOfflineAttemptError(xunfeiOfflineAttemptError{
		stage: "start",
		err:   errors.New("websocket: close sent"),
	}) {
		t.Fatalf("expected start websocket close not to be retried by offline drive retry")
	}
	if isRetryableXunfeiOfflineAttemptError(xunfeiOfflineAttemptError{
		stage: "drive",
		err:   errors.New("Xunfei avatar drive failed: code=100 message=bad audio"),
	}) {
		t.Fatalf("expected semantic drive errors not to be retried")
	}
}

func addOfflineVideoAvatarImage(t *testing.T, store *character.Store, characterID string) {
	t.Helper()
	image := character.ImageInfo{
		Filename: "avatar.png",
		OrigName: "avatar.png",
		AddedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := os.WriteFile(filepath.Join(store.ImagesDir(characterID), image.Filename), []byte("avatar"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.AddImage(characterID, image); err != nil {
		t.Fatal(err)
	}
}

func newOfflineVideoMultipartRequest(t *testing.T, characterID string, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/characters/"+characterID+"/offline-videos", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func newOfflineVideoMultipartRequestWithAudio(t *testing.T, characterID string, fields map[string]string, filename string, audio []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.CreateFormFile("audio", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(audio); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/characters/"+characterID+"/offline-videos", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}
