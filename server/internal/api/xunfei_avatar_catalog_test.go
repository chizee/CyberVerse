package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseXunfeiAvatarCatalogArray(t *testing.T) {
	resources, err := parseXunfeiAvatarCatalog([]byte(`[
		{
			"avatar_id": " avatar-1 ",
			"avatar_name": " Avatar One ",
			"scene_id": " scene-1 ",
			"thumbnail": " https://example.com/thumb.png ",
			"image_url": " https://example.com/source.png ",
			"voices": [" vcn-1 ", "vcn-2", "vcn-1"],
			"width": 720,
			"height": 1280
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected one resource, got %d", len(resources))
	}
	got := resources[0]
	if got.AvatarID != "avatar-1" || got.AvatarName != "Avatar One" {
		t.Fatalf("unexpected resource identity: %#v", got)
	}
	if got.SceneID != "scene-1" {
		t.Fatalf("unexpected scene id: %#v", got)
	}
	if got.ThumbnailURL != "https://example.com/thumb.png" || got.SourceImageURL != "https://example.com/source.png" {
		t.Fatalf("unexpected resource media: %#v", got)
	}
	if got.VCN != "vcn-1" || len(got.VCNs) != 2 || got.VCNs[1] != "vcn-2" {
		t.Fatalf("unexpected vcns: %#v", got)
	}
	if got.Width != 720 || got.Height != 1280 {
		t.Fatalf("unexpected dimensions: %#v", got)
	}
}

func TestParseXunfeiAvatarCatalogKeyedObject(t *testing.T) {
	resources, err := parseXunfeiAvatarCatalog([]byte(`{
		"avatar-2": {
			"name": "Avatar Two",
			"vcn": "vcn-2"
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected one resource, got %d", len(resources))
	}
	if resources[0].AvatarID != "avatar-2" || resources[0].AvatarName != "Avatar Two" || resources[0].VCN != "vcn-2" {
		t.Fatalf("unexpected keyed resource: %#v", resources[0])
	}
}

func TestHandleGetXunfeiAvatarFromInlineCatalog(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_CATALOG", `[{"avatar_id":"avatar-1","avatar_name":"Avatar One","scene_id":"scene-1","vcn":"vcn-1","thumbnail_url":"https://example.com/thumb.png"}]`)
	t.Setenv("XUNFEI_AVATAR_RESOURCES", "")
	t.Setenv("XUNFEI_AVATAR_CATALOG_FILE", "")
	t.Setenv("XUNFEI_AVATAR_RESOURCE_FILE", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/xunfei/avatars/avatar-1", nil)
	req.SetPathValue("avatar_id", "avatar-1")
	w := httptest.NewRecorder()

	(&Router{}).handleGetXunfeiAvatar(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp xunfeiAvatarResource
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AvatarID != "avatar-1" || resp.AvatarName != "Avatar One" || resp.SceneID != "scene-1" || resp.VCN != "vcn-1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestHandleGetXunfeiAvatarFromBuiltinCatalog(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_CATALOG", "")
	t.Setenv("XUNFEI_AVATAR_RESOURCES", "")
	t.Setenv("XUNFEI_AVATAR_CATALOG_FILE", "")
	t.Setenv("XUNFEI_AVATAR_RESOURCE_FILE", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/xunfei/avatars/201165002", nil)
	req.SetPathValue("avatar_id", "201165002")
	w := httptest.NewRecorder()

	(&Router{}).handleGetXunfeiAvatar(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp xunfeiAvatarResource
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AvatarID != "201165002" || resp.AvatarName != "昭昭-4.0" || resp.VCN != "x7_yachen_pro" {
		t.Fatalf("unexpected builtin response: %#v", resp)
	}
	if resp.SourceImageURL == "" {
		t.Fatalf("expected builtin response to include source image URL: %#v", resp)
	}
}

func TestHandleGetXunfeiAvatarFromEmbeddedCatalog(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_CATALOG", "")
	t.Setenv("XUNFEI_AVATAR_RESOURCES", "")
	t.Setenv("XUNFEI_AVATAR_CATALOG_FILE", "")
	t.Setenv("XUNFEI_AVATAR_RESOURCE_FILE", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/xunfei/avatars/110005011", nil)
	req.SetPathValue("avatar_id", "110005011")
	w := httptest.NewRecorder()

	(&Router{}).handleGetXunfeiAvatar(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp xunfeiAvatarResource
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AvatarID != "110005011" || resp.AvatarName != "晓依" || resp.SourceImageURL == "" {
		t.Fatalf("unexpected embedded response: %#v", resp)
	}
}

func TestXunfeiAvatarCatalogOverridesBuiltin(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_CATALOG", `[{"avatar_id":"201165002","avatar_name":"Custom Zhaozhao","vcn":"custom_vcn"}]`)
	t.Setenv("XUNFEI_AVATAR_RESOURCES", "")
	t.Setenv("XUNFEI_AVATAR_CATALOG_FILE", "")
	t.Setenv("XUNFEI_AVATAR_RESOURCE_FILE", "")

	resources, err := loadXunfeiAvatarCatalog()
	if err != nil {
		t.Fatal(err)
	}
	var got xunfeiAvatarResource
	for _, resource := range resources {
		if resource.AvatarID == "201165002" {
			got = resource
			break
		}
	}
	if got.AvatarName != "Custom Zhaozhao" || got.VCN != "custom_vcn" {
		t.Fatalf("expected inline catalog to override builtin, got %#v", got)
	}
}

func TestHandleGetXunfeiAvatarNotFound(t *testing.T) {
	t.Setenv("XUNFEI_AVATAR_CATALOG", "")
	t.Setenv("XUNFEI_AVATAR_RESOURCES", "")
	t.Setenv("XUNFEI_AVATAR_CATALOG_FILE", "")
	t.Setenv("XUNFEI_AVATAR_RESOURCE_FILE", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/xunfei/avatars/missing", nil)
	req.SetPathValue("avatar_id", "missing")
	w := httptest.NewRecorder()

	(&Router{}).handleGetXunfeiAvatar(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error, "not found") {
		t.Fatalf("expected not found error, got %q", resp.Error)
	}
}
