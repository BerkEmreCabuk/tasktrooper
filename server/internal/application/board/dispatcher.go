package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type DispatchInput struct {
	RepositoryID     uuid.UUID
	Task             domain.BoardTask
	EventType        domain.BoardEventType
	Payload          map[string]interface{}
	SkipPipelineGate bool
}

type RunEnqueuer interface {
	Enqueue(job RunJob)
}

var _ QADispatcher = (*Dispatcher)(nil)

type Dispatcher struct {
	board        port.BoardConfigStore
	events       port.BoardEventStore
	runs         port.TaskAgentRunStore
	runner       RunEnqueuer
	enabled      bool
	pipelineGate bool
	notifier     TaskNotifier
	spans        port.TaskColumnSpanStore
	workOrder    *WorkOrder
	gatePolicy   PipelineGatePolicy
	reviewLoop   *ReviewLoopGuard
	// workflows/roles back every mid-lifecycle-column/task-type decision in
	// this file (see workflowFor). roles is not read directly here yet.
	workflows port.WorkflowReader
	roles     port.RoleResolver
}

// workflowFor resolves the workflow a dispatch decision for this task type
// should read. False means "treat every workflow-gated behaviour as closed":
// either the reader was never wired, or Workflow() itself errored (empty/not-
// yet-loaded snapshot) — an unreadable workflow must never read the same as
// an absent behaviour, so callers suspend rather than guess.
func (d *Dispatcher) workflowFor(ctx context.Context, taskType domain.TaskType) (domain.Workflow, bool) {
	if d.workflows == nil {
		return domain.Workflow{}, false
	}
	wf, err := d.workflows.Workflow(ctx, taskType)
	if err != nil {
		log.Warn().Err(err).Str("task_type", string(taskType)).
			Msg("dispatcher: workflow lookup failed, failing closed")
		return domain.Workflow{}, false
	}
	return wf, true
}

func (d *Dispatcher) SetWorkflows(w port.WorkflowReader)  { d.workflows = w }
func (d *Dispatcher) SetRoleResolver(r port.RoleResolver) { d.roles = r }

type PipelineGatePolicy interface {
	RequirePipelineForReview(ctx context.Context, repositoryID uuid.UUID) bool
}

func (d *Dispatcher) SetPipelineGatePolicy(p PipelineGatePolicy) {
	d.gatePolicy = p
}

func NewDispatcher(board port.BoardConfigStore, events port.BoardEventStore, runs port.TaskAgentRunStore, runner RunEnqueuer, enabled bool) *Dispatcher {
	return &Dispatcher{
		board:   board,
		events:  events,
		runs:    runs,
		runner:  runner,
		enabled: enabled,
	}
}

func (d *Dispatcher) SetPipelineGate(enabled bool) {
	d.pipelineGate = enabled
}

type TaskNotifier interface {
	TaskMoved(ctx context.Context, task domain.BoardTask)
	TaskResumed(ctx context.Context, task domain.BoardTask, resource string)
}

func (d *Dispatcher) SetNotifier(n TaskNotifier) {
	d.notifier = n
}

func (d *Dispatcher) SetSpans(spans port.TaskColumnSpanStore) {
	d.spans = spans
}

func (d *Dispatcher) SetWorkOrder(w *WorkOrder) {
	d.workOrder = w
}

func (d *Dispatcher) SetReviewLoopGuard(g *ReviewLoopGuard) {
	d.reviewLoop = g
}

