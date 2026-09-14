package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	goccyjson "github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmretry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

// maxPlannerRetries bounds every stage that talks to a provider — intake,
// planner, replanner, verifier — and it is one budget for two kinds of failure:
// the provider refusing the call, and the model answering with something that
// does not parse or does not validate. A stage that burns an attempt correcting
// itself has one fewer left for a rate limit, which is deliberate: the total
// wall clock a single run may spend on one stage is what this number protects.
//
// All four stages used to re-send a failed request instantly. A rate limit was
// therefore burned through in milliseconds — three requests inside the time the
// provider expected one — and a request the provider rejects deterministically
// (a bad model name, a malformed body, a prompt over the context window)
// collected the identical rejection three times before surfacing it. The
// wait/shrink/stop policy that fixes it now lives in llmretry, shared with the
// agent loop; llmretry.Await is the variant for callers like these four, which
// assemble their prompt fresh from run facts and so have nothing to shrink.
const maxPlannerRetries = 2

// pipelineStepError is how a stage reports a terminal provider failure.
//
// It exists to keep one particular failure from reading like a flaky provider.
// A request naming a host-executed provider (an agent on the Claude Code CLI)
// is refused before any endpoint is touched, and it used to be silently
// rerouted to whatever HTTP provider the tenant had as its default — which, for
// the tenant this was written for, was a dead model. Removing that reroute makes
// the refusal reach here, and "planner failed after 3 attempts: ..." would
// invite exactly the wrong diagnosis: the stage did not fail three times, and
// no fourth attempt would help. llmretry.Classify already stops the retry loop
// on it; this makes the sentence match.
//
// Every one of these stages is LOAD-BEARING. There is no degraded mode for an
// intake that produced no goal or a planner that produced no tasks, so the run
// fails and says why, rather than continuing with an empty plan.
func pipelineStepError(step string, attempts int, err error) error {
	if errors.Is(err, domain.ErrHostExecutedUnservable) {
		return fmt.Errorf("%s cannot run for this agent: %w", step, err)
	}
	return fmt.Errorf("%s failed after %d attempts: %w", step, attempts, err)
}

type PlannerOptions struct {
	ConstrainedAgentID *uuid.UUID
	Lang               string
	ProviderType       domain.LLMProviderType
	// Workspace is the rendered projects/repositories snapshot — see
	// IntakeOptions.Workspace. The planner can also stop with questions, so it
	// needs the same grounding.
	Workspace string
}

type Planner struct {
	llm                port.LLMClient
	catalog            port.CatalogStore
	skillRetriever     port.SkillRetriever
	maxTasks           int
	skillRetrievalTopK int
}

type agentCatalogEntry struct {
	Agent  domain.Agent
	Skills []domain.Skill
	Rules  []domain.OrchestratorRule
}

func NewPlanner(
	llm port.LLMClient,
	catalog port.CatalogStore,
	skillRetriever port.SkillRetriever,
	maxTasks int,
	skillRetrievalTopK int,
) *Planner {
	if maxTasks <= 0 {
		maxTasks = 10
	}
	if skillRetrievalTopK <= 0 {
		skillRetrievalTopK = 5
	}
	return &Planner{
		llm:                llm,
		catalog:            catalog,
		skillRetriever:     skillRetriever,
		maxTasks:           maxTasks,
		skillRetrievalTopK: skillRetrievalTopK,
	}
}

