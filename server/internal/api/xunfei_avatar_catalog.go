package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/cyberverse/server/internal/character"
)

type xunfeiAvatarResource struct {
	AvatarID        string   `json:"avatar_id"`
	AvatarName      string   `json:"avatar_name,omitempty"`
	SceneID         string   `json:"scene_id,omitempty"`
	VCN             string   `json:"vcn,omitempty"`
	VCNs            []string `json:"vcns,omitempty"`
	ThumbnailURL    string   `json:"thumbnail_url,omitempty"`
	PreviewVideoURL string   `json:"preview_video_url,omitempty"`
	SourceImageURL  string   `json:"source_image_url,omitempty"`
	Status          string   `json:"status,omitempty"`
	Width           int      `json:"width,omitempty"`
	Height          int      `json:"height,omitempty"`
}

type xunfeiAvatarResourceRaw struct {
	AvatarID        string   `json:"avatar_id"`
	ID              string   `json:"id"`
	AvatarName      string   `json:"avatar_name"`
	Name            string   `json:"name"`
	SceneID         string   `json:"scene_id"`
	Scene           string   `json:"scene"`
	VCN             string   `json:"vcn"`
	VCNs            []string `json:"vcns"`
	Voices          []string `json:"voices"`
	ThumbnailURL    string   `json:"thumbnail_url"`
	Thumbnail       string   `json:"thumbnail"`
	ImageURL        string   `json:"image_url"`
	PreviewImageURL string   `json:"preview_image_url"`
	PreviewVideoURL string   `json:"preview_video_url"`
	SourceImageURL  string   `json:"source_image_url"`
	Status          string   `json:"status"`
	Width           int      `json:"width"`
	Height          int      `json:"height"`
}

type xunfeiAvatarCatalogObject struct {
	Avatars []xunfeiAvatarResourceRaw `json:"avatars"`
}

var errXunfeiAvatarNotFound = errors.New("Xunfei avatar not found")

func (r *Router) handleGetXunfeiAvatar(w http.ResponseWriter, req *http.Request) {
	avatarID := strings.TrimSpace(req.PathValue("avatar_id"))
	if avatarID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "avatar_id is required"})
		return
	}

	avatar, err := r.lookupXunfeiAvatar(avatarID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errXunfeiAvatarNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, avatar)
}

func (r *Router) lookupXunfeiAvatar(avatarID string) (xunfeiAvatarResource, error) {
	target := strings.TrimSpace(avatarID)
	if target == "" {
		return xunfeiAvatarResource{}, errXunfeiAvatarNotFound
	}

	resources, err := loadXunfeiAvatarCatalog()
	if err != nil {
		return xunfeiAvatarResource{}, err
	}
	for _, resource := range resources {
		if resource.AvatarID == target {
			return resource, nil
		}
	}

	if r != nil && r.charStore != nil {
		for _, c := range r.charStore.List() {
			if c == nil || c.AvatarBackend != character.AvatarBackendXunfei || c.Xunfei == nil {
				continue
			}
			if strings.TrimSpace(c.Xunfei.AvatarID) == target {
				return xunfeiAvatarResourceFromCharacter(c.Xunfei), nil
			}
		}
	}
	return xunfeiAvatarResource{}, errXunfeiAvatarNotFound
}

func loadXunfeiAvatarCatalog() ([]xunfeiAvatarResource, error) {
	resources := []xunfeiAvatarResource{}

	if len(embeddedXunfeiAvatarCatalog) > 0 {
		embeddedResources, err := parseXunfeiAvatarCatalog(embeddedXunfeiAvatarCatalog)
		if err != nil {
			return nil, err
		}
		resources = append(resources, embeddedResources...)
	}
	if path := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_CATALOG_FILE")); path != "" {
		fileResources, err := loadXunfeiAvatarCatalogFile(path)
		if err != nil {
			return nil, err
		}
		resources = append(resources, fileResources...)
	}
	if path := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_RESOURCE_FILE")); path != "" {
		fileResources, err := loadXunfeiAvatarCatalogFile(path)
		if err != nil {
			return nil, err
		}
		resources = append(resources, fileResources...)
	}
	if inline := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_CATALOG")); inline != "" {
		inlineResources, err := parseXunfeiAvatarCatalog([]byte(inline))
		if err != nil {
			return nil, err
		}
		resources = append(resources, inlineResources...)
	}
	if inline := strings.TrimSpace(os.Getenv("XUNFEI_AVATAR_RESOURCES")); inline != "" {
		inlineResources, err := parseXunfeiAvatarCatalog([]byte(inline))
		if err != nil {
			return nil, err
		}
		resources = append(resources, inlineResources...)
	}

	merged := make(map[string]xunfeiAvatarResource)
	order := make([]string, 0, len(resources))
	for _, resource := range resources {
		if resource.AvatarID == "" {
			continue
		}
		existing, exists := merged[resource.AvatarID]
		if !exists {
			order = append(order, resource.AvatarID)
			merged[resource.AvatarID] = resource
			continue
		}
		merged[resource.AvatarID] = mergeXunfeiAvatarResource(existing, resource)
	}
	out := make([]xunfeiAvatarResource, 0, len(order))
	for _, id := range order {
		out = append(out, merged[id])
	}
	return out, nil
}

