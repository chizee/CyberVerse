package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	baiduXilingVideoSubmitPath         = "/api/digitalhuman/open/v1/video/submit"
	baiduXilingVideoTaskPath           = "/api/digitalhuman/open/v1/video/task"
	baiduXilingAdvancedVideoSubmitPath = "/api/digitalhuman/open/v1/video/advanced/submit"
	baiduXilingAdvancedVideoTaskPath   = "/api/digitalhuman/open/v1/video/advanced/task"
)

type baiduXilingAdvancedVideoClient struct {
	APIBase string
	AppID   string
	AppKey  string
	Client  *http.Client
}

type baiduXilingAdvancedVideoSubmitInput struct {
	FigureID           string
	TemplateID         string
	DriveType          string
	InputAudioURL      string
	Text               string
	Title              string
	Width              int
	Height             int
	Model              string
	TTSPerson          string
	TTSLan             string
	TTSSpeed           string
	TTSVolume          string
	TTSPitch           string
	RiskTip            string
	Transparent        bool
	BackgroundImageURL string
	AutoAnimoji        bool
}

type baiduXilingAdvancedVideoTask struct {
	TaskID        string
	Status        string
	FailedCode    int
	FailedMessage string
	VideoURL      string
	DurationMS    int
}

type baiduXilingVideoSubmission struct {
	TaskID   string
	Advanced bool
}

type baiduXilingAdvancedVideoResponse struct {
	Code    int             `json:"code"`
	Success bool            `json:"success"`
	Message json.RawMessage `json:"message"`
	Result  struct {
		TaskID        string `json:"taskId"`
		Status        string `json:"status"`
		FailedCode    int    `json:"failedCode"`
		FailedMessage string `json:"failedMessage"`
		VideoURL      string `json:"videoUrl"`
		Duration      int    `json:"duration"`
	} `json:"result"`
}

func (c baiduXilingAdvancedVideoClient) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}

func (c baiduXilingAdvancedVideoClient) apiBase() string {
	apiBase := strings.TrimRight(strings.TrimSpace(c.APIBase), "/")
	if apiBase == "" {
		apiBase = "https://open.xiling.baidu.com"
	}
	return apiBase
}

func (c baiduXilingAdvancedVideoClient) authorization() string {
	expireTime := time.Now().UTC().Add(10 * time.Minute).Format("2006-01-02T15:04:05.000Z")
	return baiduXilingAuthorization(strings.TrimSpace(c.AppID), strings.TrimSpace(c.AppKey), expireTime)
}

