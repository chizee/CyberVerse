package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cyberverse/server/internal/character"
	"github.com/cyberverse/server/internal/orchestrator"
	pb "github.com/cyberverse/server/internal/pb"
	"github.com/cyberverse/server/internal/ws"
)

func newTestCharStore(t *testing.T) *character.Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "chartest-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := character.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newTestRouter() *Router {
	mgr := orchestrator.NewSessionManager(4)
	hub := ws.NewHub()
	// Use a temp dir for charStore in tests
	dir, _ := os.MkdirTemp("", "chartest-*")
	cs, _ := character.NewStore(dir)
	orch := orchestrator.New(&fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25},
	}, hub, mgr, nil, cs)
	return NewRouter(mgr, orch, hub, nil, nil, cs, "", "")
}

func newTestRouterWithMgr(mgr *orchestrator.SessionManager) *Router {
	hub := ws.NewHub()
	dir, _ := os.MkdirTemp("", "chartest-*")
	cs, _ := character.NewStore(dir)
	orch := orchestrator.New(&fakeInferenceService{
		avatarInfo: &pb.AvatarInfo{ModelName: "avatar.flash_head", OutputFps: 25},
	}, hub, mgr, nil, cs)
	return NewRouter(mgr, orch, hub, nil, nil, cs, "", "")
}

func TestHealthEndpoint(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
}

func TestCreateSession(t *testing.T) {
	r := newTestRouter()
	body := `{"mode": "voice_llm"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}

	var resp CreateSessionResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.SessionID == "" {
		t.Error("expected non-empty session_id")
	}
	if resp.TextInputEnabled {
		t.Error("expected text input to be disabled when QWEN_API_KEY is not configured")
	}
}

func TestCreateSessionInvalidJSON(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateSessionMaxConcurrent(t *testing.T) {
	mgr := orchestrator.NewSessionManager(1)
	r := newTestRouterWithMgr(mgr)

	body := `{"mode": "standard"}`
	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	req2 := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(body))
	w2 := httptest.NewRecorder()
	r.Handler().ServeHTTP(w2, req2)
	if w2.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w2.Code)
	}
}

func TestDeleteSession(t *testing.T) {
	r := newTestRouter()

	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(`{"mode":"voice_llm"}`))
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	var resp CreateSessionResponse
	json.NewDecoder(w.Body).Decode(&resp)

	req2 := httptest.NewRequest("DELETE", "/api/v1/sessions/"+resp.SessionID, nil)
	w2 := httptest.NewRecorder()
	r.Handler().ServeHTTP(w2, req2)

	if w2.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w2.Code)
	}
}

func TestDeleteSessionNotFound(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("DELETE", "/api/v1/sessions/nonexistent", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestSendMessage(t *testing.T) {
	t.Setenv("QWEN_API_KEY", "test-qwen-key")
	r := newTestRouter()

	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(`{"mode":"voice_llm"}`))
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	var resp CreateSessionResponse
	json.NewDecoder(w.Body).Decode(&resp)

	body := `{"text": "Hello"}`
	req2 := httptest.NewRequest("POST", "/api/v1/sessions/"+resp.SessionID+"/message", strings.NewReader(body))
	w2 := httptest.NewRecorder()
	r.Handler().ServeHTTP(w2, req2)

	if w2.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w2.Code)
	}
}

func TestSendMessageRequiresQwenKey(t *testing.T) {
	r := newTestRouter()

	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(`{"mode":"voice_llm"}`))
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	var resp CreateSessionResponse
	json.NewDecoder(w.Body).Decode(&resp)

	body := `{"text": "Hello"}`
	req2 := httptest.NewRequest("POST", "/api/v1/sessions/"+resp.SessionID+"/message", strings.NewReader(body))
	w2 := httptest.NewRecorder()
	r.Handler().ServeHTTP(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w2.Code)
	}

	var errResp ErrorResponse
	if err := json.NewDecoder(w2.Body).Decode(&errResp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errResp.Error, "QWEN_API_KEY") {
		t.Fatalf("expected QWEN_API_KEY hint, got %q", errResp.Error)
	}
}

func TestSendMessageEmptyText(t *testing.T) {
	r := newTestRouter()

	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(`{"mode":"voice_llm"}`))
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	var resp CreateSessionResponse
	json.NewDecoder(w.Body).Decode(&resp)

	req2 := httptest.NewRequest("POST", "/api/v1/sessions/"+resp.SessionID+"/message", strings.NewReader(`{"text": ""}`))
	w2 := httptest.NewRecorder()
	r.Handler().ServeHTTP(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w2.Code)
	}
}

func TestSendMessageInvalidJSON(t *testing.T) {
	r := newTestRouter()

	req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(`{"mode":"voice_llm"}`))
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	var resp CreateSessionResponse
	json.NewDecoder(w.Body).Decode(&resp)

	req2 := httptest.NewRequest("POST", "/api/v1/sessions/"+resp.SessionID+"/message", strings.NewReader("bad"))
	w2 := httptest.NewRecorder()
	r.Handler().ServeHTTP(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w2.Code)
	}
}

func TestListSessions(t *testing.T) {
	r := newTestRouter()

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(`{"mode":"voice_llm"}`))
		w := httptest.NewRecorder()
		r.Handler().ServeHTTP(w, req)
	}

	req := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var sessions []map[string]string
	json.NewDecoder(w.Body).Decode(&sessions)
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestCORSHeaders(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("OPTIONS", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS Allow-Origin header")
	}
}