func mergeXunfeiAvatarResource(base, overlay xunfeiAvatarResource) xunfeiAvatarResource {
	if overlay.AvatarID != "" {
		base.AvatarID = overlay.AvatarID
	}
	if overlay.AvatarName != "" {
		base.AvatarName = overlay.AvatarName
	}
	if overlay.SceneID != "" {
		base.SceneID = overlay.SceneID
	}
	if overlay.VCN != "" {
		base.VCN = overlay.VCN
	}
	if len(overlay.VCNs) > 0 {
		base.VCNs = overlay.VCNs
	}
	if overlay.ThumbnailURL != "" {
		base.ThumbnailURL = overlay.ThumbnailURL
	}
	if overlay.PreviewVideoURL != "" {
		base.PreviewVideoURL = overlay.PreviewVideoURL
	}
	if overlay.SourceImageURL != "" {
		base.SourceImageURL = overlay.SourceImageURL
	}
	if overlay.Status != "" {
		base.Status = overlay.Status
	}
	if overlay.Width > 0 {
		base.Width = overlay.Width
	}
	if overlay.Height > 0 {
		base.Height = overlay.Height
	}
	return base
}

func loadXunfeiAvatarCatalogFile(path string) ([]xunfeiAvatarResource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Xunfei avatar catalog: %w", err)
	}
	return parseXunfeiAvatarCatalog(data)
}

func parseXunfeiAvatarCatalog(data []byte) ([]xunfeiAvatarResource, error) {
	var list []xunfeiAvatarResourceRaw
	if err := json.Unmarshal(data, &list); err == nil {
		return normalizeXunfeiAvatarResources(list), nil
	}

	var object xunfeiAvatarCatalogObject
	if err := json.Unmarshal(data, &object); err == nil && object.Avatars != nil {
		return normalizeXunfeiAvatarResources(object.Avatars), nil
	}

	var keyed map[string]xunfeiAvatarResourceRaw
	if err := json.Unmarshal(data, &keyed); err != nil {
		return nil, fmt.Errorf("parse Xunfei avatar catalog: %w", err)
	}
	list = make([]xunfeiAvatarResourceRaw, 0, len(keyed))
	for id, raw := range keyed {
		if strings.TrimSpace(raw.AvatarID) == "" && strings.TrimSpace(raw.ID) == "" {
			raw.AvatarID = id
		}
		list = append(list, raw)
	}
	return normalizeXunfeiAvatarResources(list), nil
}

func normalizeXunfeiAvatarResources(raws []xunfeiAvatarResourceRaw) []xunfeiAvatarResource {
	resources := make([]xunfeiAvatarResource, 0, len(raws))
	for _, raw := range raws {
		if resource := normalizeXunfeiAvatarResource(raw); resource.AvatarID != "" {
			resources = append(resources, resource)
		}
	}
	return resources
}

func normalizeXunfeiAvatarResource(raw xunfeiAvatarResourceRaw) xunfeiAvatarResource {
	avatarID := strings.TrimSpace(raw.AvatarID)
	if avatarID == "" {
		avatarID = strings.TrimSpace(raw.ID)
	}
	avatarName := strings.TrimSpace(raw.AvatarName)
	if avatarName == "" {
		avatarName = strings.TrimSpace(raw.Name)
	}
	sceneID := strings.TrimSpace(raw.SceneID)
	if sceneID == "" {
		sceneID = strings.TrimSpace(raw.Scene)
	}
	sourceImageURL := firstNonEmpty(raw.SourceImageURL, raw.ImageURL, raw.PreviewImageURL)
	vcns := trimStringList(raw.VCNs)
	if len(vcns) == 0 {
		vcns = trimStringList(raw.Voices)
	}
	vcn := strings.TrimSpace(raw.VCN)
	if vcn == "" && len(vcns) > 0 {
		vcn = vcns[0]
	}
	if len(vcns) == 0 && vcn != "" {
		vcns = []string{vcn}
	}

	return xunfeiAvatarResource{
		AvatarID:        avatarID,
		AvatarName:      avatarName,
		SceneID:         sceneID,
		VCN:             vcn,
		VCNs:            vcns,
		ThumbnailURL:    firstNonEmpty(raw.ThumbnailURL, raw.Thumbnail),
		PreviewVideoURL: strings.TrimSpace(raw.PreviewVideoURL),
		SourceImageURL:  sourceImageURL,
		Status:          strings.TrimSpace(raw.Status),
		Width:           maxInt(raw.Width, 0),
		Height:          maxInt(raw.Height, 0),
	}
}

func xunfeiAvatarResourceFromCharacter(cfg *character.XunfeiAvatar) xunfeiAvatarResource {
	if cfg == nil {
		return xunfeiAvatarResource{}
	}
	resource := normalizeXunfeiAvatarResource(xunfeiAvatarResourceRaw{
		AvatarID:        cfg.AvatarID,
		AvatarName:      cfg.AvatarName,
		SceneID:         cfg.SceneID,
		VCN:             cfg.VCN,
		ThumbnailURL:    cfg.ThumbnailURL,
		PreviewVideoURL: cfg.PreviewVideoURL,
		SourceImageURL:  cfg.SourceImageURL,
		Status:          cfg.Status,
		Width:           cfg.Width,
		Height:          cfg.Height,
	})
	return resource
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func trimStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}
