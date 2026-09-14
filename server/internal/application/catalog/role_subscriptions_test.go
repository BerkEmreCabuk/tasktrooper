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
func TestEnsureRoleSubscriptions_QAOwnsItsThreeColumns(t *testing.T) {
	store := newMemCatalogStore()
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetBoardConfigStore(board)

	qaID := uuid.New()
	store.agents = []domain.Agent{{ID: qaID, Name: "qa-agent", Enabled: true}}

	require.NoError(t, svc.ensureRoleSubscriptions(context.Background()))

	assert.ElementsMatch(t,
		[]string{string(domain.TaskColumnReadyForQA), string(domain.TaskColumnInQA), string(domain.TaskColumnDone)},
		board.subs[qaID])
}

// An admin who narrowed QA's subscriptions keeps their setup across restarts.
func TestEnsureRoleSubscriptions_LeavesExistingSubscriptionsAlone(t *testing.T) {
	store := newMemCatalogStore()
	qaID := uuid.New()
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{
		qaID: {string(domain.TaskColumnReadyForQA)},
	}}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetBoardConfigStore(board)
	store.agents = []domain.Agent{{ID: qaID, Name: "qa-agent", Enabled: true}}

	require.NoError(t, svc.ensureRoleSubscriptions(context.Background()))

	assert.Equal(t, []string{string(domain.TaskColumnReadyForQA)}, board.subs[qaID])
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
