package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type DocumentFormat string

const (
	DocumentFormatMarkdown DocumentFormat = "markdown"
	DocumentFormatHTML     DocumentFormat = "html"
)

// MaxHTMLDocumentBytes bounds one html task document. An analysis report is
// read back into agent runs and rendered in the task drawer; past this size it
// is neither.
const MaxHTMLDocumentBytes = 1 << 20

var (
	ErrTaskDocumentNotFound  = errors.New("task document not found")
	ErrInvalidDocumentFormat = errors.New("invalid document format")
	ErrDocumentTooLarge      = fmt.Errorf("html document exceeds %d bytes", MaxHTMLDocumentBytes)
)

// NormalizeDocumentFormat resolves an omitted format to markdown, the only
// format documents had before html reports existed.
func NormalizeDocumentFormat(f DocumentFormat) (DocumentFormat, error) {
	switch DocumentFormat(strings.ToLower(strings.TrimSpace(string(f)))) {
	case "", DocumentFormatMarkdown:
		return DocumentFormatMarkdown, nil
	case DocumentFormatHTML:
		return DocumentFormatHTML, nil
	default:
		return "", fmt.Errorf("%w: %q (use \"markdown\" or \"html\")", ErrInvalidDocumentFormat, f)
	}
}

type AnnotationStatus string

const (
	AnnotationStatusOpen      AnnotationStatus = "open"
	AnnotationStatusSubmitted AnnotationStatus = "submitted"
	AnnotationStatusResolved  AnnotationStatus = "resolved"
)

func ValidAnnotationStatus(s AnnotationStatus) bool {
	switch s {
	case AnnotationStatusOpen, AnnotationStatusSubmitted, AnnotationStatusResolved:
		return true
	}
	return false
}

// Limits are in characters (runes), the unit a person selecting text sees.
const (
	MaxAnnotationQuoteChars   = 2000
	MaxAnnotationBodyChars    = 4000
	MaxAnnotationContextChars = 200
	MaxAnnotationReplyChars   = 2000
)

var (
	ErrAnnotationNotFound = errors.New("annotation not found")
	ErrAnnotationInvalid  = errors.New("invalid annotation")
	// ErrAnnotationConflict is a write the annotation's (or its task's) current
	// state does not allow: editing a submitted comment, submitting a review of
	// a task that is not waiting for one.
	ErrAnnotationConflict = errors.New("annotation state conflict")
	ErrNoOpenAnnotations  = errors.New("no open annotations to submit")
)

// TaskDocumentAnnotation is a reviewer's comment anchored to a passage of a
// task document. Quote/Prefix/Suffix are a text-quote selector: the rendered
// text of the passage and a little of what surrounds it, so the passage can be
// found again after the document is rewritten.
type TaskDocumentAnnotation struct {
	ID            uuid.UUID        `json:"id"`
	TaskID        uuid.UUID        `json:"task_id"`
	DocumentID    uuid.UUID        `json:"document_id"`
	Quote         string           `json:"quote"`
	Prefix        string           `json:"prefix"`
	Suffix        string           `json:"suffix"`
	Body          string           `json:"body"`
	Status        AnnotationStatus `json:"status"`
	Reply         string           `json:"reply"`
	CreatedByType string           `json:"created_by_type"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	SubmittedAt   *time.Time       `json:"submitted_at,omitempty"`
	ResolvedAt    *time.Time       `json:"resolved_at,omitempty"`
}

type CreateDocumentAnnotationRequest struct {
	Quote  string `json:"quote"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
	Body   string `json:"body"`
}

// Validate trims Body only: Quote/Prefix/Suffix locate the passage by exact
// text, so their whitespace is part of the match.
func (r *CreateDocumentAnnotationRequest) Validate() error {
	r.Body = strings.TrimSpace(r.Body)
	if strings.TrimSpace(r.Quote) == "" {
		return fmt.Errorf("%w: quote is required", ErrAnnotationInvalid)
	}
	if n := utf8.RuneCountInString(r.Quote); n > MaxAnnotationQuoteChars {
		return fmt.Errorf("%w: quote is %d characters, the limit is %d", ErrAnnotationInvalid, n, MaxAnnotationQuoteChars)
	}
	if err := validateAnnotationBody(r.Body); err != nil {
		return err
	}
	if n := utf8.RuneCountInString(r.Prefix); n > MaxAnnotationContextChars {
		return fmt.Errorf("%w: prefix is %d characters, the limit is %d", ErrAnnotationInvalid, n, MaxAnnotationContextChars)
	}
	if n := utf8.RuneCountInString(r.Suffix); n > MaxAnnotationContextChars {
		return fmt.Errorf("%w: suffix is %d characters, the limit is %d", ErrAnnotationInvalid, n, MaxAnnotationContextChars)
	}
	return nil
}

func validateAnnotationBody(body string) error {
	if body == "" {
		return fmt.Errorf("%w: body is required", ErrAnnotationInvalid)
	}
	if n := utf8.RuneCountInString(body); n > MaxAnnotationBodyChars {
		return fmt.Errorf("%w: body is %d characters, the limit is %d", ErrAnnotationInvalid, n, MaxAnnotationBodyChars)
	}
	return nil
}

// UpdateDocumentAnnotationRequest.Status accepts only "open": reopening a
// resolved comment. Every other transition belongs to submit and resolve.
type UpdateDocumentAnnotationRequest struct {
	Body   *string           `json:"body,omitempty"`
	Status *AnnotationStatus `json:"status,omitempty"`
}

type SubmitAnnotationsRequest struct {
	Note string `json:"note,omitempty"`
}

type AnnotationResolution struct {
	ID    uuid.UUID `json:"id"`
	Reply string    `json:"reply"`
}

type AnnotationResolveFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type AnnotationResolveResult struct {
	Resolved []TaskDocumentAnnotation   `json:"resolved"`
	Failed   []AnnotationResolveFailure `json:"failed,omitempty"`
}

// ApplyAnnotationUpdate is the state machine behind PATCH: a resolved comment
// may be reopened (its reply and resolved_at go with it), and only an open
// comment's body may change.
func ApplyAnnotationUpdate(a TaskDocumentAnnotation, req UpdateDocumentAnnotationRequest) (TaskDocumentAnnotation, error) {
	if req.Body == nil && req.Status == nil {
		return a, fmt.Errorf("%w: nothing to update: pass body, status, or both", ErrAnnotationInvalid)
	}
	if req.Status != nil {
		switch *req.Status {
		case AnnotationStatusOpen:
		default:
			return a, fmt.Errorf("%w: status may only be set to %q", ErrAnnotationInvalid, AnnotationStatusOpen)
		}
		switch a.Status {
		case AnnotationStatusResolved:
			a.Status = AnnotationStatusOpen
			a.Reply = ""
			a.ResolvedAt = nil
			a.SubmittedAt = nil
		case AnnotationStatusOpen:
		default:
			return a, fmt.Errorf("%w: a %s comment cannot be reopened until the agent resolves it", ErrAnnotationConflict, a.Status)
		}
	}
	if req.Body != nil {
		if a.Status != AnnotationStatusOpen {
			return a, fmt.Errorf("%w: a %s comment can no longer be edited", ErrAnnotationConflict, a.Status)
		}
		body := strings.TrimSpace(*req.Body)
		if err := validateAnnotationBody(body); err != nil {
			return a, err
		}
		a.Body = body
	}
	return a, nil
}
