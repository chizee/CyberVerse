package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/character"
	"github.com/cyberverse/server/internal/config"
	"github.com/cyberverse/server/internal/inference"
	"github.com/cyberverse/server/internal/orchestrator"
	pb "github.com/cyberverse/server/internal/pb"
	"github.com/cyberverse/server/internal/ws"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeInferenceService struct {
	avatarInfo                *pb.AvatarInfo
	infoCalls                 int
	infoErr                   error
	setAvatarErr              error
	setAvatarErrs             []error
	setAvatarCalls            int
	setAvatarSizes            []int
	setAvatarFormats          []string
	generateAvatarStreamCalls int
	generateAvatarCalls       int
	generateAvatarNotify      chan struct{}
	checkVoiceProviderError   string
	checkVoiceErr             error
	checkVoiceConfigs         chan inference.VoiceLLMSessionConfig
	voiceConfigs              chan inference.VoiceLLMSessionConfig
	ragIndexRequests          chan inference.RAGIndexSourceRequest
	ragDeleteRequests         chan string
	ragSearchRequests         chan inference.RAGSearchRequest
	ragIndexChunkCount        int
	ragIndexErr               error
	ragDeleteErr              error
	ragSearchResults          []inference.RAGSearchResult
	ragSearchErr              error
	llmChunks                 []*pb.LLMChunk
	ttsChunks                 []*pb.AudioChunk
}

func (f *fakeInferenceService) HealthCheck(ctx context.Context) error {
	_, err := f.AvatarInfo(ctx)
	return err
}

func (f *fakeInferenceService) AvatarInfo(context.Context) (*pb.AvatarInfo, error) {
	f.infoCalls++
	if f.infoErr != nil {
		return nil, f.infoErr
	}
	return f.avatarInfo, nil
}

func (f *fakeInferenceService) SetAvatar(_ context.Context, _ string, imageData []byte, format string) error {
	f.setAvatarCalls++
	f.setAvatarSizes = append(f.setAvatarSizes, len(imageData))
	f.setAvatarFormats = append(f.setAvatarFormats, format)
	if len(f.setAvatarErrs) >= f.setAvatarCalls {
		return f.setAvatarErrs[f.setAvatarCalls-1]
	}
	return f.setAvatarErr
}

func (f *fakeInferenceService) GenerateAvatarStream(context.Context, <-chan *pb.AudioChunk) (<-chan *pb.VideoChunk, <-chan error) {
	f.generateAvatarStreamCalls++
	videoCh := make(chan *pb.VideoChunk)
	errCh := make(chan error)
	close(videoCh)
	close(errCh)
	return videoCh, errCh
}
func (f *fakeInferenceService) GenerateAvatar(context.Context, []*pb.AudioChunk) (<-chan *pb.VideoChunk, <-chan error) {
	f.generateAvatarCalls++
	if f.generateAvatarNotify != nil {
		select {
		case f.generateAvatarNotify <- struct{}{}:
		default:
		}
	}
	videoCh := make(chan *pb.VideoChunk)
	errCh := make(chan error)
	close(videoCh)
	close(errCh)
	return videoCh, errCh
}
func (f *fakeInferenceService) GenerateLLMStream(context.Context, string, []inference.ChatMessage, inference.LLMConfig) (<-chan *pb.LLMChunk, <-chan error) {
	ch := make(chan *pb.LLMChunk, len(f.llmChunks))
	errCh := make(chan error)
	for _, chunk := range f.llmChunks {
		ch <- chunk
	}
	close(ch)
	close(errCh)
	return ch, errCh
}
func (f *fakeInferenceService) SynthesizeSpeechStream(context.Context, <-chan string, inference.TTSConfig) (<-chan *pb.AudioChunk, <-chan error) {
	ch := make(chan *pb.AudioChunk, len(f.ttsChunks))
	errCh := make(chan error)
	for _, chunk := range f.ttsChunks {
		ch <- chunk
	}
	close(ch)
	close(errCh)
	return ch, errCh
}
func (f *fakeInferenceService) TranscribeStream(context.Context, <-chan []byte, inference.ASRConfig) (<-chan *pb.TranscriptEvent, <-chan error) {
	ch := make(chan *pb.TranscriptEvent)
	errCh := make(chan error)
	close(ch)
	close(errCh)
	return ch, errCh
}
func (f *fakeInferenceService) CheckVoice(_ context.Context, config inference.VoiceLLMSessionConfig) (string, error) {
	if f.checkVoiceConfigs != nil {
		select {
		case f.checkVoiceConfigs <- config:
		default:
		}
	}
	if f.checkVoiceErr != nil {
		return "", f.checkVoiceErr
	}
	return f.checkVoiceProviderError, nil
}
func (f *fakeInferenceService) ConverseStream(_ context.Context, _ <-chan inference.VoiceLLMInputEvent, config inference.VoiceLLMSessionConfig) (<-chan *pb.VoiceLLMOutput, <-chan error) {
	if f.voiceConfigs != nil {
		select {
		case f.voiceConfigs <- config:
		default:
		}
	}
	ch := make(chan *pb.VoiceLLMOutput)
	errCh := make(chan error)
	close(ch)
	close(errCh)
	return ch, errCh
}
func (f *fakeInferenceService) Interrupt(context.Context, string) error { return nil }
func (f *fakeInferenceService) Close() error                            { return nil }

func (f *fakeInferenceService) IndexRAGSource(_ context.Context, req inference.RAGIndexSourceRequest) (int, error) {
	if f.ragIndexRequests != nil {
		select {
		case f.ragIndexRequests <- req:
		default:
		}
	}
	if f.ragIndexErr != nil {
		return 0, f.ragIndexErr
	}
	if f.ragIndexChunkCount > 0 {
		return f.ragIndexChunkCount, nil
	}
	return 1, nil
}

