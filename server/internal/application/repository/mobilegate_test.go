package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeDeployTargetStore is a minimal port.DeployTargetStore for
// mobileStoreGate tests: only Get (keyed by env) is exercised. getErr scripts
// a real infra failure distinct from "no row for this env", mirroring
// fakeMobileStoreAppStore's getErr below.
type fakeDeployTargetStore struct {
	targets map[string]domain.DeployTarget
	getErr  error
}

func (f *fakeDeployTargetStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeDeployTargetStore) ListAll(ctx context.Context) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeDeployTargetStore) Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) (domain.DeployTarget, error) {
	if f.getErr != nil {
		return domain.DeployTarget{}, f.getErr
	}
	t, ok := f.targets[env]
	if !ok {
		return domain.DeployTarget{}, fmt.Errorf("deploy target: %w", port.ErrNotFound)
	}
	return t, nil
}
func (f *fakeDeployTargetStore) Save(ctx context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	return t, nil
}
func (f *fakeDeployTargetStore) Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) error {
	return nil
}

// fakeMobileStoreAppStore is a minimal port.MobileStoreAppStore for
// mobileStoreGate tests: only Get (keyed by platform) is exercised.
type fakeMobileStoreAppStore struct {
	apps   map[string]domain.MobileStoreApp
	getErr error
}

func (f *fakeMobileStoreAppStore) Upsert(ctx context.Context, app domain.MobileStoreApp) (domain.MobileStoreApp, error) {
	return app, nil
}
func (f *fakeMobileStoreAppStore) Get(ctx context.Context, repositoryID uuid.UUID, platform string) (domain.MobileStoreApp, error) {
	if f.getErr != nil {
		return domain.MobileStoreApp{}, f.getErr
	}
	app, ok := f.apps[platform]
	if !ok {
		return domain.MobileStoreApp{}, fmt.Errorf("mobile store app: %w", port.ErrNotFound)
	}
	return app, nil
}
func (f *fakeMobileStoreAppStore) SetTracks(ctx context.Context, repositoryID uuid.UUID, platform string, tracks domain.StoreTracks, syncedAt time.Time) (domain.MobileStoreApp, error) {
	return domain.MobileStoreApp{}, nil
}
func (f *fakeMobileStoreAppStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error) {
	return nil, nil
}
func (f *fakeMobileStoreAppStore) ListAll(ctx context.Context) ([]domain.MobileStoreApp, error) {
	return nil, nil
}

