package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cyberverse/server/internal/character"
	"github.com/cyberverse/server/internal/config"
	"github.com/cyberverse/server/internal/xunfeiavatar"
)

type smokeResult struct {
	OK               bool   `json:"ok"`
	AvatarID         string `json:"avatar_id"`
	AvatarName       string `json:"avatar_name,omitempty"`
	SceneID          string `json:"scene_id,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	StreamURLPresent bool   `json:"stream_url_present,omitempty"`
	Error            string `json:"error,omitempty"`
}

func configRoot(configPath string) string {
	dir := filepath.Dir(configPath)
	if filepath.Base(dir) == "config" {
		return filepath.Dir(dir)
	}
	return dir
}

func envPaths(configPath string) []string {
	dir := filepath.Dir(configPath)
	root := configRoot(configPath)
	return []string{
		filepath.Join(root, ".env"),
		filepath.Join(dir, ".env"),
		filepath.Join(dir, "env"),
	}
}

func main() {
	configPath := flag.String("config", "../config/cyberverse.yaml", "CyberVerse config path used to locate env files")
	avatarID := flag.String("avatar-id", "201165002", "Xunfei avatar_id to smoke test")
	avatarName := flag.String("avatar-name", "昭昭-4.0", "Xunfei avatar display name for reporting")
	sceneID := flag.String("scene-id", "", "Optional scene_id override; defaults to XUNFEI_AVATAR_SCENE_ID")
	vcn := flag.String("vcn", "", "Optional voice override; defaults to XUNFEI_AVATAR_DEFAULT_VCN")
	protocol := flag.String("protocol", "flv", "Stream protocol requested from Xunfei")
	timeout := flag.Duration("timeout", 30*time.Second, "Start timeout")
	flag.Parse()

	for _, envPath := range envPaths(*configPath) {
		if err := config.LoadDotenv(envPath); err != nil {
			writeResult(smokeResult{OK: false, AvatarID: *avatarID, AvatarName: *avatarName, Error: err.Error()})
			os.Exit(1)
		}
	}

	client, err := xunfeiavatar.NewClientFromEnv()
	if err != nil {
		writeResult(smokeResult{OK: false, AvatarID: *avatarID, AvatarName: *avatarName, Error: err.Error()})
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	session, err := client.Start(ctx, &character.XunfeiAvatar{
		AvatarID:   *avatarID,
		AvatarName: *avatarName,
		SceneID:    *sceneID,
		VCN:        *vcn,
		Protocol:   *protocol,
	})
	if err != nil {
		writeResult(smokeResult{OK: false, AvatarID: *avatarID, AvatarName: *avatarName, Error: err.Error()})
		os.Exit(1)
	}

	result := smokeResult{
		OK:               true,
		AvatarID:         session.FrontendConfig().AvatarID,
		AvatarName:       session.FrontendConfig().AvatarName,
		SceneID:          session.FrontendConfig().SceneID,
		Protocol:         session.FrontendConfig().Protocol,
		StreamURLPresent: session.FrontendConfig().StreamURL != "",
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopCancel()
	if err := session.Stop(stopCtx); err != nil {
		result.OK = false
		result.Error = "stop failed: " + err.Error()
		writeResult(result)
		os.Exit(1)
	}

	writeResult(result)
}

func writeResult(result smokeResult) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Printf("{\"ok\":false,\"error\":%q}\n", err.Error())
		return
	}
	fmt.Println(string(data))
}
