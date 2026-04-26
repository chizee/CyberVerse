package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCharacterResponsesOmitAvatarModel(t *testing.T) {
	r := newTestRouter()

	createBody := `{
		"name":"角色A",
		"description":"test",
		"voice_provider":"doubao",
		"voice_type":"温柔文雅",
		"avatar_model":"flash_head"
	}`
	req := httptest.NewRequest("POST", "/api/v1/characters", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	var created map[string]any
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if _, ok := created["avatar_model"]; ok {
		t.Fatalf("expected create response to omit avatar_model, got %v", created["avatar_model"])
	}

	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatalf("expected response id, got %v", created["id"])
	}

	req = httptest.NewRequest("GET", "/api/v1/characters/"+id, nil)
	w = httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var fetched map[string]any
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatal(err)
	}
	if _, ok := fetched["avatar_model"]; ok {
		t.Fatalf("expected get response to omit avatar_model, got %v", fetched["avatar_model"])
	}

	updateBody := `{
		"name":"角色A",
		"description":"updated",
		"voice_provider":"doubao",
		"voice_type":"温柔文雅",
		"avatar_model":"live_act"
	}`
	req = httptest.NewRequest("PUT", "/api/v1/characters/"+id, strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var updated map[string]any
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if _, ok := updated["avatar_model"]; ok {
		t.Fatalf("expected update response to omit avatar_model, got %v", updated["avatar_model"])
	}
}
