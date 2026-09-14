package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

type WorkspaceSuite struct {
	suite.Suite
	tmpRoot string
}

func TestWorkspaceSuite(t *testing.T) {
	suite.Run(t, new(WorkspaceSuite))
}

func (s *WorkspaceSuite) SetupTest() {
	dir, err := os.MkdirTemp("", "workspace-test-*")
	s.Require().NoError(err)
	s.tmpRoot = dir
}

func (s *WorkspaceSuite) TearDownTest() {
	_ = os.RemoveAll(s.tmpRoot)
}

func (s *WorkspaceSuite) TestSessionDir() {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	tid := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	ctx := tenant.With(context.Background(), tenant.Identity{TenantID: tid, Role: tenant.RoleMember})

	dir, err := SessionDir(ctx, s.tmpRoot, id)
	s.Require().NoError(err)
	s.Equal(filepath.Join(s.tmpRoot, "tenants", tid.String(), id.String()), dir)

	// No identity, no path: a scratch directory nobody owns would sit in the
	// namespace every tenant shares.
	_, err = SessionDir(context.Background(), s.tmpRoot, id)
	s.Require().Error(err)
}

func (s *WorkspaceSuite) TestSubtaskDir() {
	sessionDir := filepath.Join(s.tmpRoot, "session")
	dir, err := SubtaskDir(sessionDir, "task-001")
	s.Require().NoError(err)
	s.Equal(filepath.Join(sessionDir, "task-001"), dir)
}

func (s *WorkspaceSuite) TestSanitizeTaskKey() {
	s.Equal("task_001", SanitizeTaskKey("task 001!"))
}

func (s *WorkspaceSuite) TestEnsureDir() {
	target := filepath.Join(s.tmpRoot, "nested", "dir")
	s.NoError(EnsureDir(target))
	info, err := os.Stat(target)
	s.Require().NoError(err)
	s.True(info.IsDir())
}

func (s *WorkspaceSuite) TestResolveRootEmpty() {
	dir, err := ResolveRoot("")
	s.Require().NoError(err)
	s.NotEmpty(dir)
}

func (s *WorkspaceSuite) TestSubtaskDirEmptySession() {
	_, err := SubtaskDir("", "t1")
	s.Error(err)
	s.Contains(err.Error(), "session workspace is empty")
}

func (s *WorkspaceSuite) TestRemoveDir() {
	target := filepath.Join(s.tmpRoot, "remove-me")
	s.Require().NoError(os.MkdirAll(target, 0o755))
	s.NoError(RemoveDir(target))
	_, err := os.Stat(target)
	s.True(os.IsNotExist(err))
}

func (s *WorkspaceSuite) TestRemoveDirEmpty() {
	s.NoError(RemoveDir(""))
}