func (f *fakeInferenceService) DeleteRAGSource(_ context.Context, _ string, _ string, sourceID string) error {
	if f.ragDeleteRequests != nil {
		select {
		case f.ragDeleteRequests <- sourceID:
		default:
		}
	}
	return f.ragDeleteErr
}

func (f *fakeInferenceService) SearchRAG(_ context.Context, req inference.RAGSearchRequest) ([]inference.RAGSearchResult, error) {
	if f.ragSearchRequests != nil {
		select {
		case f.ragSearchRequests <- req:
		default:
		}
	}
	if f.ragSearchErr != nil {
		return nil, f.ragSearchErr
	}
	return f.ragSearchResults, nil
}

func newAvatarModelTestRouter(t *testing.T, activeModel string) (*Router, *character.Store) {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "cyberverse_config.yaml")
	configYAML := `server:
  host: "0.0.0.0"
  http_port: 8080
  grpc_port: 50051
inference:
  avatar:
    default: "flash_head"
    runtime:
      cuda_visible_devices: "0,1"
      world_size: 2
    flash_head:
      plugin_class: "inference.plugins.avatar.flash_head_plugin.FlashHeadAvatarPlugin"
      checkpoint_dir: "/tmp/flash"
      wav2vec_dir: "/tmp/wav2vec"
      model_type: "pro"
      compile_model: true
      compile_vae: true
      dist_worker_main_thread: true
      infer_params:
        tgt_fps: 25
        frame_num: 33
    live_act:
      plugin_class: "inference.plugins.avatar.live_act_plugin.LiveActAvatarPlugin"
      ckpt_dir: "/tmp/live_act"
      wav2vec_dir: "/tmp/live_wav2vec"
      seed: 42
      t5_cpu: false
      fp8_gemm: true
      fp4_gemm: false
      fp8_kv_cache: false
      offload_cache: false
      block_offload: false
      mean_memory: false
      compile_wan_model: true
      compile_vae_decode: true
      dist_worker_main_thread: true
      default_prompt: "一个人在说话"
      infer_params:
        size: "320*480"
        fps: 24
        audio_cfg: 1.0
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "models", "live_act"), 0755); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	inf := &fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{ModelName: "avatar." + activeModel, OutputFps: 24, OutputWidth: 320, OutputHeight: 480},
	}
	orch := orchestrator.New(inf, nil, orchestrator.NewSessionManager(4), nil, charStore)
	return NewRouter(orchestrator.NewSessionManager(4), orch, nil, nil, cfg, charStore, "", configPath), charStore
}

func newExternalAvatarModelTestRouter(t *testing.T, activeModel string) (*Router, string) {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "cyberverse_config.yaml")
	modelDir := filepath.Join(root, "avatar_models")
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		t.Fatal(err)
	}
	configYAML := `server:
  host: "0.0.0.0"
  http_port: 8080
  grpc_port: 50051