func (d *Dispatcher) Dispatch(ctx context.Context, input DispatchInput) error {
	if !d.enabled || d.events == nil || d.runner == nil {
		return nil
	}
	payload := input.Payload
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payload["column"] = string(input.Task.Column)
	if input.Task.AssigneeAgentID != nil {
		payload["assignee_agent_id"] = input.Task.AssigneeAgentID.String()
	}

	wf, wfOK := d.workflowFor(ctx, input.Task.TaskType)
	gateDefers, gateSkipReason := d.pipelineGateDecision(ctx, wf, wfOK, input)
	if gateSkipReason != "" {
		payload[domain.EventPayloadPipelineGate] = gateSkipReason
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	var actorUserID *string
	if uid := registry.ActorUserIDFromContext(ctx); uid != "" {
		actorUserID = &uid
	}
	event, err := d.events.Create(ctx, domain.BoardEvent{
		RepositoryID: input.RepositoryID,
		TaskID:       input.Task.ID,
		EventType:    input.EventType,
		Payload:      raw,
		ActorUserID:  actorUserID,
	})
	if err != nil {
		return err
	}
	if d.spans != nil &&
		(input.EventType == domain.BoardEventTaskMoved || input.EventType == domain.BoardEventTaskCreated) {
		if err := d.spans.RecordMove(ctx, input.RepositoryID, input.Task.ID, string(input.Task.Column), event.CreatedAt); err != nil {
			// A missing span costs a KPI data point, never the move itself.
			log.Warn().Err(err).Str("task_id", input.Task.ID.String()).Msg("record column span failed")
		}
	}
	if d.notifier != nil && input.EventType == domain.BoardEventTaskMoved {

		if resource, resumed := parkResume(payload); resumed {
			d.notifier.TaskResumed(ctx, input.Task, resource)
		} else {
			d.notifier.TaskMoved(ctx, input.Task)
		}
	}

	mergeWake := doneMergeWake(wf, wfOK, input)
	watchWake := deployWatchWake(wf, wfOK, input)
	if isDispatchSuspendedTask(wf, wfOK, input.Task) && !mergeWake && !watchWake {
		return nil
	}

	if d.workOrder != nil && workOrderGateApplies(wf, wfOK, input) {
		blockers, berr := d.workOrder.Blockers(ctx, input.Task.ID)
		if berr != nil {

			log.Warn().Err(berr).Str("task_id", input.Task.ID.String()).
				Msg("dispatch: work-order check failed, not starting the task")
			return fmt.Errorf("work order check: %w", berr)
		}
		if len(blockers) > 0 {
			if perr := d.workOrder.Park(ctx, input.RepositoryID, input.Task, blockers); perr != nil {
				return fmt.Errorf("park on work order: %w", perr)
			}
			return nil
		}
	}

	if d.reviewLoop != nil && reviewLoopGateApplies(input) &&
		d.reviewLoop.Hold(ctx, input.RepositoryID, input.Task, event.ID) {
		return nil
	}

	if gateDefers {
		return nil
	}
	var agentIDs []uuid.UUID
	if watchWake {

		agentIDs, err = d.board.AgentsForColumn(ctx, string(input.Task.Column), string(input.Task.TaskType))
	} else if mergeWake {

		agentIDs, err = d.board.AgentsForColumn(ctx, string(domain.TaskColumnDone), string(input.Task.TaskType))
	} else {
		agentIDs, err = d.resolveAgents(ctx, wf, wfOK, input)
	}
	if err != nil {
		return err
	}
	actorID := actorAgentIDFromPayload(payload)
	for _, agentID := range agentIDs {
		if actorID != uuid.Nil && agentID == actorID {
			continue
		}

		pending, err := d.alreadyWorkingOn(ctx, input.EventType, input.Task.ID, agentID, mergeWake || watchWake)
		if err != nil {

			log.Warn().Err(err).Str("task_id", input.Task.ID.String()).
				Str("agent_id", agentID.String()).Msg("dispatch: pending-run check failed")
			return fmt.Errorf("pending run check: %w", err)
		}
		if pending {
			continue
		}
		run, err := d.runs.Create(ctx, domain.TaskAgentRun{
			TaskID:       input.Task.ID,
			AgentID:      agentID,
			BoardEventID: event.ID,
			Status:       domain.TaskAgentRunStatusPending,
		})
		if err != nil {
			return err
		}

		if run.BoardEventID != event.ID {
			log.Info().Str("task_id", input.Task.ID.String()).
				Str("agent_id", agentID.String()).Str("run_id", run.ID.String()).
				Msg("dispatch: agent already has a queued run for this task, skipping duplicate")
			continue
		}
		if d.spans != nil {

			if err := d.spans.AttachAgent(ctx, input.Task.ID, agentID); err != nil {
				log.Warn().Err(err).Str("task_id", input.Task.ID.String()).
					Str("agent_id", agentID.String()).Msg("attach span agent failed")
			}
		}

		d.runner.Enqueue(RunJob{
			Run:               run,
			Event:             event,
			Task:              input.Task,
			RepositoryID:      input.RepositoryID,
			ColumnInstruction: d.columnInstructionForAgent(ctx, agentID, input.Task.Column),
		})
	}
	return nil
}

func (d *Dispatcher) alreadyWorkingOn(ctx context.Context, eventType domain.BoardEventType, taskID, agentID uuid.UUID, terminalWake bool) (bool, error) {
	if terminalWake {
		return d.runs.HasLiveForTask(ctx, taskID, agentID)
	}
	switch eventType {
	case domain.BoardEventTaskCommented, domain.BoardEventTaskAssigned:
		return d.runs.HasLiveForTask(ctx, taskID, agentID)
	default:
		return d.runs.HasPendingForTask(ctx, taskID, agentID)
	}
}

// isDispatchSuspendedTask reports whether a task must never be dispatched
// from its own event. blocked/backlog/released are system columns every task
// type parks in the same way, so they stay literal; done is the one column
// whose suspension used to turn on the task's TYPE (an analiz task keeps
// working in done, decomposing itself, while task/bug/technical stop there) —
// that is now the presence of dispatch_suspended on the type's own done stage,
// read off the type's workflow instead of compared against TaskTypeAnaliz.
// !wfOK fails closed: an unreadable workflow suspends rather than guesses.
func isDispatchSuspendedTask(wf domain.Workflow, wfOK bool, task domain.BoardTask) bool {
	switch task.Column {
	case domain.TaskColumnBlocked, domain.TaskColumnBacklog, domain.TaskColumnReleased:
		return true
	case domain.TaskColumnDone:
		return !wfOK || wf.Has(domain.TaskColumnDone, domain.BehaviourDispatchSuspended)
	default:
		return false
	}
}

func doneMergeWake(wf domain.Workflow, wfOK bool, input DispatchInput) bool {
	task := input.Task
	if task.Column != domain.TaskColumnDone {
		return false
	}
	if !wfOK || !wf.Has(domain.TaskColumnDone, domain.BehaviourMergePROnEnter) {
		return false
	}
	switch input.EventType {
	case domain.BoardEventTaskMoved, domain.BoardEventTaskCreated:
	default:
		return false
	}
	if strings.TrimSpace(task.PRURL) == "" {
		return false
	}
	return strings.TrimSpace(task.MergeCommitSHA) == ""
}

func deployWatchWake(wf domain.Workflow, wfOK bool, input DispatchInput) bool {
	task := input.Task
	if task.Column != domain.TaskColumnDone && task.Column != domain.TaskColumnReleased {
		return false
	}
	if !wfOK || !wf.Has(task.Column, domain.BehaviourWatchDeployOnResume) {
		return false
	}
	if input.EventType != domain.BoardEventTaskMoved {
		return false
	}
	if input.Payload == nil {
		return false
	}
	resource, _ := input.Payload[domain.EventPayloadResumedResource].(string)
	return resource == domain.ResourceDeployWatch
}

func parkResume(payload map[string]interface{}) (string, bool) {
	if payload == nil {
		return "", false
	}
	if resumed, _ := payload["resumed"].(string); resumed == "" {
		return "", false
	}
	resource, _ := payload["resource"].(string)
	if resource == "" {
		return "", false
	}
	return resource, true
}

func actorAgentIDFromPayload(payload map[string]interface{}) uuid.UUID {
	raw, ok := payload["actor_agent_id"].(string)
	if !ok {
		if authorType, _ := payload["author_type"].(string); authorType == "agent" {
			raw, ok = payload["author_id"].(string)
		}
	}
	if !ok {
		return uuid.Nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// isHandoffGateColumn reports whether this column's move wakes every
// subscriber instead of only the task's assignee — route_to_subscribers, on
// every column that behaviour carries (the same set for every type, since
// it is a type-uniform "all types" behaviour, not a per-type one). !wfOK
// fails to the wider net (true): an unreadable workflow must not silently
// narrow a handoff gate down to a single, possibly-nil assignee.
func isHandoffGateColumn(wf domain.Workflow, wfOK bool, col domain.TaskColumn) bool {
	if !wfOK {
		return true
	}
	return wf.Has(col, domain.BehaviourRouteToSubscribers)
}

// columnInstructionForAgent is the per-agent per-column prompt the run should
// carry, keyed by the column the task was in when this run was dispatched.
// It is a config append, never an override, and a missing row is "no text".
func (d *Dispatcher) columnInstructionForAgent(ctx context.Context, agentID uuid.UUID, column domain.TaskColumn) string {
	instructions, err := d.board.ListAgentColumnInstructions(ctx, agentID)
	if err != nil {
		log.Warn().Err(err).Str("agent_id", agentID.String()).Str("column", string(column)).
			Msg("dispatch: column instruction lookup failed")
		return ""
	}
	for _, ins := range instructions {
		if ins.ColumnSlug == string(column) {
			return ins.Instruction
		}
	}
	return ""
}

func (d *Dispatcher) resolveAgents(ctx context.Context, wf domain.Workflow, wfOK bool, input DispatchInput) ([]uuid.UUID, error) {
	task := input.Task
	taskType := string(task.TaskType)

	switch input.EventType {
	case domain.BoardEventTaskCommented:
		if isHandoffGateColumn(wf, wfOK, task.Column) {
			return d.board.AgentsForColumn(ctx, string(task.Column), taskType)
		}
		if task.AssigneeAgentID != nil {
			return []uuid.UUID{*task.AssigneeAgentID}, nil
		}
		return d.board.AgentsForColumn(ctx, string(task.Column), taskType)
	case domain.BoardEventTaskAssigned:
		if task.AssigneeAgentID == nil {
			return nil, nil
		}
		return []uuid.UUID{*task.AssigneeAgentID}, nil
	case domain.BoardEventTaskCreated, domain.BoardEventTaskMoved:
		if isHandoffGateColumn(wf, wfOK, task.Column) {
			return d.board.AgentsForColumn(ctx, string(task.Column), taskType)
		}
		if task.AssigneeAgentID != nil {
			return []uuid.UUID{*task.AssigneeAgentID}, nil
		}
		return d.board.AgentsForColumn(ctx, string(task.Column), taskType)
	default:
		return nil, nil
	}
}

// pipelineGateDecision answers whether a move into code_review defers
// dispatch for the CI gate. wait_for_ci is a type-uniform "all types"
// behaviour (the pipeline gate applies the same way whatever the task type),
// so !wfOK fails closed to "defer" — an unreadable workflow must not open a
// gate it cannot actually evaluate.
func (d *Dispatcher) pipelineGateDecision(ctx context.Context, wf domain.Workflow, wfOK bool, input DispatchInput) (defers bool, skipReason string) {
	if !d.pipelineGate || input.SkipPipelineGate ||
		input.EventType != domain.BoardEventTaskMoved {
		return false, ""
	}
	if !wfOK {
		return true, ""
	}
	if !wf.Has(input.Task.Column, domain.BehaviourWaitForCI) {
		return false, ""
	}
	if d.gatePolicy == nil || d.gatePolicy.RequirePipelineForReview(ctx, input.RepositoryID) {
		return true, ""
	}
	log.Info().Str("task_id", input.Task.ID.String()).
		Str("repository_id", input.RepositoryID.String()).
		Msg("code review gate skipped: this repository does not gate review on CI")
	return false, domain.PipelineGateReasonDisabled
}

func (d *Dispatcher) DispatchQA(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, pipelineID uuid.UUID, gateReason string) error {
	payload := map[string]interface{}{
		"pipeline":                "success",
		"pipeline_id":             pipelineID.String(),
		domain.EventPayloadActor:  domain.EventActorSystem,
		domain.EventPayloadReason: domain.MoveReasonPipelinePassed,
	}
	if domain.PipelineGateReasonOpen(gateReason) {
		payload["pipeline"] = "gate_opened"
		payload[domain.EventPayloadPipelineGate] = gateReason
		payload[domain.EventPayloadReason] = domain.MoveReasonPipelineGateOpened
	}
	return d.Dispatch(ctx, DispatchInput{
		RepositoryID:     repositoryID,
		Task:             task,
		EventType:        domain.BoardEventTaskMoved,
		Payload:          payload,
		SkipPipelineGate: true,
	})
}
