package domain

import (
	"sort"

	"github.com/google/uuid"
)

// AssigneeMode is how a task type's assignee_role_id is applied when a task of
// that type is created.
type AssigneeMode string

const (
	// AssigneeModeNone never assigns from the role: the requested assignee (or
	// none) stands, exactly as task/bug/technical behave today.
	AssigneeModeNone AssigneeMode = "none"
	// AssigneeModeDefault fills the assignee from the role only when nothing
	// was requested.
	AssigneeModeDefault AssigneeMode = "default"
	// AssigneeModeOverride always assigns from the role, the way an analiz
	// task's assignee is resolved today (resolveAnalizAssignee): the role's
	// agent for the task's area wins even over a requested assignee, falling
	// back to the requested one when the role has nobody for that area.
	AssigneeModeOverride AssigneeMode = "override"
)

func ValidAssigneeMode(m AssigneeMode) bool {
	switch m {
	case AssigneeModeNone, AssigneeModeDefault, AssigneeModeOverride:
		return true
	default:
		return false
	}
}

// StageKind classifies what a column is FOR, independent of which type it
// belongs to — the engine reads Kind where it used to compare a column
// literal (e.g. "is this a review column").
type StageKind string

const (
	StageKindIntake   StageKind = "intake"
	StageKindQueue    StageKind = "queue"
	StageKindWork     StageKind = "work"
	StageKindReview   StageKind = "review"
	StageKindApproval StageKind = "approval"
	StageKindRework   StageKind = "rework"
	StageKindParked   StageKind = "parked"
	StageKindTerminal StageKind = "terminal"
)

func ValidStageKind(k StageKind) bool {
	switch k {
	case StageKindIntake, StageKindQueue, StageKindWork, StageKindReview,
		StageKindApproval, StageKindRework, StageKindParked, StageKindTerminal:
		return true
	default:
		return false
	}
}

// ParticipantMode is whether a role does the work in a stage or signs off on
// it.
type ParticipantMode string

const (
	ParticipantModeWorker   ParticipantMode = "worker"
	ParticipantModeApprover ParticipantMode = "approver"
)

func ValidParticipantMode(m ParticipantMode) bool {
	switch m {
	case ParticipantModeWorker, ParticipantModeApprover:
		return true
	default:
		return false
	}
}

// BehaviourRef is one behaviour attached to a stage or a task type, with the
// params the behaviour declares it needs (see BehaviourRegistry). Params are
// always strings on the wire — a column slug, an enum option, "true"/"false"
// for a bool param — because they round-trip through jsonb and an HTTP form
// the same way regardless of the param's logical type.
type BehaviourRef struct {
	Key    BehaviourKey
	Params map[string]string
}

// TaskTypeDef is a task type as data: what used to be one of the four
// TaskType* constants is now a row a human can add, rename (label/prefix) and
// attach type-scoped behaviours to.
type TaskTypeDef struct {
	Key            TaskType
	Label          string
	KeyPrefix      string
	Position       int
	IsDefault      bool
	IsDefect       bool
	AssigneeRoleID *uuid.UUID
	AssigneeMode   AssigneeMode
	Behaviours     []BehaviourRef
	BuiltIn        bool
	// TaskCount is populated on reads that need it (list/get), never written —
	// it is what a delete/prefix-change refusal is judged against.
	TaskCount int
}

func (t TaskTypeDef) Has(key BehaviourKey) bool {
	for _, b := range t.Behaviours {
		if b.Key == key {
			return true
		}
	}
	return false
}

// StageParticipant names one role's part in a stage: at most one worker role
// and one approver role per stage (enforced by workflow.ValidateStages), so a
// stage's roster is never ambiguous about who does the work versus who signs
// off.
type StageParticipant struct {
	RoleID       uuid.UUID
	Mode         ParticipantMode
	Instructions string
	Position     int
}

// WorkflowStage is one column's behaviour for one task type. A column with no
// stage row for a type carries no behaviours and routes to the task's
// assignee — today's fallback for a custom column nothing else claims.
type WorkflowStage struct {
	ID           uuid.UUID
	TaskType     TaskType
	Column       TaskColumn
	Position     int
	OnPath       bool
	Kind         StageKind
	Behaviours   []BehaviourRef
	Instructions string
	Participants []StageParticipant
}

func (s WorkflowStage) Has(key BehaviourKey) bool {
	for _, b := range s.Behaviours {
		if b.Key == key {
			return true
		}
	}
	return false
}

