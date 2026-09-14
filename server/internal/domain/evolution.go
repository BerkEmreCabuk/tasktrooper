package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	ReflectionTriggerPeriodic = "periodic"
	ReflectionTriggerRevision = "revision"
	ReflectionTriggerManual   = "manual"

	ReflectionStatusRunning   = "running"
	ReflectionStatusCompleted = "completed"
	ReflectionStatusFailed    = "failed"

	EvolutionImpactPending          = "pending"
	EvolutionImpactEffective        = "effective"
	EvolutionImpactRegressed        = "regressed"
	EvolutionImpactNeutral          = "neutral"
	EvolutionImpactInsufficientData = "insufficient_data"

	EvolutionChangeSkillCreated  = "skill_created"
	EvolutionChangeSkillUpdated  = "skill_updated"
	EvolutionChangeSkillDeleted  = "skill_deleted"
	EvolutionChangeRuleCreated   = "rule_created"
	EvolutionChangeRuleUpdated   = "rule_updated"
	EvolutionChangeRuleDeleted   = "rule_deleted"
	EvolutionChangeMemoryCreated = "memory_created"
	EvolutionChangeMemoryDeleted = "memory_deleted"
	EvolutionChangeRevert        = "revert"

	EvolutionTargetSkill  = "skill"
	EvolutionTargetRule   = "rule"
	EvolutionTargetMemory = "memory"
)

// PerformanceSnapshot is stored on a completed reflection and serves as the
// baseline for the next reflection (incremental analysis, no re-review).
type PerformanceSnapshot struct {
	Score          float64            `json:"score"`
	KPIComposite   float64            `json:"kpi_composite"`
	KPIs           map[string]float64 `json:"kpis,omitempty"` // metric_key -> attainment
	GoldenPassRate *float64           `json:"golden_pass_rate,omitempty"`
	// GoldenPassRateBefore is the same suite run against the pre-change
	// skills/rules — the pair is what the gate judged the changes on.
	GoldenPassRateBefore *float64  `json:"golden_pass_rate_before,omitempty"`
	CapturedAt           time.Time `json:"captured_at"`
}

type AgentReflection struct {
	ID                  uuid.UUID            `json:"id"`
	AgentID             uuid.UUID            `json:"agent_id"`
	Trigger             string               `json:"trigger"`
	Status              string               `json:"status"`
	WindowStart         time.Time            `json:"window_start"`
	WindowEnd           time.Time            `json:"window_end"`
	Summary             string               `json:"summary"`
	PerformanceSnapshot *PerformanceSnapshot `json:"performance_snapshot,omitempty"`
	RawOutput           string               `json:"raw_output,omitempty"`
	Error               string               `json:"error,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	CompletedAt         *time.Time           `json:"completed_at,omitempty"`
}

type AgentEvolutionEvent struct {
	ID                uuid.UUID       `json:"id"`
	ReflectionID      *uuid.UUID      `json:"reflection_id,omitempty"`
	AgentID           uuid.UUID       `json:"agent_id"`
	ChangeType        string          `json:"change_type"`
	TargetKind        string          `json:"target_kind"`
	TargetID          *uuid.UUID      `json:"target_id,omitempty"`
	TargetName        string          `json:"target_name"`
	Before            json.RawMessage `json:"before,omitempty"`
	After             json.RawMessage `json:"after,omitempty"`
	RevertedEventID   *uuid.UUID      `json:"reverted_event_id,omitempty"`
	ScoreAtChange     float64         `json:"score_at_change"`
	Impact            string          `json:"impact"`
	ImpactEvaluatedAt *time.Time      `json:"impact_evaluated_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
}

// ReflectionOutput is the JSON contract the reflection LLM call must produce.
type ReflectionOutput struct {
	SelfAssessment string                   `json:"self_assessment"`
	Skills         []ReflectionSkillChange  `json:"skills"`
	Rules          []ReflectionRuleChange   `json:"rules"`
	Memories       []ReflectionMemoryChange `json:"memories"`
	Reverts        []ReflectionRevert       `json:"reverts"`
}

type ReflectionSkillChange struct {
	Action      string   `json:"action"` // create | update | delete
	SkillID     string   `json:"skill_id,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Content     string   `json:"content"`
	SourceURLs  []string `json:"source_urls,omitempty"`
}

type ReflectionRuleChange struct {
	Action   string `json:"action"` // create | update | delete
	RuleID   string `json:"rule_id,omitempty"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority int    `json:"priority"`
}

type ReflectionMemoryChange struct {
	Action   string `json:"action"` // create | delete
	MemoryID string `json:"memory_id,omitempty"`
	Content  string `json:"content"`
	Category string `json:"category"`
}

type ReflectionRevert struct {
	EvolutionEventID string `json:"evolution_event_id"`
	Reason           string `json:"reason,omitempty"`
}

// MemoryPromotion is one team memory that was turned into a skill: the skill
// now exists in every listed agent's catalog and the memory itself is gone.
type MemoryPromotion struct {
	MemoryID  uuid.UUID `json:"memory_id"`
	SkillName string    `json:"skill_name"`
	Agents    []string  `json:"agents"`
}

// MemoryPromotionResult is what a shared-memory promotion sweep did.
type MemoryPromotionResult struct {
	Scanned  int               `json:"scanned"`
	Promoted []MemoryPromotion `json:"promoted"`
}

// MemoryPromotionCandidate is one proposed memory→skill move: everything the
// operator needs to judge it before anything is written, and everything the
// apply step needs to carry it out. The plan and the apply speak the same
// shape on purpose — what the operator approved is exactly what is written,
// rather than a second LLM call that might decide differently.
type MemoryPromotionCandidate struct {
	MemoryID uuid.UUID `json:"memory_id"`
	// MemoryContent is the memory as it stands today, so the dialog can show
	// what is about to be turned into a skill and deleted.
	MemoryContent string   `json:"memory_content"`
	SkillName     string   `json:"skill_name"`
	Description   string   `json:"description"`
	Category      string   `json:"category"`
	Content       string   `json:"content"`
	Agents        []string `json:"agents"`
}

// MemoryPromotionPlan is what a promotion WOULD do. Nothing is written to
// produce it: no skill is created and no memory is deleted until the operator
// sends the candidates back to the apply endpoint.
type MemoryPromotionPlan struct {
	Scanned    int                        `json:"scanned"`
	Candidates []MemoryPromotionCandidate `json:"candidates"`
	// Agents is the roster the skills would land on, so the dialog can say who
	// is affected even when a candidate targets everyone.
	Agents []string `json:"agents"`
}
