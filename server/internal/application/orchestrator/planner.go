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

type taskIDData struct{ TaskID string }
type taskAgentData struct{ TaskID, AgentID string }
type taskSkillData struct{ TaskID, SkillID string }
type taskSkillAgentData struct{ TaskID, SkillID, AgentID string }
type taskDepData struct{ TaskID, DepID string }
type maxTasksData struct{ MaxTasks int }
type duplicateTitlePriorData struct{ TaskID, PriorID, QuotedTitle string }
type duplicateDescriptionPriorData struct{ TaskID, PriorID string }
type duplicateDescriptionTurnData struct{ OtherID, TaskID string }
type disjointWritesData struct {
	Group, Count int
	Writers      string
}
type bookkeepingOnlyData struct{ TaskID, QuotedTitle, Tools string }
type multipleCreatorsData struct {
	Count int
	IDs   string
	Tool  string
}

var (
	plannerMissingSummaryKey      = prompt.Define[struct{}]("guard.planner_missing_summary", struct{}{})
	plannerNoTasksKey             = prompt.Define[struct{}]("guard.planner_no_tasks", struct{}{})
	plannerTooManyTasksKey        = prompt.Define("guard.planner_too_many_tasks", maxTasksData{MaxTasks: 10})
	plannerTaskMissingIDKey       = prompt.Define[struct{}]("guard.planner_task_missing_id", struct{}{})
	plannerTaskMissingTitle       = prompt.Define("guard.planner_task_missing_title", taskIDData{TaskID: "t1"})
	plannerTaskMissingDesc        = prompt.Define("guard.planner_task_missing_description", taskIDData{TaskID: "t1"})
	plannerTaskMissingAgent       = prompt.Define("guard.planner_task_missing_agent", taskIDData{TaskID: "t1"})
	plannerUnknownAgentKey        = prompt.Define("guard.planner_unknown_agent", taskAgentData{TaskID: "t1", AgentID: "a1"})
	plannerDuplicateTaskIDKey     = prompt.Define("guard.planner_duplicate_task_id", taskIDData{TaskID: "t1"})
	plannerReusedPriorIDKey       = prompt.Define("guard.planner_reused_prior_id", taskIDData{TaskID: "t1"})
	plannerDuplicateTitlePriorKey = prompt.Define("guard.planner_duplicate_title_prior",
		duplicateTitlePriorData{TaskID: "t1", PriorID: "t0", QuotedTitle: `"sample"`})
	plannerDuplicateDescriptionPriorKey = prompt.Define("guard.planner_duplicate_description_prior",
		duplicateDescriptionPriorData{TaskID: "t1", PriorID: "t0"})
	plannerDuplicateDescriptionTurnKey = prompt.Define("guard.planner_duplicate_description_turn",
		duplicateDescriptionTurnData{OtherID: "t1", TaskID: "t2"})
	plannerInvalidSkillIDKey    = prompt.Define("guard.planner_invalid_skill_id", taskSkillData{TaskID: "t1", SkillID: "s1"})
	plannerSkillNotOwnedKey     = prompt.Define("guard.planner_skill_not_owned", taskSkillAgentData{TaskID: "t1", SkillID: "s1", AgentID: "a1"})
	plannerUnknownDependencyKey = prompt.Define("guard.planner_unknown_dependency", taskDepData{TaskID: "t1", DepID: "t9"})
	plannerDisjointWritesKey    = prompt.Define("guard.planner_disjoint_writes",
		disjointWritesData{Group: 0, Count: 2, Writers: "t1(create_board_task), t2(move_board_task)"})
	plannerBookkeepingOnlyKey = prompt.Define("guard.planner_bookkeeping_only",
		bookkeepingOnlyData{TaskID: "t1", QuotedTitle: `"sample"`, Tools: "move_board_task"})
	plannerMultipleCreatorsKey = prompt.Define("guard.planner_multiple_creators",
		multipleCreatorsData{Count: 2, IDs: "t1, t2", Tool: createBoardTaskTool})
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
		return errors.New(prompt.Text(plannerMissingSummaryKey))
	}
	if len(output.Tasks) == 0 {
		return errors.New(prompt.Text(plannerNoTasksKey))
	}
	if len(output.Tasks) > maxTasks {
		return errors.New(plannerTooManyTasksKey.Render(maxTasksData{MaxTasks: maxTasks}))
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
			return errors.New(prompt.Text(plannerTaskMissingIDKey))
		}
		if t.Title == "" {
			return errors.New(plannerTaskMissingTitle.Render(taskIDData{TaskID: t.ID}))
		}
		if t.Description == "" {
			return errors.New(plannerTaskMissingDesc.Render(taskIDData{TaskID: t.ID}))
		}
		if t.AgentID == "" {
			return errors.New(plannerTaskMissingAgent.Render(taskIDData{TaskID: t.ID}))
		}
		if !agentIDs[t.AgentID] {
			return errors.New(plannerUnknownAgentKey.Render(taskAgentData{TaskID: t.ID, AgentID: t.AgentID}))
		}
		if seen[t.ID] {
			return errors.New(plannerDuplicateTaskIDKey.Render(taskIDData{TaskID: t.ID}))
		}
		if priorIDs[t.ID] {
			return errors.New(plannerReusedPriorIDKey.Render(taskIDData{TaskID: t.ID}))
		}
		if priorID, clash := priorTitles[normalizeTitle(t.Title)]; clash {
			return errors.New(plannerDuplicateTitlePriorKey.Render(duplicateTitlePriorData{
				TaskID: t.ID, PriorID: priorID, QuotedTitle: fmt.Sprintf("%q", t.Title),
			}))
		}
		// Long enough to be an instruction, not a stub.
		if key := normalizeTitle(t.Description); len(key) >= duplicateDescriptionMinLen {
			if priorID, clash := priorDescriptions[key]; clash {
				return errors.New(plannerDuplicateDescriptionPriorKey.Render(duplicateDescriptionPriorData{TaskID: t.ID, PriorID: priorID}))
			}
			if otherID, clash := seenDescriptions[key]; clash {
				return errors.New(plannerDuplicateDescriptionTurnKey.Render(duplicateDescriptionTurnData{OtherID: otherID, TaskID: t.ID}))
			}
			seenDescriptions[key] = t.ID
		}
		seen[t.ID] = true
		owned := skillOwnership[t.AgentID]
		for _, sid := range t.SkillIDs {
			if _, err := uuid.Parse(sid); err != nil {
				return errors.New(plannerInvalidSkillIDKey.Render(taskSkillData{TaskID: t.ID, SkillID: sid}))
			}
			if owned != nil && !owned[sid] {
				return errors.New(plannerSkillNotOwnedKey.Render(taskSkillAgentData{TaskID: t.ID, SkillID: sid, AgentID: t.AgentID}))
			}
		}
	}

	// A dependency may name this turn OR an id the run already completed; the replanner prompt relies on that.
	for _, t := range output.Tasks {
		for _, dep := range t.DependsOn {
			if !seen[dep] && !priorIDs[dep] {
				return errors.New(plannerUnknownDependencyKey.Render(taskDepData{TaskID: t.ID, DepID: dep}))
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
		return errors.New(plannerBookkeepingOnlyKey.Render(bookkeepingOnlyData{
			TaskID: t.ID, QuotedTitle: fmt.Sprintf("%q", t.Title), Tools: strings.Join(t.ToolNames, ", "),
		}))
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
	return errors.New(plannerMultipleCreatorsKey.Render(multipleCreatorsData{
		Count: len(creators), IDs: strings.Join(creators, ", "), Tool: createBoardTaskTool,
	}))
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
		return errors.New(plannerDisjointWritesKey.Render(disjointWritesData{
			Group: group, Count: len(writers), Writers: strings.Join(ids, ", "),
		}))
	}
	return nil
}

type plannerAgentSkillData struct{ ID, Name, Category, Description string }
type plannerAgentRuleData struct{ Name, Content string }
type plannerAgentData struct {
	ID, Name, Type, Description string
	HasSkills                   bool
	Skills                      []plannerAgentSkillData
	Rules                       []plannerAgentRuleData
}
type plannerRelevantSkillData struct{ ID, AgentID, Name, Category, Description string }

type plannerSystemData struct {
	Purpose, Goal      string
	Workspace          string
	SoloMode           bool
	ConstrainedAgentID string
	LanguageRule       string
	Agents             []plannerAgentData
	RelevantSkills     []plannerRelevantSkillData
}

var plannerSystemKey = prompt.Define("orchestrator.planner_system", plannerSystemData{
	Purpose: "p", Goal: "g", LanguageRule: "lang",
	Agents: []plannerAgentData{{ID: "a1", Name: "n", Type: "t", Description: "d", HasSkills: true,
		Skills: []plannerAgentSkillData{{ID: "s1", Name: "n", Category: "c", Description: "d"}},
		Rules:  []plannerAgentRuleData{{Name: "n", Content: "c"}}}},
})

func buildPlannerSystemPrompt(intake domain.GoalIntake, catalogs []agentCatalogEntry, relevantSkills []domain.Skill, opts PlannerOptions) string {
	data := plannerSystemData{
		Purpose:      intake.Purpose,
		Goal:         intake.Goal,
		Workspace:    opts.Workspace,
		LanguageRule: prompt.UserFacingLanguageRule(opts.Lang),
	}
	if opts.ConstrainedAgentID != nil {
		data.SoloMode = true
		data.ConstrainedAgentID = opts.ConstrainedAgentID.String()
	}
	for _, entry := range catalogs {
		a := entry.Agent
		ad := plannerAgentData{
			ID: a.ID.String(), Name: a.Name, Type: a.SubagentType, Description: a.Description,
			HasSkills: len(entry.Skills) > 0,
		}
		for _, sk := range entry.Skills {
			if sk.Enabled {
				ad.Skills = append(ad.Skills, plannerAgentSkillData{ID: sk.ID.String(), Name: sk.Name, Category: sk.Category, Description: sk.Description})
			}
		}
		for _, r := range entry.Rules {
			ad.Rules = append(ad.Rules, plannerAgentRuleData{Name: r.Name, Content: r.Content})
		}
		data.Agents = append(data.Agents, ad)
	}
	for _, sk := range relevantSkills {
		if sk.Enabled {
			data.RelevantSkills = append(data.RelevantSkills, plannerRelevantSkillData{
				ID: sk.ID.String(), AgentID: sk.AgentID.String(), Name: sk.Name, Category: sk.Category, Description: sk.Description,
			})
		}
	}
	return plannerSystemKey.Render(data)
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
