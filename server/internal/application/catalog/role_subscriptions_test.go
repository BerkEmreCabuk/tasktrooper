package catalog

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// QA owns all three of its columns. Subscribed to ready_for_qa alone, in_qa was
// unowned: a task moved there resolved back to the implementer-assignee, so the
// developer was dispatched onto the branch QA had just started testing.
//
// done is the third and the odd one: QA does no testing there, it merges the
// task's pull request. The column still dispatches nobody for anything else —
// the dispatcher wakes this subscription only for a move into done on a task
// whose PR is unmerged (board.doneMergeWake).
//
// This exercises the path a user actually takes now: creating the agent from
// its built-in template (CreateAgentFromTemplate), not a boot-time reconcile.
func TestCreateAgentFromTemplate_QAOwnsItsThreeColumns(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetBoardConfigStore(board)
	ctx := context.Background()

	require.NoError(t, svc.EnsureRoleTemplates(ctx))
	agent, err := svc.CreateAgentFromTemplate(ctx, findTemplateID(t, templates, "qa-agent"), domain.CreateAgentRequest{})
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{string(domain.TaskColumnReadyForQA), string(domain.TaskColumnInQA), string(domain.TaskColumnDone)},
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

	require.NoError(t, svc.EnsureRoleTemplates(ctx))

	architect, err := svc.CreateAgentFromTemplate(ctx, findTemplateID(t, templates, "system-architect"), domain.CreateAgentRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{string(domain.TaskColumnCodeReview)}, board.subs[architect.ID])

	pm, err := svc.CreateAgentFromTemplate(ctx, findTemplateID(t, templates, "product-manager"), domain.CreateAgentRequest{})
	require.NoError(t, err)
	assert.Equal(t, []string{string(domain.TaskColumnPMUAT)}, board.subs[pm.ID])
}

// Routing looks these agents up by their exact role name (domain/role_agent.go,
// repoprofile.architectAgentName, the analiz-assignment settings). An agent
// renamed on create is not the one those lookups find, so it gets no
// subscription — a second desk nobody is dispatched to.
func TestCreateAgentFromTemplate_RenamedAgentGetsNoSubscription(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetBoardConfigStore(board)
	ctx := context.Background()

	require.NoError(t, svc.EnsureRoleTemplates(ctx))
	agent, err := svc.CreateAgentFromTemplate(ctx, findTemplateID(t, templates, "qa-agent"), domain.CreateAgentRequest{Name: "qa-agent-2"})
	require.NoError(t, err)

	assert.Empty(t, board.subs[agent.ID])
}

// An admin who narrowed QA's subscriptions keeps their setup: the helper only
// sets a subscription when the agent currently has none.
func TestSetRoleSubscriptionsIfDefault_LeavesExistingSubscriptionsAlone(t *testing.T) {
	store := newMemCatalogStore()
	qaID := uuid.New()
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{
		qaID: {string(domain.TaskColumnReadyForQA)},
	}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetBoardConfigStore(board)

	require.NoError(t, svc.setRoleSubscriptionsIfDefault(context.Background(), domain.Agent{ID: qaID, Name: "qa-agent"}))

	assert.Equal(t, []string{string(domain.TaskColumnReadyForQA)}, board.subs[qaID])
}

func findTemplateID(t *testing.T, templates *memTemplateStore, name string) uuid.UUID {
	t.Helper()
	for _, tpl := range templates.templates {
		if tpl.Name == name {
			return tpl.ID
		}
	}
	t.Fatalf("no built-in template named %q", name)
	return uuid.Nil
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
	return nil, nil
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
