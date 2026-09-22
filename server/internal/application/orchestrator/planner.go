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

// One retry budget shared by intake, planner, replanner and verifier, for provider and parse failures alike.
const maxPlannerRetries = 2

// Presents a host-executor refusal as a config problem, not a flaky provider.
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
	// Rendered projects/repositories snapshot; the planner and intake share it.
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
	// requireQuestions rejects output without a "questions" key; off-schema output is worth retrying.
	requireQuestions bool
	// assumeReady treats output as a finished plan even without a "ready" flag.
	assumeReady bool
}

func parsePlannerOutput(content string) (domain.PlannerOutput, error) {
	return parsePlannerJSON(content, plannerParseOpts{requireQuestions: true})
}

// Repair plans define neither "ready" nor "questions", so the strict planner rules must not apply.
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
			// Object instead of string — flatten to JSON text.
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

// Anything not clearly "hard" defaults to "easy" so a garbled value never upgrades the model.
func normalizeDifficulty(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "hard", "high", "complex", "difficult":
		return domain.TaskDifficultyHard
	default:
		return domain.TaskDifficultyEasy
	}
}

// Checks a planning turn; priorTasks are this run's already-planned subtasks.
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

	// Dependency targets, never redefinable: reuse would collide with the stored plan_tasks row.
	priorIDs := make(map[string]bool, len(priorTasks))
	for _, t := range priorTasks {
		priorIDs[t.ID] = true
	}

	// A repair plan restating a finished subtask verbatim is the duplicate the board shows twice.
	priorTitles := make(map[string]string, len(priorTasks))
	for _, t := range priorTasks {
		priorTitles[normalizeTitle(t.Title)] = t.ID
	}

	// The same guard on the body: the title one is trivially evaded and was.
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
		// Long enough to be an instruction, not a stub.
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

	// A dependency may name this turn OR an id the run already completed; the replanner prompt relies on that.
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

// createBoardTaskTool is capped across the whole plan, not just per parallel group.
const createBoardTaskTool = "create_board_task"

func normalizeTitle(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

var bookkeepingOnlyTools = map[string]bool{
	"claim_board_task":     true,
	"move_board_task":      true,
	"add_task_comment":     true,
	domain.AskUserToolName: true,
}

// A plan with 2+ subtasks may not give a column move a subtask of its own.
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

// A subtask with no declared tools inherits its agent's toolset and is never bookkeeping-only.
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

// Counted over the original plan AND every repair plan: chained or un-owned creators both dodge it.
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

// Declared tool_names win; an empty declaration falls back to what the agent is allowed.
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

// domain's list, shared so this check and the executor can never disagree.
func boardWriteTool(name string) bool { return domain.IsBoardWriteTool(name) }

// Two board-writing subtasks in one parallel_group run blind and would create duplicates; order with depends_on.
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

// Ordering and boundary live in the description; lifecycle steps are the control plane's.
const subtaskDescriptionShape = `Shape of a subtask description (implementation work):
Write the description as ordered phases the agent works top to bottom, and state the boundary explicitly. Phases are prose inside ONE subtask — never separate subtasks, never separate parallel_groups:
1. Scope — the change to make, named concretely: which files, endpoints, screens or components. If the exact location must be discovered, say which tools find it.
2. Out of scope — what this subtask must NOT touch, stated as plainly as the scope. Name the neighbouring code, the unrelated bugs, the refactors and the dependency or config changes it must leave alone. A subtask with no boundary is how a small change becomes a diff nobody can review.
3. Verify — the build and test commands to run before finishing, and what output counts as passing. Never "test it": name the commands, or say the agent must find them in package.json / Makefile / go.mod / the README. Red output is fixed inside this same subtask.
4. Close — tick every acceptance criterion the verified change satisfies, leave the rest open with a reason, and report what changed plus the command output that proved it.
Do NOT plan phases for pulling the repository, creating the branch, claiming the task, moving columns or opening the pull request: the system does all of those around the run. The agent's phases start at the code and end at the evidence.`

// A split plan ends with one read-only subtask that verifies the merged result alone.
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
