package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cyberverse/server/internal/character"
)

const (
	baiduXilingFigureQueryItem     = "INTERACTION-LIVE_2D"
	baiduXilingFigureQueryPageSize = 50
	baiduXilingFigureQueryMaxPages = 20
	baiduXilingAudioSampleRate     = 16000
	baiduXilingAudioMaxPCMBytes    = 48000
	baiduXilingDefaultH5Base       = "https://open.xiling.baidu.com/cloud/realtime"
)

type baiduXilingFigure struct {
	FigureID        string `json:"figure_id"`
	FigureName      string `json:"figure_name"`
	CameraID        string `json:"camera_id,omitempty"`
	ThumbnailURL    string `json:"thumbnail_url"`
	PreviewVideoURL string `json:"preview_video_url"`
	SourceImageURL  string `json:"source_image_url"`
	Status          string `json:"status"`
	SystemFigure    bool   `json:"system_figure"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
}

type baiduXilingSessionConfig struct {
	IframeURL        string `json:"iframe_url"`
	Origin           string `json:"origin"`
	FigureID         string `json:"figure_id"`
	CameraID         string `json:"camera_id,omitempty"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	AudioSampleRate  int    `json:"audio_sample_rate"`
	AudioMaxPCMBytes int    `json:"audio_max_pcm_bytes"`
}

type baiduXilingQueryResponse struct {
	Code    int             `json:"code"`
	Success bool            `json:"success"`
	Message json.RawMessage `json:"message"`
	Result  struct {
		Result     []baiduXilingRawFigure `json:"result"`
		TotalCount int                    `json:"totalCount"`
	} `json:"result"`
}

type baiduXilingRawFigure struct {
	FigureID         string `json:"figureId"`
	Name             string `json:"name"`
	TemplateImg      string `json:"templateImg"`
	PictureURL       string `json:"pictureUrl"`
	TemplateVideoURL string `json:"templateVideoUrl"`
	Status           string `json:"status"`
	SystemFigure     bool   `json:"systemFigure"`
	ResolutionWidth  int    `json:"resolutionWidth"`
	ResolutionHeight int    `json:"resolutionHeight"`
	FailedMessage    string `json:"failedMessage"`
}

func (r *Router) handleGetBaiduXilingFigure(w http.ResponseWriter, req *http.Request) {
	figureID := strings.TrimSpace(req.PathValue("figure_id"))
	if figureID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "figure_id is required"})
		return
	}

	appID := strings.TrimSpace(os.Getenv("BAIDU_XILING_APP_ID"))
	appKey := strings.TrimSpace(os.Getenv("BAIDU_XILING_APP_KEY"))
	if appID == "" || appKey == "" {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "Baidu Xiling credentials are not configured"})
		return
	}

	figure, err := queryBaiduXilingFigure(req.Context(), baiduXilingQueryOptions{
		APIBase:  strings.TrimSpace(os.Getenv("BAIDU_XILING_API_BASE")),
		AppID:    appID,
		AppKey:   appKey,
		FigureID: figureID,
		Client:   http.DefaultClient,
	})
	if err != nil {
		status := http.StatusBadGateway
		if err == errBaiduXilingFigureNotFound {
			status = http.StatusNotFound
		}
		writeJSON(w, status, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, figure)
}

type baiduXilingQueryOptions struct {
	APIBase  string
	AppID    string
	AppKey   string
	FigureID string
	Client   *http.Client
}

var errBaiduXilingFigureNotFound = fmt.Errorf("Baidu Xiling figure not found")

func queryBaiduXilingFigure(ctx context.Context, opts baiduXilingQueryOptions) (baiduXilingFigure, error) {
	apiBase := strings.TrimRight(opts.APIBase, "/")
	if apiBase == "" {
		apiBase = "https://open.xiling.baidu.com"
	}
	client := opts.Client
	if client == nil {
		client = http.DefaultClient
	}

	figure, err := queryBaiduXilingCustomizedFigure(ctx, client, apiBase, opts)
	if err == nil {
		return figure, nil
	}
	if err != errBaiduXilingFigureNotFound {
		return baiduXilingFigure{}, err
	}
	return queryBaiduXilingListedFigure(ctx, client, apiBase, opts)
}