inference:
  avatar:
    default: "flash_head"
    idle_strategy: "cached_video"
    runtime:
      cuda_visible_devices: "0,1"
      world_size: 2
    model_config_dir: avatar_models
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "flash_head.yaml"), []byte(`
flash_head:
  plugin_class: "inference.plugins.avatar.flash_head_plugin.FlashHeadAvatarPlugin"
  checkpoint_dir: "/tmp/flash"
  wav2vec_dir: "/tmp/wav2vec"
  model_type: "pro"
  compile_model: true
  compile_vae: true
  dist_worker_main_thread: true
  infer_params:
    tgt_fps: 25
    frame_num: 33
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "live_act.yaml"), []byte(`
live_act:
  plugin_class: "inference.plugins.avatar.live_act_plugin.LiveActAvatarPlugin"
  ckpt_dir: "/tmp/live_act"
  wav2vec_dir: "/tmp/live_wav2vec"
  seed: 42
  t5_cpu: false
  fp8_gemm: true
  fp4_gemm: false
  compile_wan_model: true
  compile_vae_decode: true
  dist_worker_main_thread: true
  default_prompt: "一个人在说话"
  infer_params:
    size: "320*480"
    fps: 24
    audio_cfg: 1.0
`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	inf := &fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{ModelName: "avatar." + activeModel, OutputFps: 24, OutputWidth: 320, OutputHeight: 480},
	}
	orch := orchestrator.New(inf, nil, orchestrator.NewSessionManager(4), nil, charStore)
	return NewRouter(orchestrator.NewSessionManager(4), orch, nil, nil, cfg, charStore, "", configPath), modelDir
}

func TestGetAvatarModelInfoUsesRuntimeModel(t *testing.T) {
	r, _ := newAvatarModelTestRouter(t, "live_act")

	req := httptest.NewRequest("GET", "/api/v1/config/avatar-model", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp avatarModelInfoResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ActiveModel != "live_act" {
		t.Fatalf("expected active_model live_act, got %q", resp.ActiveModel)
	}
	if resp.ConfiguredDefaultModel != "flash_head" {
		t.Fatalf("expected configured_default_model flash_head, got %q", resp.ConfiguredDefaultModel)
	}
	for _, model := range resp.Models {
		if model.Name == "runtime" {
			t.Fatalf("did not expect runtime helper node to appear as an avatar model")
		}
	}
	if !resp.ConfigStatus.HasInferParams {
		t.Fatalf("expected live_act infer params to be present")
	}
}

func TestGetAvatarModelInfoIncludesExternalAvatarModels(t *testing.T) {
	r, _ := newExternalAvatarModelTestRouter(t, "live_act")

	req := httptest.NewRequest("GET", "/api/v1/config/avatar-model", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp avatarModelInfoResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	models := map[string]bool{}
	for _, model := range resp.Models {
		models[model.Name] = true
	}
	if !models["flash_head"] || !models["live_act"] {
		t.Fatalf("expected external avatar models, got %#v", models)
	}
	if models["model_config_dir"] || models["idle_strategy"] {
		t.Fatalf("did not expect avatar control keys as models: %#v", models)
	}
}

func TestGetLaunchConfigKeepsLiveActModelParamsOutOfGPUSection(t *testing.T) {
	r, _ := newAvatarModelTestRouter(t, "live_act")

	req := httptest.NewRequest("GET", "/api/v1/config/launch?model=flash_head", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp launchConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ActiveModel != "live_act" {
		t.Fatalf("expected active_model live_act, got %q", resp.ActiveModel)
	}
	foundAvatarSection := false
	foundVideoSection := false
	foundGPUSection := false
	for _, section := range resp.Sections {
		if section.Title == "头像模型 (Avatar)" {
			if section.Key != "avatar" {
				t.Fatalf("expected avatar section key, got %q", section.Key)
			}
			foundAvatarSection = true
			paths := map[string]any{}
			for _, param := range section.Params {
				paths[param.Path] = param.Value
			}
			if got := fmt.Sprint(paths["inference.avatar.live_act.t5_cpu"]); got != "false" {
				t.Fatalf("expected live_act t5_cpu in avatar section, got %#v", got)
			}
			if got := fmt.Sprint(paths["inference.avatar.live_act.fp8_gemm"]); got != "true" {
				t.Fatalf("expected live_act fp8_gemm in avatar section, got %#v", got)
			}
			if got := fmt.Sprint(paths["inference.avatar.live_act.fp4_gemm"]); got != "false" {
				t.Fatalf("expected live_act fp4_gemm in avatar section, got %#v", got)
			}
			if got := fmt.Sprint(paths["inference.avatar.live_act.compile_wan_model"]); got != "true" {
				t.Fatalf("expected live_act compile_wan_model in avatar section, got %#v", got)
			}
			if got := fmt.Sprint(paths["inference.avatar.live_act.dist_worker_main_thread"]); got != "true" {
				t.Fatalf("expected live_act dist_worker_main_thread in avatar section, got %#v", got)
			}
			if got := fmt.Sprint(paths["inference.avatar.live_act.default_prompt"]); got != "一个人在说话" {
				t.Fatalf("expected live_act default_prompt in avatar section, got %#v", got)
			}
			continue
		}
		if section.Title == "视频输出" {
			if section.Key != "video_output" {
				t.Fatalf("expected video_output section key, got %q", section.Key)
			}
			foundVideoSection = true
			paths := map[string]any{}
			for _, param := range section.Params {
				paths[param.Path] = param.Value
			}
			if got := fmt.Sprint(paths["inference.avatar.live_act.infer_params.size"]); got != "320*480" {
				t.Fatalf("expected live_act infer_params.size from main config, got %#v", got)
			}
			if got := fmt.Sprint(paths["inference.avatar.live_act.infer_params.fps"]); got != "24" {
				t.Fatalf("expected live_act infer_params.fps from main config, got %#v", got)
			}
			continue
		}
		if section.Title != "GPU 配置" {
			continue
		}
		if section.Key != "gpu" {
			t.Fatalf("expected gpu section key, got %q", section.Key)
		}
		foundGPUSection = true
		paths := map[string]bool{}
		for _, param := range section.Params {
			paths[param.Path] = true
		}
		if !paths["inference.avatar.runtime.cuda_visible_devices"] {
			t.Fatalf("expected shared avatar runtime cuda_visible_devices in GPU section")
		}
		if !paths["inference.avatar.runtime.world_size"] {
			t.Fatalf("expected shared avatar runtime world_size in GPU section")
		}
		if paths["inference.avatar.live_act.compile_wan_model"] {
			t.Fatalf("did not expect live_act compile_wan_model in GPU section")
		}
		if paths["inference.avatar.live_act.t5_cpu"] {
			t.Fatalf("did not expect live_act t5_cpu in GPU section")
		}
	}
	if !foundAvatarSection {
		t.Fatalf("expected 头像模型 (Avatar) section for live_act")
	}
	if !foundVideoSection {
		t.Fatalf("expected 视频输出 section for live_act")
	}
	if !foundGPUSection {
		t.Fatalf("expected GPU 配置 section for live_act")
	}
}

func TestGetLaunchConfigReadsVideoSectionFromMainConfig(t *testing.T) {
	r, _ := newAvatarModelTestRouter(t, "flash_head")

	req := httptest.NewRequest("GET", "/api/v1/config/launch", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp launchConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	foundVideoSection := false
	foundAvatarSection := false
	for _, section := range resp.Sections {
		if section.Title == "头像模型 (Avatar)" {
			if section.Key != "avatar" {
				t.Fatalf("expected avatar section key, got %q", section.Key)
			}
			foundAvatarSection = true
			paths := map[string]any{}
			for _, param := range section.Params {
				paths[param.Path] = param.Value
			}
			if got := fmt.Sprint(paths["inference.avatar.flash_head.compile_model"]); got != "true" {
				t.Fatalf("expected flash_head compile_model from main config, got %#v", got)
			}
			if got := fmt.Sprint(paths["inference.avatar.flash_head.compile_vae"]); got != "true" {
				t.Fatalf("expected flash_head compile_vae from main config, got %#v", got)
			}
			continue
		}
		if section.Title != "视频输出" {
			continue
		}
		if section.Key != "video_output" {
			t.Fatalf("expected video_output section key, got %q", section.Key)
		}
		foundVideoSection = true
		paths := map[string]any{}
		for _, param := range section.Params {
			paths[param.Path] = param.Value
		}
		if got := fmt.Sprint(paths["inference.avatar.flash_head.infer_params.tgt_fps"]); got != "25" {
			t.Fatalf("expected tgt_fps from main config, got %#v", got)
		}
		if got := fmt.Sprint(paths["inference.avatar.flash_head.infer_params.frame_num"]); got != "33" {
			t.Fatalf("expected frame_num from main config, got %#v", got)
		}
	}
	if !foundVideoSection {
		t.Fatalf("expected 视频输出 section for flash_head")
	}
	if !foundAvatarSection {
		t.Fatalf("expected 头像模型 (Avatar) section for flash_head")
	}
}

func TestGetLaunchConfigReadsExternalModelConfig(t *testing.T) {
	r, _ := newExternalAvatarModelTestRouter(t, "live_act")

	req := httptest.NewRequest("GET", "/api/v1/config/launch", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp launchConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	paths := map[string]any{}
	for _, section := range resp.Sections {
		for _, param := range section.Params {
			paths[param.Path] = param.Value
		}
	}
	if got := fmt.Sprint(paths["inference.avatar.live_act.default_prompt"]); got != "一个人在说话" {
		t.Fatalf("expected external live_act default_prompt, got %#v", got)
	}
	if got := fmt.Sprint(paths["inference.avatar.live_act.infer_params.fps"]); got != "24" {
		t.Fatalf("expected external live_act fps, got %#v", got)
	}
	if got := fmt.Sprint(paths["inference.avatar.runtime.world_size"]); got != "2" {
		t.Fatalf("expected shared runtime world_size, got %#v", got)
	}
}

func TestUpdateLaunchConfigRejectsNonActiveModel(t *testing.T) {
	r, _ := newAvatarModelTestRouter(t, "live_act")

	body := `{"model":"flash_head","params":[{"path":"inference.avatar.flash_head.world_size","value":1}]}`
	req := httptest.NewRequest("PUT", "/api/v1/config/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateLaunchConfigAllowsSharedAvatarRuntimeUpdates(t *testing.T) {
	r, _ := newAvatarModelTestRouter(t, "live_act")

	body := `{"model":"live_act","params":[{"path":"inference.avatar.runtime.world_size","value":1}]}`
	req := httptest.NewRequest("PUT", "/api/v1/config/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	doc, err := config.ReadYAMLNode(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	node, err := config.GetNodeAtPath(doc, "inference.avatar.runtime.world_size")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(config.NodeValue(node, true)); got != "1" {
		t.Fatalf("expected shared world_size to be updated to 1, got %#v", got)
	}
}

func TestUpdateLaunchConfigWritesInferParamsToMainConfig(t *testing.T) {
	r, _ := newAvatarModelTestRouter(t, "flash_head")

	body := `{"model":"flash_head","params":[{"path":"inference.avatar.flash_head.infer_params.frame_num","value":29}]}`
	req := httptest.NewRequest("PUT", "/api/v1/config/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	doc, err := config.ReadYAMLNode(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	node, err := config.GetNodeAtPath(doc, "inference.avatar.flash_head.infer_params.frame_num")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(config.NodeValue(node, true)); got != "29" {
		t.Fatalf("expected frame_num to be updated to 29, got %#v", got)
	}
}

func TestUpdateLaunchConfigWritesFlashHeadRootParamsToMainConfig(t *testing.T) {
	r, _ := newAvatarModelTestRouter(t, "flash_head")

	body := `{"model":"flash_head","params":[{"path":"inference.avatar.flash_head.compile_model","value":false}]}`
	req := httptest.NewRequest("PUT", "/api/v1/config/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	doc, err := config.ReadYAMLNode(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	node, err := config.GetNodeAtPath(doc, "inference.avatar.flash_head.compile_model")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(config.NodeValue(node, true)); got != "false" {
		t.Fatalf("expected compile_model to be updated to false, got %#v", got)
	}
}

func TestUpdateLaunchConfigWritesLiveActInferParamsToMainConfig(t *testing.T) {
	r, _ := newAvatarModelTestRouter(t, "live_act")

	body := `{"model":"live_act","params":[{"path":"inference.avatar.live_act.infer_params.fps","value":20}]}`
	req := httptest.NewRequest("PUT", "/api/v1/config/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	doc, err := config.ReadYAMLNode(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	node, err := config.GetNodeAtPath(doc, "inference.avatar.live_act.infer_params.fps")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(config.NodeValue(node, true)); got != "20" {
		t.Fatalf("expected fps to be updated to 20, got %#v", got)
	}
}

func TestUpdateLaunchConfigWritesLiveActRootParamsToMainConfig(t *testing.T) {
	r, _ := newAvatarModelTestRouter(t, "live_act")

	body := `{"model":"live_act","params":[{"path":"inference.avatar.live_act.t5_cpu","value":true}]}`
	req := httptest.NewRequest("PUT", "/api/v1/config/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	doc, err := config.ReadYAMLNode(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	node, err := config.GetNodeAtPath(doc, "inference.avatar.live_act.t5_cpu")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(config.NodeValue(node, true)); got != "true" {
		t.Fatalf("expected t5_cpu to be updated to true, got %#v", got)
	}
}

func TestUpdateLaunchConfigWritesExternalInferParamsToModelFile(t *testing.T) {
	r, modelDir := newExternalAvatarModelTestRouter(t, "live_act")

	body := `{"model":"live_act","params":[{"path":"inference.avatar.live_act.infer_params.fps","value":20}]}`
	req := httptest.NewRequest("PUT", "/api/v1/config/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	doc, err := config.ReadYAMLNode(filepath.Join(modelDir, "live_act.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	node, err := config.GetNodeAtPath(doc, "live_act.infer_params.fps")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(config.NodeValue(node, true)); got != "20" {
		t.Fatalf("expected external fps to be updated to 20, got %#v", got)
	}
	mainDoc, err := config.ReadYAMLNode(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.GetNodeAtPath(mainDoc, "inference.avatar.live_act"); err == nil {
		t.Fatal("did not expect live_act to be written into main config")
	}
}

func TestUpdateLaunchConfigWritesRuntimeToMainConfigWithExternalModels(t *testing.T) {
	r, _ := newExternalAvatarModelTestRouter(t, "live_act")

	body := `{"model":"live_act","params":[{"path":"inference.avatar.runtime.world_size","value":1}]}`
	req := httptest.NewRequest("PUT", "/api/v1/config/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	doc, err := config.ReadYAMLNode(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	node, err := config.GetNodeAtPath(doc, "inference.avatar.runtime.world_size")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(config.NodeValue(node, true)); got != "1" {
		t.Fatalf("expected runtime world_size in main config to be 1, got %#v", got)
	}
}

func TestCreateSessionWithCharacterUsesActiveRuntimeModelOnly(t *testing.T) {
	r, charStore := newAvatarModelTestRouter(t, "live_act")
	char, err := charStore.Create(&character.Character{
		Name:      "Character Session",
		VoiceType: "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestCreateSessionBaiduCharacterReturnsH5AudioConfig(t *testing.T) {
	t.Setenv("BAIDU_XILING_APP_ID", "app-id")
	t.Setenv("BAIDU_XILING_APP_KEY", "app-key")
	r, charStore := newAvatarModelTestRouter(t, "flash_head")
	char, err := charStore.Create(&character.Character{
		Name:          "Baidu Character",
		AvatarBackend: character.AvatarBackendBaiduXiling,
		BaiduXiling: &character.BaiduXiling{
			FigureID:        "figure-1",
			CameraID:        "camera-1",
			ThumbnailURL:    "https://example.com/baidu-thumb.png",
			PreviewVideoURL: "https://example.com/baidu-preview.mp4",
			Width:           1920,
			Height:          1080,
		},
		VoiceType: "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Mode != "omni" {
		t.Fatalf("expected Baidu Xiling session to preserve requested omni mode, got %q", resp.Mode)
	}
	if resp.StreamingMode != "direct" {
		t.Fatalf("expected Baidu to reuse direct streaming mode, got %q", resp.StreamingMode)
	}
	if resp.IdleVideoURL != "https://example.com/baidu-preview.mp4" {
		t.Fatalf("expected Baidu preview video as standby, got %q", resp.IdleVideoURL)
	}
	if len(resp.IdleVideoURLs) != 1 || resp.IdleVideoURLs[0] != resp.IdleVideoURL {
		t.Fatalf("expected Baidu preview video URLs, got %+v", resp.IdleVideoURLs)
	}
	if resp.IdleImageURL != "https://example.com/baidu-thumb.png" {
		t.Fatalf("expected Baidu thumbnail as standby image, got %q", resp.IdleImageURL)
	}
	if resp.BaiduXiling == nil {
		t.Fatal("expected Baidu Xiling session config")
	}
	if !strings.HasPrefix(resp.BaiduXiling.IframeURL, "https://open.xiling.baidu.com/cloud/realtime?") {
		t.Fatalf("expected Baidu H5 iframe URL, got %q", resp.BaiduXiling.IframeURL)
	}
	if !strings.Contains(resp.BaiduXiling.IframeURL, "figureId=figure-1") {
		t.Fatalf("expected iframe URL to include figureId, got %q", resp.BaiduXiling.IframeURL)
	}
	if !strings.Contains(resp.BaiduXiling.IframeURL, "cameraId=camera-1") {
		t.Fatalf("expected iframe URL to include cameraId, got %q", resp.BaiduXiling.IframeURL)
	}
	if resp.BaiduXiling.Origin != "https://open.xiling.baidu.com" {
		t.Fatalf("expected Baidu origin, got %q", resp.BaiduXiling.Origin)
	}
	if resp.BaiduXiling.FigureID != "figure-1" || resp.BaiduXiling.CameraID != "camera-1" {
		t.Fatalf("unexpected Baidu figure config: %+v", resp.BaiduXiling)
	}
	if resp.BaiduXiling.AudioSampleRate != 16000 || resp.BaiduXiling.AudioMaxPCMBytes != 48000 {
		t.Fatalf("unexpected Baidu audio limits: %+v", resp.BaiduXiling)
	}
}

func TestCreateSessionBaiduCharacterFallsBackToThumbnailStandby(t *testing.T) {
	t.Setenv("BAIDU_XILING_APP_ID", "app-id")
	t.Setenv("BAIDU_XILING_APP_KEY", "app-key")
	r, charStore := newAvatarModelTestRouter(t, "flash_head")
	char, err := charStore.Create(&character.Character{
		Name:          "Baidu Character",
		AvatarBackend: character.AvatarBackendBaiduXiling,
		BaiduXiling: &character.BaiduXiling{
			FigureID:     "figure-1",
			ThumbnailURL: "https://example.com/baidu-thumb.png",
			Width:        1920,
			Height:       1080,
		},
		VoiceType: "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.IdleVideoURL != "" || len(resp.IdleVideoURLs) != 0 {
		t.Fatalf("expected no Baidu standby video without preview URL, got %q %+v", resp.IdleVideoURL, resp.IdleVideoURLs)
	}
	if resp.IdleImageURL != "https://example.com/baidu-thumb.png" {
		t.Fatalf("expected Baidu thumbnail as standby image, got %q", resp.IdleImageURL)
	}
	if resp.BaiduXiling == nil {
		t.Fatal("expected Baidu Xiling session config")
	}
}

func TestCreateSessionBaiduCharacterRequiresH5Credentials(t *testing.T) {
	t.Setenv("BAIDU_XILING_APP_ID", "")
	t.Setenv("BAIDU_XILING_APP_KEY", "")
	charStore, err := character.NewStore(filepath.Join(t.TempDir(), "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{
		Name:          "Baidu Runtime",
		AvatarBackend: character.AvatarBackendBaiduXiling,
		BaiduXiling:   &character.BaiduXiling{FigureID: "figure-1", ThumbnailURL: "https://example.com/thumb.png"},
		VoiceType:     "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	inf := &fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{ModelName: "avatar.baidu_xiling", OutputFps: 20, OutputWidth: 720, OutputHeight: 406},
	}
	mgr := orchestrator.NewSessionManager(4)
	orch := orchestrator.New(inf, nil, mgr, nil, charStore)
	r := NewRouter(mgr, orch, nil, nil, nil, charStore, "", "")

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Baidu Xiling credentials are not configured") {
		t.Fatalf("expected credential error, got %s", w.Body.String())
	}
}

func TestBaiduXilingSessionSendsAudioDataOverWebSocket(t *testing.T) {
	t.Setenv("BAIDU_XILING_APP_ID", "app-id")
	t.Setenv("BAIDU_XILING_APP_KEY", "app-key")

	charStore, err := character.NewStore(filepath.Join(t.TempDir(), "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{
		Name:          "Baidu Runtime",
		AvatarBackend: character.AvatarBackendBaiduXiling,
		BaiduXiling: &character.BaiduXiling{
			FigureID:        "figure-1",
			ThumbnailURL:    "https://example.com/thumb.png",
			PreviewVideoURL: "https://example.com/standby.mp4",
		},
		VoiceType: "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	inf := &fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25, OutputWidth: 512, OutputHeight: 512},
		llmChunks: []*pb.LLMChunk{
			{Token: "你好", AccumulatedText: "你好，我是百度数字人。"},
			{Token: "，我是百度数字人。", AccumulatedText: "你好，我是百度数字人。", IsFinal: true},
		},
		ttsChunks: []*pb.AudioChunk{
			{
				Data:       []byte{0, 0, 1, 0, 2, 0, 3, 0},
				SampleRate: 16000,
				Channels:   1,
				Format:     "pcm",
			},
		},
	}
	hub := ws.NewHub()
	mgr := orchestrator.NewSessionManager(4)
	t.Cleanup(mgr.Stop)
	orch := orchestrator.New(inf, hub, mgr, nil, charStore)
	r := NewRouter(mgr, orch, hub, nil, nil, charStore, "", "")

	createBody := `{"mode":"standard","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected session create 201, got %d: %s", w.Code, w.Body.String())
	}
	var createResp CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&createResp); err != nil {
		t.Fatal(err)
	}
	if createResp.BaiduXiling == nil {
		t.Fatal("expected Baidu H5 config")
	}
	if createResp.IdleVideoURL != "https://example.com/standby.mp4" {
		t.Fatalf("expected standby preview video, got %q", createResp.IdleVideoURL)
	}

	client := &ws.Client{SessionID: createResp.SessionID, Send: make(chan []byte, 20)}
	hub.Register(client)
	t.Cleanup(func() {
		hub.Unregister(client)
	})

	req = httptest.NewRequest("POST", "/api/v1/sessions/"+createResp.SessionID+"/message", strings.NewReader(`{"text":"请说一句话"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected message 202, got %d: %s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	var sawAudio bool
	var sawSpeaking bool
	var sawIdle bool
	for time.Now().Before(deadline) && (!sawAudio || !sawIdle) {
		select {
		case raw := <-client.Send:
			var event map[string]any
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatalf("invalid websocket event: %v", err)
			}
			switch event["type"] {
			case "webrtc_config", "webrtc_offer", "ice_candidate":
				t.Fatalf("Baidu Xiling standard session should not emit Direct WebRTC setup event: %+v", event)
			case "avatar_status":
				switch event["status"] {
				case "speaking":
					sawSpeaking = true
				case "idle":
					sawIdle = true
				}
			case "baidu_xiling_audio":
				sawAudio = true
				if event["request_id"] == "" || event["audio"] == "" {
					t.Fatalf("expected request_id and audio in Baidu audio event, got %+v", event)
				}
				if event["first"] != true || event["last"] != true {
					t.Fatalf("expected one Baidu audio chunk to be first and last, got %+v", event)
				}
			}
		case <-time.After(200 * time.Millisecond):
			continue
		}
	}
	if !sawSpeaking {
		t.Fatal("expected speaking status before Baidu audio playback")
	}
	if !sawAudio {
		t.Fatal("expected baidu_xiling_audio websocket event")
	}
	if !sawIdle {
		t.Fatal("expected session to return to idle after Baidu audio event")
	}
	if inf.setAvatarCalls != 0 {
		t.Fatalf("expected no SetAvatar call for Baidu H5 session, got %d", inf.setAvatarCalls)
	}
	if inf.generateAvatarStreamCalls != 0 || inf.generateAvatarCalls != 0 {
		t.Fatalf("expected no avatar video generation for Baidu H5 session, stream=%d batch=%d", inf.generateAvatarStreamCalls, inf.generateAvatarCalls)
	}
	session, err := mgr.Get(createResp.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	session.WaitPipelineDone(2 * time.Second)
	if got := session.GetState(); got != orchestrator.StateListening {
		t.Fatalf("expected session to return to listening, got %s", got)
	}
}

func TestCreateSessionReturnsAvatarWarningWhenImageExceedsGRPCLimit(t *testing.T) {
	charStore, err := character.NewStore(filepath.Join(t.TempDir(), "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{
		Name:      "Large Avatar",
		VoiceType: "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	image := character.ImageInfo{
		Filename: "avatar.png",
		OrigName: "avatar.png",
	}
	if err := os.WriteFile(filepath.Join(charStore.ImagesDir(char.ID), image.Filename), []byte("avatar-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := charStore.AddImage(char.ID, image); err != nil {
		t.Fatal(err)
	}
	idleDir := charStore.IdleVideosForSizeDir(char.ID, image.Filename, 512, 512)
	if err := os.MkdirAll(idleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(idleDir, "cached.mp4"), []byte("cached"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := orchestrator.NewSessionManager(4)
	tooLargeErr := status.Error(codes.ResourceExhausted, "trying to send message larger than max (22347880 vs. 10485760)")
	inf := &fakeInferenceService{
		avatarInfo:    &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25, OutputWidth: 512, OutputHeight: 512},
		setAvatarErrs: []error{tooLargeErr, nil},
	}
	orch := orchestrator.New(inf, nil, mgr, nil, charStore)
	r := NewRouter(mgr, orch, nil, nil, nil, charStore, "", "")

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	var resp CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Warnings) != 1 {
		t.Fatalf("expected one warning, got %v", resp.Warnings)
	}
	if !strings.Contains(resp.Warnings[0], "10MB") {
		t.Fatalf("expected warning to mention 10MB upload limit, got %q", resp.Warnings[0])
	}
	if inf.setAvatarCalls != 2 {
		t.Fatalf("expected failed character SetAvatar plus default reset SetAvatar, got %d calls", inf.setAvatarCalls)
	}
	if got := inf.setAvatarFormats[1]; got != "png" {
		t.Fatalf("expected default reset to use png, got %q", got)
	}
	if size := inf.setAvatarSizes[1]; size <= 0 || size >= 10*1024*1024 {
		t.Fatalf("expected compact default avatar image, got %d bytes", size)
	}
}

func TestCreateSessionRejectsWhenActiveRuntimeModelUnavailable(t *testing.T) {
	charStore, err := character.NewStore(filepath.Join(t.TempDir(), "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{
		Name:      "Unavailable",
		VoiceType: "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	mgr := orchestrator.NewSessionManager(4)
	orch := orchestrator.New(&fakeInferenceService{
		infoErr: errors.New("inference unavailable"),
	}, nil, mgr, nil, charStore)
	r := NewRouter(mgr, orch, nil, nil, nil, charStore, "", "")

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestCreateSessionWithAvatarDisabledUsesCachedIdleVideoOnly(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "cyberverse_config.yaml")
	if err := os.WriteFile(configPath, []byte(`
inference:
  avatar:
    enabled: false
    default: flash_head
    flash_head:
      infer_params:
        width: 512
        height: 512
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{Name: "Voice Only", VoiceType: "Tina"})
	if err != nil {
		t.Fatal(err)
	}
	image := character.ImageInfo{Filename: "avatar.png", OrigName: "avatar.png"}
	if err := os.WriteFile(filepath.Join(charStore.ImagesDir(char.ID), image.Filename), []byte("avatar"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := charStore.AddImage(char.ID, image); err != nil {
		t.Fatal(err)
	}
	idleDir := charStore.IdleVideosForSizeDir(char.ID, image.Filename, 512, 512)
	if err := os.MkdirAll(idleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(idleDir, "cached.mp4"), []byte("cached"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := orchestrator.NewSessionManager(4)
	inf := &fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25, OutputWidth: 512, OutputHeight: 512},
	}
	orch := orchestrator.New(inf, nil, mgr, nil, charStore, cfg.Pipeline)
	r := NewRouter(mgr, orch, nil, nil, cfg, charStore, "", configPath)

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.IdleVideoURLs) != 1 || !strings.Contains(resp.IdleVideoURLs[0], "cached.mp4") {
		t.Fatalf("expected cached idle video URL, got %v", resp.IdleVideoURLs)
	}
	if inf.infoCalls != 0 {
		t.Fatalf("expected no AvatarInfo calls when avatar disabled, got %d", inf.infoCalls)
	}
	if inf.setAvatarCalls != 0 {
		t.Fatalf("expected no SetAvatar calls when avatar disabled, got %d", inf.setAvatarCalls)
	}
	if inf.generateAvatarCalls != 0 || inf.generateAvatarStreamCalls != 0 {
		t.Fatalf("expected no avatar generation calls, got GenerateAvatar=%d GenerateAvatarStream=%d", inf.generateAvatarCalls, inf.generateAvatarStreamCalls)
	}
}

func TestCreateSessionWithAvatarDisabledAndMissingIdleCacheStillSucceeds(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "cyberverse_config.yaml")
	if err := os.WriteFile(configPath, []byte(`
inference:
  avatar:
    enabled: false
    default: live_act
    live_act:
      infer_params:
        size: "320*480"
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{Name: "No Cache", VoiceType: "Tina"})
	if err != nil {
		t.Fatal(err)
	}
	image := character.ImageInfo{Filename: "avatar.png", OrigName: "avatar.png"}
	if err := os.WriteFile(filepath.Join(charStore.ImagesDir(char.ID), image.Filename), []byte("avatar"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := charStore.AddImage(char.ID, image); err != nil {
		t.Fatal(err)
	}

	mgr := orchestrator.NewSessionManager(4)
	inf := &fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{ModelName: "avatar.live_act", OutputFps: 20, OutputWidth: 320, OutputHeight: 480},
	}
	orch := orchestrator.New(inf, nil, mgr, nil, charStore, cfg.Pipeline)
	r := NewRouter(mgr, orch, nil, nil, cfg, charStore, "", configPath)

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.IdleVideoURLs) != 0 || resp.IdleVideoURL != "" {
		t.Fatalf("expected no idle video URLs without cache, got %q %v", resp.IdleVideoURL, resp.IdleVideoURLs)
	}
	if inf.infoCalls != 0 || inf.setAvatarCalls != 0 || inf.generateAvatarCalls != 0 || inf.generateAvatarStreamCalls != 0 {
		t.Fatalf("expected no avatar calls when disabled, info=%d set=%d gen=%d stream=%d", inf.infoCalls, inf.setAvatarCalls, inf.generateAvatarCalls, inf.generateAvatarStreamCalls)
	}
}

func TestCreateSessionWithCachedVideoUsesIdleCacheAndSkipsSilentRuntime(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "cyberverse_config.yaml")
	if err := os.WriteFile(configPath, []byte(`
inference:
  avatar:
    enabled: true
    default: live_act
    idle_strategy: cached_video
    live_act:
      infer_params:
        size: "320*480"
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{Name: "Cached Runtime", VoiceType: "Tina"})
	if err != nil {
		t.Fatal(err)
	}
	image := character.ImageInfo{Filename: "avatar.png", OrigName: "avatar.png"}
	if err := os.WriteFile(filepath.Join(charStore.ImagesDir(char.ID), image.Filename), []byte("avatar"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := charStore.AddImage(char.ID, image); err != nil {
		t.Fatal(err)
	}
	idleDir := charStore.IdleVideosForSizeDir(char.ID, image.Filename, 320, 480)
	if err := os.MkdirAll(idleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(idleDir, "cached.mp4"), []byte("cached"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := orchestrator.NewSessionManager(4)
	inf := &fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{ModelName: "avatar.live_act", OutputFps: 20, OutputWidth: 320, OutputHeight: 480},
	}
	orch := orchestrator.New(inf, nil, mgr, nil, charStore, cfg.Pipeline)
	r := NewRouter(mgr, orch, nil, nil, cfg, charStore, "", configPath)

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.IdleStrategy != config.AvatarIdleStrategyCachedVideo {
		t.Fatalf("expected cached_video response, got %q", resp.IdleStrategy)
	}
	if len(resp.IdleVideoURLs) != 1 || !strings.Contains(resp.IdleVideoURLs[0], "cached.mp4") {
		t.Fatalf("expected cached idle video URL, got %v", resp.IdleVideoURLs)
	}
	if inf.generateAvatarStreamCalls != 0 {
		t.Fatalf("expected cached_video not to start silent avatar stream, got %d GenerateAvatarStream calls", inf.generateAvatarStreamCalls)
	}
	if inf.generateAvatarCalls != 0 {
		t.Fatalf("expected existing cache to skip idle video generation, got %d GenerateAvatar calls", inf.generateAvatarCalls)
	}
}

func TestCreateSessionWithCachedVideoMissingCacheStartsIdleGeneration(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "cyberverse_config.yaml")
	if err := os.WriteFile(configPath, []byte(`
inference:
  avatar:
    enabled: true
    default: flash_head
    idle_strategy: cached_video
    flash_head:
      infer_params:
        width: 512
        height: 512
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{Name: "Generate Cache", VoiceType: "Tina"})
	if err != nil {
		t.Fatal(err)
	}
	image := character.ImageInfo{Filename: "avatar.png", OrigName: "avatar.png"}
	if err := os.WriteFile(filepath.Join(charStore.ImagesDir(char.ID), image.Filename), []byte("avatar"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := charStore.AddImage(char.ID, image); err != nil {
		t.Fatal(err)
	}

	mgr := orchestrator.NewSessionManager(4)
	inf := &fakeInferenceService{
		avatarInfo:           &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25, OutputWidth: 512, OutputHeight: 512},
		generateAvatarNotify: make(chan struct{}, 1),
	}
	orch := orchestrator.New(inf, nil, mgr, nil, charStore, cfg.Pipeline)
	r := NewRouter(mgr, orch, nil, nil, cfg, charStore, "", configPath)

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.IdleStrategy != config.AvatarIdleStrategyCachedVideo {
		t.Fatalf("expected cached_video response, got %q", resp.IdleStrategy)
	}
	if len(resp.IdleVideoURLs) != 0 || resp.IdleVideoURL != "" {
		t.Fatalf("expected no immediate idle URLs without cache, got %q %v", resp.IdleVideoURL, resp.IdleVideoURLs)
	}
	select {
	case <-inf.generateAvatarNotify:
	case <-time.After(time.Second):
		t.Fatal("expected cached_video to start background idle video generation")
	}
	if inf.generateAvatarStreamCalls != 0 {
		t.Fatalf("expected cached_video not to start silent avatar stream, got %d GenerateAvatarStream calls", inf.generateAvatarStreamCalls)
	}
}

func TestCreateSessionWithSilentInferenceSkipsIdleVideoGeneration(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "cyberverse_config.yaml")
	if err := os.WriteFile(configPath, []byte(`
inference:
  avatar:
    enabled: true
    default: flash_head
    idle_strategy: silent_inference
    flash_head:
      infer_params:
        width: 512
        height: 512
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatal(err)
	}
	char, err := charStore.Create(&character.Character{Name: "Silent Runtime", VoiceType: "Tina"})
	if err != nil {
		t.Fatal(err)
	}
	image := character.ImageInfo{Filename: "avatar.png", OrigName: "avatar.png"}
	if err := os.WriteFile(filepath.Join(charStore.ImagesDir(char.ID), image.Filename), []byte("avatar"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := charStore.AddImage(char.ID, image); err != nil {
		t.Fatal(err)
	}

	mgr := orchestrator.NewSessionManager(4)
	inf := &fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25, OutputWidth: 512, OutputHeight: 512, ChunkDurationS: 1.12},
	}
	orch := orchestrator.New(inf, nil, mgr, nil, charStore, cfg.Pipeline)
	r := NewRouter(mgr, orch, nil, nil, cfg, charStore, "", configPath)

	body := `{"mode":"omni","character_id":"` + char.ID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.IdleStrategy != config.AvatarIdleStrategySilentInference {
		t.Fatalf("expected silent_inference response, got %q", resp.IdleStrategy)
	}
	if len(resp.IdleVideoURLs) != 0 || resp.IdleVideoURL != "" {
		t.Fatalf("expected no idle video URLs for silent_inference, got %q %v", resp.IdleVideoURL, resp.IdleVideoURLs)
	}
	if inf.generateAvatarCalls != 0 {
		t.Fatalf("expected no cached idle video generation, got %d GenerateAvatar calls", inf.generateAvatarCalls)
	}
}
