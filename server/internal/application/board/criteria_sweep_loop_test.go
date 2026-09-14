package board

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// settlingCriteriaUpdater answers the sweep the way the database would: the
// criteria list it returns changes as the fake agent "settles" them, so the
// loop's own termination condition is what is under test rather than a counter.
type settlingCriteriaUpdater struct {
	fakeTaskUpdater
	criteria []domain.AcceptanceCriterion
	// settleAfter is the read on which every criterion becomes settled; 0 means
	// never.
	settleAfter int
	// canceled settles by cancellation instead of by a tick, which is the case
	// that must ALSO end the loop — a dropped criterion carries its reason and
	// owes nothing more.
	canceled bool
	reads    int
}

func (u *settlingCriteriaUpdater) ListTaskCriteria(context.Context, uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	u.reads++
	// The first read is the pre-loop one; a round's read is every read after it.
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

// The completion check is a LOOP, not a single question. A run that answers the
// first round with a comment ("I did not get to this") used to end there, with
// the work never done and the card parked in front of the criteria gate.
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

	ctx := registry.ContextWithWorkspaceDir(context.Background(), t.TempDir())
	r.sweepOpenCriteria(ctx, sweepJob(), claudeCodeAgent(),
		[]domain.Message{{Role: domain.RoleUser, Content: "do the work"}},
		domain.AgentResponse{Message: domain.Message{Content: "implemented"}}, "opus", domain.ToolPolicy{})

	require.Equal(t, 2, ex.callCount(), "the sweep must re-ask while a criterion is still open")
	require.Empty(t, updater.comments, "a sweep that settled needs no explanatory comment")
}

// Cancelling is a real answer: it settles the criterion, so the loop stops
// instead of driving the agent at a decision it already made and explained.
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

	ctx := registry.ContextWithWorkspaceDir(context.Background(), t.TempDir())
	r.sweepOpenCriteria(ctx, sweepJob(), claudeCodeAgent(),
		[]domain.Message{{Role: domain.RoleUser, Content: "do the work"}},
		domain.AgentResponse{Message: domain.Message{Content: "implemented"}}, "opus", domain.ToolPolicy{})

	require.Equal(t, 1, ex.callCount(), "a cancelled criterion is settled; the loop must not ask again")
}

// The loop is bounded, and what it leaves behind when it gives up is a written
// explanation on the card — not silence in front of a gate nobody can see.
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

	ctx := registry.ContextWithWorkspaceDir(context.Background(), t.TempDir())
	r.sweepOpenCriteria(ctx, sweepJob(), claudeCodeAgent(),
		[]domain.Message{{Role: domain.RoleUser, Content: "do the work"}},
		domain.AgentResponse{Message: domain.Message{Content: "implemented"}}, "opus", domain.ToolPolicy{})

	require.Equal(t, criteriaSweepRounds, ex.callCount())
	require.Len(t, updater.comments, 1)
	require.Contains(t, updater.comments[0].Content, "the nightly job emails the report")
	require.Contains(t, updater.comments[0].Content, "unsettled")
}

// Round one asks; every round after it demands the work. An escalation that
// only repeated itself would let a run answer "noted" three times.
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
