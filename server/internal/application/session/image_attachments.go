package session

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const maxLLMImagesPerRequest = 6

const maxLLMImageBytes = 5 << 20

func attachImageAttachments(ctx context.Context, store AttachmentLinker, rows []domain.SessionMessage, history []domain.Message) {
	if store == nil || len(rows) == 0 || len(rows) != len(history) {
		return
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.Role == domain.RoleUser && r.ID != uuid.Nil {
			ids = append(ids, r.ID)
		}
	}
	if len(ids) == 0 {
		return
	}

	metas, err := store.ListMetaByMessageIDs(ctx, ids)
	if err != nil {
		log.Warn().Err(err).Msg("chat attachment metadata unreadable, sending the conversation without its images")
		return
	}
	if len(metas) == 0 {
		return
	}

	type pick struct {
		index int
		meta  domain.AttachmentMeta
	}

	picks := make([]pick, 0, maxLLMImagesPerRequest)
	for i := len(rows) - 1; i >= 0 && len(picks) < maxLLMImagesPerRequest; i-- {
		if rows[i].Role != domain.RoleUser {
			continue
		}
		for _, meta := range metas[rows[i].ID] {
			if len(picks) >= maxLLMImagesPerRequest {
				break
			}
			if !strings.HasPrefix(meta.ContentType, "image/") || meta.SizeBytes > maxLLMImageBytes {
				continue
			}
			picks = append(picks, pick{index: i, meta: meta})
		}
	}

	for _, p := range picks {
		att, err := store.Get(ctx, p.meta.ID)
		if err != nil {
			log.Warn().Err(err).Str("attachment_id", p.meta.ID.String()).
				Msg("chat image attachment unreadable, skipping it")
			continue
		}
		if len(att.Data) == 0 {
			continue
		}
		history[p.index].Images = append(history[p.index].Images, domain.ToolResultImage{
			MediaType: p.meta.ContentType,
			Data:      base64.StdEncoding.EncodeToString(att.Data),
		})
	}
}
