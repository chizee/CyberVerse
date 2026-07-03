package main

import (
	"os"
	"path/filepath"
	"testing"
)

func chdirTemp(t *testing.T) string {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatal(err)
		}
	})
	return root
}

func TestResolveStartupConfigPathAllowsDefaultFallback(t *testing.T) {
	root := chdirTemp(t)
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "cyberverse.yaml"), []byte("server: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := resolveStartupConfigPath("missing.yaml", true)

	if got != "config/cyberverse.yaml" {
		t.Fatalf("expected default fallback, got %q", got)
	}
}

func TestResolveStartupConfigPathKeepsExplicitMissingPath(t *testing.T) {
	root := chdirTemp(t)
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "cyberverse.yaml"), []byte("server: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := resolveStartupConfigPath("missing.yaml", false)

	if got != "missing.yaml" {
		t.Fatalf("expected explicit missing path to be preserved, got %q", got)
	}
}

func TestResolveStartupConfigPathFallsBackForExplicitDefaultPath(t *testing.T) {
	root := chdirTemp(t)
	serverDir := filepath.Join(root, "server")
	if err := os.MkdirAll(serverDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cyberverse_config.yaml"), []byte("server: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(serverDir); err != nil {
		t.Fatal(err)
	}

	got := resolveStartupConfigPath("../config/cyberverse.yaml", false)

	if got != "../cyberverse_config.yaml" {
		t.Fatalf("expected explicit default path to fall back to legacy config, got %q", got)
	}
}
