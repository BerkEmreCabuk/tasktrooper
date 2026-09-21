package catalog

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// QA owns all four of its columns. Subscribed to ready_for_qa alone, in_qa was
// unowned: a task moved there resolved back to the implementer-assignee, so the
// developer was dispatched onto the branch QA had just started testing.
//
// done and released are the odd ones: QA does no testing there, it merges the
// task's pull request (done) and migration 105 backfilled released for parity
// on installs seeded before it. The columns still dispatch nobody for
// anything else — the dispatcher wakes this subscription only for a move
// into done on a task whose PR is unmerged (board.doneMergeWake).
//
// This exercises the path a user actually takes now: creating the agent from
// its template (CreateAgentFromTemplate), not a boot-time reconcile.
func TestCreateAgentFromTemplate_QAOwnsItsFourColumns(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetBoardConfigStore(board)
	ctx := context.Background()

	qaTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "qa-agent", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{
			domain.TaskColumnReadyForQA, domain.TaskColumnInQA,
			domain.TaskColumnDone, domain.TaskColumnReleased,
		},
	})
	require.NoError(t, err)
	agent, err := svc.CreateAgentFromTemplate(ctx, qaTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{
			string(domain.TaskColumnReadyForQA), string(domain.TaskColumnInQA),
			string(domain.TaskColumnDone), string(domain.TaskColumnReleased),
		},
		board.subs[agent.ID])
}

// system-architect and product-manager get their own single column the same
// way.
func TestCreateAgentFromTemplate_ArchitectAndPMGetTheirColumn(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetBoardConfigStore(board)
	ctx := context.Background()

	architectTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "system-architect", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{domain.TaskColumnCodeReview},
	})
	require.NoError(t, err)
	architect, err := svc.CreateAgentFromTemplate(ctx, architectTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{string(domain.TaskColumnCodeReview)}, board.subs[architect.ID])

	pmTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "product-manager", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{domain.TaskColumnPMUAT},
	})
	require.NoError(t, err)
	pm, err := svc.CreateAgentFromTemplate(ctx, pmTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{string(domain.TaskColumnPMUAT)}, board.subs[pm.ID])
}

// Under the vacancy rule a second agent from the same template no longer
// misses out because of its NAME — it misses out because the columns are
// already occupied by the first agent. This replaces the old exact-name-match
// assumption (a renamed agent used to be invisible to routing that looked it
// up by its seeded name); routing no longer looks anything up by name, so a
// renamed agent is treated exactly like any other: it would get the columns
// if they were free.
func TestCreateAgentFromTemplate_SecondAgentFindsColumnsAlreadyOccupied(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetBoardConfigStore(board)
	ctx := context.Background()

	qaTpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "qa-agent", Description: "built-in role", BuiltIn: true,
		Subscriptions: []domain.TaskColumn{
			domain.TaskColumnReadyForQA, domain.TaskColumnInQA,
			domain.TaskColumnDone, domain.TaskColumnReleased,
		},
	})
	require.NoError(t, err)
	first, err := svc.CreateAgentFromTemplate(ctx, qaTpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)
	assert.NotEmpty(t, board.subs[first.ID])

	second, err := svc.CreateAgentFromTemplate(ctx, qaTpl.ID, domain.CreateAgentRequest{Name: "qa-agent-2"})
	require.NoError(t, err)

	assert.Empty(t, board.subs[second.ID], "the columns are already claimed by the first agent")
}

// An admin who narrowed QA's subscriptions keeps their setup: the helper only
// sets a subscription when the agent currently has none.
func TestApplySuggestedSubscriptions_LeavesExistingSubscriptionsAlone(t *testing.T) {
	store := newMemCatalogStore()
	qaID := uuid.New()
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{
		qaID: {string(domain.TaskColumnReadyForQA)},
	}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetBoardConfigStore(board)

	require.NoError(t, svc.applySuggestedSubscriptions(context.Background(), domain.Agent{ID: qaID, Name: "qa-agent"},
		[]domain.TaskColumn{domain.TaskColumnReadyForQA, domain.TaskColumnInQA, domain.TaskColumnDone, domain.TaskColumnReleased}))

	assert.Equal(t, []string{string(domain.TaskColumnReadyForQA)}, board.subs[qaID])
}

// applySuggestedRoles fills a role assignment only when the role exists and
// no existing assignment already covers the suggested areas.
func TestApplySuggestedRoles_FillsOnlyVacantAreas(t *testing.T) {
	developerID := uuid.New()
	backendAgent := domain.Agent{ID: uuid.New(), Name: "backend-developer"}
	roles := &memRoleAdmin{roles: map[uuid.UUID]domain.AgentRole{
		developerID: {ID: developerID, Key: "developer"},
	}}
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetRoleAdmin(roles)

	require.NoError(t, svc.applySuggestedRoles(context.Background(), backendAgent,
		[]domain.TemplateRoleSuggestion{{Key: "developer", Areas: []string{domain.RepoKindBackend}}}))
	assert.Len(t, roles.roles[developerID].Assignments, 1)
	assert.Equal(t, backendAgent.ID, roles.roles[developerID].Assignments[0].AgentID)

	// A second backend developer must not double up on the same area.
	secondBackend := domain.Agent{ID: uuid.New(), Name: "backend-developer-2"}
	require.NoError(t, svc.applySuggestedRoles(context.Background(), secondBackend,
		[]domain.TemplateRoleSuggestion{{Key: "developer", Areas: []string{domain.RepoKindBackend}}}))
	assert.Len(t, roles.roles[developerID].Assignments, 1, "backend is already covered")

	// A frontend developer still finds its own area vacant.
	frontendAgent := domain.Agent{ID: uuid.New(), Name: "frontend-developer"}
	require.NoError(t, svc.applySuggestedRoles(context.Background(), frontendAgent,
		[]domain.TemplateRoleSuggestion{{Key: "developer", Areas: []string{domain.RepoKindFrontend}}}))
	assert.Len(t, roles.roles[developerID].Assignments, 2)
}

