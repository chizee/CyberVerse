package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func baiduXilingTestJSONResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestQueryBaiduXilingFigureSuccess(t *testing.T) {
	var gotAuth string
	var gotFigureID string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotAuth = req.Header.Get("Authorization")
		gotFigureID = req.URL.Query().Get("figureId")
		return baiduXilingTestJSONResponse(req, `{
			"code": 0,
			"success": true,
			"message": "success",
			"result": {
				"totalCount": 1,
				"result": [{
					"figureId": "figure-1",
					"name": "Figure One",
					"templateImg": "https://example.com/thumb.png",
					"pictureUrl": "https://example.com/source.png",
					"templateVideoUrl": "https://example.com/video.mp4",
					"status": "FINISHED",
					"systemFigure": false,
					"resolutionWidth": 720,
					"resolutionHeight": 406
				}]
			}
		}`), nil
	})}

	figure, err := queryBaiduXilingFigure(context.Background(), baiduXilingQueryOptions{
		APIBase:  "https://baidu.test",
		AppID:    "app-id",
		AppKey:   "app-key",
		FigureID: "figure-1",
		Client:   client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotFigureID != "figure-1" {
		t.Fatalf("expected figureId query, got %q", gotFigureID)
	}
	if !strings.HasPrefix(gotAuth, "app-id/") {
		t.Fatalf("expected Authorization to start with app-id, got %q", gotAuth)
	}
	if figure.FigureID != "figure-1" || figure.FigureName != "Figure One" {
		t.Fatalf("unexpected normalized figure: %#v", figure)
	}
	if figure.ThumbnailURL != "https://example.com/thumb.png" || figure.PreviewVideoURL != "https://example.com/video.mp4" {
		t.Fatalf("unexpected media URLs: %#v", figure)
	}
	if figure.Width != 720 || figure.Height != 406 {
		t.Fatalf("expected dimensions 720x406, got %dx%d", figure.Width, figure.Height)
	}
}

func TestQueryBaiduXilingFigureFindsSystemFigureFallback(t *testing.T) {
	var listCalls int
	var gotItem string
	var gotSystemFigure string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/digitalhuman/open/v1/figure/lite2d/query":
			return baiduXilingTestJSONResponse(req, `{"code":0,"success":true,"message":"success","result":{"totalCount":0,"result":[]}}`), nil
		case "/api/digitalhuman/open/v1/figure/query":
			listCalls++
			gotItem = req.URL.Query().Get("item")
			gotSystemFigure = req.URL.Query().Get("systemFigure")
			return baiduXilingTestJSONResponse(req, `{
				"code": 0,
				"success": true,
				"message": "success",
				"result": {
					"totalCount": 1,
					"result": [{
						"figureId": "2646047",
						"name": "林小葵",
						"templateImg": "https://example.com/system-thumb.png",
						"systemFigure": true,
						"resolutionWidth": 1920,
						"resolutionHeight": 1080
					}]
				}
			}`), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	})}

	figure, err := queryBaiduXilingFigure(context.Background(), baiduXilingQueryOptions{
		APIBase:  "https://baidu.test",
		AppID:    "app-id",
		AppKey:   "app-key",
		FigureID: "2646047",
		Client:   client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if listCalls != 1 {
		t.Fatalf("expected one list fallback call, got %d", listCalls)
	}
	if gotItem != baiduXilingFigureQueryItem || gotSystemFigure != "true" {
		t.Fatalf("unexpected list query item=%q systemFigure=%q", gotItem, gotSystemFigure)
	}
	if figure.FigureID != "2646047" || figure.FigureName != "林小葵" || !figure.SystemFigure {
		t.Fatalf("unexpected system figure: %#v", figure)
	}
	if figure.ThumbnailURL != "https://example.com/system-thumb.png" {
		t.Fatalf("unexpected thumbnail: %#v", figure)
	}
	if figure.Width != 1920 || figure.Height != 1080 {
		t.Fatalf("expected dimensions 1920x1080, got %dx%d", figure.Width, figure.Height)
	}
}

func TestQueryBaiduXilingFigureNotFound(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return baiduXilingTestJSONResponse(req, `{"code":0,"success":true,"message":"success","result":{"totalCount":0,"result":[]}}`), nil
	})}

	_, err := queryBaiduXilingFigure(context.Background(), baiduXilingQueryOptions{
		APIBase:  "https://baidu.test",
		AppID:    "app-id",
		AppKey:   "app-key",
		FigureID: "missing",
		Client:   client,
	})
	if err != errBaiduXilingFigureNotFound {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestQueryBaiduXilingFigureBaiduError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return baiduXilingTestJSONResponse(req, `{"code":123,"success":false,"message":"bad request","result":{"totalCount":0,"result":[]}}`), nil
	})}

	_, err := queryBaiduXilingFigure(context.Background(), baiduXilingQueryOptions{
		APIBase:  "https://baidu.test",
		AppID:    "app-id",
		AppKey:   "app-key",
		FigureID: "figure-1",
		Client:   client,
	})
	if err == nil || !strings.Contains(err.Error(), "code=123") {
		t.Fatalf("expected Baidu error, got %v", err)
	}
}

