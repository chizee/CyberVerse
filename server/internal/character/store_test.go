package character

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadLegacyCharacterStripsAvatarModelOnSave(t *testing.T) {
	baseDir := t.TempDir()
	charID := "123e4567-e89b-12d3-a456-426614174000"
	charDir := filepath.Join(baseDir, charDirName("Legacy", charID))
	if err := os.MkdirAll(charDir, 0755); err != nil {
		t.Fatal(err)
	}

	legacy := map[string]any{
		"id":              charID,
		"name":            "Legacy",
		"description":     "legacy payload",
		"avatar_image":    "",
		"use_face_crop":   false,
		"voice_provider":  "doubao",
		"voice_type":      "温柔文雅",
		"avatar_model":    "flash_head",
		"speaking_style":  "平静",
		"personality":     "稳定",
		"welcome_message": "你好",
		"system_prompt":   "legacy system prompt",
		"tags":            []string{"legacy"},
		"images":          []any{},
		"active_image":    "",
		"image_mode":      "fixed",
		"created_at":      "2026-04-18T00:00:00Z",
		"updated_at":      "2026-04-18T00:00:00Z",
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(charDir, "character.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(baseDir)
	if err != nil {
		t.Fatal(err)
	}

	char, err := store.Get(charID)
	if err != nil {
		t.Fatal(err)
	}
	if char.Name != "Legacy" {
		t.Fatalf("expected Legacy, got %q", char.Name)
	}

	updated := &Character{
		Name:           char.Name,
		Description:    "updated legacy payload",
		AvatarImage:    char.AvatarImage,
		UseFaceCrop:    char.UseFaceCrop,
		VoiceProvider:  char.VoiceProvider,
		VoiceType:      char.VoiceType,
		SpeakingStyle:  char.SpeakingStyle,
		Personality:    char.Personality,
		WelcomeMessage: char.WelcomeMessage,
		SystemPrompt:   char.SystemPrompt,
		Tags:           append([]string(nil), char.Tags...),
		ImageMode:      char.ImageMode,
	}
	if _, err := store.Update(charID, updated); err != nil {
		t.Fatal(err)
	}

	saved, err := os.ReadFile(filepath.Join(charDir, "character.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "\"avatar_model\"") {
		t.Fatalf("expected saved character.json to omit avatar_model, got %s", string(saved))
	}

	var savedJSON map[string]any
	if err := json.Unmarshal(saved, &savedJSON); err != nil {
		t.Fatal(err)
	}
	if _, ok := savedJSON["avatar_model"]; ok {
		t.Fatalf("expected avatar_model to be removed from saved JSON")
	}
}

func TestIdleVideoFilenameIncludesResolutionVariant(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	got := store.IdleVideoFilename("img_003.png", DefaultIdleVideoProfile)
	want := "img_003__breathing10s_v1.mp4"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBaiduXilingAvatarFieldsPersistAcrossStoreReload(t *testing.T) {
	baseDir := t.TempDir()
	store, err := NewStore(baseDir)
	if err != nil {
		t.Fatal(err)
	}

	char, err := store.Create(&Character{
		Name:          "Baidu Role",
		AvatarBackend: AvatarBackendBaiduXiling,
		BaiduXiling: &BaiduXiling{
			FigureID:        " figure-1 ",
			FigureName:      " Figure One ",
			ThumbnailURL:    " https://example.com/thumb.png ",
			PreviewVideoURL: " https://example.com/preview.mp4 ",
			SourceImageURL:  " https://example.com/source.png ",
			Status:          " FINISHED ",
			Width:           720,
			Height:          406,
		},
		VoiceType: "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(char.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AvatarBackend != AvatarBackendBaiduXiling {
		t.Fatalf("expected baidu backend, got %q", got.AvatarBackend)
	}
	if got.BaiduXiling == nil {
		t.Fatal("expected baidu_xiling config")
	}
	if got.BaiduXiling.FigureID != "figure-1" {
		t.Fatalf("expected trimmed figure_id, got %q", got.BaiduXiling.FigureID)
	}
	if got.BaiduXiling.FigureName != "Figure One" {
		t.Fatalf("expected trimmed figure name, got %q", got.BaiduXiling.FigureName)
	}
	if got.BaiduXiling.Width != 720 || got.BaiduXiling.Height != 406 {
		t.Fatalf("expected persisted dimensions, got %dx%d", got.BaiduXiling.Width, got.BaiduXiling.Height)
	}
}

func TestXunfeiAvatarFieldsPersistAcrossStoreReload(t *testing.T) {
	baseDir := t.TempDir()
	store, err := NewStore(baseDir)
	if err != nil {
		t.Fatal(err)
	}

	char, err := store.Create(&Character{
		Name:          "Xunfei Role",
		AvatarBackend: AvatarBackendXunfei,
		Xunfei: &XunfeiAvatar{
			AvatarID:        " avatar-1 ",
			AvatarName:      " Avatar One ",
			SceneID:         " scene-1 ",
			VCN:             " x4_yezi ",
			ThumbnailURL:    " https://example.com/thumb.png ",
			PreviewVideoURL: " https://example.com/preview.mp4 ",
			SourceImageURL:  " https://example.com/source.png ",
			Status:          " active ",
			Protocol:        " flv ",
			Width:           721,
			Height:          1281,
		},
		VoiceType: "Momo",
	})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(char.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AvatarBackend != AvatarBackendXunfei {
		t.Fatalf("expected xunfei backend, got %q", got.AvatarBackend)
	}
	if got.Xunfei == nil {
		t.Fatal("expected xunfei config")
	}
	if got.Xunfei.AvatarID != "avatar-1" || got.Xunfei.AvatarName != "Avatar One" || got.Xunfei.SceneID != "scene-1" || got.Xunfei.VCN != "x4_yezi" {
		t.Fatalf("expected trimmed Xunfei config, got %+v", got.Xunfei)
	}
	if got.Xunfei.ThumbnailURL != "https://example.com/thumb.png" || got.Xunfei.SourceImageURL != "https://example.com/source.png" || got.Xunfei.Status != "active" {
		t.Fatalf("expected trimmed Xunfei media metadata, got %+v", got.Xunfei)
	}
	if got.Xunfei.Protocol != "flv" || got.Xunfei.Width != 720 || got.Xunfei.Height != 1280 {
		t.Fatalf("expected normalized stream config, got %+v", got.Xunfei)
	}
	if got.Xunfei.FPS != 25 || got.Xunfei.Bitrate != 2000 || got.Xunfei.Speed != 50 || got.Xunfei.Pitch != 50 || got.Xunfei.Volume != 50 {
		t.Fatalf("expected normalized driver defaults, got %+v", got.Xunfei)
	}
}

func TestOfflineVideoTTSPersistAcrossStoreReload(t *testing.T) {
	baseDir := t.TempDir()
	store, err := NewStore(baseDir)
	if err != nil {
		t.Fatal(err)
	}

	char, err := store.Create(&Character{
		Name:          "Offline TTS",
		VoiceProvider: "qwen_omni",
		VoiceType:     "Tina",
		OfflineVideoTTS: &OfflineVideoTTS{
			Provider: " qwen ",
			Model:    " cosyvoice-v3-flash ",
			Voice:    " longanyang ",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(char.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OfflineVideoTTS == nil {
		t.Fatal("expected offline_video_tts config")
	}
	if got.OfflineVideoTTS.Provider != "qwen" ||
		got.OfflineVideoTTS.Model != "cosyvoice-v3-flash" ||
		got.OfflineVideoTTS.Voice != "longanyang" {
		t.Fatalf("expected trimmed offline_video_tts qwen/cosyvoice-v3-flash/longanyang, got %#v", got.OfflineVideoTTS)
	}
}

func TestAgentExtensionsPersistAcrossStoreReload(t *testing.T) {
	baseDir := t.TempDir()
	store, err := NewStore(baseDir)
	if err != nil {
		t.Fatal(err)
	}

	char, err := store.Create(&Character{
		Name: "Pi Role",
		AgentExtensions: []AgentExtension{
			{Name: " Research ", URL: " https://pi.dev/packages/%40pi/research ", Enabled: true},
			{Name: "Blank", URL: " ", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(char.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AgentExtensions) != 1 {
		t.Fatalf("expected one agent extension, got %#v", got.AgentExtensions)
	}
	if got.AgentExtensions[0].Name != "Research" ||
		got.AgentExtensions[0].URL != "https://pi.dev/packages/%40pi/research" ||
		!got.AgentExtensions[0].Enabled {
		t.Fatalf("expected trimmed enabled extension, got %#v", got.AgentExtensions[0])
	}
}

func TestUpdatePreservesAgentExtensionsWhenPayloadOmitsField(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	char, err := store.Create(&Character{
		Name: "Pi Role",
		AgentExtensions: []AgentExtension{
			{Name: "Research", URL: "npm:@pi/research", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.Update(char.ID, &Character{Name: "Pi Role Updated"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.AgentExtensions) != 1 || updated.AgentExtensions[0].URL != "npm:@pi/research" {
		t.Fatalf("expected update to preserve agent extensions, got %#v", updated.AgentExtensions)
	}

	cleared, err := store.Update(char.ID, &Character{Name: "Pi Role Updated", AgentExtensions: []AgentExtension{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.AgentExtensions) != 0 {
		t.Fatalf("expected explicit empty extensions to clear config, got %#v", cleared.AgentExtensions)
	}
}

func TestActivateImageMovesImageFirstAndUpdatesAvatarCover(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	char, err := store.Create(&Character{
		Name:      "Avatar Order",
		VoiceType: "温柔文雅",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, filename := range []string{"img_001.png", "img_002.png", "img_003.png"} {
		if err := store.AddImage(char.ID, ImageInfo{
			Filename: filename,
			OrigName: filename,
			AddedAt:  "1",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.ActivateImage(char.ID, "img_003.png"); err != nil {
		t.Fatal(err)
	}

	updated, err := store.Get(char.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ActiveImage != "img_003.png" {
		t.Fatalf("expected active image img_003.png, got %q", updated.ActiveImage)
	}
	wantCover := "/api/v1/characters/" + char.ID + "/images/img_003.png"
	if updated.AvatarImage != wantCover {
		t.Fatalf("expected avatar cover %q, got %q", wantCover, updated.AvatarImage)
	}
	if len(updated.Images) == 0 || updated.Images[0].Filename != "img_003.png" {
		t.Fatalf("expected active image first in stored order, got %#v", updated.Images)
	}

	imgs, err := store.ListImages(char.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 3 || imgs[0].Filename != "img_003.png" {
		t.Fatalf("expected active image first in list response, got %#v", imgs)
	}
}
