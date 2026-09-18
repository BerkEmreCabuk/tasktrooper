package localpreview

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type stubTasks struct {
	task domain.BoardTask
}

func (s stubTasks) Get(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return s.task, nil
}

type stubRepos struct {
	root string
}

func (s stubRepos) ResolveRootPath(context.Context, uuid.UUID) (string, error) {
	return s.root, nil
}

type stubGit struct {
	has bool
}

func (s stubGit) HasGit(string) bool { return s.has }
func (s stubGit) EnsureTaskWorkspace(_ context.Context, _, workspacePath, _ string) error {
	return os.MkdirAll(workspacePath, 0o755)
}

func newTestService(t *testing.T, root string) *Service {
	t.Helper()
	workspaceRoot := t.TempDir()
	return NewService(Deps{
		Tasks:         stubTasks{task: domain.BoardTask{ID: uuid.New(), Key: "T-9", TaskNumber: 9}},
		Repositories:  stubRepos{root: root},
		Git:           stubGit{has: true},
		WorkspaceRoot: workspaceRoot,
	})
}

func TestStartDetectsTheURLTheCommandPrints(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	repositoryID, taskID := uuid.New(), uuid.New()

	preview, err := svc.Start(context.Background(), repositoryID, taskID, `echo "Local: http://localhost:4321/"; sleep 5`)
	require.NoError(t, err)

	assert.Contains(t, []domain.LocalPreviewStatus{domain.LocalPreviewStarting, domain.LocalPreviewRunning}, preview.Status)

	require.Eventually(t, func() bool {
		p, ok := svc.Status(repositoryID)
		return ok && p.Status == domain.LocalPreviewRunning
	}, 3*time.Second, 20*time.Millisecond)

	p, ok := svc.Status(repositoryID)
	require.True(t, ok)
	assert.Equal(t, "http://localhost:4321/", p.URL)
	assert.NotEmpty(t, p.LogTail)

	svc.Stop(repositoryID)
	_, ok = svc.Status(repositoryID)
	assert.False(t, ok, "a stopped preview is no longer the repository's active one")
}

func TestStartingASecondPreviewReplacesTheFirst(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	repositoryID := uuid.New()
	secondTaskID := uuid.New()

	_, err := svc.Start(context.Background(), repositoryID, uuid.New(), "sleep 30")
	require.NoError(t, err)

	second, err := svc.Start(context.Background(), repositoryID, secondTaskID, "sleep 30")
	require.NoError(t, err)
	assert.Equal(t, secondTaskID, second.TaskID)

	p, ok := svc.Status(repositoryID)
	require.True(t, ok)
	assert.Equal(t, secondTaskID, p.TaskID, "only the second preview may be the repository's active one")

	svc.Stop(repositoryID)
	_, ok = svc.Status(repositoryID)
	assert.False(t, ok)
}

func TestAFailedPreviewStaysVisibleUntilCleared(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	repositoryID, taskID := uuid.New(), uuid.New()

	_, err := svc.Start(context.Background(), repositoryID, taskID, `echo "Another dev server is already running"; exit 1`)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		p, ok := svc.Status(repositoryID)
		return ok && p.Status == domain.LocalPreviewFailed
	}, 3*time.Second, 20*time.Millisecond, "a preview that died must still be reported, not vanish")

	p, _ := svc.Status(repositoryID)
	assert.Equal(t, taskID, p.TaskID)
	assert.NotEmpty(t, p.Detail)
	assert.Contains(t, p.LogTail, "Another dev server is already running")
	assert.Empty(t, loadState(svc.workspaceRoot), "an exited process is never persisted for the next boot to signal")

	svc.Stop(repositoryID)
	_, ok := svc.Status(repositoryID)
	assert.False(t, ok)
}

