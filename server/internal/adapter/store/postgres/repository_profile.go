package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// RepositoryProfileStore persists the sectioned project profile and the
// settings proposals a profiling pass produces.
type RepositoryProfileStore struct {
	pool *DB
}

func NewRepositoryProfileStore(pool *DB) *RepositoryProfileStore {
	return &RepositoryProfileStore{pool: pool}
}

const profileSectionCols = `id, repository_id, section, body_md, evidence, source_paths, source_commit, origin, stale, updated_at`

func scanProfileSection(row pgx.Row) (domain.ProfileSection, error) {
	var s domain.ProfileSection
	var evidence []byte
	err := row.Scan(&s.ID, &s.RepositoryID, &s.Section, &s.BodyMD, &evidence,
		&s.SourcePaths, &s.SourceCommit, &s.Origin, &s.Stale, &s.UpdatedAt)
	if err != nil {
		return domain.ProfileSection{}, err
	}
	if len(evidence) > 0 {
		_ = json.Unmarshal(evidence, &s.Evidence)
	}
	return s, nil
}

func (s *RepositoryProfileStore) ListSections(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProfileSection, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+profileSectionCols+`
		FROM repository_profile_sections WHERE repository_id = $1`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list profile sections: %w", err)
	}
	defer rows.Close()

	var out []domain.ProfileSection
	for rows.Next() {
		sec, serr := scanProfileSection(rows)
		if serr != nil {
			return nil, fmt.Errorf("scan profile section: %w", serr)
		}
		out = append(out, sec)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("list profile sections: %w", rows.Err())
	}
	domain.SortProfileSections(out)
	return out, nil
}

// UpsertSections replaces the named sections and leaves every other section
// untouched. That per-section replace is what lets a scoped refresh rewrite
// only the parts whose sources moved without dropping the rest of the profile.
func (s *RepositoryProfileStore) UpsertSections(ctx context.Context, repositoryID uuid.UUID, sections []domain.ProfileSection) error {
	if len(sections) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, sec := range sections {
		evidence, err := json.Marshal(sec.Evidence)
		if err != nil {
			return fmt.Errorf("marshal section evidence: %w", err)
		}
		if sec.Evidence == nil {
			evidence = []byte("[]")
		}
		paths := sec.SourcePaths
		if paths == nil {
			paths = []string{}
		}
		batch.Queue(`
			INSERT INTO repository_profile_sections
				(repository_id, section, body_md, evidence, source_paths, source_commit, origin, stale, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, FALSE, now())
			ON CONFLICT (tenant_id, repository_id, section) DO UPDATE SET
				body_md = EXCLUDED.body_md,
				evidence = EXCLUDED.evidence,
				source_paths = EXCLUDED.source_paths,
				source_commit = EXCLUDED.source_commit,
				origin = EXCLUDED.origin,
				stale = FALSE,
				updated_at = now()
		`, repositoryID, sec.Section, sec.BodyMD, evidence, paths, sec.SourceCommit, string(sec.Origin))
	}
	results := s.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()
	for range sections {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("upsert profile section: %w", err)
		}
	}
	return nil
}

// MarkStale flags every section that depends on one of the changed paths.
// Returns the sections it flagged so the caller can refresh exactly those.
func (s *RepositoryProfileStore) MarkStale(ctx context.Context, repositoryID uuid.UUID, changedPaths []string) ([]string, error) {
	if len(changedPaths) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		UPDATE repository_profile_sections
		SET stale = TRUE
		WHERE repository_id = $1 AND source_paths && $2::text[] AND stale = FALSE
		RETURNING section
	`, repositoryID, changedPaths)
	if err != nil {
		return nil, fmt.Errorf("mark profile sections stale: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var section string
		if serr := rows.Scan(&section); serr != nil {
			return nil, fmt.Errorf("scan stale section: %w", serr)
		}
		out = append(out, section)
	}
	return out, rows.Err()
}

// DeleteSections removes sections by id — used when a refresh finds a section
// no longer has anything to say (no CI, no deploy) so the profile does not
// keep asserting a fact the tree dropped.
func (s *RepositoryProfileStore) DeleteSections(ctx context.Context, repositoryID uuid.UUID, sections []string) error {
	if len(sections) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		DELETE FROM repository_profile_sections WHERE repository_id = $1 AND section = ANY($2::text[])
	`, repositoryID, sections)
	if err != nil {
		return fmt.Errorf("delete profile sections: %w", err)
	}
	return nil
}