func (p *Planner) Generate(ctx context.Context, intake domain.GoalIntake, userMessage string, history []domain.Message, model string, extraContext []domain.Message, opts PlannerOptions) (domain.PlannerOutput, error) {
	agents, err := p.catalog.ListAgents(ctx)
	if err != nil {
		return domain.PlannerOutput{}, fmt.Errorf("list agents: %w", err)
	}
	enabledAgents := filterEnabledAgents(agents)
	if opts.ConstrainedAgentID != nil {
		filtered := make([]domain.Agent, 0, 1)
		for _, a := range enabledAgents {
			if a.ID == *opts.ConstrainedAgentID {
				filtered = append(filtered, a)
			}
		}
		enabledAgents = filtered
	}
	if len(enabledAgents) == 0 {
		return domain.PlannerOutput{}, fmt.Errorf("no enabled agents available for planning: add at least one enabled agent under Admin > Agents")
	}

	catalogs := make([]agentCatalogEntry, 0, len(enabledAgents))
	skillOwnership := make(map[string]map[string]bool)
	for _, a := range enabledAgents {
		agentSkills, err := p.catalog.ListSkillsByAgent(ctx, a.ID)
		if err != nil {
			return domain.PlannerOutput{}, fmt.Errorf("list skills for agent %s: %w", a.Name, err)
		}
		agentRules, err := p.catalog.ListEnabledRulesByAgent(ctx, a.ID)
		if err != nil {
			return domain.PlannerOutput{}, fmt.Errorf("list rules for agent %s: %w", a.Name, err)
		}
		catalogs = append(catalogs, agentCatalogEntry{Agent: a, Skills: agentSkills, Rules: agentRules})
		owned := make(map[string]bool, len(agentSkills))
		for _, sk := range agentSkills {
			owned[sk.ID.String()] = true
		}
		skillOwnership[a.ID.String()] = owned
	}

	var relevantSkills []domain.Skill
	if p.skillRetriever != nil {
		relevantSkills, err = p.skillRetriever.SearchSkills(ctx, userMessage, p.skillRetrievalTopK)
		if err != nil {
			return domain.PlannerOutput{}, fmt.Errorf("search skills: %w", err)
		}
	}

	var lastErr error
	var corrections []domain.Message
	for attempt := range maxPlannerRetries + 1 {
		systemPrompt := buildPlannerSystemPrompt(intake, catalogs, relevantSkills, opts)
		messages := buildPipelineLLMMessages(systemPrompt, history, userMessage, extraContext, corrections)

		resp, err := p.llm.Chat(ctx, domain.AgentRequest{
			Messages:       messages,
			Model:          model,
			ProviderType:   opts.ProviderType,
			ResponseFormat: domain.JSONSchemaResponseFormat("planner_output", plannerOutputSchema()),
		})
		if err != nil {
			lastErr = err
			corrections = nil
			log.Warn().Err(err).Int("attempt", attempt+1).Msg("planner llm call failed")
			if giveUp := llmretry.Await(ctx, err, attempt, maxPlannerRetries); giveUp != nil {
				return domain.PlannerOutput{}, pipelineStepError("planner", attempt+1, giveUp)
			}
			continue
		}

		output, err := parsePlannerOutput(resp.Message.Content)
		if err != nil {
			lastErr = err
			log.Warn().Err(err).Int("attempt", attempt+1).Msg("planner parse failed")
			corrections = pipelineCorrection(resp.Message.Content, err)
			continue
		}

		if !output.Ready {
			if err := validateClarificationQuestions(output.Questions); err != nil {
				lastErr = err
				log.Warn().Err(err).Int("attempt", attempt+1).Msg("planner clarification validation failed")
				corrections = pipelineCorrection(resp.Message.Content, err)
				continue
			}
			output.Purpose = intake.Purpose
			output.Goal = intake.Goal
			return output, nil
		}

		if err := validatePlannerOutput(output, enabledAgents, skillOwnership, p.maxTasks, nil); err != nil {
			lastErr = err
			log.Warn().Err(err).Int("attempt", attempt+1).Msg("planner validation failed")
			corrections = pipelineRejection(resp.Message.Content, err)
			continue
		}

		output.Purpose = intake.Purpose
		output.Goal = intake.Goal

		return output, nil
	}
	return domain.PlannerOutput{}, pipelineStepError("planner", maxPlannerRetries+1, lastErr)
}

// plannerParseOpts adapts the shared parser to the two prompts that use it.
type plannerParseOpts struct {
	// requireQuestions rejects output without a "questions" key. A planner must
	// always state whether it needs answers, so a missing key means truncated
	// or off-schema output worth retrying.
	requireQuestions bool
	// assumeReady treats output as a finished plan even without a "ready" flag.
	assumeReady bool
}

// parsePlannerOutput parses a planning turn.
func parsePlannerOutput(content string) (domain.PlannerOutput, error) {
	return parsePlannerJSON(content, plannerParseOpts{requireQuestions: true})
}

// parseRepairPlanOutput parses a replanning turn. The replanner prompt defines
// neither "ready" nor "questions" — repair plans never interrogate the user —
// so running it through the strict planner rules rejected every single replan
// ("planner output missing questions field", three retries, no repair tasks)
// and silently dropped the repair step from every verified run.
func parseRepairPlanOutput(content string) (domain.PlannerOutput, error) {
	return parsePlannerJSON(content, plannerParseOpts{assumeReady: true})
}