func (c baiduXilingAdvancedVideoClient) submit(ctx context.Context, in baiduXilingAdvancedVideoSubmitInput) (baiduXilingVideoSubmission, error) {
	if strings.TrimSpace(c.AppID) == "" || strings.TrimSpace(c.AppKey) == "" {
		return baiduXilingVideoSubmission{}, fmt.Errorf("Baidu Xiling credentials are not configured")
	}
	if strings.TrimSpace(in.FigureID) == "" {
		return baiduXilingVideoSubmission{}, fmt.Errorf("Baidu Xiling figure_id is required")
	}
	driveType := strings.ToUpper(strings.TrimSpace(in.DriveType))
	if driveType == "" {
		if strings.TrimSpace(in.InputAudioURL) != "" {
			driveType = "VOICE"
		} else {
			driveType = "TEXT"
		}
	}
	if driveType != "TEXT" && driveType != "VOICE" {
		return baiduXilingVideoSubmission{}, fmt.Errorf("Baidu Xiling driveType must be TEXT or VOICE")
	}
	width, height := in.Width, in.Height
	if width <= 0 {
		width = 1080
	}
	if height <= 0 {
		height = 1920
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		text = strings.TrimSpace(in.Title)
	}
	if driveType == "TEXT" && text == "" {
		return baiduXilingVideoSubmission{}, fmt.Errorf("Baidu Xiling text is required for TEXT driveType")
	}
	if driveType == "VOICE" && strings.TrimSpace(in.InputAudioURL) == "" {
		return baiduXilingVideoSubmission{}, fmt.Errorf("Baidu Xiling input audio URL is required for VOICE driveType")
	}
	if text == "" {
		text = "Offline video"
	}

	ttsParams := map[string]string{
		"speed":  defaultString(in.TTSSpeed, "5"),
		"volume": defaultString(in.TTSVolume, "5"),
		"pitch":  defaultString(in.TTSPitch, "5"),
	}
	if person := strings.TrimSpace(in.TTSPerson); person != "" {
		ttsParams["person"] = person
	}
	if lan := strings.TrimSpace(in.TTSLan); lan != "" {
		ttsParams["lan"] = lan
	}

	videoParams := map[string]any{
		"width":       width,
		"height":      height,
		"transparent": in.Transparent,
	}
	payload := map[string]any{
		"figureId":    strings.TrimSpace(in.FigureID),
		"driveType":   driveType,
		"videoParams": videoParams,
	}
	if driveType == "TEXT" {
		payload["text"] = text
		payload["ttsParams"] = ttsParams
		if in.AutoAnimoji {
			payload["autoAnimoji"] = true
		}
	} else {
		payload["inputAudioUrl"] = strings.TrimSpace(in.InputAudioURL)
	}
	if backgroundImageURL := strings.TrimSpace(in.BackgroundImageURL); backgroundImageURL != "" {
		payload["backgroundImageUrl"] = backgroundImageURL
	}
	submitPath := baiduXilingVideoSubmitPath
	advanced := false
	if templateID := strings.TrimSpace(in.TemplateID); templateID != "" {
		payload["templateId"] = templateID
		submitPath = baiduXilingAdvancedVideoSubmitPath
		advanced = true
	}
	if title := truncateRunes(strings.TrimSpace(in.Title), 30); title != "" {
		payload["title"] = title
	}
	if model := strings.TrimSpace(in.Model); model != "" {
		payload["model"] = model
	}
	if riskTip := truncateRunes(strings.TrimSpace(in.RiskTip), 30); riskTip != "" {
		payload["riskTip"] = riskTip
	}

	var resp baiduXilingAdvancedVideoResponse
	if err := c.doJSON(ctx, http.MethodPost, submitPath, nil, payload, &resp); err != nil {
		return baiduXilingVideoSubmission{}, err
	}
	if resp.Code != 0 || !resp.Success {
		return baiduXilingVideoSubmission{}, fmt.Errorf("Baidu Xiling video submit failed: code=%d message=%s", resp.Code, baiduXilingMessage(resp.Message))
	}
	taskID := strings.TrimSpace(resp.Result.TaskID)
	if taskID == "" {
		return baiduXilingVideoSubmission{}, fmt.Errorf("Baidu Xiling video submit did not return taskId")
	}
	return baiduXilingVideoSubmission{TaskID: taskID, Advanced: advanced}, nil
}

func (c baiduXilingAdvancedVideoClient) queryTask(ctx context.Context, taskID string, advanced bool) (baiduXilingAdvancedVideoTask, error) {
	if strings.TrimSpace(c.AppID) == "" || strings.TrimSpace(c.AppKey) == "" {
		return baiduXilingAdvancedVideoTask{}, fmt.Errorf("Baidu Xiling credentials are not configured")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return baiduXilingAdvancedVideoTask{}, fmt.Errorf("Baidu Xiling task id is required")
	}
	params := url.Values{}
	params.Set("taskId", taskID)

	taskPath := baiduXilingVideoTaskPath
	if advanced {
		taskPath = baiduXilingAdvancedVideoTaskPath
	}
	var resp baiduXilingAdvancedVideoResponse
	if err := c.doJSON(ctx, http.MethodGet, taskPath, params, nil, &resp); err != nil {
		return baiduXilingAdvancedVideoTask{}, err
	}
	if resp.Code != 0 || !resp.Success {
		return baiduXilingAdvancedVideoTask{}, fmt.Errorf("Baidu Xiling video task query failed: code=%d message=%s", resp.Code, baiduXilingMessage(resp.Message))
	}
	return baiduXilingAdvancedVideoTask{
		TaskID:        strings.TrimSpace(resp.Result.TaskID),
		Status:        strings.TrimSpace(resp.Result.Status),
		FailedCode:    resp.Result.FailedCode,
		FailedMessage: strings.TrimSpace(resp.Result.FailedMessage),
		VideoURL:      strings.TrimSpace(resp.Result.VideoURL),
		DurationMS:    resp.Result.Duration,
	}, nil
}

func (c baiduXilingAdvancedVideoClient) doJSON(ctx context.Context, method, path string, params url.Values, body any, out any) error {
	endpoint := c.apiBase() + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authorization())
	if body != nil {
		req.Header.Set("Content-Type", "application/json;charset=utf-8")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Baidu Xiling video API failed: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func baiduXilingMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func truncateRunes(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
