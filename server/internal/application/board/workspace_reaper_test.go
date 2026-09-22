package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeTaskLister struct {
	tasks []domain.BoardTask
	err   error
}

func (f fakeTaskLister) ListAll(context.Context) ([]domain.BoardTask, error) {
	return f.tasks, f.err
}

type fakeActive map[uuid.UUID]bool

func (f fakeActive) HasLiveRunForTask(_ context.Context, id uuid.UUID, _ time.Duration) (bool, error) {
	return f[id], nil
}

func mkTaskDir(t *testing.T, root string, id uuid.UUID, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(root, "task-"+id.String())
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestWorkspaceReaperKeepsAndReaps(t *testing.T) {
	root := t.TempDir()
	grace := time.Hour

	finished := uuid.New()
	justDone := uuid.New()
	working := uuid.New()
	orphan := uuid.New()
	freshOrphan := uuid.New()
	running := uuid.New()

	dirs := map[uuid.UUID]string{
		finished:    mkTaskDir(t, root, finished, 10*time.Hour),
		justDone:    mkTaskDir(t, root, justDone, 10*time.Hour),
		working:     mkTaskDir(t, root, working, 10*time.Hour),
		orphan:      mkTaskDir(t, root, orphan, 10*time.Hour),
		freshOrphan: mkTaskDir(t, root, freshOrphan, time.Minute),
		running:     mkTaskDir(t, root, running, 10*time.Hour),
	}
	untouchable := []string{"agents", "repos", "lost+found", "task-not-a-uuid"}
	for _, name := range untouchable {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	lister := fakeTaskLister{tasks: []domain.BoardTask{
		{ID: finished, Column: domain.TaskColumnReleased, UpdatedAt: time.Now().Add(-10 * time.Hour)},
		{ID: justDone, Column: domain.TaskColumnDone, UpdatedAt: time.Now().Add(-time.Minute)},
		{ID: working, Column: domain.TaskColumnInProgress, UpdatedAt: time.Now().Add(-10 * time.Hour)},
		{ID: running, Column: domain.TaskColumnReleased, UpdatedAt: time.Now().Add(-10 * time.Hour)},
	}}

	r := NewWorkspaceReaper(lister, fakeActive{running: true}, root, grace)
	r.Sweep(context.Background())

	for id, want := range map[uuid.UUID]bool{
		finished:    false,
		orphan:      false,
		justDone:    true,
		working:     true,
		freshOrphan: true,
		running:     true,
	} {
		if got := exists(dirs[id]); got != want {
			t.Errorf("task %s: exists = %v, want %v", id, got, want)
		}
	}
	for _, name := range untouchable {
		if !exists(filepath.Join(root, name)) {
			t.Errorf("%s was removed; only task-<uuid> directories may be reaped", name)
		}
	}
}

func TestWorkspaceReaperKeepsEverythingWhenTheTaskListFails(t *testing.T) {
	root := t.TempDir()
	dir := mkTaskDir(t, root, uuid.New(), 100*time.Hour)

	r := NewWorkspaceReaper(fakeTaskLister{err: context.DeadlineExceeded}, nil, root, time.Hour)
	r.Sweep(context.Background())

	if !exists(dir) {
		t.Fatal("workspace was reaped despite the task list failing")
	}
}
