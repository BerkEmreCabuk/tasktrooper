package ops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The transcripts that motivated the lenient half: agents pass "agent-server"
// — the repository NAME, which is what they see in their prompt — where the
// schema asks for a UUID, and the run they are in already knows which
// repository that is.
//
// The strict half is the other half of that same fact. If "web-frontend" means
// "the repository I am working on" to a reader, it means "some OTHER repository
// I could not name properly" to a writer, and guessing there records one
// repository's URL against another. So a present-but-unparseable value is only
// a fallback for readers; for writers it is a refusal.
func TestResolveRepositoryID(t *testing.T) {
	explicit := uuid.New()
	contextRepo := uuid.New()

	cases := []struct {
		name        string
		raw         string
		strict      bool
		contextRepo uuid.UUID
		want        uuid.UUID
		wantOK      bool
	}{
		{
			name:        "explicit uuid wins over the run's repository",
			raw:         explicit.String(),
			contextRepo: contextRepo,
			want:        explicit,
			wantOK:      true,
		},
		{
			name:        "explicit uuid with no run repository",
			raw:         "  " + explicit.String() + "  ",
			contextRepo: uuid.Nil,
			want:        explicit,
			wantOK:      true,
		},
		{
			name:        "a writer takes an explicit uuid the same way",
			raw:         explicit.String(),
			strict:      true,
			contextRepo: contextRepo,
			want:        explicit,
			wantOK:      true,
		},
		{
			name:        "omitted falls back to the run's repository",
			raw:         "",
			contextRepo: contextRepo,
			want:        contextRepo,
			wantOK:      true,
		},
		{
			name:        "a writer falls back for an omitted id too",
			raw:         "   ",
			strict:      true,
			contextRepo: contextRepo,
			want:        contextRepo,
			wantOK:      true,
		},
		{
			name:        "a repository name falls back to the run's repository",
			raw:         "agent-server",
			contextRepo: contextRepo,
			want:        contextRepo,
			wantOK:      true,
		},
		{
			name:        "a writer refuses a repository name rather than guessing",
			raw:         "web-frontend",
			strict:      true,
			contextRepo: contextRepo,
			want:        uuid.Nil,
			wantOK:      false,
		},
		{
			name:        "the nil uuid is not an answer either",
			raw:         uuid.Nil.String(),
			contextRepo: contextRepo,
			want:        contextRepo,
			wantOK:      true,
		},
		{
			name:        "a writer refuses the nil uuid outright",
			raw:         uuid.Nil.String(),
			strict:      true,
			contextRepo: contextRepo,
			want:        uuid.Nil,
			wantOK:      false,
		},
		{
			name:        "a name with nothing to fall back to is unresolvable",
			raw:         "agent-server",
			contextRepo: uuid.Nil,
			want:        uuid.Nil,
			wantOK:      false,
		},
		{
			name:        "omitted with nothing to fall back to is unresolvable",
			raw:         "",
			contextRepo: uuid.Nil,
			want:        uuid.Nil,
			wantOK:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := registry.ContextWithRepositoryID(context.Background(), tc.contextRepo)

			got, ok := resolveRepositoryID(ctx, tc.raw, tc.strict)

			if ok != tc.wantOK {
				t.Fatalf("resolved = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("repository = %s, want %s", got, tc.want)
			}
		})
	}
}

// fakeDeploys serves one repository's targets and records which repository it
// was asked about.
type fakeDeploys struct {
	repoID  uuid.UUID
	gotRepo uuid.UUID
}

func (f *fakeDeploys) Targets(_ context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error) {
	f.gotRepo = repositoryID
	if repositoryID != f.repoID {
		return nil, nil
	}
	return []domain.DeployTarget{{RepositoryID: repositoryID, Env: "stage", Provider: "gcp_cloud_run"}}, nil
}

func (f *fakeDeploys) Instructions(_ context.Context, _ uuid.UUID, _ string) (string, error) {
	return "deploy it", nil
}

func (f *fakeDeploys) RecordTargetURLs(_ context.Context, repositoryID uuid.UUID, env string, _ deploy.RecordTargetURLsInput) (domain.DeployTarget, error) {
	f.gotRepo = repositoryID
	return domain.DeployTarget{RepositoryID: repositoryID, Env: env}, nil
}