func parsePlannerJSON(content string, opts plannerParseOpts) (domain.PlannerOutput, error) {
	trimmed := stripJSONWrapper(content)

	var raw struct {
		Ready     bool                           `json:"ready"`
		Purpose   string                         `json:"purpose"`
		Goal      string                         `json:"goal"`
		Summary   goccyjson.RawMessage           `json:"summary"`
		Questions []domain.ClarificationQuestion `json:"questions"`
		Tasks     []struct {
			ID            string          `json:"id"`
			Title         string          `json:"title"`
			Description   string          `json:"description"`
			AgentID       string          `json:"agent_id"`
			SkillIDs      []string        `json:"skill_ids"`
			ToolNames     json.RawMessage `json:"tool_names"`
			SubtaskRules  []string        `json:"subtask_rules"`
			DependsOn     []string        `json:"depends_on"`
			Difficulty    string          `json:"difficulty"`
			ParallelGroup int             `json:"parallel_group"`
		} `json:"tasks"`
	}
	if err := parseLLMJSON(trimmed, &raw); err != nil {
		return domain.PlannerOutput{}, fmt.Errorf("invalid planner json: %w", err)
	}

	var summaryStr string
	if len(raw.Summary) > 0 {
		if err := goccyjson.Unmarshal(raw.Summary, &summaryStr); err != nil {
			// LLM returned an object instead of string — flatten to JSON text
			summaryStr = strings.TrimSpace(string(raw.Summary))
		}
	}
	if opts.requireQuestions && raw.Questions == nil {
		return domain.PlannerOutput{}, fmt.Errorf("planner output missing questions field")
	}

	output := domain.PlannerOutput{
		Ready:     raw.Ready || opts.assumeReady,
		Purpose:   raw.Purpose,
		Goal:      raw.Goal,
		Summary:   summaryStr,
		Questions: domain.NormalizeClarificationQuestions(raw.Questions),
		Tasks:     make([]domain.PlannerTask, 0, len(raw.Tasks)),
	}

	if !output.Ready {
		return output, nil
	}

	for _, t := range raw.Tasks {
		if t.ToolNames == nil {
			return domain.PlannerOutput{}, fmt.Errorf("task %s missing tool_names field", t.ID)
		}
		var toolNames []string
		if err := goccyjson.Unmarshal(t.ToolNames, &toolNames); err != nil {
			return domain.PlannerOutput{}, fmt.Errorf("task %s invalid tool_names: %w", t.ID, err)
		}
		output.Tasks = append(output.Tasks, domain.PlannerTask{
			ID:            t.ID,
			Title:         t.Title,
			Description:   t.Description,
			AgentID:       t.AgentID,
			SkillIDs:      t.SkillIDs,
			ToolNames:     toolNames,
			SubtaskRules:  t.SubtaskRules,
			DependsOn:     t.DependsOn,
			Difficulty:    normalizeDifficulty(t.Difficulty),
			ParallelGroup: t.ParallelGroup,
		})
	}
	return output, nil
}

// normalizeDifficulty coerces the planner's free-form difficulty into the two
// values the executor understands. Anything that isn't clearly "hard" defaults
// to "easy" so an omitted/garbled value never silently upgrades the model.
func normalizeDifficulty(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "hard", "high", "complex", "difficult":
		return domain.TaskDifficultyHard
	default:
		return domain.TaskDifficultyEasy
	}
}

