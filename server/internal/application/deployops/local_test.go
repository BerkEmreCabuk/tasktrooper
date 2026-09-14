package deployops_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The whole point of the feature: a deploy nobody ran on GitHub still shows
// up as this environment's last run, so the grid stops claiming the last
// Actions run is what is live.
func TestRecordLocalStartThenFinishIsOneRunInTheMatrix(t *testing.T) {
	repoID := uuid.New()
	svc := newTestService(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "web"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod", Provider: "gke"}},
	})
	ctx := context.Background()

	started, err := svc.RecordLocal(ctx, deployops.LocalRunInput{
		RepositoryID: repoID,
		Env:          "prod",
		Status:       domain.RunStatusInProgress,
		Command:      "bash scripts/release-local.sh web",
		HeadSHA:      "abc123def456",
		HeadRef:      "main",
		Actor:        "agent",
	})
	if err != nil {
		t.Fatalf("RecordLocal(start): %v", err)
	}
	if !domain.IsLocalRun(started.RunID) {
		t.Fatalf("start run id %d is not local (must be negative)", started.RunID)
	}
	if started.Status != domain.RunStatusInProgress || started.Conclusion != "" {
		t.Fatalf("start = %+v, want in_progress with no conclusion", started)
	}
	if started.TriggerSource != domain.TriggerSourceLocal || started.HTMLURL != "" {
		t.Fatalf("start = %+v, want trigger_source local and no html_url", started)
	}

	// The finish report carries only the outcome; everything else must survive.
	finished, err := svc.RecordLocal(ctx, deployops.LocalRunInput{
		RepositoryID: repoID,
		Env:          "prod",
		RunID:        started.RunID,
		Status:       domain.RunStatusCompleted,
		Conclusion:   domain.RunConclusionSuccess,
	})
	if err != nil {
		t.Fatalf("RecordLocal(finish): %v", err)
	}
	if finished.RunID != started.RunID {
		t.Fatalf("finish minted a new run %d, want %d", finished.RunID, started.RunID)
	}
	if finished.HeadSHA != "abc123def456" || finished.WorkflowFile != "bash scripts/release-local.sh web" {
		t.Fatalf("finish blanked what start recorded: %+v", finished)
	}
	if finished.TriggeredBy != "agent" {
		t.Fatalf("finish lost the actor: %+v", finished)
	}
	if finished.CompletedAt == nil {
		t.Fatal("finish left completed_at nil")
	}

	runs, err := svc.Runs(ctx, repoID, "prod", 0)
	if err != nil {
		t.Fatalf("Runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("start+finish produced %d runs, want 1", len(runs))
	}

	view, err := svc.Matrix(ctx)
	if err != nil {
		t.Fatalf("Matrix: %v", err)
	}
	var last *domain.DeploymentRun
	for _, cell := range view.Repos[0].Cells {
		if cell.Env == "prod" {
			last = cell.LastRun
		}
	}
	if last == nil || last.TriggerSource != domain.TriggerSourceLocal {
		t.Fatalf("prod cell's last run = %+v, want the local one", last)
	}
}

// A finish report addressed at a GitHub run id would rewrite a real Actions
// run's row — the reason local ids are negative in the first place.
func TestRecordLocalRefusesAGitHubRunID(t *testing.T) {
	repoID := uuid.New()
	svc := newTestService(t, fixture{
		repos: []domain.Repository{{ID: repoID, Name: "web"}},
		runs: []domain.DeploymentRun{{
			RepositoryID: repoID, Env: "prod", RunID: 4242, Status: domain.RunStatusCompleted,
			Conclusion: domain.RunConclusionSuccess, TriggerSource: domain.TriggerSourceExternal,
		}},
	})
	_, err := svc.RecordLocal(context.Background(), deployops.LocalRunInput{
		RepositoryID: repoID, Env: "prod", RunID: 4242,
		Status: domain.RunStatusCompleted, Conclusion: domain.RunConclusionFailure,
	})
	if !errors.Is(err, deployops.ErrNotLocalRun) {
		t.Fatalf("err = %v, want ErrNotLocalRun", err)
	}
}

