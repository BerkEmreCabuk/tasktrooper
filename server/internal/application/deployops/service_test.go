package deployops_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestMatrixEmitsACellPerEnvIncludingUnconfigured(t *testing.T) {
	repoID := uuid.New()
	svc := newTestService(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "tasktrooper", Kind: "backend"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod", Provider: "gke"}},
	})

	view, err := svc.Matrix(context.Background())
	if err != nil {
		t.Fatalf("Matrix: %v", err)
	}
	if len(view.Repos) != 1 || len(view.Repos[0].Cells) != len(domain.DeployEnvs()) {
		t.Fatalf("view = %+v", view)
	}
	byEnv := map[string]deployops.MatrixCell{}
	for _, c := range view.Repos[0].Cells {
		byEnv[c.Env] = c
	}
	if !byEnv["prod"].Configured || byEnv["stage"].Configured {
		t.Fatalf("configured flags wrong: %+v", byEnv)
	}
}

func TestMatrixMarksTargetWithoutWorkflowMappingUndispatchable(t *testing.T) {
	repoID := uuid.New()
	svc := newTestService(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
	})
	view, _ := svc.Matrix(context.Background())
	for _, c := range view.Repos[0].Cells {
		if c.Env == "prod" && c.Dispatchable {
			t.Fatal("prod marked dispatchable with no workflow mapping")
		}
	}
}

func TestMatrixFillsRollbackSHAFromLastGoodRun(t *testing.T) {
	repoID := uuid.New()
	old := time.Now().Add(-2 * time.Hour)
	nowish := time.Now().Add(-time.Minute)
	svc := newTestService(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		runs: []domain.DeploymentRun{
			{RepositoryID: repoID, Env: "prod", RunID: 1, HeadSHA: "good", Conclusion: domain.RunConclusionSuccess, StartedAt: &old, CompletedAt: &old},
			{RepositoryID: repoID, Env: "prod", RunID: 2, HeadSHA: "bad", Conclusion: domain.RunConclusionFailure, StartedAt: &nowish, CompletedAt: &nowish},
		},
	})
	view, _ := svc.Matrix(context.Background())
	for _, c := range view.Repos[0].Cells {
		if c.Env == "prod" && c.RollbackSHA != "good" {
			t.Fatalf("rollback sha = %q, want good", c.RollbackSHA)
		}
	}
}

func TestMatrixOmitsRepositoryWithNoDeployTargets(t *testing.T) {
	configuredRepoID := uuid.New()
	targetlessRepoID := uuid.New()
	svc := newTestService(t, fixture{
		repos: []domain.Repository{
			{ID: configuredRepoID, Name: "has-targets", Kind: "backend"},
			{ID: targetlessRepoID, Name: "no-targets", Kind: "backend"},
		},
		targets: []domain.DeployTarget{
			{RepositoryID: configuredRepoID, Env: "prod", Provider: "gke"},
		},
	})

	view, err := svc.Matrix(context.Background())
	if err != nil {
		t.Fatalf("Matrix: %v", err)
	}
	if len(view.Repos) != 1 {
		t.Fatalf("view.Repos = %+v, want exactly the one repo with a deploy target", view.Repos)
	}
	if view.Repos[0].ID != configuredRepoID {
		t.Fatalf("view.Repos[0].ID = %v, want %v", view.Repos[0].ID, configuredRepoID)
	}
	for _, r := range view.Repos {
		if r.ID == targetlessRepoID {
			t.Fatalf("repository with no deploy targets should be omitted from view.Repos: %+v", r)
		}
	}
}

