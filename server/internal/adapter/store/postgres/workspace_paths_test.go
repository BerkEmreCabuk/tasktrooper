package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

const legacyLocalTenant = "00000000-0000-0000-0000-000000000001"

// WorkspacePathSuite covers the database half of the move off the per-tenant
// workspace layout: every column holding an absolute workspace path is
// re-pointed at the flat layout when its old location is gone, and left alone
// when the old location still exists.
type WorkspacePathSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
}

func TestWorkspacePathSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(WorkspacePathSuite))
}

func (s *WorkspacePathSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	startupCtx, cancelStartup := context.WithTimeout(s.ctx, 3*time.Minute)
	defer cancelStartup()

	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(startupCtx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: sharedPGRuntimeDir,
	})
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
}

func (s *WorkspacePathSuite) TearDownSuite() {
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

// stored reads a column back raw: the stores re-anchor paths on read, and what
// matters here is what the row itself now says.
func (s *WorkspacePathSuite) stored(query string, arg any) string {
	var v string
	s.Require().NoError(s.pool.QueryRow(s.ctx, query, arg).Scan(&v))
	return v
}

func (s *WorkspacePathSuite) mkdir(path string) {
	s.Require().NoError(os.MkdirAll(path, 0o755))
}

// run inserts a task_agent_runs row; it needs a task, an agent and a board
// event for its foreign keys.
func (s *WorkspacePathSuite) run(repoID uuid.UUID, workspacePath string) uuid.UUID {
	tasks := postgres.NewBoardTaskStore(s.db)
	number, err := tasks.NextTaskNumber(s.ctx, domain.TaskTypeTask)
	s.Require().NoError(err)
	task, err := tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: repoID,
		TaskNumber:   number,
		Title:        "workspace path",
		TaskType:     domain.TaskTypeTask,
		Column:       domain.TaskColumnTodo,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	agent, err := postgres.NewCatalogStore(s.db).CreateAgent(s.ctx, domain.Agent{
		Name:         "workspace-path-" + uuid.NewString(),
		ProviderType: domain.LLMProviderAnthropic,
		Model:        "test-model",
	})
	s.Require().NoError(err)
	event, err := postgres.NewBoardEventStore(s.db).Create(s.ctx, domain.BoardEvent{
		RepositoryID: repoID,
		TaskID:       task.ID,
		EventType:    domain.BoardEventTaskAssigned,
		Payload:      json.RawMessage(`{}`),
	})
	s.Require().NoError(err)
	run, err := postgres.NewTaskAgentRunStore(s.db).Create(s.ctx, domain.TaskAgentRun{
		TaskID:        task.ID,
		AgentID:       agent.ID,
		BoardEventID:  event.ID,
		Status:        "pending",
		WorkspacePath: workspacePath,
	})
	s.Require().NoError(err)
	return run.ID
}

func (s *WorkspacePathSuite) TestRewritesRowsIntoTheOldLayout() {
	root := filepath.Join(s.T().TempDir(), "workspaces")
	legacy := filepath.Join(root, "tenants", legacyLocalTenant)
	flat := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }

	// "moved" entries already live in the flat layout. "stuck" ones collide
	// with a flat directory of the same name, so the move leaves them in place.
	movedRepo := filepath.Join(legacy, "repos", "moved-repo")
	s.mkdir(flat("repos", "moved-repo"))
	stuckRepo := filepath.Join(legacy, "repos", "stuck-repo")
	s.mkdir(stuckRepo)
	s.mkdir(flat("repos", "stuck-repo"))
	movedTaskName := "task-" + uuid.NewString()
	movedTask := filepath.Join(legacy, movedTaskName)
	stuckTaskName := "task-" + uuid.NewString()
	stuckTask := filepath.Join(legacy, stuckTaskName)
	s.mkdir(stuckTask)
	s.mkdir(flat(stuckTaskName))
	movedCatalog := filepath.Join(legacy, "agent-cli", "claude")
	windowsRepo := `C:\Users\me\TaskTrooper\workspaces\tenants\` + legacyLocalTenant + `\repos\win-repo`

	repos := postgres.NewRepositoryStore(s.db)
	moved, err := repos.Create(s.ctx, "moved-repo", "", movedRepo, "", "")
	s.Require().NoError(err)
	stuck, err := repos.Create(s.ctx, "stuck-repo", "", stuckRepo, "", "")
	s.Require().NoError(err)
	win, err := repos.Create(s.ctx, "win-repo", "", windowsRepo, "", "")
	s.Require().NoError(err)

	indexes := postgres.NewIndexStore(s.db)
	movedIdx, err := indexes.CreateProjectIndex(s.ctx, moved.ID, movedRepo, "")
	s.Require().NoError(err)
	stuckIdx, err := indexes.CreateProjectIndex(s.ctx, stuck.ID, stuckRepo, "")
	s.Require().NoError(err)

	sessions := postgres.NewSessionStore(s.db)
	movedSess, err := sessions.Create(s.ctx, "moved", "", movedTask, nil, nil, nil)
	s.Require().NoError(err)
	s.Require().NoError(sessions.UpdateProjectRoot(s.ctx, movedSess.ID, movedRepo))
	stuckSess, err := sessions.Create(s.ctx, "stuck", "", stuckTask, nil, nil, nil)
	s.Require().NoError(err)
	s.Require().NoError(sessions.UpdateProjectRoot(s.ctx, stuckSess.ID, stuckRepo))

	movedRun := s.run(moved.ID, movedTask)
	stuckRun := s.run(stuck.ID, stuckTask)

	s.Require().NoError(postgres.NewAgentCLIStore(s.db).Set(s.ctx, domain.AgentCLIConnection{
		Flavor:       domain.AgentCLIFlavorClaude,
		ProviderType: domain.LLMProviderClaudeCode,
		CatalogPath:  movedCatalog,
	}))

	res, err := workspace.FlattenLegacyLayout(s.ctx, root, postgres.NewWorkspacePathStore(s.db))
	s.Require().NoError(err)
	s.Zero(res.Moved)
	s.Equal(2, res.Conflicts, "the stuck repository and task stay in the old layout")
	// Two repositories, one index, workspace_dir and project_root, one run,
	// the catalog.
	s.Equal(7, res.Rewritten)

	const (
		repoRoot    = `SELECT root_path FROM repositories WHERE id = $1`
		indexRoot   = `SELECT root_path FROM workspace_indexes WHERE id = $1`
		sessionDir  = `SELECT workspace_dir FROM sessions WHERE id = $1`
		sessionRoot = `SELECT project_root FROM sessions WHERE id = $1`
		runPath     = `SELECT workspace_path FROM task_agent_runs WHERE id = $1`
		catalogPath = `SELECT catalog_path FROM agent_cli_connection WHERE flavor = $1`
	)
	s.Equal(flat("repos", "moved-repo"), s.stored(repoRoot, moved.ID))
	s.Equal(`C:\Users\me\TaskTrooper\workspaces\repos\win-repo`, s.stored(repoRoot, win.ID))
	s.Equal(flat("repos", "moved-repo"), s.stored(indexRoot, movedIdx.ID))
	s.Equal(flat(movedTaskName), s.stored(sessionDir, movedSess.ID))
	s.Equal(flat("repos", "moved-repo"), s.stored(sessionRoot, movedSess.ID))
	s.Equal(flat(movedTaskName), s.stored(runPath, movedRun))
	s.Equal(flat("agent-cli", "claude"), s.stored(catalogPath, string(domain.AgentCLIFlavorClaude)))

	// Rows whose old path still exists keep it.
	s.Equal(stuckRepo, s.stored(repoRoot, stuck.ID))
	s.Equal(stuckRepo, s.stored(indexRoot, stuckIdx.ID))
	s.Equal(stuckTask, s.stored(sessionDir, stuckSess.ID))
	s.Equal(stuckRepo, s.stored(sessionRoot, stuckSess.ID))
	s.Equal(stuckTask, s.stored(runPath, stuckRun))

	// The next boot has nothing left to change.
	again, err := workspace.FlattenLegacyLayout(s.ctx, root, postgres.NewWorkspacePathStore(s.db))
	s.Require().NoError(err)
	s.Zero(again.Moved)
	s.Zero(again.Rewritten)
	s.Equal(stuckRepo, s.stored(repoRoot, stuck.ID))
}