func TestHandleGetBaiduXilingFigureMissingCredentials(t *testing.T) {
	t.Setenv("BAIDU_XILING_APP_ID", "")
	t.Setenv("BAIDU_XILING_APP_KEY", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/baidu-xiling/figures/figure-1", nil)
	req.SetPathValue("figure_id", "figure-1")
	w := httptest.NewRecorder()

	(&Router{}).handleGetBaiduXilingFigure(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error, "credentials") {
		t.Fatalf("expected credentials error, got %q", resp.Error)
	}
}

func TestBaiduXilingAdvancedVideoSubmitAndQuery(t *testing.T) {
	var gotAuth string
	var submitBody map[string]any
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotAuth = req.Header.Get("Authorization")
		switch req.URL.Path {
		case baiduXilingAdvancedVideoSubmitPath:
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST submit, got %s", req.Method)
			}
			if !strings.Contains(req.Header.Get("Content-Type"), "application/json") {
				t.Fatalf("expected JSON content type, got %q", req.Header.Get("Content-Type"))
			}
			if err := json.NewDecoder(req.Body).Decode(&submitBody); err != nil {
				t.Fatal(err)
			}
			return baiduXilingTestJSONResponse(req, `{"code":0,"success":true,"message":"success","result":{"taskId":"adv-1"}}`), nil
		case baiduXilingAdvancedVideoTaskPath:
			if req.URL.Query().Get("taskId") != "adv-1" {
				t.Fatalf("expected taskId adv-1, got %q", req.URL.Query().Get("taskId"))
			}
			return baiduXilingTestJSONResponse(req, `{"code":0,"success":true,"message":"success","result":{"taskId":"adv-1","status":"SUCCESS","videoUrl":"https://cdn.example.com/out.mp4","duration":1200}}`), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	})}

	api := baiduXilingAdvancedVideoClient{
		APIBase: "https://baidu.test",
		AppID:   "app-id",
		AppKey:  "app-key",
		Client:  client,
	}
	submission, err := api.submit(context.Background(), baiduXilingAdvancedVideoSubmitInput{
		FigureID:      "figure-1",
		TemplateID:    "tpl-1",
		InputAudioURL: "https://public.example.com/input.wav",
		Text:          "hello",
		Title:         "demo",
		Width:         1080,
		Height:        1920,
	})
	if err != nil {
		t.Fatal(err)
	}
	if submission.TaskID != "adv-1" || !submission.Advanced {
		t.Fatalf("unexpected submission: %#v", submission)
	}
	if !strings.HasPrefix(gotAuth, "app-id/") {
		t.Fatalf("expected Authorization to start with app-id, got %q", gotAuth)
	}
	if submitBody["driveType"] != "VOICE" || submitBody["inputAudioUrl"] != "https://public.example.com/input.wav" {
		t.Fatalf("unexpected submit body: %#v", submitBody)
	}
	if submitBody["figureId"] != "figure-1" || submitBody["templateId"] != "tpl-1" {
		t.Fatalf("unexpected figure/template: %#v", submitBody)
	}

	task, err := api.queryTask(context.Background(), submission.TaskID, submission.Advanced)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "SUCCESS" || task.VideoURL != "https://cdn.example.com/out.mp4" || task.DurationMS != 1200 {
		t.Fatalf("unexpected task response: %#v", task)
	}
}