// validatePlannerOutput checks a planning turn. priorTasks are the subtasks
// already planned for this run — empty for the first plan, the original plan's
// subtasks for a repair plan — so the one-creator rule is counted across the
// whole run and not per planning turn, and so depends_on can name work the run
// has already done.
func validatePlannerOutput(output domain.PlannerOutput, agents []domain.Agent, skillOwnership map[string]map[string]bool, maxTasks int, priorTasks []domain.PlannerTask) error {
	if output.Summary == "" {
		return fmt.Errorf("planner output missing summary")
	}
	if len(output.Tasks) == 0 {
		return fmt.Errorf("planner output has no tasks")
	}
	if len(output.Tasks) > maxTasks {
		return fmt.Errorf("planner output exceeds max tasks (%d)", maxTasks)
	}

	agentIDs := make(map[string]bool)
	for _, a := range agents {
		agentIDs[a.ID.String()] = true
	}

	// priorIDs are the ids this run already used. They are dependency targets,
	// never redefinable: a repair task that reuses one would collide with the
	// stored plan_tasks row it names.
	priorIDs := make(map[string]bool, len(priorTasks))
	for _, t := range priorTasks {
		priorIDs[t.ID] = true
	}

	// Titles the run has already used. A repair plan that re-states a finished
	// subtask verbatim is the duplicate the board shows twice — see
	// validateNoRepeatedTitles.
	priorTitles := make(map[string]string, len(priorTasks))
	for _, t := range priorTasks {
		priorTitles[normalizeTitle(t.Title)] = t.ID
	}

	// The same guard on the body, because the title one is trivially evaded and
	// was: one plan shipped "Verify changes and satisfy acceptance criteria" and
	// "Execute build and tests to verify changes" as separate waves carrying a
	// character-for-character identical description. Both ran, the repair round
	// ran it a third time, and QA posted the same "scenarios completed" comment
	// three times for a single round of work. Two names for one instruction is
	// one task.
	const duplicateDescriptionMinLen = 80
	priorDescriptions := make(map[string]string, len(priorTasks))
	for _, t := range priorTasks {
		priorDescriptions[normalizeTitle(t.Description)] = t.ID
	}
	seenDescriptions := make(map[string]string, len(output.Tasks))

	seen := make(map[string]bool)
	for _, t := range output.Tasks {
		if t.ID == "" {
			return fmt.Errorf("task missing id")
		}
		if t.Title == "" {
			return fmt.Errorf("task %s missing title", t.ID)
		}
		if t.Description == "" {
			return fmt.Errorf("task %s missing description", t.ID)
		}
		if t.AgentID == "" {
			return fmt.Errorf("task %s missing agent_id", t.ID)
		}
		if !agentIDs[t.AgentID] {
			return fmt.Errorf("task %s references unknown agent_id %s", t.ID, t.AgentID)
		}
		if seen[t.ID] {
			return fmt.Errorf("duplicate task id %s", t.ID)
		}
		if priorIDs[t.ID] {
			return fmt.Errorf("task %s reuses an id from the prior plan; repair tasks need new unique ids and may only reference the prior ones in depends_on", t.ID)
		}
		if priorID, clash := priorTitles[normalizeTitle(t.Title)]; clash {
			return fmt.Errorf(
				"task %s repeats the title of task %s, which this run already ran (%q). A subtask that restates finished work runs it a second time and the board shows the same step twice. Repair by describing what is still MISSING, with its own distinct title, and reference the finished task in depends_on",
				t.ID, priorID, t.Title)
		}
		// Long enough to be an instruction, not a stub. Two tasks that both say
		// "run the tests" are plausibly different work described tersely; two that
		// share a full paragraph verbatim are the same work planned twice.
		if key := normalizeTitle(t.Description); len(key) >= duplicateDescriptionMinLen {
			if priorID, clash := priorDescriptions[key]; clash {
				return fmt.Errorf(
					"task %s repeats the description of task %s, which this run already ran. Renaming a finished instruction does not make it a new one — it runs the same work again and the board shows the round twice. Describe what is still MISSING instead, and reference the finished task in depends_on",
					t.ID, priorID)
			}
			if otherID, clash := seenDescriptions[key]; clash {
				return fmt.Errorf(
					"tasks %s and %s carry the same description under different titles. That is one piece of work planned twice: it executes twice, comments twice and doubles the cost. Merge them, or give each a description of the distinct work it actually does",
					otherID, t.ID)
			}
			seenDescriptions[key] = t.ID
		}
		seen[t.ID] = true
		owned := skillOwnership[t.AgentID]
		for _, sid := range t.SkillIDs {
			if _, err := uuid.Parse(sid); err != nil {
				return fmt.Errorf("task %s has invalid skill_id %s", t.ID, sid)
			}
			if owned != nil && !owned[sid] {
				return fmt.Errorf("task %s skill_id %s does not belong to agent %s", t.ID, sid, t.AgentID)
			}
		}
	}

	// A dependency may name a task of this planning turn OR one the run already
	// completed. Checking it against this turn alone made the replanner's own
	// prompt unsatisfiable — it tells the model "depends_on may reference
	// existing task ids from the prior plan" (buildReplannerSystemPrompt), and
	// every repair plan that did so was rejected here, re-sent against the same
	// contradiction maxPlannerRetries+1 times, and then abandoned; the run went
	// on to report itself completed with the verifier's issue never repaired.
	for _, t := range output.Tasks {
		for _, dep := range t.DependsOn {
			if !seen[dep] && !priorIDs[dep] {
				return fmt.Errorf("task %s depends on unknown task %s", t.ID, dep)
			}
		}
	}

	if err := validateDisjointWrites(output.Tasks); err != nil {
		return err
	}

	if err := validateNoBookkeepingOnlySubtasks(output.Tasks, priorTasks); err != nil {
		return err
	}

	policies := make(map[string]domain.ToolPolicy, len(agents))
	for _, a := range agents {
		policies[a.ID.String()] = a.ToolPolicy
	}
	if err := validateSingleTaskCreator(append(append([]domain.PlannerTask{}, priorTasks...), output.Tasks...), policies); err != nil {
		return err
	}

	return nil
}

// createBoardTaskTool is the one board write that mints a new record rather than
// changing an existing one, so it is capped across the whole plan and not just
// per parallel group.
const createBoardTaskTool = "create_board_task"

