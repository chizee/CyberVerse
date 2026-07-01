package xunfeiavatar

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/cyberverse/server/internal/character"
)

const (
	defaultInteractURL     = "wss://avatar.cn-huadong-1.xf-yun.com/v1/interact"
	defaultInteractPath    = "/v1/interact"
	defaultPingInterval    = 10 * time.Second
	defaultDriverDoneWait  = 5 * time.Second
	defaultAudioFrameDelay = 40 * time.Millisecond
	defaultAudioSampleRate = 16000
	defaultAudioMaxBytes   = 10 * 1024
	defaultAudioEndBytes   = 1024
	defaultVCN             = "x7_yachen_pro"
)

type Client struct {
	appID       string
	apiKey      string
	apiSecret   string
	sceneID     string
	interactURL string
	dialer      *websocket.Dialer
}

type Session struct {
	client     *Client
	conn       *websocket.Conn
	appID      string
	sceneID    string
	session    string
	sid        string
	streamURL  string
	protocol   string
	width      int
	height     int
	fps        int
	bitrate    int
	avatarID   string
	avatarName string
	vcn        string

	responses  chan responseEnvelope
	readErrCh  chan error
	pingCancel context.CancelFunc
	stopped    atomic.Bool
	mu         sync.Mutex
}

type FrontendConfig struct {
	StreamURL        string `json:"stream_url"`
	PlaybackURL      string `json:"playback_url,omitempty"`
	Protocol         string `json:"protocol"`
	AvatarID         string `json:"avatar_id"`
	AvatarName       string `json:"avatar_name,omitempty"`
	SceneID          string `json:"scene_id,omitempty"`
	VCN              string `json:"vcn,omitempty"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	FPS              int    `json:"fps"`
	Bitrate          int    `json:"bitrate"`
	AudioSampleRate  int    `json:"audio_sample_rate"`
	AudioMaxPCMBytes int    `json:"audio_max_pcm_bytes"`
}

type responseEnvelope struct {
	Header struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Session string `json:"session"`
		SID     string `json:"sid"`
		Status  int    `json:"status"`
	} `json:"header"`
	Payload struct {
		Avatar *avatarResponse `json:"avatar,omitempty"`
	} `json:"payload"`
}

type avatarResponse struct {
	RequestID    string         `json:"request_id"`
	Period       string         `json:"period"`
	EventType    string         `json:"event_type"`
	VMRStatus    flexibleString `json:"vmr_status"`
	FrameNum     int            `json:"frame_num"`
	ErrorCode    int            `json:"error_code"`
	ErrorMessage string         `json:"error_message"`
	StreamURL    string         `json:"stream_url"`
	StreamExtend map[string]any `json:"stream_extend,omitempty"`
}

func NewClientFromEnv() (*Client, error) {
	appID := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_APP_ID"))
	apiKey := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_API_KEY"))
	apiSecret := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_API_SECRET"))
	if appID == "" || apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("Xunfei avatar credentials are not configured")
	}

	interactFallback := defaultInteractURL
	if wsBase := strings.TrimRight(strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_WS_BASE")), "/"); wsBase != "" {
		interactFallback = wsBase + defaultInteractPath
	}

	return &Client{
		appID:       appID,
		apiKey:      apiKey,
		apiSecret:   apiSecret,
		sceneID:     strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_SCENE_ID")),
		interactURL: envOrDefaultURL("XUNFEI_AVATAR_INTERACT_URL", envOrDefaultURL("XUNFEI_AVATAR_SERVER_URL", interactFallback)),
		dialer:      websocket.DefaultDialer,
	}, nil
}

func envOrDefaultURL(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func defaultAvatarVCN() string {
	if value := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_DEFAULT_VCN")); value != "" {
		return value
	}
	return defaultVCN
}

func (c *Client) Start(ctx context.Context, cfg *character.XunfeiAvatar) (*Session, error) {
	if c == nil {
		return nil, fmt.Errorf("Xunfei avatar client is nil")
	}
	normalized := character.NormalizeXunfeiAvatarConfig(cfg)
	if normalized == nil || strings.TrimSpace(normalized.AvatarID) == "" {
		return nil, fmt.Errorf("Xunfei avatar_id is required")
	}
	sceneID := firstNonEmpty(normalized.SceneID, c.sceneID)
	if sceneID == "" {
		return nil, fmt.Errorf("Xunfei scene_id is required")
	}
	normalized.VCN = firstNonEmpty(normalized.VCN, defaultAvatarVCN())

	signed, err := signedURL(c.interactURL, c.apiKey, c.apiSecret, http.MethodGet, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	dialer := c.dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, signed, nil)
	if err != nil {
		return nil, err
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
		_ = conn.SetReadDeadline(deadline)
		defer func() {
			_ = conn.SetWriteDeadline(time.Time{})
			_ = conn.SetReadDeadline(time.Time{})
		}()
	}

	requestID := uuid.NewString()
	if err := conn.WriteJSON(c.startRequest(requestID, sceneID, normalized)); err != nil {
		_ = conn.Close()
		return nil, err
	}

	var resp responseEnvelope
	if err := conn.ReadJSON(&resp); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := responseError(resp, "start"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if strings.TrimSpace(resp.Header.Session) == "" {
		_ = conn.Close()
		return nil, fmt.Errorf("Xunfei avatar start response missing session")
	}
	streamURL := ""
	if resp.Payload.Avatar != nil {
		streamURL = strings.TrimSpace(resp.Payload.Avatar.StreamURL)
	}
	if streamURL == "" {
		_ = conn.Close()
		return nil, fmt.Errorf("Xunfei avatar start response missing stream_url")
	}

	pingCtx, cancel := context.WithCancel(context.Background())
	s := &Session{
		client:     c,
		conn:       conn,
		appID:      c.appID,
		sceneID:    sceneID,
		session:    resp.Header.Session,
		sid:        resp.Header.SID,
		streamURL:  streamURL,
		protocol:   normalized.Protocol,
		width:      normalized.Width,
		height:     normalized.Height,
		fps:        normalized.FPS,
		bitrate:    normalized.Bitrate,
		avatarID:   normalized.AvatarID,
		avatarName: normalized.AvatarName,
		vcn:        normalized.VCN,
		responses:  make(chan responseEnvelope, 256),
		readErrCh:  make(chan error, 1),
		pingCancel: cancel,
	}
	go s.readLoop()
	go s.pingLoop(pingCtx)
	return s, nil
}

func (c *Client) startRequest(requestID, sceneID string, cfg *character.XunfeiAvatar) map[string]any {
	header := c.requestHeader("start", requestID)
	header["scene_id"] = sceneID
	return map[string]any{
		"header": header,
		"parameter": map[string]any{
			"avatar": map[string]any{
				"stream": map[string]any{
					"protocol": cfg.Protocol,
					"fps":      cfg.FPS,
					"bitrate":  cfg.Bitrate,
					"alpha":    0,
				},
				"avatar_id": cfg.AvatarID,
				"width":     cfg.Width,
				"height":    cfg.Height,
			},
			"tts": map[string]any{
				"vcn":    cfg.VCN,
				"speed":  cfg.Speed,
				"pitch":  cfg.Pitch,
				"volume": cfg.Volume,
			},
		},
	}
}

func (c *Client) requestHeader(ctrl, requestID string) map[string]any {
	return map[string]any{
		"app_id":     c.appID,
		"ctrl":       ctrl,
		"request_id": requestID,
	}
}

func (s *Session) FrontendConfig() FrontendConfig {
	if s == nil {
		return FrontendConfig{}
	}
	return FrontendConfig{
		StreamURL:        s.streamURL,
		Protocol:         s.protocol,
		AvatarID:         s.avatarID,
		AvatarName:       s.avatarName,
		SceneID:          s.sceneID,
		VCN:              s.vcn,
		Width:            s.width,
		Height:           s.height,
		FPS:              s.fps,
		Bitrate:          s.bitrate,
		AudioSampleRate:  defaultAudioSampleRate,
		AudioMaxPCMBytes: defaultAudioMaxBytes,
	}
}

func (s *Session) StreamURL() string {
	if s == nil {
		return ""
	}
	return s.streamURL
}

func (s *Session) Protocol() string {
	if s == nil {
		return ""
	}
	return s.protocol
}

func (s *Session) SendPCMStream(ctx context.Context, chunks <-chan []byte) (err error) {
	if s == nil || s.client == nil || s.conn == nil {
		return fmt.Errorf("Xunfei avatar session is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped.Load() {
		return fmt.Errorf("Xunfei avatar session is stopped")
	}

	requestID := uuid.NewString()
	startedAt := time.Now()
	started := false
	sourceChunks := 0
	sourceBytes := 0
	driverPackets := 0
	driverBytes := 0
	minDriverBytes := 0
	maxDriverBytes := 0
	lastDriverBytes := 0
	debugAudio := strings.EqualFold(strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_DEBUG_AUDIO")), "1")
	defer func() {
		log.Printf(
			"Xunfei audio_driver summary: session=%s request_id=%s avatar_id=%s source_chunks=%d source_bytes=%d driver_packets=%d driver_bytes=%d min_packet_bytes=%d max_packet_bytes=%d last_packet_bytes=%d elapsed_ms=%d err=%v",
			s.session,
			requestID,
			s.avatarID,
			sourceChunks,
			sourceBytes,
			driverPackets,
			driverBytes,
			minDriverBytes,
			maxDriverBytes,
			lastDriverBytes,
			time.Since(startedAt).Milliseconds(),
			err,
		)
	}()
	send := func(status int, pcm []byte) error {
		packet := map[string]any{
			"header": s.client.requestHeader("audio_driver", requestID),
			"parameter": map[string]any{
				"avatar_dispatch": map[string]any{
					"audio_mode": 0,
				},
			},
			"payload": map[string]any{
				"audio": map[string]any{
					"status": status,
					"audio":  base64.StdEncoding.EncodeToString(pcm),
				},
			},
		}
		if err := s.conn.WriteJSON(packet); err != nil {
			return err
		}
		if status != 2 {
			driverPackets++
			packetBytes := len(pcm)
			driverBytes += packetBytes
			lastDriverBytes = packetBytes
			if minDriverBytes == 0 || packetBytes < minDriverBytes {
				minDriverBytes = packetBytes
			}
			if packetBytes > maxDriverBytes {
				maxDriverBytes = packetBytes
			}
			if debugAudio {
				log.Printf("Xunfei audio_driver packet: session=%s request_id=%s status=%d bytes=%d packet_index=%d", s.session, requestID, status, packetBytes, driverPackets)
			}
		}
		if _, err := s.drainResponses(requestID); err != nil {
			return err
		}
		if status != 2 {
			timer := time.NewTimer(audioDriverPacketDelay(pcm))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		return nil
	}

	var pending []byte
	sendPending := func() error {
		if len(pending) == 0 {
			return nil
		}
		status := 1
		if !started {
			status = 0
			started = true
		}
		if err := send(status, pending); err != nil {
			return err
		}
		pending = nil
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case pcm, ok := <-chunks:
			if !ok {
				if err := sendPending(); err != nil {
					return err
				}
				if err := send(2, make([]byte, defaultAudioEndBytes)); err != nil {
					return err
				}
				return s.waitForDriverDone(ctx, requestID)
			}
			if len(pcm) == 0 {
				continue
			}
			sourceChunks++
			sourceBytes += len(pcm)
			for _, part := range splitChunks(pcm, defaultAudioMaxBytes) {
				for len(part) > 0 {
					space := defaultAudioMaxBytes - len(pending)
					if space <= 0 {
						if err := sendPending(); err != nil {
							return err
						}
						continue
					}
					take := len(part)
					if take > space {
						take = space
					}
					pending = append(pending, part[:take]...)
					part = part[take:]
					if len(pending) >= defaultAudioMaxBytes {
						if err := sendPending(); err != nil {
							return err
						}
					}
				}
			}
		}
	}
}

func audioDriverPacketDelay(pcm []byte) time.Duration {
	if len(pcm) <= 0 {
		return defaultAudioFrameDelay
	}
	bytesPerSecond := defaultAudioSampleRate * 2
	delay := time.Duration(len(pcm)) * time.Second / time.Duration(bytesPerSecond)
	if delay < defaultAudioFrameDelay {
		return defaultAudioFrameDelay
	}
	return delay
}

func (s *Session) Stop(ctx context.Context) error {
	if s == nil || s.client == nil || s.conn == nil {
		return nil
	}
	if !s.stopped.CompareAndSwap(false, true) {
		return nil
	}
	if s.pingCancel != nil {
		s.pingCancel()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		_ = s.conn.Close()
	}()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	requestID := uuid.NewString()
	if err := s.conn.WriteJSON(map[string]any{
		"header": s.client.requestHeader("stop", requestID),
	}); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.waitForEvent(waitCtx, requestID, "stop"); err != nil && !isWaitTimeout(waitCtx) {
		return err
	}
	return nil
}

func (s *Session) Ping(ctx context.Context) error {
	if s == nil || s.client == nil || s.conn == nil || s.stopped.Load() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped.Load() {
		return nil
	}
	requestID := uuid.NewString()
	if err := s.conn.WriteJSON(map[string]any{
		"header": s.client.requestHeader("ping", requestID),
	}); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.waitForEvent(waitCtx, requestID, "pong"); err != nil && !isWaitTimeout(waitCtx) {
		return err
	}
	return nil
}

func (s *Session) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(defaultPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			_ = s.Ping(pingCtx)
			cancel()
		}
	}
}

func (s *Session) readLoop() {
	for {
		var resp responseEnvelope
		if err := s.conn.ReadJSON(&resp); err != nil {
			if s.stopped.Load() && isNormalWebSocketClose(err) {
				err = nil
			}
			select {
			case s.readErrCh <- err:
			default:
			}
			close(s.responses)
			return
		}
		select {
		case s.responses <- resp:
		default:
		}
	}
}

func (s *Session) drainResponses(requestID string) (bool, error) {
	for {
		select {
		case err := <-s.readErrCh:
			return false, readLoopError(err)
		case resp, ok := <-s.responses:
			if !ok {
				return false, readLoopError(nil)
			}
			if err := responseError(resp, "audio_driver"); err != nil {
				return false, err
			}
			if isDriverDone(resp, requestID) {
				return true, nil
			}
		default:
			return false, nil
		}
	}
}

func (s *Session) waitForDriverDone(ctx context.Context, requestID string) error {
	waitCtx, cancel := context.WithTimeout(ctx, defaultDriverDoneWait)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		case err := <-s.readErrCh:
			return readLoopError(err)
		case resp, ok := <-s.responses:
			if !ok {
				return readLoopError(nil)
			}
			if err := responseError(resp, "audio_driver"); err != nil {
				return err
			}
			if isDriverDone(resp, requestID) {
				return nil
			}
		}
	}
}

func (s *Session) waitForEvent(ctx context.Context, requestID, eventType string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-s.readErrCh:
			return readLoopError(err)
		case resp, ok := <-s.responses:
			if !ok {
				return readLoopError(nil)
			}
			if err := responseError(resp, eventType); err != nil {
				return err
			}
			avatar := resp.Payload.Avatar
			if avatar == nil {
				continue
			}
			if avatar.RequestID != "" && requestID != "" && avatar.RequestID != requestID {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(avatar.EventType), eventType) {
				return nil
			}
		}
	}
}

func responseError(resp responseEnvelope, ctrl string) error {
	if resp.Header.Code != 0 {
		return fmt.Errorf("Xunfei avatar %s failed: code=%d message=%s", ctrl, resp.Header.Code, resp.Header.Message)
	}
	if resp.Payload.Avatar != nil && resp.Payload.Avatar.ErrorCode != 0 {
		return fmt.Errorf("Xunfei avatar %s failed: code=%d message=%s", ctrl, resp.Payload.Avatar.ErrorCode, resp.Payload.Avatar.ErrorMessage)
	}
	return nil
}

func isDriverDone(resp responseEnvelope, requestID string) bool {
	avatar := resp.Payload.Avatar
	if avatar == nil {
		return false
	}
	if avatar.RequestID != "" && requestID != "" && avatar.RequestID != requestID {
		return false
	}
	status := strings.TrimSpace(string(avatar.VMRStatus))
	return status == "2" || resp.Header.Status == 2
}

type flexibleString string

func (s *flexibleString) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = flexibleString(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		*s = flexibleString(number.String())
		return nil
	}
	*s = ""
	return nil
}

func readLoopError(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("Xunfei avatar connection closed")
}

func isWaitTimeout(ctx context.Context) bool {
	return ctx != nil && ctx.Err() == context.DeadlineExceeded
}

func isNormalWebSocketClose(err error) bool {
	if err == nil {
		return false
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "close 1000") || strings.Contains(msg, "close 1001")
}

func signedURL(rawURL, apiKey, apiSecret, method string, now time.Time) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid Xunfei avatar URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" || parsed.Path == "" {
		return "", fmt.Errorf("invalid Xunfei avatar URL")
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}

	date := now.UTC().Format(http.TimeFormat)
	signatureOrigin := fmt.Sprintf("host: %s\ndate: %s\n%s %s HTTP/1.1", parsed.Host, date, method, parsed.EscapedPath())
	mac := hmac.New(sha256.New, []byte(apiSecret))
	_, _ = mac.Write([]byte(signatureOrigin))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	authOrigin := fmt.Sprintf(
		`api_key="%s", algorithm="hmac-sha256", headers="host date request-line", signature="%s"`,
		apiKey,
		signature,
	)

	query := parsed.Query()
	query.Set("authorization", base64.StdEncoding.EncodeToString([]byte(authOrigin)))
	query.Set("date", date)
	query.Set("host", parsed.Host)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func splitChunks(data []byte, maxBytes int) [][]byte {
	if len(data) == 0 || maxBytes <= 0 {
		return nil
	}
	if maxBytes%2 != 0 {
		maxBytes--
	}
	if maxBytes <= 0 {
		return nil
	}
	out := make([][]byte, 0, (len(data)+maxBytes-1)/maxBytes)
	for offset := 0; offset < len(data); offset += maxBytes {
		end := offset + maxBytes
		if end > len(data) {
			end = len(data)
		}
		if (end-offset)%2 != 0 {
			end--
		}
		if end > offset {
			out = append(out, data[offset:end])
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
