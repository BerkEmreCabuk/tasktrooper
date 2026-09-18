package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// RoleStore is the admin CRUD surface for roles, their agent assignments and
// the system-purpose mapping. Reads used by the engine's hot path go through
// RoleResolver instead, which is backed by an in-memory snapshot rather than
// these queries.
type RoleStore interface {
	List(ctx context.Context) ([]domain.AgentRole, error)
	Get(ctx context.Context, id uuid.UUID) (domain.AgentRole, error)
	Create(ctx context.Context, role domain.AgentRole) (domain.AgentRole, error)
	Update(ctx context.Context, role domain.AgentRole) (domain.AgentRole, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// SetAssignments replaces a role's whole assignment list.
	SetAssignments(ctx context.Context, roleID uuid.UUID, assignments []domain.RoleAssignment) error
	// ListAssignmentsByAgent returns every (role, areas, priority) an agent
	// holds, across all roles — the read GET /v1/agents/:agentId/roles serves.
	ListAssignmentsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.AgentRole, error)
	// SetAgentRoles is the agent-centric write of the same relationship
	// SetAssignments writes role-centrically: it replaces every role
	// membership this agent holds with the given list, touching no other
	// agent's assignment to those (or any other) roles.
	SetAgentRoles(ctx context.Context, agentID uuid.UUID, roles []domain.AgentRoleMembership) error
	ListPurposes(ctx context.Context) ([]domain.RolePurpose, error)
	SetPurpose(ctx context.Context, purpose domain.RolePurposeKey, roleID *uuid.UUID) error
}

// WorkflowStore is the admin CRUD surface for task types and their per-column
// workflow stages.
type WorkflowStore interface {
	ListTaskTypes(ctx context.Context) ([]domain.TaskTypeDef, error)
	GetTaskType(ctx context.Context, key domain.TaskType) (domain.TaskTypeDef, error)
	// CreateTaskType inserts a new type. cloneFrom, when non-empty, copies the
	// named existing type's stages onto the new type before returning.
	CreateTaskType(ctx context.Context, def domain.TaskTypeDef, cloneFrom domain.TaskType) (domain.TaskTypeDef, error)
	UpdateTaskType(ctx context.Context, def domain.TaskTypeDef) (domain.TaskTypeDef, error)
	DeleteTaskType(ctx context.Context, key domain.TaskType) error
	ListStages(ctx context.Context, taskType domain.TaskType) ([]domain.WorkflowStage, error)
	ReplaceStages(ctx context.Context, taskType domain.TaskType, stages []domain.WorkflowStage) error
	// LoadAll reads every task type with its stages in one pass — what
	// application/workflow.Service loads at boot and after every write to
	// build its in-memory snapshot.
	LoadAll(ctx context.Context) ([]domain.Workflow, error)
	// ColumnHasBehaviourStages reports whether any workflow stage (any task
	// type) at this column slug carries at least one behaviour — what
	// workspace.Service.UpdateColumns refuses to orphan by removing the
	// column. A stage with zero behaviours is the same as no stage at all
	// (Workflow.Stage's documented fallback), so it does not block removal.
	ColumnHasBehaviourStages(ctx context.Context, slug string) (bool, error)
}

// WorkflowReader is the only workflow surface the engine's dispatch/gate path
// sees — a read against an in-memory snapshot, never the database, so a board
// move never pays a query to ask "does this stage have build_verify".
//
// Workflow for an unknown task type returns the DEFAULT type's workflow
// rather than an error: a task created before its type existed, or one whose
// type was since deleted, still needs somewhere to route.
//
// An EMPTY cache (nothing loaded yet, or the last reload failed) makes every
// method return an error instead of silently answering with nothing — gates
// built on Has/Param fail CLOSED, and a stage that is merely missing from an
// otherwise-loaded snapshot must never read the same as "the load itself
// never happened".
type WorkflowReader interface {
	Workflow(ctx context.Context, taskType domain.TaskType) (domain.Workflow, error)
	DefaultTaskType(ctx context.Context) (domain.TaskType, error)
	DefectTaskType(ctx context.Context) (domain.TaskType, error)
	TaskTypeExists(ctx context.Context, taskType domain.TaskType) (bool, error)
	KeyPrefix(ctx context.Context, taskType domain.TaskType) (string, error)
}

// RoleResolver answers "which agent holds this role/purpose for this area",
// backed by the same in-memory snapshot WorkflowReader reads.
type RoleResolver interface {
	// AgentForRole resolves the role's assignment for area: an
	// area-scoped assignment wins over one with Areas == nil (any area);
	// ties break on Priority then on assignment order. nil, nil when the role
	// has no assignment covering the area.
	AgentForRole(ctx context.Context, roleID uuid.UUID, area string) (*uuid.UUID, error)
	AgentForPurpose(ctx context.Context, purpose domain.RolePurposeKey, area string) (*uuid.UUID, error)
	// AgentArea is the single area of the agent's own area-scoped assignment
	// (across any role), or "" when the agent has none or covers more than
	// one — replacing the substring name-match profileKindForAgent used to do.
	AgentArea(ctx context.Context, agentID uuid.UUID) string
	// AssigneeForNewTask resolves CreateTask's assignee for a new task of
	// taskType in area, given what the caller requested (nil/empty for
	// "nothing requested"). Reads the type's assignee_mode: none returns
	// requested unchanged; default fills only when requested is nil; override
	// always prefers the role's agent for area, falling back to requested
	// when the role has nobody there.
	AssigneeForNewTask(ctx context.Context, taskType domain.TaskType, area string, requested *uuid.UUID) (*uuid.UUID, error)
}