func queryBaiduXilingCustomizedFigure(ctx context.Context, client *http.Client, apiBase string, opts baiduXilingQueryOptions) (baiduXilingFigure, error) {
	params := url.Values{}
	params.Set("figureId", opts.FigureID)
	params.Set("pageNo", "1")
	params.Set("pageSize", "1")

	payload, err := requestBaiduXilingFigureQuery(ctx, client, apiBase, opts, "/api/digitalhuman/open/v1/figure/lite2d/query", params)
	if err != nil {
		return baiduXilingFigure{}, err
	}
	if len(payload.Result.Result) == 0 {
		return baiduXilingFigure{}, errBaiduXilingFigureNotFound
	}
	return normalizeBaiduXilingFigure(payload.Result.Result[0]), nil
}

func queryBaiduXilingListedFigure(ctx context.Context, client *http.Client, apiBase string, opts baiduXilingQueryOptions) (baiduXilingFigure, error) {
	for _, systemFigure := range []string{"true", "false"} {
		figure, err := queryBaiduXilingListedFigurePageSet(ctx, client, apiBase, opts, systemFigure)
		if err == nil {
			return figure, nil
		}
		if err != errBaiduXilingFigureNotFound {
			return baiduXilingFigure{}, err
		}
	}
	return baiduXilingFigure{}, errBaiduXilingFigureNotFound
}

func queryBaiduXilingListedFigurePageSet(ctx context.Context, client *http.Client, apiBase string, opts baiduXilingQueryOptions, systemFigure string) (baiduXilingFigure, error) {
	target := strings.TrimSpace(opts.FigureID)
	for pageNo := 1; pageNo <= baiduXilingFigureQueryMaxPages; pageNo++ {
		params := url.Values{}
		params.Set("systemFigure", systemFigure)
		params.Set("item", baiduXilingFigureQueryItem)
		params.Set("pageNo", fmt.Sprintf("%d", pageNo))
		params.Set("pageSize", fmt.Sprintf("%d", baiduXilingFigureQueryPageSize))

		payload, err := requestBaiduXilingFigureQuery(ctx, client, apiBase, opts, "/api/digitalhuman/open/v1/figure/query", params)
		if err != nil {
			return baiduXilingFigure{}, err
		}
		for _, raw := range payload.Result.Result {
			if strings.TrimSpace(raw.FigureID) == target {
				return normalizeBaiduXilingFigure(raw), nil
			}
		}
		if len(payload.Result.Result) == 0 || pageNo*baiduXilingFigureQueryPageSize >= payload.Result.TotalCount {
			break
		}
	}
	return baiduXilingFigure{}, errBaiduXilingFigureNotFound
}

func requestBaiduXilingFigureQuery(ctx context.Context, client *http.Client, apiBase string, opts baiduXilingQueryOptions, path string, params url.Values) (baiduXilingQueryResponse, error) {
	endpoint := apiBase + path + "?" + params.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return baiduXilingQueryResponse{}, err
	}
	expireTime := time.Now().UTC().Add(10 * time.Minute).Format("2006-01-02T15:04:05.000Z")
	httpReq.Header.Set("Authorization", baiduXilingAuthorization(opts.AppID, opts.AppKey, expireTime))

	resp, err := client.Do(httpReq)
	if err != nil {
		return baiduXilingQueryResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return baiduXilingQueryResponse{}, fmt.Errorf("Baidu Xiling figure query failed: HTTP %d", resp.StatusCode)
	}

	var payload baiduXilingQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return baiduXilingQueryResponse{}, err
	}
	if payload.Code != 0 || !payload.Success {
		return baiduXilingQueryResponse{}, fmt.Errorf("Baidu Xiling figure query failed: code=%d message=%s", payload.Code, string(payload.Message))
	}
	return payload, nil
}

func normalizeBaiduXilingFigure(raw baiduXilingRawFigure) baiduXilingFigure {
	return baiduXilingFigure{
		FigureID:        strings.TrimSpace(raw.FigureID),
		FigureName:      strings.TrimSpace(raw.Name),
		ThumbnailURL:    strings.TrimSpace(raw.TemplateImg),
		PreviewVideoURL: strings.TrimSpace(raw.TemplateVideoURL),
		SourceImageURL:  strings.TrimSpace(raw.PictureURL),
		Status:          strings.TrimSpace(raw.Status),
		SystemFigure:    raw.SystemFigure,
		Width:           maxInt(raw.ResolutionWidth, 0),
		Height:          maxInt(raw.ResolutionHeight, 0),
	}
}

