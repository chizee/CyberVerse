package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cyberverse/server/internal/agenttask"
	"github.com/cyberverse/server/internal/character"
	"github.com/cyberverse/server/internal/orchestrator"
)

func TestListSessionTasksAllowsClosedSession(t *testing.T) {
	root := t.TempDir()
	taskStore, err := agenttask.OpenStore(filepath.Join(root, "tasks.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer taskStore.Close()
	task, err := taskStore.CreateTask(context.Background(), agenttask.CreateTaskInput{
		ID:          "task-closed-session",
		SessionID:   "closed-session",
		UserRequest: "恢复历史任务",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	router := NewRouter(
		orchestrator.NewSessionManager(1),
		nil,
		nil,
		nil,
		nil,
		charStore,
		"",
		"",
		agenttask.NewService(taskStore, nil),
	)
	req := httptest.NewRequest("GET", "/api/v1/sessions/closed-session/tasks", nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Tasks []agenttask.Task `json:"tasks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != task.ID {
		t.Fatalf("unexpected tasks response: %+v", resp.Tasks)
	}
}

func TestGetTaskArtifactReturnsUTF8CharsetForTextArtifact(t *testing.T) {
	root := t.TempDir()
	taskStore, err := agenttask.OpenStore(filepath.Join(root, "tasks.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer taskStore.Close()
	task, err := taskStore.CreateTask(context.Background(), agenttask.CreateTaskInput{
		ID:          "task-markdown",
		SessionID:   "session-markdown",
		UserRequest: "整理检查项",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	artifact, err := taskStore.CreateArtifact(context.Background(), task.ID, agenttask.CreateArtifactInput{
		Title:    "检查项",
		Type:     "markdown",
		MimeType: "text/markdown",
		Content:  "# 检查项\n",
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	router := NewRouter(
		orchestrator.NewSessionManager(1),
		nil,
		nil,
		nil,
		nil,
		charStore,
		"",
		"",
		agenttask.NewService(taskStore, nil),
	)
	req := httptest.NewRequest("GET", "/api/v1/tasks/task-markdown/artifacts/"+artifact.ID, nil)
	w := httptest.NewRecorder()
	router.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Fatalf("expected utf-8 markdown content type, got %q", got)
	}
	if got := w.Body.String(); got != "# 检查项\n" {
		t.Fatalf("unexpected artifact body: %q", got)
	}
}
