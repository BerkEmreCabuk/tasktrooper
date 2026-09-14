package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// AttachmentStore persists binary attachments (bytes in Postgres, not on the
// ephemeral pod disk) and their links to board tasks and chat messages.
type AttachmentStore interface {
	Create(ctx context.Context, att domain.Attachment) (domain.AttachmentMeta, error)
	// Get returns the attachment including its bytes — the raw-download path.
	Get(ctx context.Context, id uuid.UUID) (domain.Attachment, error)
	GetMeta(ctx context.Context, id uuid.UUID) (domain.AttachmentMeta, error)
	ListMetaByTask(ctx context.Context, taskID uuid.UUID) ([]domain.AttachmentMeta, error)
	// ListMetaByMessageIDs is a single bulk query keyed by message id, so
	// enriching a whole chat history never becomes N+1.
	ListMetaByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error)
	LinkTask(ctx context.Context, taskID, attachmentID uuid.UUID, position int) error
	UnlinkTask(ctx context.Context, taskID, attachmentID uuid.UUID) error
	LinkMessage(ctx context.Context, messageID, attachmentID uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
}