func baiduXilingH5Token(appID, appKey string, ttl time.Duration) string {
	expireTime := time.Now().UTC().Add(ttl).Format("2006-01-02T15:04:05.000Z")
	return baiduXilingAuthorization(appID, appKey, expireTime)
}

func baiduXilingCameraID(c *character.Character) string {
	if c == nil || c.BaiduXiling == nil {
		return ""
	}
	if cameraID := strings.TrimSpace(c.BaiduXiling.CameraID); cameraID != "" {
		return cameraID
	}
	if envCameraID := strings.TrimSpace(os.Getenv("BAIDU_XILING_CAMERA_ID")); envCameraID != "" {
		return envCameraID
	}
	if c.BaiduXiling.Width > 0 && c.BaiduXiling.Height > 0 && c.BaiduXiling.Height > c.BaiduXiling.Width {
		return "1"
	}
	return "0"
}

func baiduXilingResolution(c *character.Character) (int, int) {
	width, height := 720, 406
	if c != nil && c.BaiduXiling != nil {
		if c.BaiduXiling.Width > 0 {
			width = c.BaiduXiling.Width
		}
		if c.BaiduXiling.Height > 0 {
			height = c.BaiduXiling.Height
		}
	}
	if width%2 != 0 {
		width--
	}
	if height%2 != 0 {
		height--
	}
	if width < 400 {
		width = 720
	}
	if height < 400 {
		height = 406
	}
	return width, height
}

func buildBaiduXilingSessionConfig(c *character.Character) (*baiduXilingSessionConfig, error) {
	if c == nil || c.BaiduXiling == nil || strings.TrimSpace(c.BaiduXiling.FigureID) == "" {
		return nil, fmt.Errorf("Baidu Xiling figure_id is required")
	}
	appID := strings.TrimSpace(os.Getenv("BAIDU_XILING_APP_ID"))
	appKey := strings.TrimSpace(os.Getenv("BAIDU_XILING_APP_KEY"))
	if appID == "" || appKey == "" {
		return nil, fmt.Errorf("Baidu Xiling credentials are not configured")
	}

	h5Base := strings.TrimRight(strings.TrimSpace(os.Getenv("BAIDU_XILING_H5_BASE")), "?")
	if h5Base == "" {
		h5Base = baiduXilingDefaultH5Base
	}
	parsed, err := url.Parse(h5Base)
	if err != nil {
		return nil, fmt.Errorf("invalid Baidu Xiling H5 base URL: %w", err)
	}
	origin := parsed.Scheme + "://" + parsed.Host
	if parsed.Scheme == "" || parsed.Host == "" {
		origin = "https://open.xiling.baidu.com"
	}

	width, height := baiduXilingResolution(c)
	query := parsed.Query()
	query.Set("token", baiduXilingH5Token(appID, appKey, time.Hour))
	query.Set("initMode", "noAudio")
	query.Set("figureId", strings.TrimSpace(c.BaiduXiling.FigureID))
	query.Set("resolutionWidth", fmt.Sprintf("%d", width))
	query.Set("resolutionHeight", fmt.Sprintf("%d", height))
	query.Set("mode", "corp")
	if cameraID := baiduXilingCameraID(c); cameraID != "" {
		query.Set("cameraId", cameraID)
	}
	parsed.RawQuery = query.Encode()

	return &baiduXilingSessionConfig{
		IframeURL:        parsed.String(),
		Origin:           origin,
		FigureID:         strings.TrimSpace(c.BaiduXiling.FigureID),
		CameraID:         baiduXilingCameraID(c),
		Width:            width,
		Height:           height,
		AudioSampleRate:  baiduXilingAudioSampleRate,
		AudioMaxPCMBytes: baiduXilingAudioMaxPCMBytes,
	}, nil
}

func baiduXilingAuthorization(appID, appKey, expireTime string) string {
	mac := hmac.New(sha256.New, []byte(appKey))
	_, _ = mac.Write([]byte(appID + expireTime))
	return appID + "/" + hex.EncodeToString(mac.Sum(nil)) + "/" + expireTime
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
