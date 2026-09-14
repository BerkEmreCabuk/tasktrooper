package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// StoreCredentialStore persists encrypted store console credentials
// (App Store Connect key, Google Play service account JSON).
type StoreCredentialStore struct {
	pool *DB
}

func NewStoreCredentialStore(pool *DB) *StoreCredentialStore {
	return &StoreCredentialStore{pool: pool}
}

func (s *StoreCredentialStore) Set(ctx context.Context, provider string, encrypted []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO store_credentials (provider, data)
		VALUES ($1, $2)
		ON CONFLICT (tenant_id, provider) DO UPDATE SET
			data = EXCLUDED.data,
			updated_at = now()
	`, provider, encrypted)
	if err != nil {
		return fmt.Errorf("set store credential: %w", err)
	}
	return nil
}

func (s *StoreCredentialStore) Get(ctx context.Context, provider string) ([]byte, time.Time, error) {
	var data []byte
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT data, updated_at FROM store_credentials WHERE provider = $1
	`, provider).Scan(&data, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, fmt.Errorf("get store credential: %w", port.ErrNotFound)
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("get store credential: %w", err)
	}
	return data, updatedAt, nil
}

func (s *StoreCredentialStore) Delete(ctx context.Context, provider string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM store_credentials WHERE provider = $1`, provider); err != nil {
		return fmt.Errorf("delete store credential: %w", err)
	}
	return nil
}

func (s *StoreCredentialStore) List(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.pool.Query(ctx, `SELECT provider, updated_at FROM store_credentials ORDER BY provider`)
	if err != nil {
		return nil, fmt.Errorf("list store credentials: %w", err)
	}
	defer rows.Close()

	out := map[string]time.Time{}
	for rows.Next() {
		var provider string
		var updatedAt time.Time
		if err := rows.Scan(&provider, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan store credential: %w", err)
		}
		out[provider] = updatedAt
	}
	return out, rows.Err()
}

// MobileStoreAppStore persists the per-(repository, platform) store
// lifecycle row.
type MobileStoreAppStore struct {
	pool *DB
}

func NewMobileStoreAppStore(pool *DB) *MobileStoreAppStore {
	return &MobileStoreAppStore{pool: pool}
}

const mobileStoreAppCols = `id, repository_id, platform, identifier, store_app_id, app_name, state, review_state,
	last_submitted_version, last_released_version, checklist, onboarding_task_id, first_published_at,
	tracks, tracks_synced_at, created_at, updated_at`

func scanMobileStoreApp(row pgx.Row) (domain.MobileStoreApp, error) {
	var a domain.MobileStoreApp
	var checklistJSON []byte
	var tracksJSON []byte
	if err := row.Scan(
		&a.ID, &a.RepositoryID, &a.Platform, &a.Identifier, &a.StoreAppID, &a.AppName, &a.State, &a.ReviewState,
		&a.LastSubmittedVersion, &a.LastReleasedVersion, &checklistJSON, &a.OnboardingTaskID, &a.FirstPublishedAt,
		&tracksJSON, &a.TracksSyncedAt, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return domain.MobileStoreApp{}, err
	}
	if len(checklistJSON) > 0 {
		if err := json.Unmarshal(checklistJSON, &a.Checklist); err != nil {
			return domain.MobileStoreApp{}, fmt.Errorf("unmarshal checklist: %w", err)
		}
	}
	// '{}' (the column default, and what an app that has never synced holds)
	// unmarshals into a.Tracks' zero value on its own, so there is nothing to
	// special-case beyond skipping Unmarshal on a genuinely empty column.
	if len(tracksJSON) > 0 {
		if err := json.Unmarshal(tracksJSON, &a.Tracks); err != nil {
			return domain.MobileStoreApp{}, fmt.Errorf("unmarshal tracks: %w", err)
		}
	}
	return a, nil
}

func (s *MobileStoreAppStore) Upsert(ctx context.Context, app domain.MobileStoreApp) (domain.MobileStoreApp, error) {
	checklist := app.Checklist
	if checklist == nil {
		checklist = []domain.ChecklistItem{}
	}
	checklistJSON, err := json.Marshal(checklist)
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("marshal checklist: %w", err)
	}
	tracksJSON, err := json.Marshal(app.Tracks)
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("marshal tracks: %w", err)
	}
	a, err := scanMobileStoreApp(s.pool.QueryRow(ctx, `
		INSERT INTO mobile_store_apps (
			repository_id, platform, identifier, store_app_id, app_name, state, review_state,
			last_submitted_version, last_released_version, checklist, onboarding_task_id, first_published_at,
			tracks, tracks_synced_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (tenant_id, repository_id, platform) DO UPDATE SET
			identifier = EXCLUDED.identifier,
			store_app_id = EXCLUDED.store_app_id,
			app_name = EXCLUDED.app_name,
			state = EXCLUDED.state,
			review_state = EXCLUDED.review_state,
			last_submitted_version = EXCLUDED.last_submitted_version,
			last_released_version = EXCLUDED.last_released_version,
			checklist = EXCLUDED.checklist,
			onboarding_task_id = EXCLUDED.onboarding_task_id,
			first_published_at = EXCLUDED.first_published_at,
			tracks = EXCLUDED.tracks,
			tracks_synced_at = EXCLUDED.tracks_synced_at,
			updated_at = now()
		RETURNING `+mobileStoreAppCols,
		app.RepositoryID, app.Platform, app.Identifier, app.StoreAppID, app.AppName, app.State, app.ReviewState,
		app.LastSubmittedVersion, app.LastReleasedVersion, checklistJSON, app.OnboardingTaskID, app.FirstPublishedAt,
		tracksJSON, app.TracksSyncedAt,
	))
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("upsert mobile store app: %w", err)
	}
	return a, nil
}

// SetTracks updates the channel cache columns and nothing else. The column
// list is the whole point — see port.MobileStoreAppStore.SetTracks for the
// lifecycle state an Upsert here would silently roll back.
func (s *MobileStoreAppStore) SetTracks(ctx context.Context, repositoryID uuid.UUID, platform string, tracks domain.StoreTracks, syncedAt time.Time) (domain.MobileStoreApp, error) {
	tracksJSON, err := json.Marshal(tracks)
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("marshal tracks: %w", err)
	}
	a, err := scanMobileStoreApp(s.pool.QueryRow(ctx, `
		UPDATE mobile_store_apps SET
			tracks = $3,
			tracks_synced_at = $4,
			updated_at = now()
		WHERE repository_id = $1 AND platform = $2
		RETURNING `+mobileStoreAppCols,
		repositoryID, platform, tracksJSON, syncedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MobileStoreApp{}, fmt.Errorf("set mobile store app tracks: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("set mobile store app tracks: %w", err)
	}
	return a, nil
}

func (s *MobileStoreAppStore) Get(ctx context.Context, repositoryID uuid.UUID, platform string) (domain.MobileStoreApp, error) {
	a, err := scanMobileStoreApp(s.pool.QueryRow(ctx, `SELECT `+mobileStoreAppCols+`
		FROM mobile_store_apps WHERE repository_id = $1 AND platform = $2`, repositoryID, platform))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MobileStoreApp{}, fmt.Errorf("get mobile store app: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.MobileStoreApp{}, fmt.Errorf("get mobile store app: %w", err)
	}
	return a, nil
}

func (s *MobileStoreAppStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+mobileStoreAppCols+`
		FROM mobile_store_apps WHERE repository_id = $1 ORDER BY platform`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list mobile store apps by repository: %w", err)
	}
	defer rows.Close()
	return collectMobileStoreApps(rows)
}

func (s *MobileStoreAppStore) ListAll(ctx context.Context) ([]domain.MobileStoreApp, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+mobileStoreAppCols+`
		FROM mobile_store_apps ORDER BY repository_id, platform`)
	if err != nil {
		return nil, fmt.Errorf("list all mobile store apps: %w", err)
	}
	defer rows.Close()
	return collectMobileStoreApps(rows)
}

func collectMobileStoreApps(rows pgx.Rows) ([]domain.MobileStoreApp, error) {
	var out []domain.MobileStoreApp
	for rows.Next() {
		a, err := scanMobileStoreApp(rows)
		if err != nil {
			return nil, fmt.Errorf("scan mobile store app: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SigningAssetStore persists encrypted signing artifacts (distribution
// certificates, provisioning profiles, upload keystores).
type SigningAssetStore struct {
	pool *DB
}

func NewSigningAssetStore(pool *DB) *SigningAssetStore {
	return &SigningAssetStore{pool: pool}
}

const signingAssetCols = `id, kind, identifier, serial, data, expires_at, created_at, updated_at`

func scanSigningAsset(row pgx.Row) (domain.SigningAsset, error) {
	var a domain.SigningAsset
	if err := row.Scan(&a.ID, &a.Kind, &a.Identifier, &a.Serial, &a.Data, &a.ExpiresAt, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return domain.SigningAsset{}, err
	}
	return a, nil
}

func (s *SigningAssetStore) Upsert(ctx context.Context, asset domain.SigningAsset) (domain.SigningAsset, error) {
	a, err := scanSigningAsset(s.pool.QueryRow(ctx, `
		INSERT INTO signing_assets (kind, identifier, serial, data, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, kind, identifier) DO UPDATE SET
			serial = EXCLUDED.serial,
			data = EXCLUDED.data,
			expires_at = EXCLUDED.expires_at,
			updated_at = now()
		RETURNING `+signingAssetCols,
		asset.Kind, asset.Identifier, asset.Serial, asset.Data, asset.ExpiresAt,
	))
	if err != nil {
		return domain.SigningAsset{}, fmt.Errorf("upsert signing asset: %w", err)
	}
	return a, nil
}

func (s *SigningAssetStore) Get(ctx context.Context, kind, identifier string) (domain.SigningAsset, error) {
	a, err := scanSigningAsset(s.pool.QueryRow(ctx, `SELECT `+signingAssetCols+`
		FROM signing_assets WHERE kind = $1 AND identifier = $2`, kind, identifier))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SigningAsset{}, fmt.Errorf("get signing asset: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.SigningAsset{}, fmt.Errorf("get signing asset: %w", err)
	}
	return a, nil
}

func (s *SigningAssetStore) ListExpiring(ctx context.Context, before time.Time) ([]domain.SigningAsset, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+signingAssetCols+`
		FROM signing_assets WHERE expires_at IS NOT NULL AND expires_at < $1 ORDER BY expires_at`, before)
	if err != nil {
		return nil, fmt.Errorf("list expiring signing assets: %w", err)
	}
	defer rows.Close()

	var out []domain.SigningAsset
	for rows.Next() {
		a, err := scanSigningAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("scan signing asset: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