// The write must never land on a repository the caller did not name. A value
// like "web-frontend" is the agent naming a DIFFERENT repository badly, so
// recording this URL against the run's repository would put repo B's address on
// repo A's target with nothing in the transcript to show for it.
func TestUpdateDeployTargetRefusesAnUnparseableRepository(t *testing.T) {
	deploys := &fakeDeploys{repoID: uuid.New()}
	tool := &updateDeployTargetTool{kit: &ToolKit{Deploys: deploys}}
	ctx := registry.ContextWithRepositoryID(context.Background(), uuid.New())

	res := tool.Execute(ctx, `{"repository_id":"web-frontend","env":"stage","base_url":"https://b.example.com"}`)

	if !res.IsError {
		t.Fatalf("expected a refusal, got: %s", res.Content)
	}
	if res.Content != repositoryIDHelp {
		t.Fatalf("message = %q, want %q", res.Content, repositoryIDHelp)
	}
	if deploys.gotRepo != uuid.Nil {
		t.Fatalf("wrote to repository %s, want no write at all", deploys.gotRepo)
	}
}

// get_deploy_target used to require the argument, so a repository name was a
// dead end 12 times in production. It now reads the deploy target of the
// repository the run is working in — reading is lenient because a wrong read
// costs one tool call, not a wrong record.
func TestGetDeployTargetFallsBackToTheRunsRepository(t *testing.T) {
	repoID := uuid.New()
	deploys := &fakeDeploys{repoID: repoID}
	tool := &deployTargetTool{kit: &ToolKit{Deploys: deploys}}
	ctx := registry.ContextWithRepositoryID(context.Background(), repoID)

	for _, arguments := range []string{`{"env":"stage"}`, `{"repository_id":"agent-server","env":"stage"}`} {
		res := tool.Execute(ctx, arguments)

		if res.IsError {
			t.Fatalf("%s: unexpected tool error: %s", arguments, res.Content)
		}
		if deploys.gotRepo != repoID {
			t.Fatalf("%s: read repository %s, want the run's %s", arguments, deploys.gotRepo, repoID)
		}
		var payload struct {
			Target       domain.DeployTarget `json:"target"`
			Instructions string              `json:"instructions"`
		}
		if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
			t.Fatalf("%s: unmarshal result: %v", arguments, err)
		}
		if payload.Target.Env != "stage" || payload.Instructions == "" {
			t.Fatalf("%s: result does not carry the target: %s", arguments, res.Content)
		}
	}
}

// repository_id is no longer required, so the refusal has to say what to do
// instead of "invalid repository_id" — a message an agent can only read as
// "try another string".
func TestDeployToolsExplainAnUnresolvableRepository(t *testing.T) {
	kit := &ToolKit{Deploys: &fakeDeploys{repoID: uuid.New()}}
	cases := map[string]struct {
		res domain.ToolResult
	}{
		deployTargetToolName: {
			res: (&deployTargetTool{kit: kit}).Execute(context.Background(), `{"repository_id":"agent-server","env":"stage"}`),
		},
		updateDeployTargetToolName: {
			res: (&updateDeployTargetTool{kit: kit}).Execute(context.Background(), `{"env":"stage","base_url":"https://api.example.com"}`),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if !tc.res.IsError {
				t.Fatalf("expected a tool error, got: %s", tc.res.Content)
			}
			if tc.res.Content != repositoryIDHelp {
				t.Fatalf("message = %q, want %q", tc.res.Content, repositoryIDHelp)
			}
		})
	}
}

// update_deploy_target writes, so it must write to the repository the run is
// in — and its schema must no longer demand the id it can work out itself.
func TestUpdateDeployTargetUsesTheRunsRepository(t *testing.T) {
	repoID := uuid.New()
	deploys := &fakeDeploys{repoID: repoID}
	tool := &updateDeployTargetTool{kit: &ToolKit{Deploys: deploys}}
	ctx := registry.ContextWithRepositoryID(context.Background(), repoID)

	res := tool.Execute(ctx, `{"env":"stage","health_url":"https://api-stage.example.com/health"}`)

	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if deploys.gotRepo != repoID {
		t.Fatalf("wrote to repository %s, want the run's %s", deploys.gotRepo, repoID)
	}

	required, _ := tool.Definition().Function.Parameters["required"].([]string)
	if strings.Join(required, ",") != "env" {
		t.Fatalf("required = %v, want env alone", required)
	}
}

