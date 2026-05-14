package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/agenttask"
	"github.com/cyberverse/server/internal/character"
	"github.com/cyberverse/server/internal/inference"
	"github.com/cyberverse/server/internal/kanshan"
	"github.com/cyberverse/server/internal/orchestrator"
	pb "github.com/cyberverse/server/internal/pb"
	"github.com/cyberverse/server/internal/ws"
)

func addZhihuTestCookie(r *Router, req *http.Request, cookieValue string, user zhihuUser) {
	r.zhihuAuth.storeSession(cookieValue, zhihuSession{
		AccessToken: "token-" + cookieValue,
		User:        user,
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	req.AddCookie(&http.Cookie{Name: zhihuSessionCookie, Value: cookieValue})
}

func createKanshanSessionRequest(r *Router, user zhihuUser) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(`{"mode":"omni","character_id":"`+kanshan.CharacterID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if user.UID != 0 || user.HashID != "" {
		addZhihuTestCookie(r, req, "cookie-"+time.Now().Format("150405.000000000"), user)
	}
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	return w
}

func responseSessionIDs(t *testing.T, body *strings.Reader) map[string]bool {
	t.Helper()
	var sessions []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	ids := map[string]bool{}
	for _, session := range sessions {
		ids[session.ID] = true
	}
	return ids
}

func TestKanshanSessionRequiresZhihuOwnerAndIsolatesAccess(t *testing.T) {
	charStore := newTestCharStore(t)
	mgr := orchestrator.NewSessionManager(10)
	r := NewRouter(mgr, nil, nil, nil, nil, charStore, "", "")

	w := createKanshanSessionRequest(r, zhihuUser{})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated Kanshan session create to return 401, got %d: %s", w.Code, w.Body.String())
	}

	w = createKanshanSessionRequest(r, zhihuUser{UID: 101, Fullname: "用户 A"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected authenticated Kanshan session create to return 201, got %d: %s", w.Code, w.Body.String())
	}
	var kanshanResp CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&kanshanResp); err != nil {
		t.Fatal(err)
	}
	session, err := mgr.Get(kanshanResp.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if got := session.OwnerIDSnapshot(); got != "zhihu:101" {
		t.Fatalf("expected owner zhihu:101, got %q", got)
	}

	publicReq := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(`{"mode":"standard"}`))
	publicW := httptest.NewRecorder()
	r.Handler().ServeHTTP(publicW, publicReq)
	if publicW.Code != http.StatusCreated {
		t.Fatalf("expected public session create 201, got %d: %s", publicW.Code, publicW.Body.String())
	}
	var publicResp CreateSessionResponse
	if err := json.NewDecoder(publicW.Body).Decode(&publicResp); err != nil {
		t.Fatal(err)
	}

	listReq := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	listW := httptest.NewRecorder()
	r.Handler().ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("expected list sessions 200, got %d: %s", listW.Code, listW.Body.String())
	}
	ids := responseSessionIDs(t, strings.NewReader(listW.Body.String()))
	if ids[kanshanResp.SessionID] || !ids[publicResp.SessionID] {
		t.Fatalf("unauthenticated session list should hide Kanshan and include public sessions, got %+v", ids)
	}

	listReqA := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	addZhihuTestCookie(r, listReqA, "cookie-list-a", zhihuUser{UID: 101})
	listWA := httptest.NewRecorder()
	r.Handler().ServeHTTP(listWA, listReqA)
	ids = responseSessionIDs(t, strings.NewReader(listWA.Body.String()))
	if !ids[kanshanResp.SessionID] || !ids[publicResp.SessionID] {
		t.Fatalf("owner session list should include owned Kanshan and public sessions, got %+v", ids)
	}

	listReqB := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	addZhihuTestCookie(r, listReqB, "cookie-list-b", zhihuUser{UID: 202})
	listWB := httptest.NewRecorder()
	r.Handler().ServeHTTP(listWB, listReqB)
	ids = responseSessionIDs(t, strings.NewReader(listWB.Body.String()))
	if ids[kanshanResp.SessionID] || !ids[publicResp.SessionID] {
		t.Fatalf("other user session list should hide Kanshan and include public sessions, got %+v", ids)
	}

	messageReq := httptest.NewRequest("POST", "/api/v1/sessions/"+kanshanResp.SessionID+"/message", strings.NewReader(`{"text":"hi"}`))
	messageReq.Header.Set("Content-Type", "application/json")
	addZhihuTestCookie(r, messageReq, "cookie-message-b", zhihuUser{UID: 202})
	messageW := httptest.NewRecorder()
	r.Handler().ServeHTTP(messageW, messageReq)
	if messageW.Code != http.StatusNotFound {
		t.Fatalf("expected cross-owner message to return 404, got %d: %s", messageW.Code, messageW.Body.String())
	}

	wsReq := httptest.NewRequest("GET", "/ws/chat/"+kanshanResp.SessionID, nil)
	addZhihuTestCookie(r, wsReq, "cookie-ws-b", zhihuUser{UID: 202})
	wsW := httptest.NewRecorder()
	r.Handler().ServeHTTP(wsW, wsReq)
	if wsW.Code != http.StatusNotFound {
		t.Fatalf("expected cross-owner websocket to return 404, got %d: %s", wsW.Code, wsW.Body.String())
	}

	deleteReq := httptest.NewRequest("DELETE", "/api/v1/sessions/"+kanshanResp.SessionID, nil)
	addZhihuTestCookie(r, deleteReq, "cookie-delete-a", zhihuUser{UID: 101})
	deleteW := httptest.NewRecorder()
	r.Handler().ServeHTTP(deleteW, deleteReq)
	if deleteW.Code != http.StatusNoContent {
		t.Fatalf("expected owner delete to return 204, got %d: %s", deleteW.Code, deleteW.Body.String())
	}
}

