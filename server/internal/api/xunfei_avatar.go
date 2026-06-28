package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyberverse/server/internal/character"
	"github.com/cyberverse/server/internal/xunfeiavatar"
)

type xunfeiAvatarSessionConfig = xunfeiavatar.FrontendConfig

func startXunfeiAvatarSession(ctx context.Context, c *character.Character) (*xunfeiavatar.Session, *xunfeiAvatarSessionConfig, error) {
	if c == nil || c.Xunfei == nil || strings.TrimSpace(c.Xunfei.AvatarID) == "" {
		return nil, nil, fmt.Errorf("Xunfei avatar_id is required")
	}

	client, err := xunfeiavatar.NewClientFromEnv()
	if err != nil {
		return nil, nil, err
	}
	session, err := client.Start(ctx, characterForXunfei(c))
	if err != nil {
		return nil, nil, err
	}
	cfg := session.FrontendConfig()
	return session, &cfg, nil
}

func characterForXunfei(c *character.Character) *character.XunfeiAvatar {
	if c == nil || c.Xunfei == nil {
		return nil
	}
	cfg := character.NormalizeXunfeiAvatarConfig(c.Xunfei)
	if cfg == nil || strings.TrimSpace(cfg.AvatarID) == "" {
		return cfg
	}
	if resource, ok := lookupXunfeiAvatarCatalogResource(cfg.AvatarID); ok {
		if strings.TrimSpace(cfg.AvatarName) == "" {
			cfg.AvatarName = resource.AvatarName
		}
		if strings.TrimSpace(cfg.SceneID) == "" {
			cfg.SceneID = resource.SceneID
		}
		if strings.TrimSpace(cfg.VCN) == "" {
			cfg.VCN = resource.VCN
		}
		if strings.TrimSpace(cfg.ThumbnailURL) == "" {
			cfg.ThumbnailURL = resource.ThumbnailURL
		}
		if strings.TrimSpace(cfg.PreviewVideoURL) == "" {
			cfg.PreviewVideoURL = resource.PreviewVideoURL
		}
		if strings.TrimSpace(cfg.SourceImageURL) == "" {
			cfg.SourceImageURL = resource.SourceImageURL
		}
		if strings.TrimSpace(cfg.Status) == "" {
			cfg.Status = resource.Status
		}
		if cfg.Width <= 0 {
			cfg.Width = resource.Width
		}
		if cfg.Height <= 0 {
			cfg.Height = resource.Height
		}
	}
	cfg = character.NormalizeXunfeiAvatarConfig(cfg)
	return cfg
}

func lookupXunfeiAvatarCatalogResource(avatarID string) (xunfeiAvatarResource, bool) {
	target := strings.TrimSpace(avatarID)
	if target == "" {
		return xunfeiAvatarResource{}, false
	}
	resources, err := loadXunfeiAvatarCatalog()
	if err != nil {
		return xunfeiAvatarResource{}, false
	}
	for _, resource := range resources {
		if resource.AvatarID == target {
			return resource, true
		}
	}
	return xunfeiAvatarResource{}, false
}