// A completed run with no usable conclusion would render as "cancelled" on the
// grid, which is a different claim from "we do not know".
func TestRecordLocalRejectsCompletedWithoutConclusion(t *testing.T) {
	repoID := uuid.New()
	svc := newTestService(t, fixture{repos: []domain.Repository{{ID: repoID, Name: "web"}}})
	_, err := svc.RecordLocal(context.Background(), deployops.LocalRunInput{
		RepositoryID: repoID, Env: "prod", Status: domain.RunStatusCompleted,
	})
	if !errors.Is(err, deployops.ErrInvalidConclusion) {
		t.Fatalf("err = %v, want ErrInvalidConclusion", err)
	}
}

func TestRecordLocalRejectsUnknownEnv(t *testing.T) {
	repoID := uuid.New()
	svc := newTestService(t, fixture{repos: []domain.Repository{{ID: repoID, Name: "web"}}})
	_, err := svc.RecordLocal(context.Background(), deployops.LocalRunInput{
		RepositoryID: repoID, Env: "production", Status: domain.RunStatusInProgress,
	})
	if !errors.Is(err, deployops.ErrInvalidEnv) {
		t.Fatalf("err = %v, want ErrInvalidEnv", err)
	}
}

// A deploy still in flight has no outcome. Keeping a conclusion the caller
// passed anyway would paint the cell green before the script finished.
func TestRecordLocalDropsAConclusionOnAnUnfinishedRun(t *testing.T) {
	repoID := uuid.New()
	svc := newTestService(t, fixture{repos: []domain.Repository{{ID: repoID, Name: "web"}}})
	run, err := svc.RecordLocal(context.Background(), deployops.LocalRunInput{
		RepositoryID: repoID, Env: "prod",
		Status: domain.RunStatusInProgress, Conclusion: domain.RunConclusionSuccess,
	})
	if err != nil {
		t.Fatalf("RecordLocal: %v", err)
	}
	if run.Conclusion != "" {
		t.Fatalf("conclusion = %q on an in_progress run, want empty", run.Conclusion)
	}
}

// One deploy is one audit entry, written when the outcome is known — and a
// failed break-glass deploy is audited as an error, not silently as ok.
func TestRecordLocalAuditsOnlyTheFinish(t *testing.T) {
	repoID := uuid.New()
	svc := newTestService(t, fixture{repos: []domain.Repository{{ID: repoID, Name: "web"}}})
	ctx := context.Background()

	started, err := svc.RecordLocal(ctx, deployops.LocalRunInput{
		RepositoryID: repoID, Env: "prod", Status: domain.RunStatusInProgress, Actor: "agent",
	})
	if err != nil {
		t.Fatalf("RecordLocal(start): %v", err)
	}
	entries, err := svc.Audit(ctx, &repoID, 0)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("start wrote %d audit entries, want 0", len(entries))
	}

	if _, err := svc.RecordLocal(ctx, deployops.LocalRunInput{
		RepositoryID: repoID, Env: "prod", RunID: started.RunID,
		Status: domain.RunStatusCompleted, Conclusion: domain.RunConclusionFailure, Actor: "agent",
	}); err != nil {
		t.Fatalf("RecordLocal(finish): %v", err)
	}
	entries, err = svc.Audit(ctx, &repoID, 0)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("finish wrote %d audit entries, want 1", len(entries))
	}
	if entries[0].Outcome != domain.OpsOutcomeError {
		t.Fatalf("failed deploy audited as %q, want error", entries[0].Outcome)
	}
	if entries[0].Detail["source"] != domain.TriggerSourceLocal {
		t.Fatalf("audit detail = %v, want source=local", entries[0].Detail)
	}
}
