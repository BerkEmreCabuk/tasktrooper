package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	PlanStatusPending   = "pending"
	PlanStatusRunning   = "running"
	PlanStatusCompleted = "completed"
	PlanStatusFailed    = "failed"
	// PlanStatusIncomplete is a plan that ran all the way to the end and whose
	// result was never confirmed — the verifier's last word was that the goal
	// was not met, and no repair round fixed it. Nothing died: the subtasks ran,
	// the output exists and is handed back to the stakeholder. "failed" is the
	// other thing entirely — the process stopped (an executor error, a pod
	// replaced under a live run, an exit that settled nothing) — and carrying
	// both on one status is why a plan outcome could not be acted on: you could
	// not tell a run that broke from one that merely went unverified. Same line
	// the domain already draws one level down at TaskStatusIncomplete. Terminal
	// like "completed" and "failed": nothing waits on it, and only a plan still
	// claiming to run gets closed out by the reconciler.
	PlanStatusIncomplete = "incomplete"

	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	// TaskStatusIncomplete is a subtask that returned output without doing what
	// it was for — the canonical case is one whose whole tool ledger was "claim
	// the task, move it to in_progress". The run does not stop for it: the result
	// is kept and the plan carries on. Recording that as "failed" is what put a
	// red cross on a subtask of a run that was still working, so it gets its own
	// status. Like "failed" it is non-terminal for a resume, which re-runs
	// anything that is not completed.
	TaskStatusIncomplete = "incomplete"
)

type Skill struct {
	ID          uuid.UUID `json:"id"`
	AgentID     uuid.UUID `json:"agent_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Tags        []string  `json:"tags"`
	Content     string    `json:"content"`
	Embedding   []float32 `json:"embedding,omitempty"`
	Enabled     bool      `json:"enabled"`
	// TechStackID is the one TechStack of this agent the skill belongs to.
	// Nil is the general skill, which is a different thing from "unset" and is
	// why the field is always rendered.
	TechStackID *uuid.UUID `json:"tech_stack_id"`
	CreatedAt   time.Time  `json:"created_at"`
}

type Agent struct {
	ID           uuid.UUID       `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	SubagentType string          `json:"subagent_type"`
	SystemPrompt string          `json:"system_prompt"`
	ProviderType LLMProviderType `json:"provider_type"`
	Model        string          `json:"model"`
	// ModelHeavy is an optional stronger model used for subtasks the planner
	// rates as "hard". Empty = always use Model. The planner itself runs on the
	// session's model and decides each subtask's difficulty; the executor then
	// picks Model vs ModelHeavy.
	ModelHeavy string `json:"model_heavy"`
	// MaxTurns caps one claude_code CLI session for this agent. 0 = the
	// executor's own default. It is per-agent because a session's cost grows
	// with the SQUARE of its turns — every turn resends the whole transcript —
	// so one global ceiling was priced for the most open-ended agent and
	// charged to all of them. A reviewer that reads and reports has no use for
	// the turns an implementer needs.
	MaxTurns int `json:"max_turns"`
	// Effort is the CLI's --effort level (low, medium, high, xhigh, max) for
	// this agent. Empty = the CLI's own default. It is the most direct control
	// over how much a session thinks, and the right level is a property of the
	// work: a verification pass and a multi-file refactor do not want the same
	// one.
	Effort               string      `json:"effort"`
	ToolPolicy           ToolPolicy  `json:"tool_policy"`
	SkillIDs             []uuid.UUID `json:"skill_ids"`
	Enabled              bool        `json:"enabled"`
	SelfEvolutionEnabled bool        `json:"self_evolution_enabled"`
	CreatedAt            time.Time   `json:"created_at"`
}

type OrchestratorRule struct {
	ID        uuid.UUID `json:"id"`
	AgentID   uuid.UUID `json:"agent_id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	Priority  int       `json:"priority"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type OrchestrationPlan struct {
	ID        uuid.UUID       `json:"id"`
	RunID     uuid.UUID       `json:"run_id"`
	Status    string          `json:"status"`
	Summary   string          `json:"summary"`
	PlanJSON  json.RawMessage `json:"plan_json,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type GoalIntake struct {
	Ready       bool                    `json:"ready"`
	Purpose     string                  `json:"purpose"`
	Goal        string                  `json:"goal"`
	Constraints []string                `json:"constraints"`
	Questions   []ClarificationQuestion `json:"questions"`
}

type VerificationResult struct {
	Passed  bool     `json:"passed"`
	Issues  []string `json:"issues"`
	Summary string   `json:"summary"`
}

type PlanTask struct {
	ID          uuid.UUID   `json:"id"`
	PlanID      uuid.UUID   `json:"plan_id"`
	TaskKey     string      `json:"task_key"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	AgentID     *uuid.UUID  `json:"agent_id,omitempty"`
	SkillIDs    []uuid.UUID `json:"skill_ids"`
	ToolNames   []string    `json:"tool_names"`
	DependsOn   []string    `json:"depends_on"`
	// Difficulty is the planner's rating of this subtask ("easy"/"hard"); the
	// executor uses it to pick the assigned agent's Model vs ModelHeavy.
	Difficulty string    `json:"difficulty,omitempty"`
	Status     string    `json:"status"`
	Result     string    `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type PlanView struct {
	ID           uuid.UUID           `json:"id"`
	RunID        uuid.UUID           `json:"run_id"`
	Status       string              `json:"status"`
	Summary      string              `json:"summary"`
	Purpose      string              `json:"purpose,omitempty"`
	Goal         string              `json:"goal,omitempty"`
	Verification *VerificationResult `json:"verification,omitempty"`
	Tasks        []PlanTask          `json:"tasks"`
}

type CreateSkillRequest struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags"`
	Content     string     `json:"content"`
	Enabled     bool       `json:"enabled"`
	TechStackID *uuid.UUID `json:"tech_stack_id"`
}

// UpdateSkillRequest replaces every field, TechStackID included — omitting it
// files the skill as general, the same way omitting Tags clears them.
type UpdateSkillRequest struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags"`
	Content     string     `json:"content"`
	Enabled     bool       `json:"enabled"`
	TechStackID *uuid.UUID `json:"tech_stack_id"`
}

