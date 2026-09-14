package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MaxAttachmentBytes caps a single binary attachment. Attachments live as
// BYTEA rows in Postgres (the tenant pod's disk is ephemeral), so the cap
// protects the database, the connection and the HTTP path all at once.
const MaxAttachmentBytes = 10 << 20

// ErrAttachmentTooLarge rejects an upload over MaxAttachmentBytes. The HTTP
// layer maps it to 413.
var ErrAttachmentTooLarge = fmt.Errorf("attachment exceeds the %d MB limit", MaxAttachmentBytes>>20)

// ErrAttachmentTypeNotAllowed rejects a content type outside the allowlist.
// The HTTP layer maps it to 415.
var ErrAttachmentTypeNotAllowed = errors.New("attachment content type is not allowed")

// AllowedAttachmentTypes is the server-side allowlist for binary attachments:
// the image formats browsers render inline plus common document formats. The
// type is validated after server-side sniffing, never trusted from the client
// alone.
var AllowedAttachmentTypes = map[string]bool{
	"image/png":        true,
	"image/jpeg":       true,
	"image/webp":       true,
	"image/gif":        true,
	"application/pdf":  true,
	"text/plain":       true,
	"text/markdown":    true,
	"text/csv":         true,
	"application/json": true,
	"application/zip":  true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       true,
}

// Attachment is one stored binary file (image or document). Data is never
// serialized into JSON responses — the raw bytes are served only by the
// dedicated GET endpoint.
type Attachment struct {
	ID            uuid.UUID  `json:"id"`
	RepositoryID  *uuid.UUID `json:"repository_id,omitempty"`
	Filename      string     `json:"filename"`
	ContentType   string     `json:"content_type"`
	SizeBytes     int64      `json:"size_bytes"`
	SHA256        string     `json:"sha256"`
	Data          []byte     `json:"-"`
	CreatedByType string     `json:"created_by_type"`
	CreatedByID   string     `json:"created_by_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// AttachmentMeta is Attachment without the bytes — what lists, task detail and
// chat history carry.
type AttachmentMeta struct {
	ID            uuid.UUID  `json:"id"`
	RepositoryID  *uuid.UUID `json:"repository_id,omitempty"`
	Filename      string     `json:"filename"`
	ContentType   string     `json:"content_type"`
	SizeBytes     int64      `json:"size_bytes"`
	SHA256        string     `json:"sha256"`
	CreatedByType string     `json:"created_by_type"`
	CreatedByID   string     `json:"created_by_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Meta strips the bytes.
func (a Attachment) Meta() AttachmentMeta {
	return AttachmentMeta{
		ID:            a.ID,
		RepositoryID:  a.RepositoryID,
		Filename:      a.Filename,
		ContentType:   a.ContentType,
		SizeBytes:     a.SizeBytes,
		SHA256:        a.SHA256,
		CreatedByType: a.CreatedByType,
		CreatedByID:   a.CreatedByID,
		CreatedAt:     a.CreatedAt,
	}
}