func TestRunsClampsLimit(t *testing.T) {
	repoID := uuid.New()
	env := domain.DeployEnvProd

	seedRuns := func(n int) []domain.DeploymentRun {
		rows := make([]domain.DeploymentRun, 0, n)
		for i := 0; i < n; i++ {
			started := time.Now().Add(-time.Duration(i) * time.Minute)
			rows = append(rows, domain.DeploymentRun{
				RepositoryID: repoID,
				Env:          env,
				RunID:        int64(i + 1),
				HeadSHA:      "sha",
				StartedAt:    &started,
			})
		}
		return rows
	}

	tests := []struct {
		name      string
		seedCount int
		limit     int
		wantCount int
	}{
		{name: "limit <= 0 applies the default of 20", seedCount: 30, limit: 0, wantCount: 20},
		{name: "negative limit applies the default of 20", seedCount: 30, limit: -5, wantCount: 20},
		{name: "limit above the max of 100 is clamped down", seedCount: 150, limit: 1000, wantCount: 100},
		{name: "limit exactly at the max of 100 passes through (inclusive)", seedCount: 150, limit: 100, wantCount: 100},
		{name: "in-range limit passes through unchanged", seedCount: 30, limit: 12, wantCount: 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t, fixture{runs: seedRuns(tt.seedCount)})
			got, err := svc.Runs(context.Background(), repoID, env, tt.limit)
			if err != nil {
				t.Fatalf("Runs: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Fatalf("len(got) = %d, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func seedAuditEntries(repositoryID uuid.UUID, n int) []domain.OpsAuditEntry {
	rows := make([]domain.OpsAuditEntry, 0, n)
	for i := 0; i < n; i++ {
		id := repositoryID
		rows = append(rows, domain.OpsAuditEntry{
			ID:           uuid.New(),
			RepositoryID: &id,
			Action:       "dispatch",
			CreatedAt:    time.Now().Add(-time.Duration(i) * time.Minute),
		})
	}
	return rows
}

func TestAuditClampsLimit(t *testing.T) {
	repoID := uuid.New()
	manyForOneRepo := seedAuditEntries(repoID, 250)

	tests := []struct {
		name         string
		entries      []domain.OpsAuditEntry
		repositoryID *uuid.UUID
		limit        int
		wantCount    int
	}{
		{
			name:         "limit <= 0 applies the default of 50",
			entries:      manyForOneRepo,
			repositoryID: &repoID,
			limit:        0,
			wantCount:    50,
		},
		{
			name:         "negative limit applies the default of 50",
			entries:      manyForOneRepo,
			repositoryID: &repoID,
			limit:        -7,
			wantCount:    50,
		},
		{
			name:         "limit above the max of 200 is clamped down",
			entries:      manyForOneRepo,
			repositoryID: &repoID,
			limit:        1000,
			wantCount:    200,
		},
		{
			name:         "limit exactly at the max of 200 passes through (inclusive)",
			entries:      manyForOneRepo,
			repositoryID: &repoID,
			limit:        200,
			wantCount:    200,
		},
		{
			name:         "in-range limit passes through unchanged",
			entries:      manyForOneRepo,
			repositoryID: &repoID,
			limit:        30,
			wantCount:    30,
		},
		{
			name:         "nil repositoryID returns entries across every repository",
			entries:      append(seedAuditEntries(uuid.New(), 20), seedAuditEntries(uuid.New(), 25)...),
			repositoryID: nil,
			limit:        0,
			wantCount:    45,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t, fixture{audit: tt.entries})
			got, err := svc.Audit(context.Background(), tt.repositoryID, tt.limit)
			if err != nil {
				t.Fatalf("Audit: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Fatalf("len(got) = %d, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestDispatchProdRequiresExactConfirmPhrase(t *testing.T) {
	repoID := uuid.New()
	svc, actions := newTestServiceWithActions(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "tasktrooper"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
	})
	for _, confirm := range []string{"", "TaskTrooper", "tasktroope"} {
		_, err := svc.Dispatch(context.Background(), deployops.DispatchInput{RepositoryID: repoID, Env: "prod", Confirm: confirm, Actor: "akif"})
		if !errors.Is(err, deployops.ErrConfirmMismatch) {
			t.Fatalf("confirm %q: err = %v, want ErrConfirmMismatch", confirm, err)
		}
	}
	if actions.dispatches != 0 {
		t.Fatalf("dispatched %d times despite a failed confirm", actions.dispatches)
	}
}

func TestDispatchStageNeedsNoConfirm(t *testing.T) {
	repoID := uuid.New()
	svc, actions := newTestServiceWithActions(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "stage"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "stage_deploy", TargetKind: "workflow", TargetRef: "stage.yml"}},
	})
	svc.SetRepoResolver(func(_ context.Context, _ domain.Repository) (string, string, error) {
		return "acme", "r", nil
	})
	d, err := svc.Dispatch(context.Background(), deployops.DispatchInput{RepositoryID: repoID, Env: "stage", Actor: "akif"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if d.Ref != "main" || actions.lastRef != "main" {
		t.Fatalf("empty ref did not default to %q: %+v", "main", d)
	}
	if d.State != domain.DispatchStatePending {
		t.Fatalf("state = %q", d.State)
	}
}

func TestDispatchWithoutWorkflowMappingFails(t *testing.T) {
	repoID := uuid.New()
	svc, _ := newTestServiceWithActions(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "stage"}},
	})
	_, err := svc.Dispatch(context.Background(), deployops.DispatchInput{RepositoryID: repoID, Env: "stage"})
	if !errors.Is(err, deployops.ErrNoWorkflowMapping) {
		t.Fatalf("err = %v, want ErrNoWorkflowMapping", err)
	}
}

func TestRollbackTagsLastGoodSHAAndDispatchesTheTag(t *testing.T) {
	repoID := uuid.New()
	old := time.Now().Add(-2 * time.Hour)
	nowish := time.Now().Add(-time.Minute)
	svc, actions := newTestServiceWithActions(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
		runs: []domain.DeploymentRun{
			{RepositoryID: repoID, Env: "prod", RunID: 1, HeadSHA: "good", Conclusion: domain.RunConclusionSuccess, StartedAt: &old, CompletedAt: &old},
			{RepositoryID: repoID, Env: "prod", RunID: 2, HeadSHA: "bad", Conclusion: domain.RunConclusionFailure, StartedAt: &nowish, CompletedAt: &nowish},
		},
	})
	svc.SetRepoResolver(func(_ context.Context, _ domain.Repository) (string, string, error) {
		return "acme", "r", nil
	})
	d, err := svc.Rollback(context.Background(), deployops.RollbackInput{RepositoryID: repoID, Env: "prod", Confirm: "r", Actor: "akif"})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if actions.lastTagSHA != "good" {
		t.Fatalf("tagged sha = %q, want good", actions.lastTagSHA)
	}
	if !strings.HasPrefix(actions.lastRef, "rollback/prod/") || actions.lastRef != actions.lastTag {
		t.Fatalf("dispatched ref %q is not the tag %q", actions.lastRef, actions.lastTag)
	}
	if d.Kind != domain.DispatchKindRollback || d.RollbackOfSHA != "good" {
		t.Fatalf("dispatch = %+v", d)
	}
}

func TestRollbackWithoutAGoodRunFails(t *testing.T) {
	repoID := uuid.New()
	svc, actions := newTestServiceWithActions(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
	})
	_, err := svc.Rollback(context.Background(), deployops.RollbackInput{RepositoryID: repoID, Env: "prod", Confirm: "r"})
	if !errors.Is(err, deployops.ErrNoRollbackTarget) {
		t.Fatalf("err = %v, want ErrNoRollbackTarget", err)
	}
	if actions.tags != 0 {
		t.Fatalf("created %d tags for an impossible rollback", actions.tags)
	}
}

func TestDispatchWithoutRepoResolverFails(t *testing.T) {
	repoID := uuid.New()
	svc, actions := newTestServiceWithActions(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "stage"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "stage_deploy", TargetKind: "workflow", TargetRef: "stage.yml"}},
	})
	_, err := svc.Dispatch(context.Background(), deployops.DispatchInput{RepositoryID: repoID, Env: "stage"})
	if !errors.Is(err, deployops.ErrNoRepoResolver) {
		t.Fatalf("err = %v, want ErrNoRepoResolver", err)
	}
	if actions.dispatches != 0 {
		t.Fatalf("dispatched %d times with no repo resolver configured", actions.dispatches)
	}
}