// normalizeTitle folds a subtask title to what two titles have to differ in to
// be different work: case and spacing are not it.
func normalizeTitle(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// bookkeepingOnlyTools are the calls that announce where work stands without
// producing any of it. ask_user rides along because asking is not producing
// either — the same reading the executor's hasWorkTool uses.
var bookkeepingOnlyTools = map[string]bool{
	"claim_board_task":     true,
	"move_board_task":      true,
	"add_task_comment":     true,
	domain.AskUserToolName: true,
}

// validateNoBookkeepingOnlySubtasks rejects a plan that gives a column move a
// subtask of its own.
//
// The prompt has told planners since the beginning that claiming a task and
// moving it to in_progress belong inside the implementing subtask. The move at
// the OTHER end was never named, and that is the one every plan grew: a final
// "Move task to code_review" step. It costs a whole subtask — its own model
// call, its own retries, its own card on the board — to make one tool call, and
// it is worse than wasteful, because nothing verifies it. The executor treats a
// bookkeeping subtask as finished when the agent stops talking, so DE-1's plan
// showed "Move task to code_review — completed" while the task's own history
// recorded no move at all: the call had failed, or was never made, and the
// subtask reported success either way.
//
// The move is the system's job now (board.Runner.advanceToCodeReview), so a
// subtask for it has nothing left to do but invent work — which is exactly what
// DE-1's did, for 45 iterations.
//
// A single-subtask plan is left alone: "move DE-1 to done" is a legitimate
// request whose entire deliverable IS the move, and the prior tasks count in
// because a repair plan's one task is still the second task of the run.
func validateNoBookkeepingOnlySubtasks(tasks, priorTasks []domain.PlannerTask) error {
	if len(tasks)+len(priorTasks) < 2 {
		return nil
	}
	for _, t := range tasks {
		if !isBookkeepingOnlySubtask(t) {
			continue
		}
		return fmt.Errorf(
			"subtask %s (%q) declares nothing but board bookkeeping (%s). Moving a task between columns is not a deliverable: it takes one tool call, nothing verifies it, and the system performs the move to code_review itself when the implementing run finishes. Drop this subtask and name the move in the implementing subtask's description instead",
			t.ID, t.Title, strings.Join(t.ToolNames, ", "))
	}
	return nil
}

// isBookkeepingOnlySubtask reports whether a subtask's whole declaration is
// progress announcements. A subtask that declares no tools inherits its agent's
// toolset and is therefore never bookkeeping-only, and one that only comments
// (a hand-off note, a stakeholder answer) is left alone — the shape being
// rejected is a step whose point is the column change.
func isBookkeepingOnlySubtask(t domain.PlannerTask) bool {
	if len(t.ToolNames) == 0 {
		return false
	}
	movesColumn := false
	for _, name := range t.ToolNames {
		if !bookkeepingOnlyTools[name] {
			return false
		}
		if domain.IsBoardProgressTool(name) {
			movesColumn = true
		}
	}
	return movesColumn
}

// validateSingleTaskCreator rejects a run that spreads board-task creation over
// more than one subtask. It is counted over the original plan AND every repair
// plan appended to it, because both ways of getting a second creator were seen
// in the same afternoon:
//
// validateDisjointWrites already stops two creators inside one parallel_group,
// but chaining them with depends_on satisfied it: a plan for one request came
// back as "determine where the link goes" → "decide how it looks" → "add it",
// each in its own group, each opening its own board task. Three records, one
// piece of work. Sequencing does not make a second creator correct — the
// planning steps were never separate deliverables to begin with.
//
// The other route was verification. The plan opened the right task, the verifier
// scored the run failed because the feature was not live yet, and the repair
// plan opened a second "technical analysis" task for work the first one already
// covered. Verification only runs once every subtask succeeded, so a repair plan
// never needs to create a record the original plan was already responsible for.
//
// One subtask may still create several tasks in a single run; what it may not do
// is share the job with a sibling that cannot see what it opened.
//
// policies maps agent id to that agent's tool policy, and closes the hole this
// check used to have: a subtask with no tool_names declares nothing, so counting
// declarations alone made it invisible while the executor still handed it the
// agent's whole toolset. Such a subtask is counted as a creator exactly when its
// agent is allowed to create — in the seeded roles only product-manager is, so
// developer and QA subtasks are unaffected.
func validateSingleTaskCreator(tasks []domain.PlannerTask, policies map[string]domain.ToolPolicy) error {
	creators := make([]string, 0, 2)
	for _, t := range tasks {
		if subtaskMayCreateTasks(t, policies) {
			creators = append(creators, t.ID)
		}
	}
	if len(creators) < 2 {
		return nil
	}
	sort.Strings(creators)
	return fmt.Errorf(
		"%d subtasks can create board tasks (%s), but at most one subtask in a plan may. Give every board task this request needs to ONE subtask — it can open several in a single run — and for the others either drop them or list tool_names without %s, which is what marks a subtask as not creating records (a subtask with no tool_names inherits its agent's whole toolset and counts as a creator)",
		len(creators), strings.Join(creators, ", "), createBoardTaskTool)
}

// subtaskMayCreateTasks reports whether a subtask will reach create_board_task
// once the executor resolves its policy: declared tool_names win, and an empty
// declaration falls back to what the assigned agent is allowed.
func subtaskMayCreateTasks(t domain.PlannerTask, policies map[string]domain.ToolPolicy) bool {
	if len(t.ToolNames) > 0 {
		for _, name := range t.ToolNames {
			if name == createBoardTaskTool {
				return true
			}
		}
		return false
	}
	policy, ok := policies[t.AgentID]
	if !ok {
		return false
	}
	return domain.ToolAllowedByPolicy(createBoardTaskTool, policy)
}

// boardWriteTools is domain's list, shared so this check and the executor's
// policy resolution can never disagree about what counts as a board write.
func boardWriteTool(name string) bool { return domain.IsBoardWriteTool(name) }

// validateDisjointWrites rejects a plan that puts two board-writing subtasks in
// the same parallel_group. Waves run concurrently and each subtask sees the
// conversation as it looked before the run, so neither can observe what the
// other just wrote: told to open the same task, both open it. Ordering has to be
// declared with depends_on, and the planner's retry loop feeds this error back
// so it can re-plan.
//
// This only sees subtasks that declare tool_names. A subtask that declares none
// inherits its agent's whole policy and is invisible here — the tool-level
// duplicate guard covers that remainder.
func validateDisjointWrites(tasks []domain.PlannerTask) error {
	type writer struct {
		taskID string
		tool   string
	}
	byGroup := make(map[int][]writer)
	for _, t := range tasks {
		for _, name := range t.ToolNames {
			if boardWriteTool(name) {
				byGroup[t.ParallelGroup] = append(byGroup[t.ParallelGroup], writer{taskID: t.ID, tool: name})
				break
			}
		}
	}
	for group, writers := range byGroup {
		if len(writers) < 2 {
			continue
		}
		ids := make([]string, 0, len(writers))
		for _, w := range writers {
			ids = append(ids, fmt.Sprintf("%s(%s)", w.taskID, w.tool))
		}
		sort.Strings(ids)
		return fmt.Errorf(
			"parallel_group %d has %d subtasks that write to the board (%s); they run concurrently and cannot see each other, so they would create duplicate records. Give the board write to exactly one subtask and chain the others with depends_on",
			group, len(writers), strings.Join(ids, ", "))
	}
	return nil
}

// subtaskDescriptionShape puts the ordering INSIDE the subtask instead of
// spreading it over waves.
//
// The pull, the branch and the hand-off move are the control plane's (the
// workspace is cloned and checked out before any agent starts, and a finished
// run with a diff is moved to code_review for it), so a wave per lifecycle step
// plans work nobody has to do — and the plan validators reject exactly those
// subtasks. What was genuinely missing is the order and the boundary within the
// one subtask that does the work: runs implemented, declared success and never
// executed anything, or wandered into refactors nobody asked for.
//
// So the phases are written as the shape of the description field. They cost no
// extra model call, they cannot be dropped by a validator, and the executing
// agent reads them in the order it must work them.
const subtaskDescriptionShape = `Shape of a subtask description (implementation work):
Write the description as ordered phases the agent works top to bottom, and state the boundary explicitly. Phases are prose inside ONE subtask — never separate subtasks, never separate parallel_groups:
1. Scope — the change to make, named concretely: which files, endpoints, screens or components. If the exact location must be discovered, say which tools find it.
2. Out of scope — what this subtask must NOT touch, stated as plainly as the scope. Name the neighbouring code, the unrelated bugs, the refactors and the dependency or config changes it must leave alone. A subtask with no boundary is how a small change becomes a diff nobody can review.
3. Verify — the build and test commands to run before finishing, and what output counts as passing. Never "test it": name the commands, or say the agent must find them in package.json / Makefile / go.mod / the README. Red output is fixed inside this same subtask.
4. Close — tick every acceptance criterion the verified change satisfies, leave the rest open with a reason, and report what changed plus the command output that proved it.
Do NOT plan phases for pulling the repository, creating the branch, claiming the task, moving columns or opening the pull request: the system does all of those around the run. The agent's phases start at the code and end at the evidence.`

// verificationSubtaskRule adds the check that a split plan cannot perform on
// itself.
//
// Subtasks in one wave share the branch but not the context: each sees the
// workspace as it looked when the wave started, and none sees what the others
// wrote. Two implementers on one repository is enough for the failure — the one
// adding a button and the one editing the same page can leave the branch with
// the button in place and a whole section gone, and both report success
// truthfully, because each verified only its own change.
//
// So the last wave is a reader, not a writer: it builds and tests the merged
// state, reads the WHOLE branch diff for damage no single implementer could
// see, and only then ticks the criteria. The column move stays with the control
// plane (board.Runner.advanceToCodeReview) — a subtask whose deliverable is a
// move is the shape DE-1 failed as, reported "completed" twice with no move in
// the task's history, and the plan validators reject it.
const verificationSubtaskRule = `Final verification subtask (mandatory when the plan has MORE THAN ONE subtask that changes code in the same repository):
- Add exactly one last subtask that depends_on every implementing subtask and sits alone in the last parallel_group. It changes nothing by default: it is the pass that judges the merged result.
- Its description states, in this order: (1) build and test the repository as a whole and read the output; (2) read the complete branch diff and judge it against the original request — a subtask that ran earlier could not see what the later ones wrote, so this is the only pass that can catch one change breaking or deleting another's work; (3) check every acceptance criterion against what the build/test output and the diff actually show; (4) tick each criterion that holds with set_criterion_completed, leave the rest open, and report findings with add_task_comment.
- Out of scope for it: new features, refactors, and anything the request did not ask for. A regression it finds is either a small, named repair inside this subtask or a reported finding — never a redesign.
- It ticks the criteria that the implementers therefore must NOT tick: say so in their descriptions. One pass owns the verdict, and it is the pass that saw everything.
- Do not give it move_board_task and do not plan the column move: when the run ends verified, with the criteria ticked and a real diff on the branch, the system moves the task to code_review and opens the pull request itself. A subtask whose deliverable is that move is rejected before the plan runs.
- A single-subtask plan needs no verification subtask: its own Verify phase is the same pass, done by the agent that has the whole context.`

func buildPlannerSystemPrompt(intake domain.GoalIntake, catalogs []agentCatalogEntry, relevantSkills []domain.Skill, opts PlannerOptions) string {
	var sb strings.Builder
	sb.WriteString(`You are an orchestration planner. Decompose the user request into a structured execution plan.

Respond with a single JSON object matching the provided schema.

Rules:
- Read the full conversation thread in the messages. Prior turns may include clarification questions and user answers.
- If the conversation already answers open questions, set ready to true and plan from the combined context. Do not ask again for information the user already provided in this thread.
- Clarification answers appear as user messages ("prompt: answer"). Never repeat those questions in a new ask_user or ready=false plan.
- Never create a task whose only purpose is to ask the user questions. Set ready to false and use the questions array instead.
- A subtask must produce a CHANGE, not a decision or a fact. "Determine where the link goes", "decide the visual style", "figure out the site structure", "get access to the repository" are not subtasks. Resolve them instead:
  - Only the stakeholder can answer (product preference, priority, deadline, content) → set ready to false and put it in questions.
  - The answer is discoverable — in the repository, on the board, in the workspace snapshot, or on the live site → the implementing subtask looks it up itself with its own tools. Say so in that subtask's description; do not give the lookup its own subtask and never open a board task for it.
- A system message may list the records this conversation already created (board tasks, projects, comments) with their ids. When the request is about one of those records — move it, advance it, update it, chase it — plan exactly ONE subtask that acts on that id with move_board_task / update_board_task / add_task_comment. Never plan a create for work that already has a record; a second record for the same work is the failure this rule exists to prevent.
- HARD CONSTRAINT, machine-checked before the plan runs: AT MOST ONE subtask in the whole plan may create board tasks. One agent opening every task sees them all and can size them against each other; several agents each opening "their" task produce one board record per planning step for a single piece of work.
- A subtask with an EMPTY tool_names inherits its assigned agent's entire toolset, so if that agent may create board tasks the subtask counts as a creator. List tool_names explicitly on every subtask that must not open records — that is the only way to declare it.
- tool_names governs BOARD WRITES only (create_board_task, move_board_task, update_board_task): listing them grants them, leaving them out withholds them. Everything else the assigned agent is configured for — reading and changing code, the shell, the web, its skills — it keeps on every subtask regardless of what you list, so a short list can never leave an implementer unable to look at the repository. Still list the tools the description actually needs: the list is what the subtask is FOR, and one that declares only claim_board_task / move_board_task / add_task_comment is read as pure bookkeeping.
- Never assume missing information. If you still cannot decompose the work without guessing, set ready to false and ask only for what remains unanswered (follow ask_user tool schema per question; choice mode must end with id other; set allow_multiple true when multiple selections are valid).
- Never ask the user for state the system already stores — repository/codebase access, which repos or projects exist, board contents, team members. The workspace state section below is authoritative; plan a task to look it up instead of asking.
- When ready is true: questions must be an empty array; provide summary and tasks.
- When ready is false: tasks must be an empty array; provide questions only.
- Every task must have a unique id, title, description, and agent_id from the available agents list.
- Every task must include tool_names (array of tool name strings, may be empty). Never put tool names in skill_ids.
- skill_ids must be UUIDs from that task's agent skills only (may be empty).
- For a simple single-shot request, return exactly ONE task.
- depends_on lists task ids that must complete before this task starts.
- parallel_group is an integer grouping tasks that can run in parallel (same group = parallelizable).
- Subtasks in one parallel_group run CONCURRENTLY and cannot see each other's work. Give them disjoint deliverables. A board write — creating, moving or updating a task — belongs to exactly ONE subtask. Never let two subtasks produce the same record; two agents told to open the same task will open it twice, each with its own wording.
- HARD CONSTRAINT, machine-checked before the plan runs: within one parallel_group AT MOST ONE subtask may list a board-write tool (create_board_task, move_board_task, update_board_task) in tool_names. A second one rejects the entire plan. Before returning, count the board writers per parallel_group yourself; if any group has two, move the extras into later groups with depends_on.
- HARD CONSTRAINT, machine-checked: board bookkeeping is never a subtask of its own. Claiming a task, moving it between columns and announcing that work has started take one tool call and produce nothing; they belong INSIDE the subtask that does the work. A subtask titled "move the task to in_progress", "claim the task", "move the task to code_review", "hand off to review" — or any subtask whose tool_names are only claim_board_task / move_board_task / add_task_comment / ask_user — is rejected. Fold it into the implementing subtask's description.
- The hand-off move at the END of implementation work is the system's, not a step you plan. When an implementing run finishes with a green build and a real diff, the control plane moves the task to code_review itself. Plan the work; never plan the move.
- Never plan a move into a column the task is already in. The conversation states the task's current column; a move to that same column is a no-op, and the subtask planned for it has nothing left to do but invent work.
- Do not plan one subtask per delivery lifecycle stage. Analysis, implementation, QA verification, PM/UAT review and stakeholder approval are board COLUMNS a single task moves through as the assigned agents work it — not planning subtasks. Planning them as subtasks opens one board record per stage for what is one piece of work.
- Return the smallest plan that satisfies the request. Work that ends up as one board task is ONE subtask, not a chain of one subtask per stage. Only split when the subtasks have genuinely different deliverables.
- HARD CONSTRAINT, machine-checked: no two subtasks may carry the same description, and no subtask may repeat the title or the description of a task that already ran in this conversation. Two names for one instruction is one piece of work planned twice — it executes twice, comments twice and doubles the cost. When earlier work left something unfinished, describe only what is still MISSING, in its own words, and reference the finished task in depends_on.
- If a subtask needs a record another subtask produces, put it in depends_on rather than the same parallel_group. Waves are barriers: a dependent subtask starts only after its dependencies finish, and receives their results.
- When a subtask creates a board record other subtasks act on, its description must say to report the created task's key/id in its result, so the dependents can address it instead of re-creating it.
- difficulty rates the subtask: "hard" for complex/ambiguous/high-risk work (architecture, tricky debugging, security-sensitive), "easy" for routine/mechanical work. Executors run "hard" subtasks on a stronger model when the assigned agent has one configured.
- Do not include any text outside the JSON object.

`)
	sb.WriteString(subtaskDescriptionShape)
	sb.WriteString("\n\n")
	sb.WriteString(verificationSubtaskRule)
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("Purpose: %s\nGoal: %s\n\n", intake.Purpose, intake.Goal))
	if opts.Workspace != "" {
		sb.WriteString(opts.Workspace)
		sb.WriteString("\n\n")
	}
	if opts.ConstrainedAgentID != nil {
		sb.WriteString(fmt.Sprintf("Solo mode: all tasks must use agent_id=%s only. Decompose according to that agent's rules and skills listed below.\n\n", opts.ConstrainedAgentID.String()))
	}
	sb.WriteString(prompt.UserFacingLanguageRule(opts.Lang))
	sb.WriteString("\n\n")
	sb.WriteString(`Available agents (each with scoped skills and rules):
`)
	for _, entry := range catalogs {
		a := entry.Agent
		sb.WriteString(fmt.Sprintf("\n## Agent id=%s name=%s type=%s description=%s\n", a.ID, a.Name, a.SubagentType, a.Description))
		if len(entry.Skills) > 0 {
			sb.WriteString("Skills:\n")
			for _, sk := range entry.Skills {
				if sk.Enabled {
					// Metadata only: the planner picks skill_ids from
					// name/description; the full skill body is loaded on-demand
					// by the executor (GetSkill) when the task runs. Dumping every
					// skill's full content bloated the prompt past provider token
					// limits even for a one-line request.
					sb.WriteString(fmt.Sprintf("- id=%s name=%s category=%s description=%s\n", sk.ID, sk.Name, sk.Category, sk.Description))
				}
			}
		}
		if len(entry.Rules) > 0 {
			sb.WriteString("Rules:\n")
			for _, r := range entry.Rules {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", r.Name, r.Content))
			}
		}
	}
	if len(relevantSkills) > 0 {
		sb.WriteString("\nRelevant skills (semantic matches — metadata only):\n")
		for _, sk := range relevantSkills {
			if sk.Enabled {
				sb.WriteString(fmt.Sprintf("- id=%s agent_id=%s name=%s category=%s description=%s\n", sk.ID, sk.AgentID, sk.Name, sk.Category, sk.Description))
			}
		}
	}
	return sb.String()
}

