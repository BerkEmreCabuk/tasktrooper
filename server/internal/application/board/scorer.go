package board

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// ScoreApplier is the slice of AgentPerformanceStore the tracker writes through.
type ScoreApplier interface {
	ApplyDelta(ctx context.Context, input domain.ApplyScoreInput) (domain.AgentPerformanceScore, error)
}

// SpanOwnerLookup resolves which agent worked each column of a task.
type SpanOwnerLookup interface {
	OwnersForTask(ctx context.Context, taskID uuid.UUID) (map[string]uuid.UUID, error)
}

type ScoreTracker struct {
	scores         ScoreApplier
	spans          SpanOwnerLookup
	OnScoreUpdated func(agentID string, score float64, delta float64)
}

func NewScoreTracker(scores ScoreApplier) *ScoreTracker {
	return &ScoreTracker{scores: scores}
}

// SetSpans attaches the span ledger used to decide who a defect belongs to.
// Without it the tracker falls back to the task's assignee, which is the wrong
// agent as soon as a task has changed hands.
func (st *ScoreTracker) SetSpans(spans SpanOwnerLookup) {
	st.spans = spans
}

// blameColumns lists, per rejecting column, the columns whose owners are
// accountable for the defect. A defect that reached the human escaped every
// gate before it, so all of them are charged; one the PM caught only reaches
// the dev and QA.
var blameColumns = map[domain.TaskColumn][]string{
	domain.TaskColumnHumanUAT:   {"in_progress", "in_qa", "pm_uat"},
	domain.TaskColumnPMUAT:      {"in_progress", "in_qa"},
	domain.TaskColumnCodeReview: {"in_progress"},
	domain.TaskColumnReadyForQA: {"in_progress"},
	domain.TaskColumnInQA:       {"in_progress"},
}

type rejectionRule struct {
	evType string
	delta  float64
	reason string
}

var rejectionRules = map[domain.TaskColumn]rejectionRule{
	domain.TaskColumnHumanUAT:   {domain.ScoreEventHumanUATFailed, domain.ScoreDeltaHumanUATFailed, "Human UAT failed"},
	domain.TaskColumnPMUAT:      {domain.ScoreEventPMUATFailed, domain.ScoreDeltaPMUATFailed, "PM UAT failed"},
	domain.TaskColumnCodeReview: {domain.ScoreEventRevisionRequested, domain.ScoreDeltaRevisionRequested, "Architect returned for revision"},
	domain.TaskColumnReadyForQA: {domain.ScoreEventRevisionRequested, domain.ScoreDeltaRevisionRequested, "QA returned for revision"},
	domain.TaskColumnInQA:       {domain.ScoreEventRevisionRequested, domain.ScoreDeltaRevisionRequested, "QA returned for revision"},
}

func (st *ScoreTracker) OnColumnTransition(ctx context.Context, task domain.BoardTask, from, to domain.TaskColumn) {
	if st == nil || st.scores == nil {
		return
	}

	if to == domain.TaskColumnNeedRevision {
		rule, ok := rejectionRules[from]
		if !ok {
			return
		}
		for _, agentID := range st.blamed(ctx, task, blameColumns[from]) {
			st.apply(ctx, task, agentID, rule.evType, rule.delta, rule.reason)
		}
		return
	}

	// Completion credit follows the assignee: it is the task's outcome, not one
	// stage's, and every contributor already carries their own penalties.
	if to == domain.TaskColumnDone || to == domain.TaskColumnReleased {
		if task.AssigneeAgentID == nil {
			return
		}
		evType, delta, reason := domain.ScoreEventTaskCompleted, domain.ScoreDeltaTaskCompleted, "Task completed"
		if to == domain.TaskColumnReleased {
			evType, delta, reason = domain.ScoreEventTaskReleased, domain.ScoreDeltaTaskReleased, "Task released"
		}
		st.apply(ctx, task, *task.AssigneeAgentID, evType, delta, reason)
	}
}

// ApplyReviewEscape charges a reviewing agent for approving something a human
// then rejected at the same gate.
func (st *ScoreTracker) ApplyReviewEscape(ctx context.Context, task domain.BoardTask, agentID uuid.UUID) {
	if st == nil || st.scores == nil {
		return
	}
	st.apply(ctx, task, agentID, domain.ScoreEventReviewEscape, domain.ScoreDeltaReviewEscape,
		"Human rejected a change the reviewer approved")
}

// blamed resolves the accountable agents, de-duplicated: one agent that both
// developed and tested a task is charged once, not twice.
func (st *ScoreTracker) blamed(ctx context.Context, task domain.BoardTask, columns []string) []uuid.UUID {
	if st.spans == nil {
		return assigneeOnly(task)
	}
	owners, err := st.spans.OwnersForTask(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("span owners lookup failed")
		return assigneeOnly(task)
	}
	seen := make(map[uuid.UUID]bool, len(columns))
	out := make([]uuid.UUID, 0, len(columns))
	for _, col := range columns {
		agentID, ok := owners[col]
		if !ok || seen[agentID] {
			continue
		}
		seen[agentID] = true
		out = append(out, agentID)
	}
	return out
}

func assigneeOnly(task domain.BoardTask) []uuid.UUID {
	if task.AssigneeAgentID == nil {
		return nil
	}
	return []uuid.UUID{*task.AssigneeAgentID}
}

func (st *ScoreTracker) apply(ctx context.Context, task domain.BoardTask, agentID uuid.UUID, evType string, delta float64, reason string) {
	taskIDPtr := &task.ID
	updated, err := st.scores.ApplyDelta(ctx, domain.ApplyScoreInput{
		AgentID:   agentID,
		TaskID:    taskIDPtr,
		EventType: evType,
		Delta:     delta,
		Reason:    fmt.Sprintf("%s: %s", reason, task.Title),
	})
	if err != nil {
		log.Warn().Err(err).Str("agent_id", agentID.String()).Msg("score delta failed")
		return
	}
	if st.OnScoreUpdated != nil {
		st.OnScoreUpdated(agentID.String(), updated.Score, delta)
	}
}