func TestDispatchUsesResolvedRepoCoordinates(t *testing.T) {
	repoID := uuid.New()
	svc, actions := newTestServiceWithActions(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r", RootPath: "/repos/r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "stage"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "stage_deploy", TargetKind: "workflow", TargetRef: "stage.yml"}},
	})
	var gotRootPath string
	svc.SetRepoResolver(func(_ context.Context, repo domain.Repository) (string, string, error) {
		gotRootPath = repo.RootPath
		return "acme", "widgets", nil
	})

	_, err := svc.Dispatch(context.Background(), deployops.DispatchInput{RepositoryID: repoID, Env: "stage"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if gotRootPath != "/repos/r" {
		t.Fatalf("resolver did not receive the repository row: root_path = %q", gotRootPath)
	}
	if len(actions.DispatchWorkflowCalls) != 1 {
		t.Fatalf("DispatchWorkflowCalls = %+v", actions.DispatchWorkflowCalls)
	}
	if call := actions.DispatchWorkflowCalls[0]; call.Owner != "acme" || call.Repo != "widgets" {
		t.Fatalf("dispatch call = %+v, want owner=acme repo=widgets", call)
	}
}

func TestRollbackWithoutRepoResolverFails(t *testing.T) {
	repoID := uuid.New()
	old := time.Now().Add(-2 * time.Hour)
	nowish := time.Now().Add(-time.Minute)
	svc, actions := newTestServiceWithActions(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
		runs: []domain.DeploymentRun{
			{RepositoryID: repoID, Env: "prod", RunID: 1, HeadSHA: "good", Conclusion: domain.RunConclusionSuccess, StartedAt: &old, CompletedAt: &old},
			{RepositoryID: repoID, Env: "prod", RunID: 2, HeadSHA: "bad", Conclusion: domain.RunConclusionFailure, StartedAt: &nowish, CompletedAt: &nowish},
		},
	})
	_, err := svc.Rollback(context.Background(), deployops.RollbackInput{RepositoryID: repoID, Env: "prod", Confirm: "r"})
	if !errors.Is(err, deployops.ErrNoRepoResolver) {
		t.Fatalf("err = %v, want ErrNoRepoResolver", err)
	}
	if actions.tags != 0 {
		t.Fatalf("created %d tags with no repo resolver configured", actions.tags)
	}
}

