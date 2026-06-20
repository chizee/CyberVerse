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
