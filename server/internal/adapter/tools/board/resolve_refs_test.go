package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refWorkspace struct {
	WorkspaceLister
	projects []domain.InitiativeProject
	repos    []domain.Repository
	err      error
}

func (w refWorkspace) ListProjects(context.Context) ([]domain.InitiativeProject, error) {
	return w.projects, w.err
}

func (w refWorkspace) ListRepositories(context.Context) ([]domain.Repository, error) {
	return w.repos, w.err
}

// refTasks answers the board listing resolveTaskRef walks, and nothing else.
type refTasks struct {
	TaskManager
	tasks []domain.BoardTask
	err   error
}

func (m refTasks) ListAllTasks(context.Context) ([]domain.BoardTask, error) {
	return m.tasks, m.err
}

func TestResolveTaskRef_ByBoardKey(t *testing.T) {
	// "DE-1" is what the chat, the board card and the session action ledger all
	// show, so it is what the model passes. UUID-only rejected it as an invalid
	// task_id, and a move that cannot land is what pushes the model into opening
	// a second task instead.
	id := uuid.New()
	kit := &ToolKit{Tasks: refTasks{tasks: []domain.BoardTask{
		{ID: uuid.New(), Key: "DE-2", Title: "other"},
		{ID: id, Key: "DE-1", Title: "Android bağlantısı"},
	}}}

	got, err := kit.resolveTaskRef(context.Background(), "DE-1")

	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveTaskRef_KeyIsCaseInsensitive(t *testing.T) {
	id := uuid.New()
	kit := &ToolKit{Tasks: refTasks{tasks: []domain.BoardTask{{ID: id, Key: "DE-1"}}}}

	got, err := kit.resolveTaskRef(context.Background(), "de-1")

	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveTaskRef_UUIDPassesThroughWithoutLookup(t *testing.T) {
	id := uuid.New()
	kit := &ToolKit{Tasks: refTasks{err: errors.New("board unreachable")}}

	got, err := kit.resolveTaskRef(context.Background(), id.String())

	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveTaskRef_UnknownKeyNamesBothForms(t *testing.T) {
	kit := &ToolKit{Tasks: refTasks{tasks: []domain.BoardTask{{ID: uuid.New(), Key: "DE-1"}}}}

	_, err := kit.resolveTaskRef(context.Background(), "DE-9")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "DE-9")
	assert.Contains(t, err.Error(), "list_board_tasks")
}

func TestResolveRepositoryRef_ByName(t *testing.T) {
	id := uuid.New()
	kit := &ToolKit{Workspace: refWorkspace{repos: []domain.Repository{{ID: id, Name: "acme-web"}}}}

	got, err := kit.resolveRepositoryRef(context.Background(), "acme-web")

	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveRepositoryRef_NameIsCaseInsensitive(t *testing.T) {
	id := uuid.New()
	kit := &ToolKit{Workspace: refWorkspace{repos: []domain.Repository{{ID: id, Name: "Acme-Web"}}}}

	got, err := kit.resolveRepositoryRef(context.Background(), "acme-web")

	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveRepositoryRef_UUIDPassesThroughWithoutLookup(t *testing.T) {
	id := uuid.New()
	// A workspace that would error if consulted proves the UUID short-circuits.
	kit := &ToolKit{Workspace: refWorkspace{err: errors.New("must not be called")}}

	got, err := kit.resolveRepositoryRef(context.Background(), id.String())

	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveRepositoryRef_UnknownNameListsValidOnes(t *testing.T) {
	kit := &ToolKit{Workspace: refWorkspace{repos: []domain.Repository{{ID: uuid.New(), Name: "acme-web"}}}}

	_, err := kit.resolveRepositoryRef(context.Background(), "nope")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "acme-web")
}

func TestResolveRepositoryRef_NoWorkspaceIsAnError(t *testing.T) {
	kit := &ToolKit{}

	_, err := kit.resolveRepositoryRef(context.Background(), "acme-web")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unavailable")
}

func TestResolveProjectRef_ByName(t *testing.T) {
	id := uuid.New()
	kit := &ToolKit{Workspace: refWorkspace{projects: []domain.InitiativeProject{{ID: id, Name: "Acme"}}}}

	got, err := kit.resolveProjectRef(context.Background(), "Acme")

	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveProjectRef_EmptyWorkspaceSuggestsCreate(t *testing.T) {
	kit := &ToolKit{Workspace: refWorkspace{}}

	_, err := kit.resolveProjectRef(context.Background(), "Acme")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create_project")
}

func TestCreateTaskArgs_RefsPreferNameOverLegacyUUIDField(t *testing.T) {
	legacyRepo, legacyProject := uuid.New().String(), uuid.New().String()
	args := createTaskArgs{
		Repository:          "acme-web",
		Project:             "Acme",
		RepositoryID:        legacyRepo,
		InitiativeProjectID: legacyProject,
	}

	assert.Equal(t, "acme-web", args.repositoryRef())
	assert.Equal(t, "Acme", args.projectRef())

	legacyOnly := createTaskArgs{RepositoryID: legacyRepo, InitiativeProjectID: legacyProject}
	assert.Equal(t, legacyRepo, legacyOnly.repositoryRef())
	assert.Equal(t, legacyProject, legacyOnly.projectRef())

	empty := createTaskArgs{}
	assert.Empty(t, empty.repositoryRef())
	assert.Empty(t, empty.projectRef())
}