func TestRollbackUsesResolvedRepoCoordinates(t *testing.T) {
	repoID := uuid.New()
	old := time.Now().Add(-2 * time.Hour)
	nowish := time.Now().Add(-time.Minute)
	svc, actions := newTestServiceWithActions(t, fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "r"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
		runs: []domain.DeploymentRun{
			{RepositoryID: repoID, Env: "prod", RunID: 1, HeadSHA: "good", Conclusion: domain.RunConclusionSuccess, StartedAt: &old, CompletedAt: &old},
			{RepositoryID: repoID, Env: "prod", RunID: 2, HeadSHA: "bad", Conclusion: domain.RunConclusionFailure, StartedAt: &nowish, CompletedAt: &nowish},
		},
	})
	svc.SetRepoResolver(func(_ context.Context, _ domain.Repository) (string, string, error) {
		return "acme", "widgets", nil
	})

	_, err := svc.Rollback(context.Background(), deployops.RollbackInput{RepositoryID: repoID, Env: "prod", Confirm: "r", Actor: "akif"})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if len(actions.CreateTagCalls) != 1 {
		t.Fatalf("CreateTagCalls = %+v", actions.CreateTagCalls)
	}
	if call := actions.CreateTagCalls[0]; call.Owner != "acme" || call.Repo != "widgets" {
		t.Fatalf("create tag call = %+v, want owner=acme repo=widgets", call)
	}
	if len(actions.DispatchWorkflowCalls) != 1 {
		t.Fatalf("DispatchWorkflowCalls = %+v", actions.DispatchWorkflowCalls)
	}
	if call := actions.DispatchWorkflowCalls[0]; call.Owner != "acme" || call.Repo != "widgets" {
		t.Fatalf("dispatch call = %+v, want owner=acme repo=widgets", call)
	}
}