func filterEnabledAgents(agents []domain.Agent) []domain.Agent {
	var out []domain.Agent
	for _, a := range agents {
		if a.Enabled {
			out = append(out, a)
		}
	}
	return out
}

func BuildPlannerSystemPromptForTest(catalogs []agentCatalogEntry, relevantSkills []domain.Skill) string {
	intake := domain.GoalIntake{Ready: true, Purpose: "test purpose", Goal: "test goal", Constraints: []string{}, Questions: []domain.ClarificationQuestion{}}
	return buildPlannerSystemPrompt(intake, catalogs, relevantSkills, PlannerOptions{Lang: "tr"})
}

func BuildPlannerSystemPromptWithOptionsForTest(opts PlannerOptions) string {
	intake := domain.GoalIntake{Ready: true, Purpose: "test purpose", Goal: "test goal", Constraints: []string{}, Questions: []domain.ClarificationQuestion{}}
	return buildPlannerSystemPrompt(intake, nil, nil, opts)
}

func BuildPlannerSystemPromptLegacyForTest(agents []domain.Agent, skills []domain.Skill, rules []domain.OrchestratorRule) string {
	entry := agentCatalogEntry{Agent: domain.Agent{}}
	if len(agents) > 0 {
		entry.Agent = agents[0]
	}
	entry.Skills = skills
	entry.Rules = rules
	intake := domain.GoalIntake{Ready: true, Purpose: "test purpose", Goal: "test goal", Constraints: []string{}, Questions: []domain.ClarificationQuestion{}}
	return buildPlannerSystemPrompt(intake, []agentCatalogEntry{entry}, nil, PlannerOptions{Lang: "tr"})
}