type CreateAgentRequest struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	SubagentType string          `json:"subagent_type"`
	SystemPrompt string          `json:"system_prompt"`
	ProviderType LLMProviderType `json:"provider_type"`
	Model        string          `json:"model"`
	ModelHeavy   string          `json:"model_heavy"`
	// MaxTurns caps one claude_code CLI session for this agent. 0 = the
	// executor's own default. It is per-agent because a session's cost grows
	// with the SQUARE of its turns — every turn resends the whole transcript —
	// so one global ceiling was priced for the most open-ended agent and
	// charged to all of them. A reviewer that reads and reports has no use for
	// the turns an implementer needs.
	MaxTurns int `json:"max_turns"`
	// Effort is the CLI's --effort level (low, medium, high, xhigh, max) for
	// this agent. Empty = the CLI's own default. It is the most direct control
	// over how much a session thinks, and the right level is a property of the
	// work: a verification pass and a multi-file refactor do not want the same
	// one.
	Effort               string     `json:"effort"`
	ToolPolicy           ToolPolicy `json:"tool_policy"`
	Enabled              bool       `json:"enabled"`
	SelfEvolutionEnabled bool       `json:"self_evolution_enabled"`
}

type UpdateAgentRequest struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	SubagentType string          `json:"subagent_type"`
	SystemPrompt string          `json:"system_prompt"`
	ProviderType LLMProviderType `json:"provider_type"`
	Model        string          `json:"model"`
	ModelHeavy   string          `json:"model_heavy"`
	// MaxTurns caps one claude_code CLI session for this agent. 0 = the
	// executor's own default. It is per-agent because a session's cost grows
	// with the SQUARE of its turns — every turn resends the whole transcript —
	// so one global ceiling was priced for the most open-ended agent and
	// charged to all of them. A reviewer that reads and reports has no use for
	// the turns an implementer needs.
	MaxTurns int `json:"max_turns"`
	// Effort is the CLI's --effort level (low, medium, high, xhigh, max) for
	// this agent. Empty = the CLI's own default. It is the most direct control
	// over how much a session thinks, and the right level is a property of the
	// work: a verification pass and a multi-file refactor do not want the same
	// one.
	Effort               string     `json:"effort"`
	ToolPolicy           ToolPolicy `json:"tool_policy"`
	Enabled              bool       `json:"enabled"`
	SelfEvolutionEnabled bool       `json:"self_evolution_enabled"`
}

type CreateOrchestratorRuleRequest struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

type UpdateOrchestratorRuleRequest struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

type PlannerTask struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	AgentID      string   `json:"agent_id"`
	SkillIDs     []string `json:"skill_ids"`
	ToolNames    []string `json:"tool_names"`
	SubtaskRules []string `json:"subtask_rules"`
	DependsOn    []string `json:"depends_on"`
	// Difficulty ("easy"/"hard") drives per-subtask model selection: the
	// executor runs "hard" subtasks on the assigned agent's ModelHeavy.
	Difficulty    string `json:"difficulty"`
	ParallelGroup int    `json:"parallel_group"`
}

type PlannerOutput struct {
	Ready     bool                    `json:"ready"`
	Purpose   string                  `json:"purpose"`
	Goal      string                  `json:"goal"`
	Summary   string                  `json:"summary"`
	Tasks     []PlannerTask           `json:"tasks"`
	Questions []ClarificationQuestion `json:"questions"`
}

const (
	SubtaskHistoryModeIsolated = "isolated"
	SubtaskHistoryModeFull     = "full"
)

// Subtask difficulty ratings emitted by the planner and consumed by the
// executor to pick between an agent's default Model and its ModelHeavy.
const (
	TaskDifficultyEasy = "easy"
	TaskDifficultyHard = "hard"
)

type OrchestrationConfig struct {
	Enabled                  bool   `koanf:"enabled"`
	FastPath                 bool   `koanf:"fast_path"`
	MaxParallelTasks         int    `koanf:"max_parallel_tasks"`
	MaxPlanTasks             int    `koanf:"max_plan_tasks"`
	SkillRetrievalTopK       int    `koanf:"skill_retrieval_top_k"`
	SubtaskHistoryMode       string `koanf:"subtask_history_mode"`
	DependencyOutputMaxChars int    `koanf:"dependency_output_max_chars"`
	SynthesisEnabled         bool   `koanf:"synthesis_enabled"`
	VerificationEnabled      bool   `koanf:"verification_enabled"`
	MaxReplanIterations      int    `koanf:"max_replan_iterations"`
}