// A suggested role that no longer exists (deleted or renamed by an admin) is
// silently skipped rather than erroring.
func TestApplySuggestedRoles_SkipsUnknownRole(t *testing.T) {
	roles := &memRoleAdmin{roles: map[uuid.UUID]domain.AgentRole{}}
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetRoleAdmin(roles)

	err := svc.applySuggestedRoles(context.Background(), domain.Agent{ID: uuid.New()},
		[]domain.TemplateRoleSuggestion{{Key: "no-such-role"}})

	require.NoError(t, err)
}

// memRoleAdmin is an in-memory catalog.RoleAdmin, and also
// ListAssignmentsByAgent (agentRoleSuggestions' optional read-back
// interface).
type memRoleAdmin struct {
	roles map[uuid.UUID]domain.AgentRole
}

func (m *memRoleAdmin) ListRoles(context.Context) ([]domain.AgentRole, error) {
	out := make([]domain.AgentRole, 0, len(m.roles))
	for _, r := range m.roles {
		out = append(out, r)
	}
	return out, nil
}

func (m *memRoleAdmin) SetRoleAssignments(_ context.Context, roleID uuid.UUID, assignments []domain.RoleAssignment) error {
	r := m.roles[roleID]
	r.Assignments = assignments
	m.roles[roleID] = r
	return nil
}

func (m *memRoleAdmin) ListAssignmentsByAgent(_ context.Context, agentID uuid.UUID) ([]domain.AgentRole, error) {
	var out []domain.AgentRole
	for _, r := range m.roles {
		for _, a := range r.Assignments {
			if a.AgentID == agentID {
				out = append(out, domain.AgentRole{ID: r.ID, Key: r.Key, Name: r.Name, Assignments: []domain.RoleAssignment{a}})
			}
		}
	}
	return out, nil
}

type memBoardConfigStore struct {
	subs map[uuid.UUID][]string
}

func (m *memBoardConfigStore) GetSettings(context.Context) (domain.BoardSettings, error) {
	return domain.BoardSettings{}, nil
}

func (m *memBoardConfigStore) UpdateSettings(context.Context, string) (domain.BoardSettings, error) {
	return domain.BoardSettings{}, nil
}

func (m *memBoardConfigStore) ListColumns(context.Context) ([]domain.BoardColumn, error) {
	return nil, nil
}

func (m *memBoardConfigStore) ReplaceColumns(context.Context, []domain.BoardColumnInput) error {
	return nil
}

func (m *memBoardConfigStore) ListMembers(context.Context) ([]domain.BoardMember, error) {
	return nil, nil
}

func (m *memBoardConfigStore) SetMembers(context.Context, []uuid.UUID) error { return nil }

func (m *memBoardConfigStore) ListSubscriptions(context.Context) ([]domain.BoardSubscription, error) {
	var out []domain.BoardSubscription
	for agentID, slugs := range m.subs {
		for _, slug := range slugs {
			out = append(out, domain.BoardSubscription{AgentID: agentID, ColumnSlug: slug})
		}
	}
	return out, nil
}

func (m *memBoardConfigStore) SetSubscriptions(context.Context, []domain.BoardSubscriptionInput) error {
	return nil
}

func (m *memBoardConfigStore) ListAgentSubscriptions(_ context.Context, agentID uuid.UUID) ([]string, error) {
	return m.subs[agentID], nil
}

func (m *memBoardConfigStore) SetAgentSubscriptions(_ context.Context, agentID uuid.UUID, columnSlugs []string) error {
	m.subs[agentID] = append([]string(nil), columnSlugs...)
	return nil
}

func (m *memBoardConfigStore) ListAgentSubscriptionsDetailed(_ context.Context, agentID uuid.UUID) ([]domain.AgentColumnSubscription, error) {
	slugs := m.subs[agentID]
	out := make([]domain.AgentColumnSubscription, 0, len(slugs))
	for _, slug := range slugs {
		out = append(out, domain.AgentColumnSubscription{ColumnSlug: slug})
	}
	return out, nil
}

func (m *memBoardConfigStore) SetAgentSubscriptionsDetailed(_ context.Context, agentID uuid.UUID, subs []domain.AgentColumnSubscription) error {
	slugs := make([]string, 0, len(subs))
	for _, s := range subs {
		slugs = append(slugs, s.ColumnSlug)
	}
	m.subs[agentID] = slugs
	return nil
}

func (m *memBoardConfigStore) ListTransitions(context.Context) ([]domain.BoardTransition, error) {
	return nil, nil
}

func (m *memBoardConfigStore) SetTransitions(context.Context, []domain.BoardTransition) error {
	return nil
}

func (m *memBoardConfigStore) AgentsForColumn(context.Context, string, string) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *memBoardConfigStore) ValidateColumnSlug(context.Context, string) (bool, error) {
	return true, nil
}
