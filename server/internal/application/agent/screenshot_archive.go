package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ScreenshotArchiver interface {
	Upload(ctx context.Context, filename, contentType string, data []byte, repositoryID *uuid.UUID, createdByType, createdByID string) (domain.AttachmentMeta, error)
}

func (l *Loop) SetScreenshotArchiver(a ScreenshotArchiver) { l.screenshots = a }

func (l *Loop) archiveImages(ctx context.Context, toolName string, images []domain.ToolResultImage) []string {
	if l.screenshots == nil || len(images) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	ids := make([]string, 0, len(images))
	for i, img := range images {
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			log.Warn().Err(err).Str("tool", toolName).Msg("archiving a tool screenshot failed: not valid base64")
			continue
		}
		name := fmt.Sprintf("%s-%d%s", toolName, i+1, extensionFor(img.MediaType))
		meta, err := l.screenshots.Upload(ctx, name, img.MediaType, data, nil, "agent", toolName)
		if err != nil {
			log.Warn().Err(err).Str("tool", toolName).Msg("archiving a tool screenshot failed")
			continue
		}
		ids = append(ids, meta.ID.String())
	}
	return ids
}

func extensionFor(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}
