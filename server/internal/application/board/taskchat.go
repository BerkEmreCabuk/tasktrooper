package board

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TaskChatSessions is the session store as the opener uses it. Narrow on purpose:
// opening a task's chat is four store calls, and naming them here keeps the
// use case testable without a session service.
type TaskChatSessions interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Session, error)
	Create(ctx context.Context, title, model, workspaceDir string, projectID, agentID *uuid.UUID, expiresAt *time.Time) (domain.Session, error)
	BindTask(ctx context.Context, id, taskID uuid.UUID) error
	FindByTask(ctx context.Context, taskID uuid.UUID) (domain.Session, bool, error)
	AppendMessage(ctx context.Context, sessionID uuid.UUID, role domain.Role, content string, toolCalls []byte, clarification []byte) (domain.SessionMessage, error)
}

// TaskChatAgents resolves who the human is talking to.
type TaskChatAgents interface {
	GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error)
}

// TaskChatColumns answers "which agents own this column?", the same question the
// dispatcher asks when it decides who works a task. It is the fallback for an
// unassigned task: the agent that would pick the task up is the one that can
// actually talk about it.
type TaskChatColumns interface {
	AgentsForColumn(ctx context.Context, column, taskType string) ([]uuid.UUID, error)
}

// TaskChatOpenerDeps wires the opener.
type TaskChatOpenerDeps struct {
	Tasks    TaskPRTasks
	Sessions TaskChatSessions
	Agents   TaskChatAgents
	Columns  TaskChatColumns
	Repos    RootPathResolver
	Criteria port.AcceptanceCriterionStore
}

// TaskChatOpener opens (or reuses) the chat thread a human talks about one board
// task in.
//
// One thread per task, permanently. A fresh chat per click would split the
// conversation across threads where neither the human nor the agent could see
// what was already settled — the same failure the clarification thread exists to
// prevent, which is why an existing clarification thread is adopted here rather
// than being left beside a second chat about the same card.
type TaskChatOpener struct {
	tasks    TaskPRTasks
	sessions TaskChatSessions
	agents   TaskChatAgents
	columns  TaskChatColumns
	repos    RootPathResolver
	criteria port.AcceptanceCriterionStore
}

func NewTaskChatOpener(deps TaskChatOpenerDeps) *TaskChatOpener {
	return &TaskChatOpener{
		tasks:    deps.Tasks,
		sessions: deps.Sessions,
		agents:   deps.Agents,
		columns:  deps.Columns,
		repos:    deps.Repos,
		criteria: deps.Criteria,
	}
}

// Open returns the chat about taskID and the agent answering in it, creating the
// thread on first use.
//
// The task is read through its repository, which is the ownership check: a task id
// from another repository comes back as domain.ErrBoardTaskNotFound rather than
// opening a chat about a card the caller cannot see (same reasoning as
// Controller.CancelRun's run→task→repository chain).
func (o *TaskChatOpener) Open(ctx context.Context, repositoryID, taskID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if o.sessions == nil || o.tasks == nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("task chat is not available on this deployment")
	}
	task, err := o.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	if sess, ok, findErr := o.sessions.FindByTask(ctx, taskID); findErr != nil {
		return uuid.Nil, uuid.Nil, findErr
	} else if ok {
		return sess.ID, agentIDOf(sess), nil
	}

	// Adopt the thread the task already asks its questions in, if it still
	// exists. Everything the agent asked and the human answered is in there;
	// opening a second chat would hide it from both sides.
	if existing := task.ClarificationSessionID; existing != nil {
		if sess, getErr := o.sessions.Get(ctx, *existing); getErr == nil {
			if bindErr := o.sessions.BindTask(ctx, sess.ID, taskID); bindErr != nil {
				return uuid.Nil, uuid.Nil, bindErr
			}
			return sess.ID, agentIDOf(sess), nil
		}
		log.Info().Str("task_id", taskID.String()).Str("session_id", existing.String()).
			Msg("task chat: recorded clarification thread is gone, opening a new one")
	}

	agentRec, hasAgent := o.resolveAgent(ctx, task)
	var agentID *uuid.UUID
	model := ""
	if hasAgent {
		id := agentRec.ID
		agentID = &id
		model = agentRec.Model
	}
	// The workspace recorded at creation is the repository's shared working copy,
	// exactly like a repo-scoped chat. It is corrected on the first turn: a
	// task-bound session runs in the task's own branch checkout (see the session
	// service's task workspace resolution), which cannot be prepared here without
	// dragging git into the request path.
	rootPath := ""
	if o.repos != nil {
		if resolved, rootErr := o.repos.ResolveRootPath(ctx, repositoryID); rootErr == nil {
			rootPath = resolved
		}
	}
	title := domain.TruncateHead(strings.TrimSpace(task.Key+" "+task.Title), 120)
	sess, err := o.sessions.Create(ctx, title, model, rootPath, &repositoryID, agentID, nil)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := o.sessions.BindTask(ctx, sess.ID, taskID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	o.seedOpeningMessage(ctx, sess.ID, task)
	return sess.ID, agentIDOf(sess), nil
}

// resolveAgent picks who answers in the task's chat: its assignee when it has one
// and that agent is still usable, otherwise an agent subscribed to the column the
// task is in — the one that would pick the task up next, which is the only
// defensible default. A board with no subscriptions at all yields no agent, and
// the chat runs agentless (the session service handles that: no system prompt, no
// agent policy) rather than failing to open.
func (o *TaskChatOpener) resolveAgent(ctx context.Context, task domain.BoardTask) (domain.Agent, bool) {
	if o.agents == nil {
		return domain.Agent{}, false
	}
	if task.AssigneeAgentID != nil {
		if agentRec, err := o.agents.GetAgent(ctx, *task.AssigneeAgentID); err == nil && agentRec.Enabled {
			return agentRec, true
		}
	}
	if o.columns == nil {
		return domain.Agent{}, false
	}
	ids, err := o.columns.AgentsForColumn(ctx, string(task.Column), string(task.TaskType))
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("task chat: column agent lookup failed")
		return domain.Agent{}, false
	}
	for _, id := range ids {
		if agentRec, err := o.agents.GetAgent(ctx, id); err == nil && agentRec.Enabled {
			return agentRec, true
		}
	}
	return domain.Agent{}, false
}

// seedOpeningMessage puts the task (and its PR, when one exists) into the thread
// as the assistant's first turn, so the human opens a chat that already knows what
// it is about instead of an empty box.
//
// Best-effort: a thread that failed to get its opening line still works — the
// per-turn task context message carries the same facts to the model.
func (o *TaskChatOpener) seedOpeningMessage(ctx context.Context, sessionID uuid.UUID, task domain.BoardTask) {
	var criteria []domain.AcceptanceCriterion
	if o.criteria != nil {
		if items, err := o.criteria.ListByTask(ctx, task.ID); err == nil {
			criteria = items
		}
	}
	content := prompt.TaskChatOpeningMessage(task, criteria)
	if content == "" {
		return
	}
	if _, err := o.sessions.AppendMessage(ctx, sessionID, domain.RoleAssistant, content, nil, nil); err != nil {
		log.Warn().Err(err).Str("session_id", sessionID.String()).Msg("task chat: opening message append failed")
	}
}

func agentIDOf(sess domain.Session) uuid.UUID {
	if sess.AgentID == nil {
		return uuid.Nil
	}
	return *sess.AgentID
}
