package prodops_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeTaskBoard struct {
	created []domain.CreateBoardTaskRequest
}

func (f *fakeTaskBoard) CreateTask(_ context.Context, _ uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.created = append(f.created, req)
	return domain.BoardTask{ID: uuid.New()}, nil
}

func (f *fakeTaskBoard) AddComment(context.Context, uuid.UUID, uuid.UUID, domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	return domain.TaskComment{}, nil
}

// TestGoldenOpenRemediationTaskDescription pins the exact wording of the
// remediation task Ingest opens (openRemediationTask), byte-for-byte, for
// both incident policies, before that prose moves into
// catalog/system/prompts/briefs/**.
func TestGoldenOpenRemediationTaskDescription(t *testing.T) {
	t.Run("suggest policy tells the agent not to touch production code", func(t *testing.T) {
		incidents := newFakeIncidents()
		tasks := &fakeTaskBoard{}
		svc := prodops.NewService(prodops.Deps{Incidents: incidents, Tasks: tasks})

		repositoryID := uuid.New()
		incident, err := svc.Ingest(context.Background(), domain.IncidentInput{
			RepositoryID: repositoryID,
			Env:          domain.DeployEnvProd,
			Source:       domain.IncidentSourceProbe,
			Severity:     domain.IncidentSeverityCritical,
			Title:        "prod health check failing",
			Detail:       "something odd happened",
			Fingerprint:  "fp-golden-suggest",
		})
		require.NoError(t, err)
		require.Len(t, tasks.created, 1)

		req := tasks.created[0]
		require.Equal(t, "Incident: prod health check failing", req.Title)

		wantPrefix := "Production incident on **prod** (severity: critical, source: probe, occurrences: 1).\n\n" +
			"**Symptom:** prod health check failing\n" +
			"\n```\nsomething odd happened\n```\n" +
			"\n**First-pass hypothesis (unknown, confidence 25):**\n"
		wantSuffix := "\nIncident id: `" + incident.ID.String() + "` — call `get_incident` for the full payload and timeline.\n" +
			"\n**Policy: suggest.** Do NOT change production code. Diagnose only, then call " +
			"`propose_incident_remedy` with the concrete fix (commands, files, config) and move the task to human_uat for the decision.\n"
		require.Contains(t, req.Description, wantPrefix)
		require.True(t, len(req.Description) >= len(wantPrefix)+len(wantSuffix))
		require.Equal(t, wantSuffix, req.Description[len(req.Description)-len(wantSuffix):])
	})

	t.Run("auto_fix policy tells the agent to implement the fix", func(t *testing.T) {
		incidents := newFakeIncidents()
		tasks := &fakeTaskBoard{}
		repositoryID := uuid.New()
		repos := fakeRepoResolverWithPolicy{repositoryID: repositoryID, policy: domain.IncidentPolicyAutoFix}
		svc := prodops.NewService(prodops.Deps{Incidents: incidents, Tasks: tasks, Repos: repos})

		_, err := svc.Ingest(context.Background(), domain.IncidentInput{
			RepositoryID: repositoryID,
			Env:          domain.DeployEnvProd,
			Source:       domain.IncidentSourceProbe,
			Severity:     domain.IncidentSeverityCritical,
			Title:        "prod health check failing",
			Fingerprint:  "fp-golden-autofix",
		})
		require.NoError(t, err)
		require.Len(t, tasks.created, 1)

		wantSuffix := "\n**Policy: auto_fix.** Diagnose, then implement the fix and take it through the normal pipeline. " +
			"Record what you concluded with `propose_incident_remedy` before you start changing code.\n"
		req := tasks.created[0]
		require.Equal(t, wantSuffix, req.Description[len(req.Description)-len(wantSuffix):])
	})
}

// fakeRepoResolverWithPolicy is a RepositoryResolver whose repository always
// carries the given incident policy, so policy() resolves to it.
type fakeRepoResolverWithPolicy struct {
	repositoryID uuid.UUID
	policy       domain.IncidentPolicy
}

func (f fakeRepoResolverWithPolicy) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	return domain.Repository{ID: id, IncidentPolicy: f.policy}, nil
}