func TestBaiduXilingBasicVideoSubmitAndQuery(t *testing.T) {
	var submitBody map[string]any
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case baiduXilingVideoSubmitPath:
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST submit, got %s", req.Method)
			}
			if err := json.NewDecoder(req.Body).Decode(&submitBody); err != nil {
				t.Fatal(err)
			}
			return baiduXilingTestJSONResponse(req, `{"code":0,"success":true,"message":"success","result":{"taskId":"basic-1"}}`), nil
		case baiduXilingVideoTaskPath:
			if req.URL.Query().Get("taskId") != "basic-1" {
				t.Fatalf("expected taskId basic-1, got %q", req.URL.Query().Get("taskId"))
			}
			return baiduXilingTestJSONResponse(req, `{"code":0,"success":true,"message":"success","result":{"taskId":"basic-1","status":"SUCCESS","videoUrl":"https://cdn.example.com/basic.mp4","duration":900}}`), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	})}

	api := baiduXilingAdvancedVideoClient{
		APIBase: "https://baidu.test",
		AppID:   "app-id",
		AppKey:  "app-key",
		Client:  client,
	}
	submission, err := api.submit(context.Background(), baiduXilingAdvancedVideoSubmitInput{
		FigureID:           "2646047",
		DriveType:          "TEXT",
		Text:               "你好，这是离线视频。",
		Width:              1920,
		Height:             1080,
		Transparent:        true,
		TTSPerson:          "person-1",
		TTSLan:             "Chinese",
		TTSSpeed:           "6",
		TTSVolume:          "7",
		TTSPitch:           "4",
		BackgroundImageURL: "https://public.example.com/background.png",
		AutoAnimoji:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if submission.TaskID != "basic-1" || submission.Advanced {
		t.Fatalf("unexpected submission: %#v", submission)
	}
	if submitBody["figureId"] != "2646047" {
		t.Fatalf("expected figureId 2646047, got %#v", submitBody)
	}
	if _, ok := submitBody["templateId"]; ok {
		t.Fatalf("basic submit must not send templateId: %#v", submitBody)
	}
	if submitBody["driveType"] != "TEXT" || submitBody["text"] != "你好，这是离线视频。" {
		t.Fatalf("unexpected text drive fields: %#v", submitBody)
	}
	if _, ok := submitBody["inputAudioUrl"]; ok {
		t.Fatalf("TEXT submit must not send inputAudioUrl: %#v", submitBody)
	}
	videoParams, ok := submitBody["videoParams"].(map[string]any)
	if !ok {
		t.Fatalf("expected videoParams, got %#v", submitBody)
	}
	if videoParams["width"] != float64(1920) || videoParams["height"] != float64(1080) || videoParams["transparent"] != true {
		t.Fatalf("unexpected videoParams: %#v", videoParams)
	}
	ttsParams, ok := submitBody["ttsParams"].(map[string]any)
	if !ok {
		t.Fatalf("expected ttsParams, got %#v", submitBody)
	}
	if ttsParams["person"] != "person-1" || ttsParams["lan"] != "Chinese" || ttsParams["speed"] != "6" || ttsParams["volume"] != "7" || ttsParams["pitch"] != "4" {
		t.Fatalf("unexpected ttsParams: %#v", ttsParams)
	}
	if submitBody["backgroundImageUrl"] != "https://public.example.com/background.png" || submitBody["autoAnimoji"] != true {
		t.Fatalf("unexpected background/animoji fields: %#v", submitBody)
	}
	if _, ok := submitBody["dhParams"]; ok {
		t.Fatalf("basic submit must not send dhParams: %#v", submitBody)
	}

	task, err := api.queryTask(context.Background(), submission.TaskID, submission.Advanced)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "SUCCESS" || task.VideoURL != "https://cdn.example.com/basic.mp4" || task.DurationMS != 900 {
		t.Fatalf("unexpected task response: %#v", task)
	}
}

func TestBaiduXilingInputAudioURLFromRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/characters/c/offline-videos", strings.NewReader("input_audio_url=https%3A%2F%2Fpublic.example.com%2Finput.wav"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	inputAudioURL, err := baiduXilingInputAudioURLFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if inputAudioURL != "https://public.example.com/input.wav" {
		t.Fatalf("unexpected inputAudioUrl: %q", inputAudioURL)
	}
}

func TestBaiduXilingInputAudioURLFromRequestRejectsInvalidURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/characters/c/offline-videos", strings.NewReader("input_audio_url=ftp%3A%2F%2Fpublic.example.com%2Finput.wav"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err := baiduXilingInputAudioURLFromRequest(req)
	if err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("expected http(s) URL error, got %v", err)
	}
}
