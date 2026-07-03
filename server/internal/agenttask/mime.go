package agenttask

import (
	"mime"
	"strings"
)

// NormalizeArtifactMimeType makes text artifacts explicit UTF-8 so browsers do not guess legacy encodings.
func NormalizeArtifactMimeType(mimeType, artifactType string) string {
	trimmed := strings.TrimSpace(mimeType)
	normalizedType := strings.ToLower(strings.TrimSpace(artifactType))
	if trimmed == "" {
		trimmed = defaultArtifactMimeType(normalizedType)
	}

	mediaType, params, err := mime.ParseMediaType(trimmed)
	if err != nil {
		if isTextArtifactMimeType(trimmed, normalizedType) && !strings.Contains(strings.ToLower(trimmed), "charset=") {
			return strings.TrimRight(trimmed, "; ") + "; charset=utf-8"
		}
		return trimmed
	}

	if isTextArtifactMimeType(mediaType, normalizedType) {
		if _, ok := params["charset"]; !ok {
			params["charset"] = "utf-8"
		}
		return mime.FormatMediaType(mediaType, params)
	}
	return trimmed
}

func defaultArtifactMimeType(artifactType string) string {
	if strings.Contains(artifactType, "html") {
		return "text/html"
	}
	if artifactType == "text" || artifactType == "txt" || strings.Contains(artifactType, "plain") {
		return "text/plain"
	}
	return "text/markdown"
}

func isTextArtifactMimeType(mediaType, artifactType string) bool {
	normalizedMime := strings.ToLower(strings.TrimSpace(mediaType))
	if strings.HasPrefix(normalizedMime, "text/") {
		return true
	}
	if normalizedMime == "application/json" || strings.HasSuffix(normalizedMime, "+json") {
		return true
	}
	if normalizedMime == "application/xml" || strings.HasSuffix(normalizedMime, "+xml") {
		return true
	}
	if normalizedMime == "application/javascript" || normalizedMime == "application/x-javascript" {
		return true
	}
	return artifactType == "markdown" ||
		artifactType == "md" ||
		strings.Contains(artifactType, "text") ||
		strings.Contains(artifactType, "html")
}
