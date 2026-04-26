package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyberverse/server/internal/character"
)

type characterResponse struct {
	*character.Character
	IdleVideoURL  string   `json:"idle_video_url,omitempty"`
	IdleVideoURLs []string `json:"idle_video_urls,omitempty"`
}

// idleVideoURLs returns all idle video URLs for a character's active image.
func (r *Router) idleVideoURLs(characterID, imageFilename string) []string {
	if r.charStore == nil || characterID == "" || imageFilename == "" {
		return nil
	}
	imgBase := strings.TrimSuffix(imageFilename, filepath.Ext(imageFilename))
	files, err := r.charStore.ListIdleVideos(characterID, imageFilename)
	if err != nil || len(files) == 0 {
		return nil
	}
	urls := make([]string, 0, len(files))
	for _, f := range files {
		urls = append(urls, fmt.Sprintf("/api/v1/characters/%s/idle-videos/%s/%s", characterID, imgBase, f))
	}
	return urls
}

// idleVideoURL returns the first idle video URL (backward compatibility).
func (r *Router) idleVideoURL(characterID, imageFilename string) string {
	urls := r.idleVideoURLs(characterID, imageFilename)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func (r *Router) buildCharacterResponse(c *character.Character) characterResponse {
	if c == nil {
		return characterResponse{}
	}
	urls := r.idleVideoURLs(c.ID, c.ActiveImage)
	firstURL := ""
	if len(urls) > 0 {
		firstURL = urls[0]
	}
	return characterResponse{
		Character:     c,
		IdleVideoURL:  firstURL,
		IdleVideoURLs: urls,
	}
}

func (r *Router) handleListCharacters(w http.ResponseWriter, req *http.Request) {
	chars := r.charStore.List()
	result := make([]characterResponse, 0, len(chars))
	for _, c := range chars {
		result = append(result, r.buildCharacterResponse(c))
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleGetCharacter(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	c, err := r.charStore.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, r.buildCharacterResponse(c))
}

func (r *Router) handleCreateCharacter(w http.ResponseWriter, req *http.Request) {
	var c character.Character
	if err := json.NewDecoder(req.Body).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if c.Name == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "name is required"})
		return
	}

	created, err := r.charStore.Create(&c)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, r.buildCharacterResponse(created))
}

func (r *Router) handleUpdateCharacter(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var c character.Character
	if err := json.NewDecoder(req.Body).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	updated, err := r.charStore.Update(id, &c)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, r.buildCharacterResponse(updated))
}

func (r *Router) handleDeleteCharacter(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if err := r.charStore.Delete(id); err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUploadAvatar uploads an image to the character's images/ directory.
// Kept at POST /api/v1/characters/{id}/avatar for frontend compatibility.
func (r *Router) handleUploadAvatar(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if _, err := r.charStore.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}

	if err := req.ParseMultipartForm(10 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "file too large"})
		return
	}

	file, handler, err := req.FormFile("avatar")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "avatar file required"})
		return
	}
	defer file.Close()

	ext := filepath.Ext(handler.Filename)
	if ext == "" {
		ext = ".png"
	}

	imgDir := r.charStore.ImagesDir(id)
	if imgDir == "" {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "character directory not found"})
		return
	}
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		log.Printf("Failed to create images dir: %v", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "server error"})
		return
	}

	baseName := r.charStore.NextImageFilename(id)
	filename := baseName + ext
	destPath := filepath.Join(imgDir, filename)

	dest, err := os.Create(destPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to save file"})
		return
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to write file"})
		return
	}

	// Add image to character's image list
	info := character.ImageInfo{
		Filename: filename,
		OrigName: handler.Filename,
		AddedAt:  fmt.Sprintf("%d", handler.Size),
	}
	if err := r.charStore.AddImage(id, info); err != nil {
		log.Printf("Failed to add image record: %v", err)
	}

	c, _ := r.charStore.Get(id)
	writeJSON(w, http.StatusOK, map[string]string{"path": c.AvatarImage})
}

// handleListImages returns all images for a character.
func (r *Router) handleListImages(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	imgs, err := r.charStore.ListImages(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}

	// Add URL field for each image
	type imageResp struct {
		character.ImageInfo
		URL string `json:"url"`
	}
	result := make([]imageResp, len(imgs))
	for i, img := range imgs {
		result[i] = imageResp{
			ImageInfo: img,
			URL:       fmt.Sprintf("/api/v1/characters/%s/images/%s", id, img.Filename),
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// handleGetCharacterImage serves an image file from the character's images/ directory.
func (r *Router) handleGetCharacterImage(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	filename := req.PathValue("filename")

	if filename == "" || filename != filepath.Base(filename) || strings.Contains(filename, "..") {
		http.NotFound(w, req)
		return
	}

	imgDir := r.charStore.ImagesDir(id)
	if imgDir == "" {
		http.NotFound(w, req)
		return
	}

	imgPath := filepath.Join(imgDir, filename)
	if _, err := os.Stat(imgPath); err != nil {
		http.NotFound(w, req)
		return
	}

	http.ServeFile(w, req, imgPath)
}

// handleGetIdleVideo serves a cached idle MP4 from the character's idle_videos/{imgbase}/ directory.
func (r *Router) handleGetIdleVideo(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	imgbase := req.PathValue("imgbase")
	filename := req.PathValue("filename")

	// Validate path components to prevent traversal
	for _, part := range []string{imgbase, filename} {
		if part == "" || part != filepath.Base(part) || strings.Contains(part, "..") {
			http.NotFound(w, req)
			return
		}
	}

	videoDir := r.charStore.IdleVideosDir(id)
	if videoDir == "" {
		http.NotFound(w, req)
		return
	}

	videoPath := filepath.Join(videoDir, imgbase, filename)
	if _, err := os.Stat(videoPath); err != nil {
		http.NotFound(w, req)
		return
	}

	http.ServeFile(w, req, videoPath)
}

// handleDeleteImage removes an image from a character.
func (r *Router) handleDeleteImage(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	filename := req.PathValue("filename")

	if filename == "" || filename != filepath.Base(filename) || strings.Contains(filename, "..") {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid filename"})
		return
	}

	// Delete file from disk
	imgDir := r.charStore.ImagesDir(id)
	if imgDir == "" {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "character not found"})
		return
	}
	imgPath := filepath.Join(imgDir, filename)
	os.Remove(imgPath)

	// Remove from character record
	if err := r.charStore.RemoveImage(id, filename); err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleActivateImage sets a specific image as the character's active avatar.
func (r *Router) handleActivateImage(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	filename := req.PathValue("filename")

	if err := r.charStore.ActivateImage(id, filename); err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}

	c, _ := r.charStore.Get(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"active_image":    c.ActiveImage,
		"avatar_image":    c.AvatarImage,
		"idle_video_url":  r.idleVideoURL(c.ID, c.ActiveImage),
		"idle_video_urls": r.idleVideoURLs(c.ID, c.ActiveImage),
	})
}

// handleGetAvatar serves avatar files (backward compatibility for old /api/v1/avatars/{filename} URLs).
func (r *Router) handleGetAvatar(w http.ResponseWriter, req *http.Request) {
	filename := req.PathValue("filename")
	if filename == "" || filename != filepath.Base(filename) || strings.Contains(filename, "..") {
		http.NotFound(w, req)
		return
	}

	// Legacy path: look in old data/avatars/ directory
	avatarPath := filepath.Join(filepath.Dir(r.charStore.BaseDir()), "avatars", filename)
	if _, err := os.Stat(avatarPath); err != nil {
		http.NotFound(w, req)
		return
	}

	http.ServeFile(w, req, avatarPath)
}
