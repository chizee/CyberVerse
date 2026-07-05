package config

import (
	"fmt"
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

func writeResolvedConfigFixture(t *testing.T, mainBody string, models map[string]string) string {
	t.Helper()
	root := t.TempDir()
	modelDir := filepath.Join(root, "avatar_models")
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range models {
		if err := os.WriteFile(filepath.Join(modelDir, name+".yaml"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "cyberverse_config.yaml")
	if err := os.WriteFile(path, []byte(mainBody), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadResolvedYAMLNodeMergesAvatarModelConfigDir(t *testing.T) {
	path := writeResolvedConfigFixture(t, `
inference:
  avatar:
    default: flash_head
    model_config_dir: avatar_models
`, map[string]string{
		"flash_head": `
flash_head:
  plugin_class: pkg.FlashHead
  infer_params:
    width: 512
`,
	})

	doc, err := ReadResolvedYAMLNode(path)
	if err != nil {
		t.Fatal(err)
	}
	node, err := GetNodeAtPath(doc, "inference.avatar.flash_head.infer_params.width")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(NodeValue(node, true)); got != "512" {
		t.Fatalf("expected merged width 512, got %q", got)
	}
}

func TestReadResolvedYAMLNodeKeepsInlineAvatarModelConfig(t *testing.T) {
	path := writeResolvedConfigFixture(t, `
inference:
  avatar:
    model_config_dir: avatar_models
    flash_head:
      plugin_class: pkg.Inline
      compile_model: true
`, map[string]string{
		"flash_head": `
flash_head:
  plugin_class: pkg.External
  compile_model: false
`,
	})

	doc, err := ReadResolvedYAMLNode(path)
	if err != nil {
		t.Fatal(err)
	}
	node, err := GetNodeAtPath(doc, "inference.avatar.flash_head.plugin_class")
	if err != nil {
		t.Fatal(err)
	}
	if got := NodeScalarValue(node, true); got != "pkg.Inline" {
		t.Fatalf("expected inline plugin class, got %q", got)
	}
}

func TestReadResolvedYAMLNodeRejectsDuplicateExternalAvatarModels(t *testing.T) {
	path := writeResolvedConfigFixture(t, `
inference:
  avatar:
    model_config_dir: avatar_models
`, map[string]string{
		"one": `
flash_head:
  plugin_class: pkg.One
`,
		"two": `
flash_head:
  plugin_class: pkg.Two
`,
	})

	if _, err := ReadResolvedYAMLNode(path); err == nil {
		t.Fatal("expected duplicate external avatar model to fail")
	}
}

func TestAvatarModelConfigSourceReturnsExternalModelFile(t *testing.T) {
	path := writeResolvedConfigFixture(t, `
inference:
  avatar:
    model_config_dir: avatar_models
`, map[string]string{
		"live_act": `
live_act:
  plugin_class: pkg.LiveAct
`,
	})

	source, external, err := AvatarModelConfigSource(path, "live_act")
	if err != nil {
		t.Fatal(err)
	}
	if !external {
		t.Fatal("expected external source")
	}
	if filepath.Base(source) != "live_act.yaml" {
		t.Fatalf("expected live_act.yaml, got %s", source)
	}
}

func TestAvatarModelConfigSourcePrefersInlineModelConfig(t *testing.T) {
	path := writeResolvedConfigFixture(t, `
inference:
  avatar:
    model_config_dir: avatar_models
    flash_head:
      plugin_class: pkg.Inline
`, map[string]string{
		"flash_head": `
flash_head:
  plugin_class: pkg.External
`,
	})

	source, external, err := AvatarModelConfigSource(path, "flash_head")
	if err != nil {
		t.Fatal(err)
	}
	if external {
		t.Fatal("expected inline source")
	}
	if source != path {
		t.Fatalf("expected main config path, got %s", source)
	}
}

func writeConventionalConfigFixture(t *testing.T, mainBody string, files map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for dirName, dirFiles := range files {
		dir := filepath.Join(root, dirName)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for name, body := range dirFiles {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "cyberverse.yaml")
	if err := os.WriteFile(path, []byte(mainBody), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadResolvedYAMLNodeMergesConventionalModelDirs(t *testing.T) {
	path := writeConventionalConfigFixture(t, "inference: {}\n", map[string]map[string]string{
		filepath.Join("infra", "config", "llm_models"): {
			"qwen.yaml": `
qwen:
  plugin_class: pkg.Qwen
  model: qwen-test
`,
		},
	})

	doc, err := ReadResolvedYAMLNode(path)
	if err != nil {
		t.Fatal(err)
	}
	node, err := GetNodeAtPath(doc, "inference.llm.qwen.model")
	if err != nil {
		t.Fatal(err)
	}
	if got := NodeScalarValue(node, true); got != "qwen-test" {
		t.Fatalf("expected qwen-test, got %q", got)
	}
	if _, err := GetNodeAtPath(doc, "inference.llm.default"); err == nil {
		t.Fatal("did not expect default to be injected into resolved config")
	}
}

func TestReadResolvedYAMLNodeLocalConventionalConfigOverridesBuiltIn(t *testing.T) {
	path := writeConventionalConfigFixture(t, "inference: {}\n", map[string]map[string]string{
		filepath.Join("infra", "config", "tts_models"): {
			"qwen.yaml": `
qwen:
  plugin_class: pkg.Builtin
  voice: BuiltinVoice
`,
		},
		filepath.Join("config", "tts_models"): {
			"qwen.yaml": `
qwen:
  plugin_class: pkg.Local
  voice: LocalVoice
`,
		},
	})

	doc, err := ReadResolvedYAMLNode(path)
	if err != nil {
		t.Fatal(err)
	}
	node, err := GetNodeAtPath(doc, "inference.tts.qwen.voice")
	if err != nil {
		t.Fatal(err)
	}
	if got := NodeScalarValue(node, true); got != "LocalVoice" {
		t.Fatalf("expected LocalVoice, got %q", got)
	}
}

func TestReadResolvedYAMLNodeKeepsInlineConventionalModelConfig(t *testing.T) {
	path := writeConventionalConfigFixture(t, `
inference:
  asr:
    qwen:
      plugin_class: pkg.Inline
      model: inline-asr
`, map[string]map[string]string{
		filepath.Join("infra", "config", "asr_models"): {
			"qwen.yaml": `
qwen:
  plugin_class: pkg.External
  model: external-asr
`,
		},
	})

	doc, err := ReadResolvedYAMLNode(path)
	if err != nil {
		t.Fatal(err)
	}
	node, err := GetNodeAtPath(doc, "inference.asr.qwen.plugin_class")
	if err != nil {
		t.Fatal(err)
	}
	if got := NodeScalarValue(node, true); got != "pkg.Inline" {
		t.Fatalf("expected inline plugin class, got %q", got)
	}
}

func TestReadResolvedYAMLNodeRejectsDuplicateConventionalExternalModels(t *testing.T) {
	path := writeConventionalConfigFixture(t, "inference: {}\n", map[string]map[string]string{
		filepath.Join("infra", "config", "llm_models"): {
			"one.yaml": `
qwen:
  plugin_class: pkg.One
`,
			"two.yaml": `
qwen:
  plugin_class: pkg.Two
`,
		},
	})

	if _, err := ReadResolvedYAMLNode(path); err == nil {
		t.Fatal("expected duplicate external model config to fail")
	}
}
