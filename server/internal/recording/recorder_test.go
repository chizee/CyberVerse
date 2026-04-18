package recording

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cyberverse/server/internal/config"
)

func TestSaveTranscriptWritesTurnTextFile(t *testing.T) {
	tmpDir := t.TempDir()
	recorder := NewVideoRecorder(config.RecordingConfig{
		Enabled:   true,
		OutputDir: tmpDir,
	})

	const transcript = "豆包回复文本"
	if err := recorder.SaveTranscript("session-1", "turn1", transcript); err != nil {
		t.Fatalf("SaveTranscript returned error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(tmpDir, "session-1", "turn1.txt"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(got) != transcript {
		t.Fatalf("expected transcript %q, got %q", transcript, string(got))
	}
}

func TestSaveTranscriptUsesAbsoluteSessionDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "sessions", "session-abs")
	recorder := NewVideoRecorder(config.RecordingConfig{
		Enabled:   true,
		OutputDir: filepath.Join(tmpDir, "ignored"),
	})

	if err := recorder.SaveTranscript(sessionDir, "turn2", "absolute transcript"); err != nil {
		t.Fatalf("SaveTranscript returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sessionDir, "turn2.txt")); err != nil {
		t.Fatalf("expected transcript file in absolute session dir: %v", err)
	}
}
