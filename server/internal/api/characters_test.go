package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/character"
	"github.com/cyberverse/server/internal/inference"
	pb "github.com/cyberverse/server/internal/pb"
)

func TestCharacterResponsesOmitAvatarModel(t *testing.T) {
	r := newTestRouter()

	createBody := `{
		"name":"角色A",
		"description":"test",
		"voice_provider":"doubao",
		"voice_type":"温柔文雅",
		"avatar_model":"flash_head"
	}`
	req := httptest.NewRequest("POST", "/api/v1/characters", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	var created map[string]any
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if _, ok := created["avatar_model"]; ok {
		t.Fatalf("expected create response to omit avatar_model, got %v", created["avatar_model"])
	}

	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatalf("expected response id, got %v", created["id"])
	}

	req = httptest.NewRequest("GET", "/api/v1/characters/"+id, nil)
	w = httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var fetched map[string]any
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatal(err)
	}
	if _, ok := fetched["avatar_model"]; ok {
		t.Fatalf("expected get response to omit avatar_model, got %v", fetched["avatar_model"])
	}

	updateBody := `{
		"name":"角色A",
		"description":"updated",
		"voice_provider":"doubao",
		"voice_type":"温柔文雅",
		"avatar_model":"live_act"
	}`
	req = httptest.NewRequest("PUT", "/api/v1/characters/"+id, strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var updated map[string]any
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if _, ok := updated["avatar_model"]; ok {
		t.Fatalf("expected update response to omit avatar_model, got %v", updated["avatar_model"])
	}
}

func TestCharacterVoiceTypeAllowsCustomSpeakerID(t *testing.T) {
	r := newTestRouter()

	createBody := `{
		"name":"角色A",
		"description":"test",
		"voice_provider":"doubao",
		"voice_type":"S_123456"
	}`
	req := httptest.NewRequest("POST", "/api/v1/characters", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	var created map[string]any
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if got := created["voice_type"]; got != "S_123456" {
		t.Fatalf("expected custom voice_type to round-trip on create, got %v", got)
	}

	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatalf("expected response id, got %v", created["id"])
	}

	req = httptest.NewRequest("GET", "/api/v1/characters/"+id, nil)
	w = httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var fetched map[string]any
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatal(err)
	}
	if got := fetched["voice_type"]; got != "S_123456" {
		t.Fatalf("expected custom voice_type to round-trip on get, got %v", got)
	}

	updateBody := `{
		"name":"角色A",
		"description":"updated",
		"voice_provider":"doubao",
		"voice_type":"S_987654"
	}`
	req = httptest.NewRequest("PUT", "/api/v1/characters/"+id, strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var updated map[string]any
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if got := updated["voice_type"]; got != "S_987654" {
		t.Fatalf("expected custom voice_type to round-trip on update, got %v", got)
	}
}

