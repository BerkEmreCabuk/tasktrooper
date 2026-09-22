package board

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type settlingCriteriaUpdater struct {
	fakeTaskUpdater
	criteria    []domain.AcceptanceCriterion
	settleAfter int
	canceled    bool
	reads       int
}

func (u *settlingCriteriaUpdater) ListTaskCriteria(context.Context, uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	u.reads++
	if u.settleAfter > 0 && u.reads > u.settleAfter {
		out := make([]domain.AcceptanceCriterion, len(u.criteria))
		copy(out, u.criteria)
		for i := range out {
			if u.canceled {
				out[i].Canceled = true
				out[i].CancelReason = "superseded by T-9"
				continue
			}
			out[i].Completed = true
		}
		return out, nil
	}
	return u.criteria, nil
}

func sweepJob() RunJob {
	return RunJob{
		Task:         domain.BoardTask{ID: uuid.New(), Key: "tt-9", Title: "t", Column: domain.TaskColumnInProgress},
		RepositoryID: uuid.New(),
	}
}

func TestSweepOpenCriteriaKeepsAskingUntilTheCriteriaAreSettled(t *testing.T) {
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "ok"}},
	}
	router, _ := hostRouter(ex)
	updater := &settlingCriteriaUpdater{
		criteria:    []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "the export includes archived rows"}},
		settleAfter: 2,
	}
	r := NewRunner(RunnerDeps{AgentLoop: router})
	r.SetTaskUpdater(updater)
	r.SetWorkflows(workflowtest.Default().Reader())

	ctx := registry.ContextWithWorkspaceDir(context.Background(), t.TempDir())
	r.sweepOpenCriteria(ctx, sweepJob(), claudeCodeAgent(),
		[]domain.Message{{Role: domain.RoleUser, Content: "do the work"}},
		domain.AgentResponse{Message: domain.Message{Content: "implemented"}}, "opus", domain.ToolPolicy{})

	require.Equal(t, 2, ex.callCount(), "the sweep must re-ask while a criterion is still open")
	require.Empty(t, updater.comments, "a sweep that settled needs no explanatory comment")
}

func TestSweepOpenCriteriaStopsOnACancelledCriterion(t *testing.T) {
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "cancelled"}},
	}
	router, _ := hostRouter(ex)
	updater := &settlingCriteriaUpdater{
		criteria:    []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "single sign-on is supported"}},
		settleAfter: 1,
		canceled:    true,
	}
	r := NewRunner(RunnerDeps{AgentLoop: router})
	r.SetTaskUpdater(updater)
	r.SetWorkflows(workflowtest.Default().Reader())

	ctx := registry.ContextWithWorkspaceDir(context.Background(), t.TempDir())
	r.sweepOpenCriteria(ctx, sweepJob(), claudeCodeAgent(),
		[]domain.Message{{Role: domain.RoleUser, Content: "do the work"}},
		domain.AgentResponse{Message: domain.Message{Content: "implemented"}}, "opus", domain.ToolPolicy{})

	require.Equal(t, 1, ex.callCount(), "a cancelled criterion is settled; the loop must not ask again")
}

func TestSweepOpenCriteriaStopsAtTheRoundCapAndSaysSo(t *testing.T) {
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "still cannot"}},
	}
	router, _ := hostRouter(ex)
	updater := &settlingCriteriaUpdater{
		criteria: []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "the nightly job emails the report"}},
	}
	r := NewRunner(RunnerDeps{AgentLoop: router})
	r.SetTaskUpdater(updater)
	r.SetWorkflows(workflowtest.Default().Reader())

	ctx := registry.ContextWithWorkspaceDir(context.Background(), t.TempDir())
	r.sweepOpenCriteria(ctx, sweepJob(), claudeCodeAgent(),
		[]domain.Message{{Role: domain.RoleUser, Content: "do the work"}},
		domain.AgentResponse{Message: domain.Message{Content: "implemented"}}, "opus", domain.ToolPolicy{})

	require.Equal(t, criteriaSweepRounds, ex.callCount())
	require.Len(t, updater.comments, 1)
	require.Contains(t, updater.comments[0].Content, "the nightly job emails the report")
	require.Contains(t, updater.comments[0].Content, "unsettled")
}

func TestCriteriaSweepPromptEscalates(t *testing.T) {
	open := []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "criterion"}}

	first := criteriaSweepPrompt(open, 1)
	require.Contains(t, first, "cancel_criterion")
	require.Contains(t, first, "DO THE WORK NOW")

	second := criteriaSweepPrompt(open, 2)
	require.Contains(t, second, "STILL open")
	require.Contains(t, second, "Do not reply with a summary of what you would do; make the change.")
	require.True(t, strings.Contains(second, open[0].ID.String()), "every round must carry the ids it is asking about")
}