func TestStartStopsAStaleNextDevServerHoldingTheCheckout(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	taskID := uuid.New()
	workspacePath, err := workspace.TaskDir(svc.workspaceRoot, taskID)
	require.NoError(t, err)

	script := filepath.Join(t.TempDir(), "next-dev.sh")
	require.NoError(t, os.WriteFile(script, []byte("sleep 30\n"), 0o755))
	stale := exec.Command("sh", script)
	stale.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, stale.Start())
	exited := make(chan struct{})
	go func() { _ = stale.Wait(); close(exited) }()
	t.Cleanup(func() { _ = syscall.Kill(-stale.Process.Pid, syscall.SIGKILL) })

	require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".next", "dev"), 0o755))
	lock := fmt.Sprintf(`{"pid":%d,"port":3000,"appUrl":"http://localhost:3000"}`, stale.Process.Pid)
	require.NoError(t, os.WriteFile(filepath.Join(workspacePath, ".next", "dev", "lock"), []byte(lock), 0o644))

	repositoryID := uuid.New()
	_, err = svc.Start(context.Background(), repositoryID, taskID, "sleep 30")
	require.NoError(t, err)
	t.Cleanup(func() { svc.Stop(repositoryID) })

	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("the next dev server named by the checkout's lock must be stopped before the preview starts")
	}
}

func TestStartLeavesALockWhosePidIsNotNextAlone(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	taskID := uuid.New()
	workspacePath, err := workspace.TaskDir(svc.workspaceRoot, taskID)
	require.NoError(t, err)

	other := exec.Command("sleep", "30")
	other.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, other.Start())
	t.Cleanup(func() { _ = syscall.Kill(-other.Process.Pid, syscall.SIGKILL); _ = other.Wait() })

	require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".next", "dev"), 0o755))
	lock := fmt.Sprintf(`{"pid":%d}`, other.Process.Pid)
	require.NoError(t, os.WriteFile(filepath.Join(workspacePath, ".next", "dev", "lock"), []byte(lock), 0o644))

	repositoryID := uuid.New()
	_, err = svc.Start(context.Background(), repositoryID, taskID, "sleep 30")
	require.NoError(t, err)
	t.Cleanup(func() { svc.Stop(repositoryID) })

	assert.NoError(t, syscall.Kill(other.Process.Pid, 0), "a reused pid that is not a Next server must not be touched")
}

func TestNewServiceReapsAPreviousProcessesOrphan(t *testing.T) {
	workspaceRoot := t.TempDir()
	deps := func() Deps {
		return Deps{
			Tasks:         stubTasks{task: domain.BoardTask{ID: uuid.New(), Key: "T-9", TaskNumber: 9}},
			Repositories:  stubRepos{root: t.TempDir()},
			Git:           stubGit{has: true},
			WorkspaceRoot: workspaceRoot,
		}
	}

	first := NewService(deps())
	repositoryID := uuid.New()
	_, err := first.Start(context.Background(), repositoryID, uuid.New(), "sleep 30")
	require.NoError(t, err)

	first.mu.Lock()
	pid := first.active[repositoryID].cmd.Process.Pid
	first.mu.Unlock()
	require.NoError(t, syscall.Kill(pid, 0), "the preview's process must actually be running before this test means anything")

	NewService(deps())

	require.Eventually(t, func() bool {
		return syscall.Kill(pid, 0) != nil
	}, 2*time.Second, 20*time.Millisecond, "the orphaned process from the old Service must be reaped by the new one")
}

func TestStartRefusesAnEmptyCommand(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	_, err := svc.Start(context.Background(), uuid.New(), uuid.New(), "   ")
	require.Error(t, err)
}

func TestDetectRunCommandPrefersNpmDevScript(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"dev":"vite","start":"node server.js"}}`), 0o644))
	assert.Equal(t, "npm run dev", DetectRunCommand(dir))
}

func TestDetectRunCommandFallsBackToNpmStart(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"start":"node server.js"}}`), 0o644))
	assert.Equal(t, "npm start", DetectRunCommand(dir))
}

func TestDetectRunCommandFallsBackToGoRun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644))
	assert.Equal(t, "go run .", DetectRunCommand(dir))
}

func TestDetectRunCommandFindsNothingItRecognises(t *testing.T) {
	assert.Equal(t, "", DetectRunCommand(t.TempDir()))
}

func TestDetectRunCommandRunsALoneDotnetProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Api.csproj"), []byte(`<Project Sdk="Microsoft.NET.Sdk.Web"/>`), 0o644))
	assert.Equal(t, "dotnet run --project Api.csproj", DetectRunCommand(dir))
}

func TestDetectRunCommandDoesNotGuessADotnetSolutionsStartupProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Shop.sln"), nil, 0o644))
	assert.Equal(t, "", DetectRunCommand(dir))
}