const profileProposalCols = `id, repository_id, field, slot, value, current_value, label, evidence, status, created_at, applied_at`

func scanProfileProposal(row pgx.Row) (domain.ProfileProposal, error) {
	var p domain.ProfileProposal
	var value, evidence []byte
	err := row.Scan(&p.ID, &p.RepositoryID, &p.Field, &p.Slot, &value, &p.Current,
		&p.Label, &evidence, &p.Status, &p.CreatedAt, &p.AppliedAt)
	if err != nil {
		return domain.ProfileProposal{}, err
	}
	p.Value = json.RawMessage(value)
	if len(evidence) > 0 {
		_ = json.Unmarshal(evidence, &p.Evidence)
	}
	return p, nil
}

// ReplaceProposals writes this refresh's proposals, replacing any pending
// proposal for the same (field, slot) and clearing pending proposals the new
// pass no longer believes in. Applied and dismissed rows are left alone —
// dismissing a proposal has to survive the next refresh, or the UI would keep
// re-asking a question the human already answered.
func (s *RepositoryProfileStore) ReplaceProposals(ctx context.Context, repositoryID uuid.UUID, proposals []domain.ProfileProposal) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("replace profile proposals: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	keep := make([]string, 0, len(proposals))
	for _, p := range proposals {
		keep = append(keep, p.Field+"\x00"+p.Slot)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM repository_profile_proposals
		WHERE repository_id = $1 AND status = 'pending' AND (field || E'\\000' || slot) <> ALL($2::text[])
	`, repositoryID, keep); err != nil {
		return fmt.Errorf("clear stale proposals: %w", err)
	}

	for _, p := range proposals {
		evidence, merr := json.Marshal(p.Evidence)
		if merr != nil {
			return fmt.Errorf("marshal proposal evidence: %w", merr)
		}
		if p.Evidence == nil {
			evidence = []byte("[]")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO repository_profile_proposals
				(repository_id, field, slot, value, current_value, label, evidence, status, created_at, applied_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), $9)
			ON CONFLICT (tenant_id, repository_id, field, slot) DO UPDATE SET
				value = EXCLUDED.value,
				current_value = EXCLUDED.current_value,
				label = EXCLUDED.label,
				evidence = EXCLUDED.evidence,
				status = EXCLUDED.status,
				created_at = now(),
				applied_at = EXCLUDED.applied_at
			WHERE repository_profile_proposals.status <> 'dismissed'
		`, repositoryID, p.Field, p.Slot, []byte(p.Value), p.Current, p.Label, evidence, string(p.Status), p.AppliedAt); err != nil {
			return fmt.Errorf("upsert profile proposal: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("replace profile proposals: %w", err)
	}
	return nil
}

func (s *RepositoryProfileStore) ListProposals(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProfileProposal, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+profileProposalCols+`
		FROM repository_profile_proposals WHERE repository_id = $1
		ORDER BY created_at DESC, field`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list profile proposals: %w", err)
	}
	defer rows.Close()
	var out []domain.ProfileProposal
	for rows.Next() {
		p, perr := scanProfileProposal(rows)
		if perr != nil {
			return nil, fmt.Errorf("scan profile proposal: %w", perr)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *RepositoryProfileStore) GetProposal(ctx context.Context, id uuid.UUID) (domain.ProfileProposal, error) {
	p, err := scanProfileProposal(s.pool.QueryRow(ctx, `SELECT `+profileProposalCols+`
		FROM repository_profile_proposals WHERE id = $1`, id))
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.ProfileProposal{}, fmt.Errorf("get profile proposal: %w", port.ErrNotFound)
		}
		return domain.ProfileProposal{}, fmt.Errorf("get profile proposal: %w", err)
	}
	return p, nil
}

func (s *RepositoryProfileStore) SetProposalStatus(ctx context.Context, id uuid.UUID, status domain.ProfileProposalStatus) (domain.ProfileProposal, error) {
	var appliedAt *time.Time
	if status == domain.ProposalApplied {
		now := time.Now().UTC()
		appliedAt = &now
	}
	p, err := scanProfileProposal(s.pool.QueryRow(ctx, `
		UPDATE repository_profile_proposals SET status = $2, applied_at = $3
		WHERE id = $1 RETURNING `+profileProposalCols, id, string(status), appliedAt))
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.ProfileProposal{}, fmt.Errorf("set proposal status: %w", port.ErrNotFound)
		}
		return domain.ProfileProposal{}, fmt.Errorf("set proposal status: %w", err)
	}
	return p, nil
}
