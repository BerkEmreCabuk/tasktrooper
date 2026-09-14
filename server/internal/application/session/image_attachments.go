package session

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// maxLLMImagesPerRequest caps how many attached images one request carries.
// Images are the most expensive thing a chat turn can hold, and a long thread
// accumulates them without limit — six is enough for "look at these screens"
// while keeping a re-read of the whole history from blowing the context budget
// (and the bill) on pictures the user stopped talking about turns ago.
const maxLLMImagesPerRequest = 6

// maxLLMImageBytes skips an attachment that is too big to be worth sending.
// The upload limit is 10 MB, but a single image over 5 MB costs more context
// than any provider will usefully spend on it, so it stays in the transcript
// (where the user can still see it) without entering the prompt.
const maxLLMImageBytes = 5 << 20

// attachImageAttachments gives the model the images the user attached to their
// chat messages, by loading their bytes onto the matching history entries.
//
// rows and history must be index-parallel — history[i] is the domain message
// built from rows[i] — which is only true inside buildMessageHistory, before
// anything is inserted into the history.
//
// Everything here is best-effort: an unreadable attachment costs one image, not
// the user's message. Only image/* attachments are considered; documents are
// not something these providers can read as bytes.
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
	// Newest message first: when a conversation holds more images than the cap
	// allows, the ones the user just sent are the ones being asked about. The
	// older ones drop out silently — they are still in the transcript.
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