func newSeededKanshanRouter(t *testing.T, inf *fakeInferenceService) *Router {
	t.Helper()
	root := t.TempDir()
	charDir := filepath.Join(root, "liukanshan")
	if err := os.MkdirAll(filepath.Join(charDir, "sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	record := character.Character{
		ID:        kanshan.CharacterID,
		Name:      "刘看山",
		Mode:      "omni",
		VoiceType: "温柔文雅",
		CreatedAt: now,
		UpdatedAt: now,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(charDir, "character.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	charStore, err := character.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if inf.avatarInfo == nil {
		inf.avatarInfo = &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25, OutputWidth: 512, OutputHeight: 512}
	}
	mgr := orchestrator.NewSessionManager(4)
	hub := ws.NewHub()
	orch := orchestrator.New(inf, hub, mgr, nil, charStore)
	return NewRouter(mgr, orch, hub, nil, nil, charStore, "", "")
}

func TestKanshanHistoryLoadsOnlyCurrentZhihuOwner(t *testing.T) {
	inf := &fakeInferenceService{
		avatarInfo:   &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25, OutputWidth: 512, OutputHeight: 512},
		voiceConfigs: make(chan inference.VoiceLLMSessionConfig, 1),
	}
	r := newSeededKanshanRouter(t, inf)

	started := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := r.charStore.SaveConversation(kanshan.CharacterID, "global-session", started, started.Add(time.Minute), []map[string]any{
		{"role": "user", "content": "global memory", "timestamp": started.Format(time.RFC3339Nano)},
		{"role": "assistant", "content": "global reply", "timestamp": started.Add(time.Second).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.charStore.SaveConversationForOwner(kanshan.CharacterID, "zhihu:101", "owner-a-session", started.Add(time.Hour), started.Add(time.Hour+time.Minute), []map[string]any{
		{"role": "user", "content": "owner A memory", "timestamp": started.Add(time.Hour).Format(time.RFC3339Nano)},
		{"role": "assistant", "content": "owner A reply", "timestamp": started.Add(time.Hour + time.Second).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.charStore.SaveConversationForOwner(kanshan.CharacterID, "zhihu:202", "owner-b-session", started.Add(2*time.Hour), started.Add(2*time.Hour+time.Minute), []map[string]any{
		{"role": "user", "content": "owner B memory", "timestamp": started.Add(2 * time.Hour).Format(time.RFC3339Nano)},
		{"role": "assistant", "content": "owner B reply", "timestamp": started.Add(2*time.Hour + time.Second).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatal(err)
	}

	w := createKanshanSessionRequest(r, zhihuUser{UID: 101, Fullname: "用户 A"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected session create 201, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case config := <-inf.voiceConfigs:
		if len(config.DialogContext) != 2 {
			t.Fatalf("expected owner-scoped dialog context pair, got %+v", config.DialogContext)
		}
		if config.DialogContext[0].Text != "owner A memory" {
			t.Fatalf("expected owner A memory only, got %+v", config.DialogContext)
		}
		if config.DialogContext[1].Text != "owner A reply" {
			t.Fatalf("expected owner A reply only, got %+v", config.DialogContext)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for omni config")
	}
}

func TestKanshanTaskAccessRequiresMatchingOwner(t *testing.T) {
	root := t.TempDir()
	taskStore, err := agenttask.OpenStore(filepath.Join(root, "tasks.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer taskStore.Close()
	task, err := taskStore.CreateTask(context.Background(), agenttask.CreateTaskInput{
		ID:          "task-owner-a",
		SessionID:   "session-owner-a",
		CharacterID: kanshan.CharacterID,
		OwnerID:     "zhihu:101",
		UserRequest: "整理我的知乎动态",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, _, err := taskStore.AppendEvent(context.Background(), task.ID, agenttask.AppendEventInput{
		EventType: "task.started",
		Status:    agenttask.StatusRunning,
		Message:   "started",
		Progress:  10,
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	artifact, err := taskStore.CreateArtifact(context.Background(), task.ID, agenttask.CreateArtifactInput{
		ID:      "artifact-owner-a",
		Title:   "private artifact",
		Content: "private content",
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	charStore, err := character.NewStore(filepath.Join(root, "characters"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	r := NewRouter(
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

	unauthReq := httptest.NewRequest("GET", "/api/v1/tasks/"+task.ID, nil)
	unauthW := httptest.NewRecorder()
	r.Handler().ServeHTTP(unauthW, unauthReq)
	if unauthW.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated private task to return 401, got %d: %s", unauthW.Code, unauthW.Body.String())
	}

	otherReq := httptest.NewRequest("GET", "/api/v1/tasks/"+task.ID, nil)
	addZhihuTestCookie(r, otherReq, "cookie-task-b", zhihuUser{UID: 202})
	otherW := httptest.NewRecorder()
	r.Handler().ServeHTTP(otherW, otherReq)
	if otherW.Code != http.StatusNotFound {
		t.Fatalf("expected cross-owner private task to return 404, got %d: %s", otherW.Code, otherW.Body.String())
	}

	ownerReq := httptest.NewRequest("GET", "/api/v1/tasks/"+task.ID, nil)
	addZhihuTestCookie(r, ownerReq, "cookie-task-a", zhihuUser{UID: 101})
	ownerW := httptest.NewRecorder()
	r.Handler().ServeHTTP(ownerW, ownerReq)
	if ownerW.Code != http.StatusOK {
		t.Fatalf("expected owner private task to return 200, got %d: %s", ownerW.Code, ownerW.Body.String())
	}
	var raw map[string]any
	if err := json.NewDecoder(ownerW.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["owner_id"]; ok {
		t.Fatalf("owner_id should not be exposed in task JSON: %+v", raw)
	}

	eventsReq := httptest.NewRequest("GET", "/api/v1/tasks/"+task.ID+"/events", nil)
	addZhihuTestCookie(r, eventsReq, "cookie-events-b", zhihuUser{UID: 202})
	eventsW := httptest.NewRecorder()
	r.Handler().ServeHTTP(eventsW, eventsReq)
	if eventsW.Code != http.StatusNotFound {
		t.Fatalf("expected cross-owner events to return 404, got %d: %s", eventsW.Code, eventsW.Body.String())
	}

	artifactReq := httptest.NewRequest("GET", "/api/v1/tasks/"+task.ID+"/artifacts/"+artifact.ID, nil)
	addZhihuTestCookie(r, artifactReq, "cookie-artifact-a", zhihuUser{UID: 101})
	artifactW := httptest.NewRecorder()
	r.Handler().ServeHTTP(artifactW, artifactReq)
	if artifactW.Code != http.StatusOK || artifactW.Body.String() != "private content" {
		t.Fatalf("expected owner artifact content, got code=%d body=%q", artifactW.Code, artifactW.Body.String())
	}

	sessionTasksReq := httptest.NewRequest("GET", "/api/v1/sessions/session-owner-a/tasks", nil)
	sessionTasksW := httptest.NewRecorder()
	r.Handler().ServeHTTP(sessionTasksW, sessionTasksReq)
	if sessionTasksW.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated private session tasks to return 401, got %d: %s", sessionTasksW.Code, sessionTasksW.Body.String())
	}

	sessionTasksOtherReq := httptest.NewRequest("GET", "/api/v1/sessions/session-owner-a/tasks", nil)
	addZhihuTestCookie(r, sessionTasksOtherReq, "cookie-session-tasks-b", zhihuUser{UID: 202})
	sessionTasksOtherW := httptest.NewRecorder()
	r.Handler().ServeHTTP(sessionTasksOtherW, sessionTasksOtherReq)
	if sessionTasksOtherW.Code != http.StatusNotFound {
		t.Fatalf("expected cross-owner private session tasks to return 404, got %d: %s", sessionTasksOtherW.Code, sessionTasksOtherW.Body.String())
	}

	sessionTasksOwnerReq := httptest.NewRequest("GET", "/api/v1/sessions/session-owner-a/tasks", nil)
	addZhihuTestCookie(r, sessionTasksOwnerReq, "cookie-session-tasks-a", zhihuUser{UID: 101})
	sessionTasksOwnerW := httptest.NewRecorder()
	r.Handler().ServeHTTP(sessionTasksOwnerW, sessionTasksOwnerReq)
	if sessionTasksOwnerW.Code != http.StatusOK {
		t.Fatalf("expected owner private session tasks to return 200, got %d: %s", sessionTasksOwnerW.Code, sessionTasksOwnerW.Body.String())
	}
	var sessionTasksResp struct {
		Tasks []agenttask.Task `json:"tasks"`
	}
	if err := json.NewDecoder(sessionTasksOwnerW.Body).Decode(&sessionTasksResp); err != nil {
		t.Fatal(err)
	}
	if len(sessionTasksResp.Tasks) != 1 || sessionTasksResp.Tasks[0].ID != task.ID {
		t.Fatalf("expected only owner task in session task list, got %+v", sessionTasksResp.Tasks)
	}
}
