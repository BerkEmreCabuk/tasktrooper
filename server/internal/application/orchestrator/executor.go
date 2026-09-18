package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"golang.org/x/sync/errgroup"
)

const maxTaskRetries = 2

// persistTimeout bounds a status write that outlives its run's context.
const persistTimeout = 10 * time.Second

// persistCtx detaches a terminal status write from the run being cancelled.
// A subtask's last write happens when the pod is draining or the client hung
// up; on the run's own context those writes fail and the row stays "running"
// with nothing left to move it.
func persistCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

const dependencyTruncateNote = "\n[truncated — use run_terminal to read full output]"

// SessionActionReader reads the ledger of board records this conversation has
// already produced.
type SessionActionReader interface {
	ListActions(ctx context.Context, sessionID uuid.UUID) ([]domain.SessionAction, error)
}

type Executor struct {
	// agentLoop is the router in production, so a subtask assigned to an agent
	// on a host-executed provider runs on that host's CLI instead of failing.
	agentLoop      agent.Runner
	catalog        port.CatalogStore
	cfg            domain.OrchestrationConfig
	contextBuilder *ContextBuilder
	actions        SessionActionReader
}

func NewExecutor(agentLoop agent.Runner, catalog port.CatalogStore, cfg domain.OrchestrationConfig) *Executor {
	if cfg.MaxParallelTasks <= 0 {
		cfg.MaxParallelTasks = 3
	}
	if cfg.DependencyOutputMaxChars <= 0 {
		cfg.DependencyOutputMaxChars = 4000
	}
	if cfg.SubtaskHistoryMode == "" {
		cfg.SubtaskHistoryMode = domain.SubtaskHistoryModeIsolated
	}
	return &Executor{agentLoop: agentLoop, catalog: catalog, cfg: cfg}
}

type taskContext struct {
	intake      domain.GoalIntake
	plannerTask domain.PlannerTask
	planTask    domain.PlanTask
}