func TestUpdateCharacterOfflineVideoTTS(t *testing.T) {
	r := newTestRouter()
	char, err := r.charStore.Create(&character.Character{
		Name:          "Offline TTS",
		VoiceProvider: "qwen_omni",
		VoiceType:     "Tina",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/characters/"+char.ID+"/offline-video-tts",
		strings.NewReader(`{"provider":"qwen","model":"cosyvoice-v3-flash","voice":"longanyang"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	offlineTTS, ok := resp["offline_video_tts"].(map[string]any)
	if !ok {
		t.Fatalf("expected offline_video_tts in response, got %#v", resp["offline_video_tts"])
	}
	if offlineTTS["provider"] != "qwen" || offlineTTS["model"] != "cosyvoice-v3-flash" || offlineTTS["voice"] != "longanyang" {
		t.Fatalf("unexpected offline_video_tts: %#v", offlineTTS)
	}

	updated, err := r.charStore.Get(char.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.OfflineVideoTTS == nil ||
		updated.OfflineVideoTTS.Provider != "qwen" ||
		updated.OfflineVideoTTS.Model != "cosyvoice-v3-flash" ||
		updated.OfflineVideoTTS.Voice != "longanyang" {
		t.Fatalf("expected stored offline tts qwen/cosyvoice-v3-flash/longanyang, got %#v", updated.OfflineVideoTTS)
	}
}

func TestUpdateCharacterOfflineVideoTTSRejectsUnconfiguredProvider(t *testing.T) {
	r := newTestRouter()
	char, err := r.charStore.Create(&character.Character{Name: "Offline TTS"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/characters/"+char.ID+"/offline-video-tts",
		strings.NewReader(`{"provider":"openai","voice":"nova"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestCharacterVoiceSuccess(t *testing.T) {
	inf := &fakeInferenceService{
		avatarInfo:        &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25, OutputWidth: 512, OutputHeight: 512},
		checkVoiceConfigs: make(chan inference.VoiceLLMSessionConfig, 1),
	}
	r := newTestRouterWithInference(inf)

	req := httptest.NewRequest(
		"POST",
		"/api/v1/characters/test-voice",
		strings.NewReader(`{"voice_provider":"doubao","voice_type":"温柔文雅"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", resp["status"])
	}

	select {
	case config := <-inf.checkVoiceConfigs:
		if config.Provider != "doubao" || config.Voice != "温柔文雅" {
			t.Fatalf("unexpected check voice config: %+v", config)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for check voice config")
	}
}

func TestTestCharacterVoiceSupportsQwenOmniProvider(t *testing.T) {
	inf := &fakeInferenceService{
		avatarInfo:        &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25, OutputWidth: 512, OutputHeight: 512},
		checkVoiceConfigs: make(chan inference.VoiceLLMSessionConfig, 1),
	}
	r := newTestRouterWithInference(inf)

	req := httptest.NewRequest(
		"POST",
		"/api/v1/characters/test-voice",
		strings.NewReader(`{"voice_provider":"qwen_omni","voice_type":"Tina"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	select {
	case config := <-inf.checkVoiceConfigs:
		if config.Provider != "qwen_omni" || config.Voice != "Tina" {
			t.Fatalf("unexpected check voice config: %+v", config)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for check voice config")
	}
}

func TestTestCharacterVoiceSupportsGrokProvider(t *testing.T) {
	inf := &fakeInferenceService{
		avatarInfo:        &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25, OutputWidth: 512, OutputHeight: 512},
		checkVoiceConfigs: make(chan inference.VoiceLLMSessionConfig, 1),
	}
	r := newTestRouterWithInference(inf)

	req := httptest.NewRequest(
		"POST",
		"/api/v1/characters/test-voice",
		strings.NewReader(`{"voice_provider":"grok","voice_type":"eve"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	select {
	case config := <-inf.checkVoiceConfigs:
		if config.Provider != "grok" || config.Voice != "eve" {
			t.Fatalf("unexpected check voice config: %+v", config)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for check voice config")
	}
}

func TestTestCharacterVoiceSupportsQwenTTS(t *testing.T) {
	inf := &fakeInferenceService{
		ttsConfigs: make(chan inference.TTSConfig, 1),
		ttsChunks:  []*pb.AudioChunk{{Data: []byte{1, 2, 3}, SampleRate: 16000, Channels: 1, Format: "pcm"}},
	}
	r := newTestRouterWithInference(inf)

	req := httptest.NewRequest(
		"POST",
		"/api/v1/characters/test-voice",
		strings.NewReader(`{"voice_provider":"qwen","model":"qwen3-tts-flash-realtime","voice_type":"Momo"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case config := <-inf.ttsConfigs:
		if config.Provider != "qwen" || config.Model != "qwen3-tts-flash-realtime" || config.Voice != "Momo" {
			t.Fatalf("unexpected tts config: %+v", config)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tts config")
	}
}

func TestTestCharacterVoiceSupportsCosyVoiceTTS(t *testing.T) {
	inf := &fakeInferenceService{
		ttsConfigs: make(chan inference.TTSConfig, 1),
		ttsChunks:  []*pb.AudioChunk{{Data: []byte{1, 2, 3}, SampleRate: 16000, Channels: 1, Format: "pcm"}},
	}
	r := newTestRouterWithInference(inf)

	req := httptest.NewRequest(
		"POST",
		"/api/v1/characters/test-voice",
		strings.NewReader(`{"voice_provider":"qwen","model":"cosyvoice-v3.5-flash","voice_type":"cosyvoice-v3.5-flash-peiyin-abc"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case config := <-inf.ttsConfigs:
		if config.Provider != "qwen" || config.Model != "cosyvoice-v3.5-flash" || config.Voice != "cosyvoice-v3.5-flash-peiyin-abc" {
			t.Fatalf("unexpected tts config: %+v", config)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tts config")
	}
}

func TestTestCharacterVoiceSupportsOpenAITTS(t *testing.T) {
	inf := &fakeInferenceService{
		ttsConfigs: make(chan inference.TTSConfig, 1),
		ttsChunks:  []*pb.AudioChunk{{Data: []byte{1, 2, 3}, SampleRate: 16000, Channels: 1, Format: "pcm"}},
	}
	r := newTestRouterWithInference(inf)
	configPath := filepath.Join(t.TempDir(), "cyberverse_config.yaml")
	if err := os.WriteFile(configPath, []byte(`
inference:
  tts:
    default: "qwen"
    qwen:
      plugin_class: "inference.plugins.tts.qwen_tts_plugin.QwenTTSPlugin"
      model: "qwen3-tts-flash-realtime"
    openai:
      plugin_class: "inference.plugins.tts.openai_tts_plugin.OpenAITTSPlugin"
      model: "tts-1"
`), 0644); err != nil {
		t.Fatal(err)
	}
	r.configPath = configPath

	req := httptest.NewRequest(
		"POST",
		"/api/v1/characters/test-voice",
		strings.NewReader(`{"voice_provider":"openai","voice_type":"nova"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case config := <-inf.ttsConfigs:
		if config.Provider != "openai" || config.Voice != "nova" {
			t.Fatalf("unexpected tts config: %+v", config)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tts config")
	}
}

func TestTestCharacterVoiceRejectsUnsupportedProvider(t *testing.T) {
	r := newTestRouter()

	req := httptest.NewRequest(
		"POST",
		"/api/v1/characters/test-voice",
		strings.NewReader(`{"voice_provider":"other","voice_type":"温柔文雅"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTestCharacterVoiceReturnsProviderRawError(t *testing.T) {
	r := newTestRouterWithInference(&fakeInferenceService{
		checkVoiceProviderError: `{"error":"resource ID is mismatched with speaker related resource"}`,
	})

	req := httptest.NewRequest(
		"POST",
		"/api/v1/characters/test-voice",
		strings.NewReader(`{"voice_provider":"doubao","voice_type":"S_123456"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	want := `{"error":"resource ID is mismatched with speaker related resource"}`
	if resp["error"] != want {
		t.Fatalf("expected raw provider error %q, got %q", want, resp["error"])
	}
}

func TestTestCharacterVoiceReturnsServiceError(t *testing.T) {
	r := newTestRouterWithInference(&fakeInferenceService{
		checkVoiceErr: errors.New("voice check timed out"),
	})

	req := httptest.NewRequest(
		"POST",
		"/api/v1/characters/test-voice",
		strings.NewReader(`{"voice_provider":"doubao","voice_type":"温柔文雅"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "voice check timed out" {
		t.Fatalf("expected service error, got %q", resp["error"])
	}
}

func TestIdleVideoURLsOnlyReturnCurrentResolutionVariant(t *testing.T) {
	r := newTestRouterWithInference(&fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{
			ModelName:    "avatar.live_act",
			OutputFps:    24,
			OutputWidth:  320,
			OutputHeight: 480,
		},
	})

	char, err := r.charStore.Create(&character.Character{
		Name:      "角色A",
		VoiceType: "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	image := character.ImageInfo{
		Filename: "img_001.png",
		OrigName: "avatar.png",
		AddedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := r.charStore.AddImage(char.ID, image); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(r.charStore.IdleVideosForImageDir(char.ID, image.Filename), 0755); err != nil {
		t.Fatal(err)
	}

	wrongDir := r.charStore.IdleVideosForSizeDir(char.ID, image.Filename, 512, 512)
	if err := os.MkdirAll(wrongDir, 0755); err != nil {
		t.Fatal(err)
	}
	wrongPath := filepath.Join(wrongDir, "wrong.mp4")
	if err := os.WriteFile(wrongPath, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	rightDir := r.charStore.IdleVideosForSizeDir(char.ID, image.Filename, 320, 480)
	if err := os.MkdirAll(rightDir, 0755); err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(rightDir, "custom_a.mp4")
	if err := os.WriteFile(firstPath, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(rightDir, "custom_b.mp4")
	if err := os.WriteFile(secondPath, []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	target := r.currentIdleVideoTarget(context.Background())
	urls := r.idleVideoURLs(char.ID, image.Filename, target)
	if len(urls) != 2 {
		t.Fatalf("expected 2 idle video URLs for current resolution, got %d (%v)", len(urls), urls)
	}

	wantFirst := "/api/v1/characters/" + char.ID + "/idle-videos/img_001/320x480/custom_a.mp4"
	wantSecond := "/api/v1/characters/" + char.ID + "/idle-videos/img_001/320x480/custom_b.mp4"
	if urls[0] != wantFirst || urls[1] != wantSecond {
		t.Fatalf("expected idle video URLs [%q %q], got %v", wantFirst, wantSecond, urls)
	}
}
