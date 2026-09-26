package catalog

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// columnInstructionFixture is a bare Service wired to a memBoardConfigStore,
// with no agent/catalog store plumbing: reconcileColumnInstructions only
// touches boardConfig, so exercising it directly (rather than through a full
// SyncFromCatalog) keeps each case to the one row it is about.
func columnInstructionFixture() (*memBoardConfigStore, *Service) {
	board := &memBoardConfigStore{subs: map[uuid.UUID][]string{}}
	svc := NewService(newMemCatalogStore(), stubLLMClient{}, "")
	svc.SetBoardConfigStore(board)
	return board, svc
}

func TestReconcileColumnInstructions_FreshInsertRecordsSHA(t *testing.T) {
	board, svc := columnInstructionFixture()
	agentID := uuid.New()
	def := domain.UpstreamAgent{
		Name: "Shippy",
		ColumnInstructions: []domain.UpstreamColumnInstruction{
			{Column: "todo", Instruction: "do the ship thing"},
		},
	}

	require.NoError(t, svc.reconcileColumnInstructions(context.Background(), agentID, def))

	stored, err := board.ListAgentColumnInstructions(context.Background(), agentID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "do the ship thing", stored[0].Instruction)
	assert.Equal(t, hashContent("do the ship thing"), stored[0].CatalogSHA,
		"a newly written row must record the sha of the text the sync itself wrote")
}

func TestReconcileColumnInstructions_StaleCatalogOwnedRowIsUpdated(t *testing.T) {
	board, svc := columnInstructionFixture()
	agentID := uuid.New()
	board.setInstruction(agentID, "todo", "old ship text", hashContent("old ship text"))

	def := domain.UpstreamAgent{
		Name: "Shippy",
		ColumnInstructions: []domain.UpstreamColumnInstruction{
			{Column: "todo", Instruction: "new ship text"},
		},
	}
	require.NoError(t, svc.reconcileColumnInstructions(context.Background(), agentID, def))

	stored, err := board.ListAgentColumnInstructions(context.Background(), agentID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "new ship text", stored[0].Instruction,
		"a row untouched since the last sync wrote it must pick up the new catalog text")
	assert.Equal(t, hashContent("new ship text"), stored[0].CatalogSHA)
}

func TestReconcileColumnInstructions_OperatorEditedRowIsKept(t *testing.T) {
	board, svc := columnInstructionFixture()
	agentID := uuid.New()
	// CatalogSHA "" is the operator-edited/unknown-provenance marker the
	// operator's own SetAgentColumnInstruction leaves behind.
	board.setInstruction(agentID, "todo", "the operator's own wording", "")

	def := domain.UpstreamAgent{
		Name: "Shippy",
		ColumnInstructions: []domain.UpstreamColumnInstruction{
			{Column: "todo", Instruction: "new ship text"},
		},
	}
	require.NoError(t, svc.reconcileColumnInstructions(context.Background(), agentID, def))

	stored, err := board.ListAgentColumnInstructions(context.Background(), agentID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "the operator's own wording", stored[0].Instruction,
		"an operator edit must never be clobbered by a later catalog change")
	assert.Empty(t, stored[0].CatalogSHA, "provenance stays cleared once an operator has touched the row")
}

func TestReconcileColumnInstructions_DroppedCatalogOwnedRowIsDeleted(t *testing.T) {
	board, svc := columnInstructionFixture()
	agentID := uuid.New()
	board.setInstruction(agentID, "released", "old released text", hashContent("old released text"))

	def := domain.UpstreamAgent{Name: "Shippy"} // the catalog no longer ships this agent's "released" column
	require.NoError(t, svc.reconcileColumnInstructions(context.Background(), agentID, def))

	stored, err := board.ListAgentColumnInstructions(context.Background(), agentID)
	require.NoError(t, err)
	assert.Empty(t, stored, "a catalog-owned row for a column the catalog stopped shipping must be removed")
}

func TestReconcileColumnInstructions_DroppedOperatorRowIsKept(t *testing.T) {
	board, svc := columnInstructionFixture()
	agentID := uuid.New()
	board.setInstruction(agentID, "released", "the operator's own wording", "")

	def := domain.UpstreamAgent{Name: "Shippy"}
	require.NoError(t, svc.reconcileColumnInstructions(context.Background(), agentID, def))

	stored, err := board.ListAgentColumnInstructions(context.Background(), agentID)
	require.NoError(t, err)
	require.Len(t, stored, 1, "an operator's row survives even once the catalog drops the column entirely")
	assert.Equal(t, "the operator's own wording", stored[0].Instruction)
}