func TestMobileStoreGate(t *testing.T) {
	repoID := uuid.New()
	ctx := context.Background()

	cases := []struct {
		name    string
		env     string
		target  domain.DeployTarget
		app     *domain.MobileStoreApp
		wantErr error
	}{
		{
			name:    "prod blocked when app is test_ready not live",
			env:     domain.DeployEnvProd,
			target:  domain.DeployTarget{Provider: domain.DeployProviderGooglePlay},
			app:     &domain.MobileStoreApp{State: domain.MobileStoreStateTestReady},
			wantErr: domain.ErrMobileAppNotLive,
		},
		{
			name:   "prod allowed when live",
			env:    domain.DeployEnvProd,
			target: domain.DeployTarget{Provider: domain.DeployProviderGooglePlay},
			app:    &domain.MobileStoreApp{State: domain.MobileStoreStateLive},
		},
		{
			name:    "stage blocked while onboarding",
			env:     domain.DeployEnvStage,
			target:  domain.DeployTarget{Provider: domain.DeployProviderAppStore},
			app:     &domain.MobileStoreApp{State: domain.MobileStoreStateOnboarding},
			wantErr: domain.ErrMobileAppNotTestReady,
		},
		{
			name:   "stage allowed when test_ready",
			env:    domain.DeployEnvStage,
			target: domain.DeployTarget{Provider: domain.DeployProviderAppStore},
			app:    &domain.MobileStoreApp{State: domain.MobileStoreStateTestReady},
		},
		{
			name:   "stage allowed when live",
			env:    domain.DeployEnvStage,
			target: domain.DeployTarget{Provider: domain.DeployProviderAppStore},
			app:    &domain.MobileStoreApp{State: domain.MobileStoreStateLive},
		},
		{
			name:   "non-store provider is a no-op on prod",
			env:    domain.DeployEnvProd,
			target: domain.DeployTarget{Provider: domain.DeployProviderFly},
		},
		{
			name:   "non-store provider is a no-op on stage",
			env:    domain.DeployEnvStage,
			target: domain.DeployTarget{Provider: domain.DeployProviderFly},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apps := map[string]domain.MobileStoreApp{}
			if tc.app != nil {
				apps[domain.StoreProviderPlatform(tc.target.Provider)] = *tc.app
			}
			svc := &Service{
				deployTargets:   &fakeDeployTargetStore{targets: map[string]domain.DeployTarget{tc.env: tc.target}},
				mobileStoreApps: &fakeMobileStoreAppStore{apps: apps},
			}
			err := svc.mobileStoreGate(ctx, repoID, tc.env)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// A store target with no app row at all must never panic — it is treated as
// the env's "not ready" sentinel, same as an app that has never onboarded.
func TestMobileStoreGateNoAppRowTreatedAsNotReady(t *testing.T) {
	repoID := uuid.New()
	ctx := context.Background()

	cases := []struct {
		name    string
		env     string
		wantErr error
	}{
		{name: "prod", env: domain.DeployEnvProd, wantErr: domain.ErrMobileAppNotLive},
		{name: "stage", env: domain.DeployEnvStage, wantErr: domain.ErrMobileAppNotTestReady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{
				deployTargets: &fakeDeployTargetStore{targets: map[string]domain.DeployTarget{
					tc.env: {Provider: domain.DeployProviderAppStore},
				}},
				mobileStoreApps: &fakeMobileStoreAppStore{apps: map[string]domain.MobileStoreApp{}},
			}
			err := svc.mobileStoreGate(ctx, repoID, tc.env)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// SetMobileStoreApps was never called (store is nil): a store target still
// cannot be verified, so it must gate the same as a missing row — never
// nil-deref.
func TestMobileStoreGateNoStoreWiredTreatsAsNotReady(t *testing.T) {
	repoID := uuid.New()
	ctx := context.Background()
	svc := &Service{
		deployTargets: &fakeDeployTargetStore{targets: map[string]domain.DeployTarget{
			domain.DeployEnvProd: {Provider: domain.DeployProviderAppStore},
		}},
	}
	err := svc.mobileStoreGate(ctx, repoID, domain.DeployEnvProd)
	if !errors.Is(err, domain.ErrMobileAppNotLive) {
		t.Fatalf("want ErrMobileAppNotLive, got %v", err)
	}
}

// A store lookup error that is not "not found" (e.g. the DB is down) is a
// real failure and must propagate, not be swallowed into a sentinel.
func TestMobileStoreGatePropagatesUnexpectedStoreError(t *testing.T) {
	repoID := uuid.New()
	ctx := context.Background()
	boom := errors.New("boom: store db down")
	svc := &Service{
		deployTargets: &fakeDeployTargetStore{targets: map[string]domain.DeployTarget{
			domain.DeployEnvProd: {Provider: domain.DeployProviderAppStore},
		}},
		mobileStoreApps: &fakeMobileStoreAppStore{getErr: boom},
	}
	err := svc.mobileStoreGate(ctx, repoID, domain.DeployEnvProd)
	if !errors.Is(err, boom) {
		t.Fatalf("want boom to propagate, got %v", err)
	}
}

// A deploy-target lookup error that is not "not found" (e.g. the DB is down)
// is a real infra failure and must propagate, not be swallowed into "no
// target configured, nothing to gate" — a transient DB failure must never
// silently disable the mobile-store gate and let an unverified app ship.
func TestMobileStoreGatePropagatesUnexpectedDeployTargetError(t *testing.T) {
	repoID := uuid.New()
	ctx := context.Background()
	boom := errors.New("boom: deploy target db down")
	svc := &Service{
		deployTargets:   &fakeDeployTargetStore{getErr: boom},
		mobileStoreApps: &fakeMobileStoreAppStore{},
	}
	err := svc.mobileStoreGate(ctx, repoID, domain.DeployEnvProd)
	if !errors.Is(err, boom) {
		t.Fatalf("want boom to propagate, got %v", err)
	}
}

// Non-store deploy paths (no target row at all for the env, e.g. a repo that
// never called SetDeployTargets) must never be blocked by this gate.
func TestMobileStoreGateNoDeployTargetsWiredNeverBlocks(t *testing.T) {
	repoID := uuid.New()
	ctx := context.Background()
	svc := &Service{}
	if err := svc.mobileStoreGate(ctx, repoID, domain.DeployEnvProd); err != nil {
		t.Fatalf("unexpected error with no deploy targets wired: %v", err)
	}
	if err := svc.mobileStoreGate(ctx, repoID, domain.DeployEnvStage); err != nil {
		t.Fatalf("unexpected error with no deploy targets wired: %v", err)
	}
}