// list_incidents is the one ops tool that can answer with no repository at
// all, so omitting the argument has to keep meaning what it always meant:
// every repository. Scoping an omission to the run's repository would silently
// take away the cross-repo sweep an operator asks for by leaving it out. Only a
// value the model passed and we could not parse is read as "the one I am
// working on", and even that narrows nothing when the run names no repository.
func TestListIncidentsRepositoryScope(t *testing.T) {
	runRepo := uuid.New()
	otherRepo := uuid.New()

	cases := []struct {
		name        string
		arguments   string
		contextRepo uuid.UUID
		want        *uuid.UUID
	}{
		{
			name:        "omitted lists every repository",
			arguments:   `{}`,
			contextRepo: runRepo,
			want:        nil,
		},
		{
			name:        "blank lists every repository",
			arguments:   `{"repository_id":"  "}`,
			contextRepo: runRepo,
			want:        nil,
		},
		{
			name:        "an explicit uuid narrows to that repository",
			arguments:   `{"repository_id":"` + otherRepo.String() + `"}`,
			contextRepo: runRepo,
			want:        &otherRepo,
		},
		{
			name:        "a repository name narrows to the run's repository",
			arguments:   `{"repository_id":"agent-server"}`,
			contextRepo: runRepo,
			want:        &runRepo,
		},
		{
			name:        "a repository name with no run repository narrows nothing",
			arguments:   `{"repository_id":"agent-server"}`,
			contextRepo: uuid.Nil,
			want:        nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			incidents := &fakeIncidents{}
			tool := &listIncidentsTool{kit: &ToolKit{Incidents: incidents}}
			ctx := registry.ContextWithRepositoryID(context.Background(), tc.contextRepo)

			if res := tool.Execute(ctx, tc.arguments); res.IsError {
				t.Fatalf("unexpected tool error: %s", res.Content)
			}

			got := incidents.filter.RepositoryID
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("filter repository = %s, want every repository", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("filter repository = every repository, want %s", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("filter repository = %s, want %s", *got, *tc.want)
			}
		})
	}
}

// fakeIncidents records the filter it was asked for.
type fakeIncidents struct{ filter domain.IncidentFilter }

func (f *fakeIncidents) List(_ context.Context, filter domain.IncidentFilter) ([]domain.Incident, error) {
	f.filter = filter
	return nil, nil
}

func (f *fakeIncidents) Get(_ context.Context, _ uuid.UUID) (domain.Incident, error) {
	return domain.Incident{}, nil
}

func (f *fakeIncidents) ProposeRemedy(_ context.Context, _ uuid.UUID, _ domain.Remedy, _ string) (domain.Incident, error) {
	return domain.Incident{}, nil
}

func (f *fakeIncidents) Resolve(_ context.Context, _ uuid.UUID, _ string) (domain.Incident, error) {
	return domain.Incident{}, nil
}

// fakeLocalDeploys records what record_local_deploy handed the service.
type fakeLocalDeploys struct {
	got deployops.LocalRunInput
}

func (f *fakeLocalDeploys) RecordLocal(_ context.Context, in deployops.LocalRunInput) (domain.DeploymentRun, error) {
	f.got = in
	return domain.DeploymentRun{
		RepositoryID: in.RepositoryID, Env: in.Env, RunID: -1756400000123,
		Status: in.Status, Conclusion: in.Conclusion, HeadSHA: in.HeadSHA,
		TriggerSource: domain.TriggerSourceLocal,
	}, nil
}

// A writer, like update_deploy_target: a repository named but not as a UUID is
// refused rather than recorded against the run's own repository. Recording repo
// B's deploy on repo A's row would tell the console the wrong thing is live.
func TestRecordLocalDeployRefusesAnUnparseableRepository(t *testing.T) {
	local := &fakeLocalDeploys{}
	tool := &recordLocalDeployTool{kit: &ToolKit{LocalDeploys: local}}
	ctx := registry.ContextWithRepositoryID(context.Background(), uuid.New())

	res := tool.Execute(ctx, `{"repository_id":"web-frontend","env":"prod","status":"in_progress"}`)

	if !res.IsError || res.Content != repositoryIDHelp {
		t.Fatalf("res = %+v, want the repository_id refusal", res)
	}
	if local.got.RepositoryID != uuid.Nil {
		t.Fatalf("recorded against %s, want no record at all", local.got.RepositoryID)
	}
}

// The start call must hand back the run_id, because the finish call has
// nothing else to address the row it is completing.
func TestRecordLocalDeployEchoesTheRunID(t *testing.T) {
	repoID := uuid.New()
	local := &fakeLocalDeploys{}
	tool := &recordLocalDeployTool{kit: &ToolKit{LocalDeploys: local}}
	ctx := registry.ContextWithRepositoryID(context.Background(), repoID)

	res := tool.Execute(ctx, `{"env":"prod","status":"in_progress","command":"bash scripts/release-local.sh web","head_sha":"abc123"}`)

	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	var payload struct {
		RunID int64  `json:"run_id"`
		Env   string `json:"env"`
	}
	if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
		t.Fatalf("content is not JSON: %v (%s)", err, res.Content)
	}
	if payload.RunID >= 0 {
		t.Fatalf("run_id = %d, want the negative local id", payload.RunID)
	}
	if local.got.RepositoryID != repoID || local.got.Command != "bash scripts/release-local.sh web" {
		t.Fatalf("service got %+v", local.got)
	}
	// No human actor on an agent run: the row still has to say who deployed.
	if local.got.Actor != "agent" {
		t.Fatalf("actor = %q, want agent", local.got.Actor)
	}
}