func (s WorkflowStage) Param(key BehaviourKey, name string) (string, bool) {
	for _, b := range s.Behaviours {
		if b.Key != key {
			continue
		}
		v, ok := b.Params[name]
		return v, ok
	}
	return "", false
}

// Workflow is one task type's full set of stages — the shape every gate,
// dispatch decision and prompt used to read off hardcoded column/type
// literals now asks of instead.
type Workflow struct {
	Type   TaskTypeDef
	Stages []WorkflowStage
}

// Stage returns the stage for a column, and false when the column has no row
// for this type (the "custom column, no behaviours" case).
func (w Workflow) Stage(col TaskColumn) (WorkflowStage, bool) {
	for _, s := range w.Stages {
		if s.Column == col {
			return s, true
		}
	}
	return WorkflowStage{}, false
}

// Has reports whether the stage at col carries the named behaviour. A column
// with no stage row never has any behaviour — the same "unclaimed column"
// fallback Stage documents.
func (w Workflow) Has(col TaskColumn, key BehaviourKey) bool {
	stage, ok := w.Stage(col)
	return ok && stage.Has(key)
}

// Param reads one param of a stage behaviour. The bool is false when the
// column has no stage, the stage lacks the behaviour, or the behaviour has no
// such param — three different absences a caller usually treats the same way
// (fall back to a default), which is why they collapse into one bool here.
func (w Workflow) Param(col TaskColumn, key BehaviourKey, name string) (string, bool) {
	stage, ok := w.Stage(col)
	if !ok {
		return "", false
	}
	return stage.Param(key, name)
}

// TypeHas reports whether the task type itself (not a stage) carries the
// named behaviour — the (type)-scoped rows of BehaviourRegistry.
func (w Workflow) TypeHas(key BehaviourKey) bool {
	return w.Type.Has(key)
}

// KindOf is StageKind for a column, or "" when the column has no stage.
func (w Workflow) KindOf(col TaskColumn) StageKind {
	stage, ok := w.Stage(col)
	if !ok {
		return ""
	}
	return stage.Kind
}

// sortedOnPath returns the on_path stages ordered by Position — the spine of
// the type's lifecycle, off-path drag targets (need_revision, blocked, a
// custom column) excluded.
func (w Workflow) sortedOnPath() []WorkflowStage {
	out := make([]WorkflowStage, 0, len(w.Stages))
	for _, s := range w.Stages {
		if s.OnPath {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

// NextOnPath is the next stage after col on the on-path spine — what
// auto_enter/advance_on_diff/advance_on_document used to name as a literal
// column. False when col is not on the spine, or is its last stage.
func (w Workflow) NextOnPath(col TaskColumn) (TaskColumn, bool) {
	path := w.sortedOnPath()
	for i, s := range path {
		if s.Column == col && i+1 < len(path) {
			return path[i+1].Column, true
		}
	}
	return "", false
}

// WorkColumn is the first on-path stage of kind "work" — the column an
// implementer actually writes code (or, for analiz, documents) in. It is what
// verify.go's build-gate fix round used to ask for as domain.TaskColumnInProgress
// literally.
func (w Workflow) WorkColumn() (TaskColumn, bool) {
	for _, s := range w.sortedOnPath() {
		if s.Kind == StageKindWork {
			return s.Column, true
		}
	}
	return "", false
}

// ReviewChain is the ordered list of stages this type's review_chain_stage
// behaviour marks mandatory — what domain.ReviewChainForType used to return
// as a hardcoded per-type slice. Ordered by the stage's on-path position so a
// block message lists them in lifecycle order.
func (w Workflow) ReviewChain() []ReviewStage {
	path := w.sortedOnPath()
	order := make(map[TaskColumn]int, len(path))
	for i, s := range path {
		order[s.Column] = i
	}
	var chain []ReviewStage
	for _, s := range w.Stages {
		label, hasLabel := s.Param(BehaviourReviewChainStage, "label")
		if !hasLabel {
			continue
		}
		remedy, _ := s.Param(BehaviourReviewChainStage, "remedy")
		chain = append(chain, ReviewStage{Column: s.Column, Label: label, Remedy: remedy})
	}
	sort.SliceStable(chain, func(i, j int) bool {
		oi, oki := order[chain[i].Column]
		oj, okj := order[chain[j].Column]
		if !oki || !okj {
			return false
		}
		return oi < oj
	})
	return chain
}
