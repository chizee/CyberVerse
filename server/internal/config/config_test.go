package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cyberverse_config.yaml")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAvatarEnabledDefaultsTrue(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, "inference: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AvatarEnabled() {
		t.Fatal("expected avatar to default enabled")
	}
	if cfg.Pipeline.AvatarEnabled == nil || !*cfg.Pipeline.AvatarEnabled {
		t.Fatalf("expected pipeline avatar enabled pointer to be true, got %#v", cfg.Pipeline.AvatarEnabled)
	}
	if cfg.AvatarIdleStrategy() != AvatarIdleStrategyCachedVideo {
		t.Fatalf("expected cached_video default, got %q", cfg.AvatarIdleStrategy())
	}
	if cfg.Pipeline.AvatarIdleStrategy != AvatarIdleStrategyCachedVideo {
		t.Fatalf("expected pipeline cached_video default, got %q", cfg.Pipeline.AvatarIdleStrategy)
	}
}

func TestAvatarEnabledCanBeDisabled(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, `
inference:
  avatar:
    enabled: false
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AvatarEnabled() {
		t.Fatal("expected avatar to be disabled")
	}
	if cfg.Pipeline.AvatarEnabled == nil || *cfg.Pipeline.AvatarEnabled {
		t.Fatalf("expected pipeline avatar enabled pointer to be false, got %#v", cfg.Pipeline.AvatarEnabled)
	}
}

func TestAvatarIdleStrategyCanBeSilentInference(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, `
inference:
  avatar:
    idle_strategy: silent_inference
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AvatarIdleStrategy() != AvatarIdleStrategySilentInference {
		t.Fatalf("expected silent_inference, got %q", cfg.AvatarIdleStrategy())
	}
	if cfg.Pipeline.AvatarIdleStrategy != AvatarIdleStrategySilentInference {
		t.Fatalf("expected pipeline silent_inference, got %q", cfg.Pipeline.AvatarIdleStrategy)
	}
}

func TestAvatarIdleStrategyRejectsInvalidValue(t *testing.T) {
	_, err := Load(writeTestConfig(t, `
inference:
  avatar:
    idle_strategy: looped_mp4
`))
	if err == nil {
		t.Fatal("expected invalid idle_strategy to fail")
	}
}
