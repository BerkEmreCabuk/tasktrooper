package deployops_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// A run the console dispatched must end up attributed to the person who
// pushed the button, not to "external".
func TestSweepReconcilesPendingDispatchIntoTheRun(t *testing.T) {
	repoID := uuid.New()
	dispatchedAt := time.Now().Add(-time.Minute)
	m, f := newTestMonitor(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
		dispatches: []domain.DeployDispatch{{
			ID: uuid.New(), RepositoryID: repoID, Env: "prod", WorkflowFile: "prod.yml",
			Ref: "main", Kind: domain.DispatchKindDeploy, Actor: "akif",
			State: domain.DispatchStatePending, CreatedAt: dispatchedAt,
		}},
		actionsRuns: []port.ActionsRun{{
			ID: 99, HeadSHA: "abc", HeadBranch: "main", Status: "completed",
			Conclusion: "success", Event: "workflow_dispatch",
			RunStartedAt: dispatchedAt.Add(5 * time.Second),
		}},
	})

	m.Sweep(context.Background())

	run, err := f.runs.Latest(context.Background(), repoID, "prod")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if run.TriggerSource != domain.TriggerSourceUI || run.TriggeredBy != "akif" {
		t.Fatalf("run not attributed: %+v", run)
	}
	pending, _ := f.dispatches.ListPending(context.Background())
	if len(pending) != 0 {
		t.Fatalf("dispatch still pending after a match")
	}
}

// A run that started BEFORE the dispatch cannot be that dispatch's run.
func TestSweepDoesNotMatchARunOlderThanTheDispatch(t *testing.T) {
	repoID := uuid.New()
	dispatchedAt := time.Now().Add(-time.Minute)
	m, f := newTestMonitor(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
		dispatches: []domain.DeployDispatch{{
			ID: uuid.New(), RepositoryID: repoID, Env: "prod", WorkflowFile: "prod.yml",
			Ref: "main", State: domain.DispatchStatePending, Actor: "akif", CreatedAt: dispatchedAt,
		}},
		actionsRuns: []port.ActionsRun{{
			ID: 1, HeadBranch: "main", Status: "completed", Conclusion: "success",
			RunStartedAt: dispatchedAt.Add(-time.Hour),
		}},
	})
	m.Sweep(context.Background())
	run, _ := f.runs.Latest(context.Background(), repoID, "prod")
	if run.TriggerSource != domain.TriggerSourceExternal {
		t.Fatalf("stale run wrongly attributed: %+v", run)
	}
}

// A dispatch nothing ever matched must not stay pending forever.
func TestSweepAbandonsStaleDispatch(t *testing.T) {
	repoID := uuid.New()
	m, f := newTestMonitor(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
		dispatches: []domain.DeployDispatch{{
			ID: uuid.New(), RepositoryID: repoID, Env: "prod", WorkflowFile: "prod.yml",
			Ref: "main", State: domain.DispatchStatePending,
			CreatedAt: time.Now().Add(-20 * time.Minute),
		}},
	})
	m.Sweep(context.Background())
	pending, _ := f.dispatches.ListPending(context.Background())
	if len(pending) != 0 {
		t.Fatalf("stale dispatch still pending")
	}
}

// A newly failed prod deploy becomes an incident exactly once, no matter how
// many times the sweep sees the same completed run.
func TestSweepIngestsIncidentOnceForAFailedRun(t *testing.T) {
	repoID := uuid.New()
	m, f := newTestMonitor(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
		actionsRuns: []port.ActionsRun{{
			ID: 5, HeadSHA: "bad", Status: "completed", Conclusion: "failure",
			RunStartedAt: time.Now().Add(-time.Minute),
		}},
	})
	m.Sweep(context.Background())
	m.Sweep(context.Background())
	if f.ingester.count != 1 {
		t.Fatalf("ingested %d incidents, want 1", f.ingester.count)
	}
	if f.ingester.last.Source != "deploy" || f.ingester.last.Env != "prod" {
		t.Fatalf("incident = %+v", f.ingester.last)
	}
}
