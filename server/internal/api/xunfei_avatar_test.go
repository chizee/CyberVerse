package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/character"
	"github.com/gorilla/websocket"
)

func TestStartXunfeiAvatarSessionConfig(t *testing.T) {
	stopCh := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected WebSocket GET, got %s", req.Method)
		}
		if req.URL.Query().Get("authorization") == "" || req.URL.Query().Get("date") == "" || req.URL.Query().Get("host") == "" {
			t.Fatalf("expected signed query, got %s", req.URL.RawQuery)
		}
		if req.URL.Path != "/v1/interact" {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		for {
			var packet struct {
				Header struct {
					Ctrl      string `json:"ctrl"`
					RequestID string `json:"request_id"`
					SceneID   string `json:"scene_id"`
				} `json:"header"`
				Parameter struct {
					Avatar struct {
						AvatarID string `json:"avatar_id"`
						Stream   struct {
							Protocol string `json:"protocol"`
						} `json:"stream"`
					} `json:"avatar"`
					TTS struct {
						VCN string `json:"vcn"`
					} `json:"tts"`
				} `json:"parameter"`
			}
			if err := conn.ReadJSON(&packet); err != nil {
				t.Fatal(err)
			}
			switch packet.Header.Ctrl {
			case "start":
				if packet.Header.SceneID != "scene-1" || packet.Parameter.Avatar.AvatarID != "avatar-1" {
					t.Fatalf("unexpected start packet: %+v", packet)
				}
				if packet.Parameter.TTS.VCN != "default-vcn" {
					t.Fatalf("expected default VCN in start packet, got %+v", packet.Parameter.TTS)
				}
				_ = conn.WriteJSON(map[string]any{
					"header": map[string]any{
						"code":    0,
						"message": "success",
						"session": "xf-session",
						"sid":     "xf-sid",
						"status":  0,
					},
					"payload": map[string]any{
						"avatar": map[string]any{
							"request_id": packet.Header.RequestID,
							"event_type": "stream_info",
							"stream_url": "https://example.test/live/avatar.flv",
						},
					},
				})
			case "stop":
				_ = conn.WriteJSON(map[string]any{
					"header": map[string]any{"code": 0, "message": "success", "session": "xf-session"},
					"payload": map[string]any{
						"avatar": map[string]any{
							"request_id": packet.Header.RequestID,
							"event_type": "stop",
						},
					},
				})
				stopCh <- struct{}{}
				return
			default:
				t.Fatalf("unexpected ctrl %q", packet.Header.Ctrl)
			}
		}
	}))
	t.Cleanup(apiServer.Close)

	t.Setenv("XUNFEI_AVATAR_APP_ID", "app-id")
	t.Setenv("XUNFEI_AVATAR_API_KEY", "api-key")
	t.Setenv("XUNFEI_AVATAR_API_SECRET", "api-secret")
	t.Setenv("XUNFEI_AVATAR_INTERACT_URL", "ws"+strings.TrimPrefix(apiServer.URL, "http")+"/v1/interact")
	t.Setenv("XUNFEI_AVATAR_DEFAULT_VCN", "default-vcn")

	runtime, cfg, err := startXunfeiAvatarSession(context.Background(), &character.Character{
		AvatarBackend: character.AvatarBackendXunfei,
		Xunfei: &character.XunfeiAvatar{
			AvatarID: " avatar-1 ",
			SceneID:  " scene-1 ",
			Width:    721,
			Height:   1281,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = runtime.Stop(context.Background())
	})
	if cfg.StreamURL != "https://example.test/live/avatar.flv" {
		t.Fatalf("expected stream URL from start response, got %+v", cfg)
	}
	if cfg.AvatarID != "avatar-1" || cfg.SceneID != "scene-1" || cfg.VCN != "default-vcn" {
		t.Fatalf("expected trimmed avatar config, got %+v", cfg)
	}
	if cfg.Protocol != "flv" || cfg.Width != 720 || cfg.Height != 1280 || cfg.FPS != 25 || cfg.Bitrate != 2000 {
		t.Fatalf("expected normalized stream defaults, got %+v", cfg)
	}
	if cfg.AudioSampleRate != 16000 || cfg.AudioMaxPCMBytes != 10240 {
		t.Fatalf("expected audio driver limits, got %+v", cfg)
	}
	_ = runtime.Stop(context.Background())
	select {
	case <-stopCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stop")
	}
}

func TestStartXunfeiAvatarSessionRequiresCredentials(t *testing.T) {
	runtime, cfg, err := startXunfeiAvatarSession(context.Background(), &character.Character{
		AvatarBackend: character.AvatarBackendXunfei,
		Xunfei:        &character.XunfeiAvatar{AvatarID: "avatar-1"},
	})
	if err == nil {
		t.Fatalf("expected missing credential error, got runtime=%+v config=%+v", runtime, cfg)
	}
	if !strings.Contains(err.Error(), "credentials are not configured") {
		t.Fatalf("expected credential error, got %v", err)
	}
}

func TestStartXunfeiAvatarSessionRequiresSceneID(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_APP_ID", "app-id")
	t.Setenv("XUNFEI_AVATAR_API_KEY", "api-key")
	t.Setenv("XUNFEI_AVATAR_API_SECRET", "api-secret")

	runtime, cfg, err := startXunfeiAvatarSession(context.Background(), &character.Character{
		AvatarBackend: character.AvatarBackendXunfei,
		Xunfei:        &character.XunfeiAvatar{AvatarID: "avatar-1", VCN: "vcn-1"},
	})
	if err == nil {
		t.Fatalf("expected missing scene_id error, got runtime=%+v config=%+v", runtime, cfg)
	}
	if !strings.Contains(err.Error(), "scene_id is required") {
		t.Fatalf("expected scene_id error, got %v", err)
	}
}

func TestCharacterForXunfeiDoesNotInventVCN(t *testing.T) {
	cfg := characterForXunfei(&character.Character{
		AvatarBackend: character.AvatarBackendXunfei,
		Xunfei:        &character.XunfeiAvatar{AvatarID: "201165002"},
	})
	if cfg == nil {
		t.Fatal("expected Xunfei config")
	}
	if cfg.VCN != "" {
		t.Fatalf("expected VCN to stay empty when the catalog has no VCN, got %+v", cfg)
	}
	if cfg.SourceImageURL == "" {
		t.Fatalf("expected catalog media metadata, got %+v", cfg)
	}
}

func TestCharacterForXunfeiPreservesRequestedProtocol(t *testing.T) {
	cfg := characterForXunfei(&character.Character{
		AvatarBackend: character.AvatarBackendXunfei,
		Xunfei: &character.XunfeiAvatar{
			AvatarID: "avatar-1",
			Protocol: "rtmp",
		},
	})
	if cfg == nil {
		t.Fatal("expected Xunfei config")
	}
	if cfg.Protocol != "rtmp" {
		t.Fatalf("expected requested protocol to be preserved, got %+v", cfg)
	}
}
