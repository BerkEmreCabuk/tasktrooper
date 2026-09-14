package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// IncidentStore persists production incidents and their timeline.
type IncidentStore interface {
	// Upsert folds an ingested alert into the live incident with the same
	// (repository, env, fingerprint) — bumping occurrences and last_seen_at —
	// or creates a new one. created reports which happened, so the caller only
	// triages (and notifies) on a genuinely new incident.
	Upsert(ctx context.Context, in domain.IncidentInput) (incident domain.Incident, created bool, err error)
	Get(ctx context.Context, id uuid.UUID) (domain.Incident, error)
	List(ctx context.Context, filter domain.IncidentFilter) ([]domain.Incident, error)
	// FindLive returns the non-terminal incident for a fingerprint, if any.
	// The health probe uses it to close its own incident on recovery.
	FindLive(ctx context.Context, repositoryID uuid.UUID, env, fingerprint string) (domain.Incident, error)
	// History returns past incidents with the same fingerprint, newest first —
	// the remedy engine's "this happened before, and here is what fixed it".
	History(ctx context.Context, repositoryID uuid.UUID, fingerprint string, limit int) ([]domain.Incident, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.IncidentStatus) (domain.Incident, error)
	// UpdateRemedy overwrites the current proposal and stamps its author (one of
	// the domain.RemedyAuthor* values), which is what lets the ingest path
	// refuse to replace a human's diagnosis with rules-engine boilerplate.
	UpdateRemedy(ctx context.Context, id uuid.UUID, remedy, remedyKind, author string, confidence int) (domain.Incident, error)
	AttachTask(ctx context.Context, id, taskID uuid.UUID) (domain.Incident, error)
	// ByTask resolves the incident a remediation task belongs to.
	ByTask(ctx context.Context, taskID uuid.UUID) (domain.Incident, error)
	AppendEvent(ctx context.Context, incidentID uuid.UUID, kind, message string) error
	ListEvents(ctx context.Context, incidentID uuid.UUID) ([]domain.IncidentEvent, error)
}
