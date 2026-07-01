package xunfeiavatar

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cyberverse/server/internal/character"
	"github.com/gorilla/websocket"
)

func TestInteractStartAndAudioDriverUseSignedWebSocket(t *testing.T) {
	const (
		apiKey    = "api-key"
		apiSecret = "api-secret"
	)

	errCh := make(chan error, 1)
	resultCh := make(chan audioDriverTestResult, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			errCh <- fmt.Errorf("expected WebSocket handshake GET, got %s", req.Method)
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if err := verifySignedRequest(req, apiKey, apiSecret, http.MethodGet); err != nil {
			errCh <- err
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		var statuses []int
		var audioLengths []int
		for {
			var packet interactTestPacket
			if err := conn.ReadJSON(&packet); err != nil {
				errCh <- err
				return
			}
			switch packet.Header.Ctrl {
			case "start":
				if packet.Header.SceneID != "scene-1" {
					errCh <- fmt.Errorf("expected scene_id scene-1, got %q", packet.Header.SceneID)
					return
				}
				var body map[string]any
				if err := json.Unmarshal(packet.Raw, &body); err != nil {
					errCh <- err
					return
				}
				parameter := body["parameter"].(map[string]any)
				avatar := parameter["avatar"].(map[string]any)
				stream := avatar["stream"].(map[string]any)
				if avatar["avatar_id"] != "avatar-1" || stream["protocol"] != "flv" {
					errCh <- fmt.Errorf("unexpected start avatar payload: %+v", avatar)
					return
				}
				tts := parameter["tts"].(map[string]any)
				if tts["vcn"] != "vcn-1" {
					errCh <- fmt.Errorf("unexpected tts payload: %+v", tts)
					return
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
			case "audio_driver":
				statuses = append(statuses, packet.Payload.Audio.Status)
				if packet.Payload.Audio.Status != 2 {
					audio, err := base64.StdEncoding.DecodeString(packet.Payload.Audio.Audio)
					if err != nil {
						errCh <- err
						return
					}
					audioLengths = append(audioLengths, len(audio))
				}
				if packet.Payload.Audio.Status == 2 {
					_ = conn.WriteJSON(map[string]any{
						"header": map[string]any{"code": 0, "message": "success", "sid": "xf-sid"},
						"payload": map[string]any{
							"avatar": map[string]any{
								"request_id": packet.Header.RequestID,
								"period":     "driver",
								"event_type": "driver_status",
								"vmr_status": "2",
							},
						},
					})
				}
			case "stop":
				resultCh <- audioDriverTestResult{statuses: statuses, audioLengths: audioLengths}
				_ = conn.WriteJSON(map[string]any{
					"header": map[string]any{"code": 0, "message": "success", "sid": "xf-sid", "session": "xf-session"},
					"payload": map[string]any{
						"avatar": map[string]any{
							"request_id": packet.Header.RequestID,
							"period":     "gloable",
							"event_type": "stop",
						},
					},
				})
				return
			default:
				errCh <- fmt.Errorf("unexpected ctrl %q", packet.Header.Ctrl)
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + defaultInteractPath
	client := &Client{
		appID:       "app-id",
		apiKey:      apiKey,
		apiSecret:   apiSecret,
		interactURL: wsURL,
		dialer:      websocket.DefaultDialer,
	}
	session, err := client.Start(context.Background(), &character.XunfeiAvatar{
		AvatarID: " avatar-1 ",
		SceneID:  " scene-1 ",
		VCN:      " vcn-1 ",
		Protocol: "flv",
		Width:    721,
		Height:   1281,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if session.StreamURL() != "https://example.test/live/avatar.flv" || session.Protocol() != "flv" {
		t.Fatalf("unexpected frontend stream config: %+v", session.FrontendConfig())
	}

	chunks := make(chan []byte, 4)
	chunks <- make([]byte, defaultAudioMaxBytes/2)
	chunks <- make([]byte, defaultAudioMaxBytes/2)
	chunks <- make([]byte, defaultAudioMaxBytes/2)
	chunks <- make([]byte, defaultAudioMaxBytes/2)
	close(chunks)
	if err := session.SendPCMStream(context.Background(), chunks); err != nil {
		t.Fatalf("SendPCMStream failed: %v", err)
	}
	if err := session.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case result := <-resultCh:
		if fmt.Sprint(result.statuses) != "[0 1 2]" {
			t.Fatalf("expected audio status sequence [0 1 2], got %v", result.statuses)
		}
		if fmt.Sprint(result.audioLengths) != "[10240 10240]" {
			t.Fatalf("expected small upstream chunks to be coalesced, got audio lengths %v", result.audioLengths)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for interact WebSocket request")
	}
}

func TestAudioDriverPacketDelayUsesPCMDuration(t *testing.T) {
	if got := audioDriverPacketDelay(make([]byte, defaultAudioMaxBytes)); got != 320*time.Millisecond {
		t.Fatalf("expected 10KiB PCM packet delay to be 320ms, got %s", got)
	}
	if got := audioDriverPacketDelay(make([]byte, 640)); got != defaultAudioFrameDelay {
		t.Fatalf("expected small PCM packet delay to keep frame minimum, got %s", got)
	}
}

type audioDriverTestResult struct {
	statuses     []int
	audioLengths []int
}

type interactTestPacket struct {
	Raw    json.RawMessage
	Header struct {
		AppID     string `json:"app_id"`
		Ctrl      string `json:"ctrl"`
		RequestID string `json:"request_id"`
		SceneID   string `json:"scene_id"`
	} `json:"header"`
	Payload struct {
		Audio struct {
			Status int    `json:"status"`
			Audio  string `json:"audio"`
		} `json:"audio"`
	} `json:"payload"`
}

func (p *interactTestPacket) UnmarshalJSON(data []byte) error {
	type alias interactTestPacket
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = interactTestPacket(decoded)
	p.Raw = append(p.Raw[:0], data...)
	return nil
}

func verifySignedRequest(req *http.Request, apiKey, apiSecret, method string) error {
	query := req.URL.Query()
	authRaw := query.Get("authorization")
	if authRaw == "" {
		return fmt.Errorf("missing authorization query")
	}
	authBytes, err := base64.StdEncoding.DecodeString(authRaw)
	if err != nil {
		return fmt.Errorf("decode authorization: %w", err)
	}
	auth := string(authBytes)
	if !strings.Contains(auth, `api_key="`+apiKey+`"`) {
		return fmt.Errorf("authorization does not contain api key: %s", auth)
	}
	signature, ok := extractAuthValue(auth, "signature")
	if !ok {
		return fmt.Errorf("authorization missing signature: %s", auth)
	}

	host := query.Get("host")
	date := query.Get("date")
	if host == "" || date == "" {
		return fmt.Errorf("missing host or date query")
	}
	escapedPath := (&url.URL{Path: req.URL.Path}).EscapedPath()
	signatureOrigin := fmt.Sprintf("host: %s\ndate: %s\n%s %s HTTP/1.1", host, date, method, escapedPath)
	mac := hmac.New(sha256.New, []byte(apiSecret))
	_, _ = mac.Write([]byte(signatureOrigin))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if signature != expected {
		return fmt.Errorf("signature mismatch: expected GET signature %q, got %q", expected, signature)
	}
	return nil
}

func extractAuthValue(auth, key string) (string, bool) {
	prefix := key + `="`
	start := strings.Index(auth, prefix)
	if start < 0 {
		return "", false
	}
	start += len(prefix)
	end := strings.Index(auth[start:], `"`)
	if end < 0 {
		return "", false
	}
	return auth[start : start+end], true
}