func TestFailedDispatchIsStillAudited(t *testing.T) {
	repoID := uuid.New()
	svc, _ := newTestServiceWithActions(t, fixture{
		repos:       []domain.Repository{{ID: repoID, Name: "r"}},
		targets:     []domain.DeployTarget{{RepositoryID: repoID, Env: "prod"}},
		jobs:        []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
		dispatchErr: errors.New("github exploded"),
	})
	svc.SetRepoResolver(func(_ context.Context, _ domain.Repository) (string, string, error) {
		return "acme", "r", nil
	})
	_, err := svc.Dispatch(context.Background(), deployops.DispatchInput{RepositoryID: repoID, Env: "prod", Confirm: "r", Actor: "akif"})
	if !errors.Is(err, deployops.ErrProvider) {
		t.Fatalf("err = %v, want the github failure (ErrProvider) to surface", err)
	}
	entries, _ := svc.Audit(context.Background(), &repoID, 10)
	if len(entries) != 1 || entries[0].Outcome != domain.OpsOutcomeError || entries[0].Actor != "akif" {
		t.Fatalf("audit = %+v", entries)
	}
}

func agentRollbackFixture(repoID uuid.UUID) fixture {
	old := time.Now().Add(-2 * time.Hour)
	nowish := time.Now().Add(-time.Minute)
	return fixture{
		repos:   []domain.Repository{{ID: repoID, Name: "acme"}},
		targets: []domain.DeployTarget{{RepositoryID: repoID, Env: "prod", AutoRollback: true}},
		jobs:    []domain.RepositoryPipelineJob{{RepositoryID: repoID, Category: "prod_deploy", TargetKind: "workflow", TargetRef: "prod.yml"}},
		runs: []domain.DeploymentRun{
			{RepositoryID: repoID, Env: "prod", RunID: 1, HeadSHA: "good", Conclusion: domain.RunConclusionSuccess, StartedAt: &old, CompletedAt: &old},
			{RepositoryID: repoID, Env: "prod", RunID: 2, HeadSHA: "bad", Conclusion: domain.RunConclusionFailure, StartedAt: &nowish, CompletedAt: &nowish},
		},
	}
}

func TestRollbackForTaskDispatchesWithoutAConfirmationPhrase(t *testing.T) {
	repoID := uuid.New()
	taskID := uuid.New()
	svc, actions := newTestServiceWithActions(t, agentRollbackFixture(repoID))
	svc.SetRepoResolver(func(_ context.Context, _ domain.Repository) (string, string, error) {
		return "acme-org", "acme", nil
	})

	d, err := svc.RollbackForTask(context.Background(),
		deployops.RollbackInput{RepositoryID: repoID, Env: "prod", Actor: "agent:qa-agent"},
		deployops.AgentRollbackAuthorization{
			TaskID: taskID, TaskKey: "T-7", OwnedMergeSHA: "bad",
			Trigger: "deploy_failed", AgentName: "qa-agent",
		})
	if err != nil {
		t.Fatalf("RollbackForTask: %v", err)
	}
	if actions.lastTagSHA != "good" || d.RollbackOfSHA != "good" {
		t.Fatalf("rollback did not target the last good SHA: tag=%q dispatch=%+v", actions.lastTagSHA, d)
	}
	if !strings.HasPrefix(actions.lastRef, "rollback/prod/") {
		t.Fatalf("dispatched ref = %q, want the rollback tag", actions.lastRef)
	}
}

