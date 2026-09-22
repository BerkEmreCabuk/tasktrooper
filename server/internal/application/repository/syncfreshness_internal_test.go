package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeSyncGit struct {
	fakeReleaseGit
	syncErr error
}

func (f *fakeSyncGit) SyncDefaultBranch(context.Context, string) error { return f.syncErr }

func newSyncService(git *fakeSyncGit) *Service {
	return &Service{
		git:           git,
		syncWarnings:  make(map[uuid.UUID]string),
		syncCheckedAt: make(map[uuid.UUID]time.Time),
	}
}

func TestPullFailureIsRecordedAndCleared(t *testing.T) {
	git := &fakeSyncGit{syncErr: errors.New("git fetch: authentication failed")}
	git.hasGit = true
	svc := newSyncService(git)
	id := uuid.New()

	svc.pullProjectRoot(context.Background(), id, "/data/workspaces/repos/app")
	if got := svc.syncWarning(id); got == "" {
		t.Fatal("failed pull left no warning")
	}

	git.syncErr = nil
	svc.pullProjectRoot(context.Background(), id, "/data/workspaces/repos/app")
	if got := svc.syncWarning(id); got != "" {
		t.Fatalf("successful pull did not clear the warning: %q", got)
	}
}

func TestFreshnessCheckIsThrottled(t *testing.T) {
	svc := newSyncService(&fakeSyncGit{})
	id := uuid.New()

	if !svc.claimFreshnessCheck(id) {
		t.Fatal("first check was not allowed")
	}
	if svc.claimFreshnessCheck(id) {
		t.Fatal("second check ran inside the throttle window")
	}

	svc.syncMu.Lock()
	svc.syncCheckedAt[id] = time.Now().Add(-freshnessCheckInterval - time.Second)
	svc.syncMu.Unlock()

	if !svc.claimFreshnessCheck(id) {
		t.Fatal("check did not resume after the interval elapsed")
	}
}

func TestPullSkippedWithoutWorkingCopy(t *testing.T) {
	git := &fakeSyncGit{syncErr: errors.New("should not be called")}
	git.hasGit = false
	svc := newSyncService(git)
	id := uuid.New()

	svc.pullProjectRoot(context.Background(), id, "/data/workspaces/repos/app")
	if got := svc.syncWarning(id); got != "" {
		t.Fatalf("non-git root produced a warning: %q", got)
	}
}
