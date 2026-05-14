package agenttask

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestStoreTaskEventAndArtifactLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "tasks.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	task, err := store.CreateTask(ctx, CreateTaskInput{
		SessionID:   "session-1",
		CharacterID: "char-1",
		Kind:        "research",
		UserRequest: "今天知乎有哪些热门信息",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Status != StatusQueued || task.Progress != 0 {
		t.Fatalf("unexpected task initial state: %+v", task)
	}

	event1, updated, err := store.AppendEvent(ctx, task.ID, AppendEventInput{
		EventType: "task.started",
		Status:    StatusRunning,
		Message:   "后台任务已启动。",
		Progress:  5,
	})
	if err != nil {
		t.Fatalf("AppendEvent started: %v", err)
	}
	if event1.Seq != 1 || updated.Status != StatusRunning || updated.Progress != 5 {
		t.Fatalf("unexpected started event/task: event=%+v task=%+v", event1, updated)
	}

	artifact, err := store.CreateArtifact(ctx, task.ID, CreateArtifactInput{
		Title:   "知乎热点资料",
		Content: "# 知乎热点\n",
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	htmlArtifact, err := store.CreateArtifact(ctx, task.ID, CreateArtifactInput{
		Type:     "html",
		Title:    "知乎热点页面",
		MimeType: "text/html; charset=utf-8",
		Content:  "<!doctype html><html></html>",
	})
	if err != nil {
		t.Fatalf("CreateArtifact html: %v", err)
	}
	if filepath.Ext(htmlArtifact.ContentPath) != ".html" {
		t.Fatalf("expected html artifact extension, got %s", htmlArtifact.ContentPath)
	}

	event2, updated, err := store.AppendEvent(ctx, task.ID, AppendEventInput{
		EventType: "task.completed",
		Status:    StatusCompleted,
		Message:   "任务完成。",
		Progress:  100,
	})
	if err != nil {
		t.Fatalf("AppendEvent completed: %v", err)
	}
	if event2.Seq != 2 || updated.Status != StatusCompleted || updated.FinishedAt == nil {
		t.Fatalf("unexpected completed event/task: event=%+v task=%+v", event2, updated)
	}

	events, err := store.ListEventsAfter(ctx, task.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEventsAfter: %v", err)
	}
	if len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("unexpected events: %+v", events)
	}

	gotArtifact, content, err := store.GetArtifact(ctx, task.ID, artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if gotArtifact.Title != "知乎热点资料" || string(content) != "# 知乎热点\n" {
		t.Fatalf("unexpected artifact: artifact=%+v content=%q", gotArtifact, content)
	}
}

func TestStorePersistsTaskOwnerAndFiltersByOwner(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "tasks.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	ownerTask, err := store.CreateTask(ctx, CreateTaskInput{
		ID:          "owner-task",
		SessionID:   "session-1",
		OwnerID:     "zhihu:101",
		UserRequest: "owner task",
	})
	if err != nil {
		t.Fatalf("CreateTask owner: %v", err)
	}
	if ownerTask.OwnerID != "zhihu:101" {
		t.Fatalf("expected owner on created task, got %+v", ownerTask)
	}
	if _, err := store.CreateTask(ctx, CreateTaskInput{
		ID:          "other-task",
		SessionID:   "session-1",
		OwnerID:     "zhihu:202",
		UserRequest: "other task",
	}); err != nil {
		t.Fatalf("CreateTask other: %v", err)
	}

	got, err := store.GetTask(ctx, ownerTask.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.OwnerID != "zhihu:101" {
		t.Fatalf("expected owner from GetTask, got %+v", got)
	}

	tasks, err := store.ListSessionTasksForOwner(ctx, "session-1", "zhihu:101", 10)
	if err != nil {
		t.Fatalf("ListSessionTasksForOwner: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != ownerTask.ID {
		t.Fatalf("expected only owner task, got %+v", tasks)
	}
}

func TestStoreMigratesLegacyTasksOwnerColumn(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "tasks.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE tasks (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		character_id TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL,
		title TEXT NOT NULL,
		user_request TEXT NOT NULL,
		status TEXT NOT NULL,
		progress INTEGER NOT NULL DEFAULT 0,
		result_summary TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		finished_at TEXT NOT NULL DEFAULT ''
	);`); err != nil {
		t.Fatalf("create legacy tasks table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks
		(id, session_id, character_id, kind, title, user_request, status, progress, created_at, updated_at)
		VALUES ('legacy-task', 'legacy-session', '', 'research', 'legacy', 'legacy', 'queued', 0, '2026-05-01T00:00:00Z', '2026-05-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert legacy task: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := OpenStore(dbPath, filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("OpenStore migrated: %v", err)
	}
	defer store.Close()
	task, err := store.GetTask(ctx, "legacy-task")
	if err != nil {
		t.Fatalf("GetTask legacy: %v", err)
	}
	if task.OwnerID != "" {
		t.Fatalf("expected legacy owner_id to default empty, got %+v", task)
	}
}

func TestStoreRejectsLateEventAndArtifactAfterTerminal(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "tasks.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	task, err := store.CreateTask(ctx, CreateTaskInput{
		SessionID:   "session-1",
		UserRequest: "取消后不要再写入",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, _, err := store.AppendEvent(ctx, task.ID, AppendEventInput{
		EventType: "task.cancelled",
		Status:    StatusCancelled,
		Message:   "任务已取消。",
		Progress:  0,
	}); err != nil {
		t.Fatalf("cancel task: %v", err)
	}

	if _, _, err := store.AppendEvent(ctx, task.ID, AppendEventInput{
		EventType: "task.completed",
		Status:    StatusCompleted,
		Message:   "迟到完成事件",
		Progress:  100,
	}); !errors.Is(err, ErrTerminal) {
		t.Fatalf("expected ErrTerminal for late event, got %v", err)
	}
	if _, err := store.CreateArtifact(ctx, task.ID, CreateArtifactInput{
		Title:   "迟到 artifact",
		Content: "# should not exist\n",
	}); !errors.Is(err, ErrTerminal) {
		t.Fatalf("expected ErrTerminal for late artifact, got %v", err)
	}

	events, err := store.ListEventsAfter(ctx, task.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEventsAfter: %v", err)
	}
	if len(events) != 1 || events[0].Status != StatusCancelled {
		t.Fatalf("unexpected events after terminal protection: %+v", events)
	}
}

func TestStoreLatestActiveTask(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "tasks.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	first, err := store.CreateTask(ctx, CreateTaskInput{SessionID: "session-1", UserRequest: "第一个任务"})
	if err != nil {
		t.Fatalf("CreateTask first: %v", err)
	}
	if _, _, err := store.AppendEvent(ctx, first.ID, AppendEventInput{EventType: "task.completed", Status: StatusCompleted, Progress: 100}); err != nil {
		t.Fatalf("complete first: %v", err)
	}
	second, err := store.CreateTask(ctx, CreateTaskInput{SessionID: "session-1", UserRequest: "第二个任务"})
	if err != nil {
		t.Fatalf("CreateTask second: %v", err)
	}
	if _, _, err := store.AppendEvent(ctx, second.ID, AppendEventInput{EventType: "task.started", Status: StatusRunning, Progress: 10}); err != nil {
		t.Fatalf("start second: %v", err)
	}

	active, err := store.LatestActiveTask(ctx, "session-1")
	if err != nil {
		t.Fatalf("LatestActiveTask: %v", err)
	}
	if active.ID != second.ID {
		t.Fatalf("expected second active task, got %+v", active)
	}
}

func TestStoreAcceptsExternalTaskAndArtifactIDs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "tasks.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	task, err := store.CreateTask(ctx, CreateTaskInput{
		ID:          "persona-task-1",
		SessionID:   "session-1",
		UserRequest: "查看知乎热榜",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID != "persona-task-1" {
		t.Fatalf("expected external task id, got %q", task.ID)
	}
	tasks, err := store.ListSessionTasks(ctx, "session-1", 10)
	if err != nil {
		t.Fatalf("ListSessionTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "persona-task-1" {
		t.Fatalf("expected external task in list, got %+v", tasks)
	}

	artifact, err := store.CreateArtifact(ctx, task.ID, CreateArtifactInput{
		ID:       "persona-artifact-1",
		Type:     "html",
		Title:    "知乎热榜",
		MimeType: "text/html; charset=utf-8",
		Content:  "<!doctype html><html><body>ok</body></html>",
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	if artifact.ID != "persona-artifact-1" {
		t.Fatalf("expected external artifact id, got %q", artifact.ID)
	}
	gotArtifact, content, err := store.GetArtifact(ctx, task.ID, "persona-artifact-1")
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if gotArtifact.MimeType != "text/html; charset=utf-8" || string(content) != "<!doctype html><html><body>ok</body></html>" {
		t.Fatalf("unexpected artifact: artifact=%+v content=%q", gotArtifact, content)
	}
}

func TestStoreRejectsExternalIDsWithPathSegments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := OpenStore(filepath.Join(root, "tasks.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	if _, err := store.CreateTask(ctx, CreateTaskInput{
		ID:          "../persona-task",
		SessionID:   "session-1",
		UserRequest: "非法 task id",
	}); err == nil {
		t.Fatal("expected external task id with path segment to be rejected")
	}

	task, err := store.CreateTask(ctx, CreateTaskInput{
		ID:          "persona-task-safe",
		SessionID:   "session-1",
		UserRequest: "安全 task id",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := store.CreateArtifact(ctx, task.ID, CreateArtifactInput{
		ID:      `..\persona-artifact`,
		Content: "bad",
	}); err == nil {
		t.Fatal("expected external artifact id with path segment to be rejected")
	}
}