func TestRollbackForTaskWithoutOwnershipProofIsRefused(t *testing.T) {
	repoID := uuid.New()
	svc, actions := newTestServiceWithActions(t, agentRollbackFixture(repoID))
	svc.SetRepoResolver(func(_ context.Context, _ domain.Repository) (string, string, error) {
		return "acme-org", "acme", nil
	})

	_, err := svc.RollbackForTask(context.Background(),
		deployops.RollbackInput{RepositoryID: repoID, Env: "prod"},
		deployops.AgentRollbackAuthorization{})
	if !errors.Is(err, domain.ErrRollbackNotOwner) {
		t.Fatalf("err = %v, want ErrRollbackNotOwner", err)
	}
	if len(actions.CreateTagCalls) != 0 || len(actions.DispatchWorkflowCalls) != 0 {
		t.Fatalf("an unauthorized agent rollback touched GitHub: tags=%d dispatches=%d", len(actions.CreateTagCalls), len(actions.DispatchWorkflowCalls))
	}
}

func TestHumanRollbackStillRequiresTheTypedConfirmation(t *testing.T) {
	repoID := uuid.New()
	svc, actions := newTestServiceWithActions(t, agentRollbackFixture(repoID))
	svc.SetRepoResolver(func(_ context.Context, _ domain.Repository) (string, string, error) {
		return "acme-org", "acme", nil
	})

	_, err := svc.Rollback(context.Background(), deployops.RollbackInput{RepositoryID: repoID, Env: "prod", Confirm: "wrong"})
	if !errors.Is(err, deployops.ErrConfirmMismatch) {
		t.Fatalf("err = %v, want ErrConfirmMismatch", err)
	}
	if len(actions.CreateTagCalls) != 0 || len(actions.DispatchWorkflowCalls) != 0 {
		t.Fatalf("an unconfirmed human rollback touched GitHub: tags=%d dispatches=%d", len(actions.CreateTagCalls), len(actions.DispatchWorkflowCalls))
	}
}

func TestRollbackForTaskIsAuditedWithTheCardAndTrigger(t *testing.T) {
	repoID := uuid.New()
	taskID := uuid.New()
	f := agentRollbackFixture(repoID)
	audit := newFakeOpsAuditStore(f.audit)
	svc := deployops.New(
		newFakeDeploymentRunStore(f.runs),
		newFakeDeployDispatchStore(f.dispatches),
		audit,
		newFakeDeployTargetStore(f.targets),
		newFakeRepositoryStore(f.repos),
		newFakeRepositoryPipelineJobStore(f.jobs),
		&fakeActionsClient{},
	)
	svc.SetRepoResolver(func(_ context.Context, _ domain.Repository) (string, string, error) {
		return "acme-org", "acme", nil
	})

	if _, err := svc.RollbackForTask(context.Background(),
		deployops.RollbackInput{RepositoryID: repoID, Env: "prod", Actor: "agent:qa-agent"},
		deployops.AgentRollbackAuthorization{
			TaskID: taskID, TaskKey: "T-7", OwnedMergeSHA: "bad",
			Trigger: "health_incident", AgentName: "qa-agent",
		}); err != nil {
		t.Fatalf("RollbackForTask: %v", err)
	}

	entries, err := audit.List(context.Background(), &repoID, 10)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("an agent rollback recorded no audit entry")
	}
	last := entries[0]
	for key, want := range map[string]string{
		"actor_kind":      "agent",
		"task_key":        "T-7",
		"owned_merge_sha": "bad",
		"trigger":         "health_incident",
		"agent":           "qa-agent",
	} {
		if got := last.Detail[key]; got != want {
			t.Fatalf("audit detail[%q] = %q, want %q (full: %+v)", key, got, want, last.Detail)
		}
	}
}