func (e *Executor) Execute(ctx context.Context, planID uuid.UUID, intake domain.GoalIntake, output domain.PlannerOutput, planTasks []domain.PlanTask, history []domain.Message, defaultModel string, policy domain.ToolPolicy, lang string, sessionID uuid.UUID, seedResults map[string]string) (string, map[string]string, error) {
	// seedResults is what an earlier round of this run already produced — empty
	// for a first plan, the original plan's task results for a repair plan. Those
	// ids are also the dependencies a repair task is allowed to name, so the same
	// map decides both questions: a depends_on is pre-satisfied exactly when the
	// result it wants is already here to hand to the dependent subtask.
	completed := make(map[string]bool, len(seedResults))
	for id := range seedResults {
		completed[id] = true
	}

	waves, err := TopologicalWavesWithCompleted(output.Tasks, completed)
	if err != nil {
		return "", nil, err
	}

	taskByKey := make(map[string]taskContext)
	for i, pt := range output.Tasks {
		taskByKey[pt.ID] = taskContext{intake: intake, plannerTask: pt, planTask: planTasks[i]}
	}

	results := make(map[string]string)
	for k, v := range seedResults {
		results[k] = v
	}
	var resultsMu sync.Mutex

	for _, wave := range waves {
		g, waveCtx := errgroup.WithContext(ctx)
		sem := make(chan struct{}, e.cfg.MaxParallelTasks)

		for _, taskID := range wave {
			tc := taskByKey[taskID]
			g.Go(func() error {
				sem <- struct{}{}
				defer func() { <-sem }()

				result, err := e.runTask(waveCtx, planID, tc, results, &resultsMu, history, defaultModel, policy, lang, sessionID)
				if err != nil {
					return fmt.Errorf("task %s: %w", taskID, err)
				}
				resultsMu.Lock()
				results[taskID] = result
				resultsMu.Unlock()
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return "", nil, err
		}
	}

	var sb strings.Builder
	sb.WriteString(output.Summary)
	sb.WriteString("\n\n")
	for _, t := range output.Tasks {
		if r, ok := results[t.ID]; ok {
			sb.WriteString("## ")
			sb.WriteString(t.Title)
			sb.WriteString("\n")
			sb.WriteString(r)
			sb.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(sb.String()), results, nil
}

// resolveSubtaskWorkspace decides which directory a subtask works in.
//
// Every code tool and every shell command resolves to the subtask workspace
// when one is set (registry.EffectiveWorkspaceDir), so this choice decides
// whether the agent can see the project at all. A board run clones the
// repository into the run's workspace and checks out the task branch BEFORE any
// agent starts; carving a per-subtask scratch directory inside that checkout
// handed the agent an empty folder. It grepped nothing, reported "we could not
// examine the project structure", and spent its whole iteration budget looking
// for a repository it was standing next to.
//
// So: a run that already has a checkout works IN it. Only a run without one —
// chat orchestration on a bare workspace — gets the per-subtask scratch
// directory, which is what that isolation was written for.
func resolveSubtaskWorkspace(sessionWorkspace, taskKey string) (string, error) {
	if sessionWorkspace == "" {
		return "", nil
	}
	if workspace.IsRepoCheckout(sessionWorkspace) {
		return sessionWorkspace, nil
	}
	dir, err := workspace.SubtaskDir(sessionWorkspace, taskKey)
	if err != nil {
		return "", err
	}
	if err := workspace.EnsureDir(dir); err != nil {
		return "", fmt.Errorf("create subtask workspace: %w", err)
	}
	return dir, nil
}

// enabledAgentSkills returns what the agent is configured to know, minus what
// the operator switched off. A disabled skill never reaches the index and
// load_skill refuses it too, so "off" means off on both ends.
func (e *Executor) enabledAgentSkills(ctx context.Context, agentID uuid.UUID) ([]domain.Skill, error) {
	all, err := e.catalog.ListSkillsByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	enabled := make([]domain.Skill, 0, len(all))
	for _, sk := range all {
		if sk.Enabled {
			enabled = append(enabled, sk)
		}
	}
	return enabled, nil
}

func (e *Executor) runTask(ctx context.Context, planID uuid.UUID, tc taskContext, priorResults map[string]string, mu *sync.Mutex, history []domain.Message, defaultModel string, policy domain.ToolPolicy, lang string, sessionID uuid.UUID) (string, error) {
	if tc.planTask.Status == domain.TaskStatusCompleted && tc.planTask.Result != "" {
		return tc.planTask.Result, nil
	}
	if tc.planTask.AgentID == nil {
		return "", fmt.Errorf("task has no agent")
	}

	agentRec, err := e.catalog.GetAgent(ctx, *tc.planTask.AgentID)
	if err != nil {
		return "", err
	}

	// Every skill the operator enabled on this agent, exactly like a board run.
	// The plan's skill_ids used to be the whole index, so an agent with 19
	// configured skills ran a subtask knowing about the 3 the planner happened
	// to name — the rest were unreachable even though load_skill would have
	// served them. The picks survive as emphasis in the task prompt.
	skills, err := e.enabledAgentSkills(ctx, agentRec.ID)
	if err != nil {
		return "", err
	}
	stacks, err := e.catalog.ListTechStacksByAgent(ctx, agentRec.ID)
	if err != nil {
		return "", err
	}

	taskPolicy := domain.EnsureAskUserTool(domain.MergeToolPolicy(policy, agentRec.ToolPolicy))
	taskPolicy = domain.RestrictToPlannedTools(taskPolicy, tc.plannerTask.ToolNames)
	model := agentRec.Model
	if model == "" {
		model = defaultModel
	}
	// lightModel is captured before a possible escalation below, so the
	// subtask's own utility calls (history summarization, the wrap-up on a
	// spent budget) stay on the agent's plain model even when the subtask
	// itself escalates to ModelHeavy — that escalation is for the work, not
	// for bookkeeping. See agent.WithLightModel.
	lightModel := model
	// Per-subtask model selection: the planner rates each subtask's difficulty;
	// a "hard" subtask runs on the assigned agent's stronger ModelHeavy when one
	// is configured. Everything else stays on the agent's default Model.
	if tc.plannerTask.Difficulty == domain.TaskDifficultyHard && agentRec.ModelHeavy != "" {
		model = agentRec.ModelHeavy
	}
	provider := agentRec.ProviderType

	_ = e.catalog.UpdateTaskStatus(ctx, tc.planTask.ID, domain.TaskStatusRunning, "", "")

	subtaskWorkspace, err := resolveSubtaskWorkspace(registry.WorkspaceDirFromContext(ctx), tc.planTask.TaskKey)
	if err != nil {
		return "", err
	}
	taskCtx := registry.ContextWithSubtaskWorkspace(ctx, subtaskWorkspace)

	if rec := activity.FromContext(ctx); rec != nil {
		// model/provider are recorded together on purpose: every "invalid model"
		// failure so far has been a model name that belonged to a provider the
		// agent is no longer on, and the pair is the only way to see that.
		rec.Step("subtask_started", map[string]string{
			"task_key": tc.planTask.TaskKey, "title": tc.planTask.Title, "agent": agentRec.Name, "working_dir": subtaskWorkspace,
			"model": model, "provider": string(provider),
		})
	}

	// The tracker is normally installed by the board runner and shared by every
	// subtask of a run. Chat orchestration installs none, so its subtasks were
	// unmeasured and nothing could tell a finished one from a started one.
	if registry.ToolUsageFromContext(taskCtx) == nil {
		taskCtx, _ = registry.ContextWithToolUsage(taskCtx)
	}
	usage := registry.ToolUsageFromContext(taskCtx)
	// The plan's declaration, not the resolved policy: it states what this
	// subtask is FOR. A "move DE-1 onto the board" subtask declares the move and
	// nothing else, and its run is finished when the move lands — even though
	// its policy also carries the read tools every agent keeps.
	effectiveTools := tc.plannerTask.ToolNames
	if len(effectiveTools) == 0 {
		effectiveTools = taskPolicy.AllowTools
	}

	var lastErr error
	var lastResult string
	var incompleteReason string
	var prior priorAttempt
	for attempt := range maxTaskRetries + 1 {
		prior.Number = attempt
		messages := e.buildTaskMessages(ctx, sessionID, history, tc, skills, stacks, agentRec, priorResults, mu, prior, subtaskWorkspace, lang)
		before := usage.Snapshot()
		// taskCtx carries the subtask's own workspace, which is what a host
		// executor is started in when this agent's provider is a local CLI.
		resp, err := e.agentLoop.RunTask(taskCtx, messages, model, provider, taskPolicy,
			agent.WithLightModel(lightModel),
			agent.WithSessionLimits(agentRec.MaxTurns, agentRec.Effort),
			agent.WithCLILabel(tc.planTask.TaskKey, tc.planTask.Title))
		if err != nil {
			lastErr = err
			prior.Err = err
			prior.Stats, _ = agent.StatsFromError(err)
			prior.Used = registry.UsageDelta(before, usage.Snapshot())
			// A spent iteration budget is not a transient failure: re-running
			// the same subtask from scratch spends the same budget again and
			// ends the same way (DE-1 burned 3×30 turns that way). The caller
			// commits what the agent produced and the next run resumes from
			// that branch instead.
			var budgetErr *agent.BudgetExhaustedError
			if errors.As(err, &budgetErr) {
				break
			}
			// Only now, on the path that really does run again: the digest costs
			// a read of the run's trace and nothing else would use it. This
			// failure carried no answer of its own, so the digest is the whole
			// account of what the attempt managed.
			prior.Digest = subtaskFindingsDigest(taskCtx, tc.planTask.TaskKey, "")
			continue
		}
		if resp.Clarification != nil {
			e.markTaskBlocked(taskCtx, tc.planTask, *resp.Clarification, resp.Message.Content)
			return "", ClarificationNeededError{Request: *resp.Clarification}
		}
		lastResult = resp.Message.Content
		delta := registry.UsageDelta(before, usage.Snapshot())
		incompleteReason = startedNotFinishedReason(delta, effectiveTools)
		if incompleteReason == "" {
			incompleteReason = boardWriteNotLandedReason(delta, effectiveTools)
		}
		if incompleteReason != "" && attempt < maxTaskRetries {
			// Tell it what is missing and let it spend another attempt. The
			// prompt builder replays this as "Previous attempt failed: …",
			// together with what that attempt already got done.
			lastErr = errors.New(incompleteReason)
			prior.Err = lastErr
			prior.Stats = agent.RunStats{}
			prior.Used = delta
			prior.Digest = subtaskFindingsDigest(taskCtx, tc.planTask.TaskKey, resp.Message.Content)
			continue
		}
		lastErr = nil
		break
	}

	if lastErr != nil {
		e.finishTask(ctx, tc.planTask.ID, domain.TaskStatusFailed, "", lastErr.Error())
		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("subtask_failed", map[string]string{
				"task_key": tc.planTask.TaskKey, "error": lastErr.Error(),
			})
		}
		return "", lastErr
	}

	// Out of attempts and still only board bookkeeping. Record the honest status
	// but hand the result back rather than failing the run: the plan's other
	// subtasks and the verifier still have something to work with, and the user
	// gets output instead of an error.
	if incompleteReason != "" {
		e.finishTask(ctx, tc.planTask.ID, domain.TaskStatusIncomplete, lastResult, incompleteReason)
		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("subtask_incomplete", map[string]string{
				"task_key": tc.planTask.TaskKey, "reason": incompleteReason,
			})
		}
		return lastResult, nil
	}

	e.finishTask(ctx, tc.planTask.ID, domain.TaskStatusCompleted, lastResult, "")
	if rec := activity.FromContext(ctx); rec != nil {
		preview := lastResult
		if len(preview) > 500 {
			preview = domain.TruncateHead(preview, 500)
		}
		rec.Step("subtask_completed", map[string]string{
			"task_key": tc.planTask.TaskKey, "result": preview,
		})
	}
	return lastResult, nil
}

// subtaskFindingsDigest renders what this subtask has already done, for the
// attempt that is about to repeat it.
//
// Every attempt rebuilds its messages from scratch, and only a sentence of text
// used to survive: an agent that spent thirty turns finding the right files
// began the next attempt knowing none of it. The run's activity trace outlives
// the loop that wrote it, so the digest is read back from there — no extra model
// call, one read per failed attempt.
//
// Two things it is honest about rather than precise about:
//
//   - The window starts at this subtask's own subtask_started step, so a second
//     retry sees the first attempt's work as well. That is wanted: the findings
//     accumulate, and the cap keeps the newest.
//   - A wave runs up to MaxParallelTasks subtasks against ONE run, and their
//     steps interleave, so a sibling's calls can land in this digest. The same
//     is already true of the tool counts this note carries (they come from the
//     run-wide usage tracker), and a slightly generous "already looked at"
//     costs the next attempt far less than an empty one.
func subtaskFindingsDigest(ctx context.Context, taskKey, summary string) string {
	rec := activity.FromContext(ctx)
	if rec == nil {
		return ""
	}
	return agent.DigestFromSteps(stepsSinceSubtaskStart(rec.Steps(ctx), taskKey), summary, 0)
}

// stepsSinceSubtaskStart trims a run's trace to what happened after this
// subtask began. A trace that never names the subtask — a store that keeps no
// steps, a trace trimmed behind us — yields nothing rather than the whole plan's
// activity attributed to one subtask.
func stepsSinceSubtaskStart(steps []domain.SessionStep, taskKey string) []domain.SessionStep {
	if taskKey == "" {
		return nil
	}
	start := -1
	for i, step := range steps {
		if step.StepType != "subtask_started" {
			continue
		}
		var payload struct {
			TaskKey string `json:"task_key"`
		}
		if json.Unmarshal(step.Payload, &payload) == nil && payload.TaskKey == taskKey {
			start = i
		}
	}
	if start < 0 {
		return nil
	}
	return steps[start+1:]
}

// finishTask writes a subtask's terminal status on a context that outlives the
// run's own cancellation.
func (e *Executor) finishTask(ctx context.Context, planTaskID uuid.UUID, status, result, errMsg string) {
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	_ = e.catalog.UpdateTaskStatus(pctx, planTaskID, status, result, errMsg)
}

// markTaskBlocked records the end state of a subtask that stopped to ask the
// stakeholder a question.
//
// Returning the clarification straight to the caller wrote nothing at all: the
// plan_tasks row stayed "running" and no subtask_completed/subtask_failed step
// was ever emitted, so the run's card kept a spinner on a subtask that had
// already stopped and was waiting on a human. Failed is the resumable status —
// runTask re-runs anything that is not completed once the answer arrives.
func (e *Executor) markTaskBlocked(ctx context.Context, planTask domain.PlanTask, req domain.ClarificationRequest, partial string) {
	reason := clarificationBlockedReason(req)
	e.finishTask(ctx, planTask.ID, domain.TaskStatusFailed, partial, reason)
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("subtask_failed", map[string]string{
			"task_key": planTask.TaskKey, "error": reason,
		})
	}
}

// clarificationBlockedReason states what the subtask is waiting for, so the
// card says "waiting for an answer: <question>" instead of going quiet.
func clarificationBlockedReason(req domain.ClarificationRequest) string {
	detail := req.Context
	if len(req.Questions) > 0 && req.Questions[0].Prompt != "" {
		detail = req.Questions[0].Prompt
	}
	if strings.TrimSpace(detail) == "" {
		return "waiting for an answer from the stakeholder"
	}
	return "waiting for an answer: " + detail
}

// startedNotFinishedReason reports why a subtask that returned without an error
// nonetheless did not finish, or "" when it did.
//
// The shape it catches: an implementation subtask whose entire successful tool
// ledger was claim_board_task + move_board_task, i.e. it told the board it had
// picked the work up and then stopped. The agent's own closing message reads
// like progress ("I claimed the task and moved it to in_progress; now I will
// review the project structure"), so nothing in the text distinguishes it from
// a finished run — only the ledger does.
//
// Two cases deliberately pass:
//   - An empty ledger. A subtask that answers from context (a status question, a
//     summary) legitimately calls nothing, and failing those would break every
//     conversational plan.
//   - A subtask whose plan declares nothing but board bookkeeping. When claim/move
//     is the whole declaration, bookkeeping IS the deliverable — that is exactly
//     the "move DE-1 onto the board" request, and it is complete when the move
//     lands. What the resolved policy also grants is irrelevant here: an agent
//     keeps its read tools on every subtask, and holding them is not a job.
func startedNotFinishedReason(delta map[string]int, effectiveTools []string) string {
	if len(delta) == 0 {
		return ""
	}
	for name := range delta {
		if !domain.IsBoardProgressTool(name) {
			return ""
		}
	}
	if !hasWorkTool(effectiveTools) {
		return ""
	}
	return "This attempt only claimed the task and moved it to another column — no work was produced. " +
		"Moving a task to in_progress announces that you started; it does not complete the subtask. " +
		"Do the work the subtask describes with the tools you have, then report what you changed."
}

// boardWriteNotLandedReason reports a bookkeeping subtask whose one deliverable
// — the board write it declares — never actually succeeded.
//
// startedNotFinishedReason deliberately passes a subtask whose declaration is
// nothing but bookkeeping, on the grounds that bookkeeping IS the deliverable
// there. What it never checked is whether the write landed, and the tracker
// counts successes only, so a subtask whose every move_board_task call was
// REJECTED left an empty ledger and read exactly like a subtask that had
// nothing to call. DE-1's "Move task to code_review" reported completed twice
// while the task's history recorded no move at all.
//
// Only pure-bookkeeping subtasks are judged: an implementing subtask that also
// happens to list move_board_task is finished by its code, not by its column.
func boardWriteNotLandedReason(delta map[string]int, effectiveTools []string) string {
	if hasWorkTool(effectiveTools) {
		return ""
	}
	var declared []string
	for _, name := range effectiveTools {
		if domain.IsBoardWriteTool(name) {
			declared = append(declared, name)
		}
	}
	if len(declared) == 0 {
		return ""
	}
	for _, name := range declared {
		if delta[name] > 0 {
			return ""
		}
	}
	return "This subtask's whole deliverable is the board write it declares (" + strings.Join(declared, ", ") +
		"), and no such call succeeded — either it was never made or the board rejected it. " +
		"Make the call, read what it returns, and if it is rejected say so with the exact error instead of reporting the work as done."
}

// plannedSkillFocus names the skills the plan singled out for this subtask.
// They are a hint, not the agent's toolbox: the index carries every enabled
// skill and load_skill serves any of them, so a planner pick that turns out to
// be the wrong one no longer hides the right one. Ids that name a disabled or
// foreign skill are dropped without failing the subtask — the plan is not the
// authority on what this agent may load.
func plannedSkillFocus(skillIDs []string, skills []domain.Skill) string {
	if len(skillIDs) == 0 || len(skills) == 0 {
		return ""
	}
	picked := make(map[string]bool, len(skillIDs))
	for _, id := range skillIDs {
		picked[strings.TrimSpace(id)] = true
	}
	names := make([]string, 0, len(skillIDs))
	for _, sk := range skills {
		if picked[sk.ID.String()] {
			names = append(names, sk.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "The plan flagged these of your skills as most relevant here: " + strings.Join(names, ", ") +
		". Load them with load_skill before you apply them; your other skills still apply when the work calls for them."
}

// hasWorkTool reports whether the subtask was equipped to do more than move the
// board. ask_user is excluded: asking is not producing either.
func hasWorkTool(effectiveTools []string) bool {
	for _, name := range effectiveTools {
		if domain.IsBoardProgressTool(name) || name == domain.AskUserToolName {
			continue
		}
		return true
	}
	return false
}

// priorAttempt is what the previous attempt at this subtask left behind.
//
// Only the error used to survive. Every attempt rebuilds its messages from
// scratch, so an agent that spent thirty turns finding the right files started
// the next attempt knowing none of that and spent thirty more finding them
// again — three attempts, one subtask's worth of progress. Carrying what the
// last attempt actually ran, and which tools kept rejecting it, is what turns a
// retry into a continuation.
type priorAttempt struct {
	Number int
	Err    error
	Stats  agent.RunStats
	// Used is the per-tool call count for the failed attempt, from the run's
	// usage tracker. It is populated even when the loop returned no stats.
	Used map[string]int
	// Digest names what the attempt touched — the files it changed, the files
	// it read, the commands it ran and how they ended — rendered from the run's
	// activity trace. The counts above say how much work happened; this says
	// what the work WAS, which is the part a retry cannot reconstruct.
	Digest string
}

func (p priorAttempt) note() string {
	if p.Number == 0 || p.Err == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nPrevious attempt failed: ")
	b.WriteString(p.Err.Error())

	if p.Digest != "" {
		b.WriteString("\n\n")
		b.WriteString(p.Digest)
	}
	if did := p.workDone(); did != "" {
		b.WriteString("\n\nWhat that attempt already did — continue from it, do not repeat it:\n")
		b.WriteString(did)
	}
	if pattern := p.Stats.FailurePattern(); pattern != "" {
		b.WriteString("\nTools that kept failing: ")
		b.WriteString(pattern)
		b.WriteString(". Use a different approach for those rather than the same call with new arguments.")
	}
	b.WriteString("\nPlease fix the issue and complete the task.")
	return b.String()
}

// workDone renders the tool counts, preferring the loop's own stats and falling
// back to the run's usage tracker when the failure carried none (a provider
// that never answered, say).
func (p priorAttempt) workDone() string {
	counts := p.Stats.ByTool
	if len(counts) == 0 {
		counts = p.Used
	}
	if len(counts) == 0 {
		return ""
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s×%d", name, counts[name]))
	}
	out := "- tool calls: " + strings.Join(parts, ", ")
	if len(p.Stats.LastCalls) > 0 {
		out += "\n- it stopped on: " + strings.Join(p.Stats.LastCalls, " → ")
	}
	return out
}

func (e *Executor) buildTaskMessages(ctx context.Context, sessionID uuid.UUID, history []domain.Message, tc taskContext, skills []domain.Skill, stacks []domain.TechStack, agentRec domain.Agent, priorResults map[string]string, mu *sync.Mutex, prior priorAttempt, subtaskWorkspace, lang string) []domain.Message {
	systemContent := prompt.BuildSystemPrompt(agentRec, skills, stacks, tc.plannerTask.SubtaskRules, lang)

	var taskPrompt strings.Builder
	if tc.intake.Purpose != "" || tc.intake.Goal != "" {
		taskPrompt.WriteString("Purpose: ")
		taskPrompt.WriteString(tc.intake.Purpose)
		taskPrompt.WriteString("\nGoal: ")
		taskPrompt.WriteString(tc.intake.Goal)
		taskPrompt.WriteString("\n\n")
	}
	taskPrompt.WriteString("Task: ")
	taskPrompt.WriteString(tc.plannerTask.Title)
	taskPrompt.WriteString("\n\n")
	taskPrompt.WriteString(tc.plannerTask.Description)
	taskPrompt.WriteString("\n\n")
	if focus := plannedSkillFocus(tc.plannerTask.SkillIDs, skills); focus != "" {
		taskPrompt.WriteString(focus)
		taskPrompt.WriteString("\n\n")
	}
	taskPrompt.WriteString(prompt.AskUserTaskGuidance())
	if subtaskWorkspace != "" {
		taskPrompt.WriteString(prompt.SubtaskWorkspaceNote(subtaskWorkspace))
	}

	mu.Lock()
	for _, dep := range tc.plannerTask.DependsOn {
		if r, ok := priorResults[dep]; ok {
			if agentRec.SubagentType == "generalPurpose" {
				if e.contextBuilder != nil {
					taskPrompt.WriteString("\n\n")
					taskPrompt.WriteString(e.contextBuilder.FormatExplorerFindings(dep, dep, truncateDependencyOutput(r, e.cfg.DependencyOutputMaxChars)))
					continue
				}
			}
			taskPrompt.WriteString("\n\nResult from dependency ")
			taskPrompt.WriteString(dep)
			taskPrompt.WriteString(":\n")
			taskPrompt.WriteString(truncateDependencyOutput(r, e.cfg.DependencyOutputMaxChars))
		}
	}
	mu.Unlock()

	taskPrompt.WriteString(prior.note())

	taskHistory := history
	if e.cfg.SubtaskHistoryMode == domain.SubtaskHistoryModeIsolated {
		taskHistory = isolatedSubtaskHistory(history)
	}
	// The digest carried in history was rendered once, before the run started.
	// A subtask that waits on another one must see what that dependency just
	// created, or it reports the record missing and creates it again. Re-read
	// the ledger per subtask so each one starts from current board state.
	taskHistory = e.withFreshActionDigest(ctx, sessionID, taskHistory)

	messages := make([]domain.Message, 0, len(taskHistory)+3)
	messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: systemContent})

	if e.contextBuilder != nil && sessionID != uuid.Nil {
		if agentRec.SubagentType == "explore" {
			if ctxMsgs, err := e.contextBuilder.BuildExplorerContext(ctx, sessionID, tc.plannerTask.Description); err == nil {
				for _, m := range ctxMsgs {
					if m.Role == domain.RoleSystem {
						messages = append(messages, m)
					}
				}
			}
		}
	}

	messages = append(messages, taskHistory...)
	messages = append(messages, domain.Message{Role: domain.RoleUser, Content: taskPrompt.String()})
	return messages
}

// withFreshActionDigest replaces any stale ledger digest in the history with one
// rendered from current board state, so a subtask sees records its dependencies
// created moments ago in this same run. Without a reader, or when the ledger is
// empty or unreadable, the history is returned untouched — a stale digest is
// still better than none.
func (e *Executor) withFreshActionDigest(ctx context.Context, sessionID uuid.UUID, history []domain.Message) []domain.Message {
	if e.actions == nil || sessionID == uuid.Nil {
		return history
	}
	actions, err := e.actions.ListActions(ctx, sessionID)
	if err != nil || len(actions) == 0 {
		return history
	}
	digest := domain.SessionActionDigest(actions)
	if digest == "" {
		return history
	}
	out := make([]domain.Message, 0, len(history)+1)
	for _, m := range history {
		if m.Role == domain.RoleSystem && domain.IsSessionActionDigest(m.Content) {
			continue
		}
		out = append(out, m)
	}
	return append(out, domain.Message{Role: domain.RoleSystem, Content: digest})
}

// isolatedSubtaskHistory strips tool-call chatter from the conversation a
// subtask agent sees, so it never inherits an orphaned tool_calls message whose
// results belong to a different loop.
//
// It keeps every user turn. Dropping all but the first used to rewrite the
// conversation so that the agent's most recent visible instruction was the one
// that opened the session — ask it to create a task, come back later and ask it
// to move that task, and it read "create a task" again and made a second one.
func isolatedSubtaskHistory(history []domain.Message) []domain.Message {
	var out []domain.Message
	for _, m := range history {
		switch m.Role {
		case domain.RoleSystem:
			// Session-level system prompts are rebuilt per subtask from the
			// assigned agent, so they are dropped here — except the action
			// ledger, which is the only record of what this conversation
			// already created and must reach every subtask.
			if domain.IsSessionActionDigest(m.Content) {
				out = append(out, m)
			}
		case domain.RoleUser:
			out = append(out, m)
		case domain.RoleAssistant:
			if len(m.ToolCalls) == 0 && m.Content != "" {
				out = append(out, m)
			}
		}
	}
	return out
}

func truncateDependencyOutput(content string, maxChars int) string {
	if maxChars <= 0 || len(content) <= maxChars {
		return content
	}
	// Byte-safe: a dependency result is LLM prose, and in Turkish a raw slice
	// lands mid-rune often enough that the provider rejected the message.
	return domain.TruncateHead(content, maxChars) + dependencyTruncateNote
}
