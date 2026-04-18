package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var envMu sync.Mutex

// LoadDotenv reads a .env file and sets each KEY=VALUE pair into the process
// environment via os.Setenv. Lines starting with # and blank lines are skipped.
// Returns nil if the file does not exist.
func LoadDotenv(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Strip surrounding quotes if present
		if len(value) >= 2 &&
			((value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		os.Setenv(key, value)
	}
	return scanner.Err()
}

// SaveDotenv merges updates into an existing .env file. Existing keys are
// updated in place; new keys are appended. The write is atomic (temp file +
// rename) and protected by a package-level mutex.
func SaveDotenv(path string, updates map[string]string) error {
	envMu.Lock()
	defer envMu.Unlock()

	// Read existing lines (may not exist yet).
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		lines = strings.Split(string(data), "\n")
	}

	// Track which keys we've already updated in existing lines.
	updated := make(map[string]bool, len(updates))

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if newVal, exists := updates[key]; exists {
			lines[i] = fmt.Sprintf("%s=%s", key, newVal)
			updated[key] = true
		}
	}

	// Append keys that weren't already present.
	for key, val := range updates {
		if !updated[key] {
			lines = append(lines, fmt.Sprintf("%s=%s", key, val))
		}
	}

	// Remove trailing empty lines, then ensure a final newline.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	content := strings.Join(lines, "\n") + "\n"

	// Atomic write: temp file in same dir, then rename.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".env.tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
