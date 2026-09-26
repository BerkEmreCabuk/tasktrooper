package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// BoardConfigColumnInstructionSuite covers migration 165's catalog_sha
// provenance column: the operator write path (SetAgentColumnInstruction)
// clears it, while the catalog-only write path (SetCatalogColumnInstruction)
// stamps it, and DeleteAgentColumnInstruction removes a row outright.
type BoardConfigColumnInstructionSuite struct {
	suite.Suite
	ctx     context.Context
	cancel  context.CancelFunc
	pg      *database.Embedded
	pool    *pgxpool.Pool
	board   *postgres.BoardConfigStore
	agentID uuid.UUID
}

func TestBoardConfigColumnInstructionSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(BoardConfigColumnInstructionSuite))
}

func (s *BoardConfigColumnInstructionSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.board = postgres.NewBoardConfigStore(postgres.NewDB(pool))

	agents := postgres.NewCatalogStore(postgres.NewDB(pool))
	agent, err := agents.CreateAgent(s.ctx, domain.Agent{
		Name:         "board-config-column-instruction-test",
		ProviderType: domain.LLMProviderAnthropic,
		Model:        "test-model",
	})
	s.Require().NoError(err)
	s.agentID = agent.ID
}

func (s *BoardConfigColumnInstructionSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *BoardConfigColumnInstructionSuite) TestCatalogWriteStampsSHAAndOperatorWriteClearsIt() {
	agentID := s.agentID

	s.Require().NoError(s.board.SetCatalogColumnInstruction(s.ctx, agentID, "todo", "catalog text", "deadbeef"))
	stored, err := s.board.ListAgentColumnInstructions(s.ctx, agentID)
	s.Require().NoError(err)
	s.Require().Len(stored, 1)
	s.Equal("catalog text", stored[0].Instruction)
	s.Equal("deadbeef", stored[0].CatalogSHA)

	// The operator path always clears catalog_sha, even when overwriting a
	// catalog-owned row: from here the text is the operator's.
	s.Require().NoError(s.board.SetAgentColumnInstruction(s.ctx, agentID, "todo", "operator text"))
	stored, err = s.board.ListAgentColumnInstructions(s.ctx, agentID)
	s.Require().NoError(err)
	s.Require().Len(stored, 1)
	s.Equal("operator text", stored[0].Instruction)
	s.Empty(stored[0].CatalogSHA, "an operator write must clear provenance")
}

func (s *BoardConfigColumnInstructionSuite) TestDeleteAgentColumnInstructionRemovesRow() {
	agentID := s.agentID
	s.Require().NoError(s.board.SetCatalogColumnInstruction(s.ctx, agentID, "code_review", "text", "abc123"))

	s.Require().NoError(s.board.DeleteAgentColumnInstruction(s.ctx, agentID, "code_review"))

	stored, err := s.board.ListAgentColumnInstructions(s.ctx, agentID)
	s.Require().NoError(err)
	for _, ins := range stored {
		s.NotEqual("code_review", ins.ColumnSlug)
	}
}

func (s *BoardConfigColumnInstructionSuite) TestEmptyOperatorInstructionDeletesRow() {
	agentID := s.agentID
	s.Require().NoError(s.board.SetCatalogColumnInstruction(s.ctx, agentID, "need_revision", "text", "abc123"))

	s.Require().NoError(s.board.SetAgentColumnInstruction(s.ctx, agentID, "need_revision", ""))

	stored, err := s.board.ListAgentColumnInstructions(s.ctx, agentID)
	s.Require().NoError(err)
	for _, ins := range stored {
		s.NotEqual("need_revision", ins.ColumnSlug)
	}
}
