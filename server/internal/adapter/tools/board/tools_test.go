package board

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// taskRepoLookup reuses fakeTaskManager's inert stubs and scripts the board-wide
// lookup resolveTaskRepositoryID falls back to. The cheap membership check it
// tries first is fakeTaskManager.GetTask, scripted through taskRepoID.
type taskRepoLookup struct {
	*fakeTaskManager
	repoID uuid.UUID
	err    error
	calls  int
}

func (m *taskRepoLookup) FindTaskRepositoryID(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
	m.calls++
	if m.err != nil {
		return uuid.Nil, m.err
	}
	return m.repoID, nil
}

func TestResolveTaskRepositoryID(t *testing.T) {
	contextRepo := uuid.New()
	taskRepo := uuid.New()
	taskID := uuid.New()
	notFound := errors.New("task not found")

	cases := []struct {
		name        string
		contextRepo uuid.UUID
		// taskInRepo is the repository the targeted single-task read finds the
		// task in. Nil means the fast path misses and the board-wide lookup
		// below is what decides.
		taskInRepo uuid.UUID
		lookupRepo uuid.UUID
		lookupErr  error
		want       uuid.UUID
		wantErr    error
		// wantInMessage are substrings the refusal must name, so the agent
		// reading it knows which task and which repository it is bound to.
		wantInMessage []string
		// wantLookups counts the board-wide FindTaskRepositoryID — ListAllTasks
		// over every repository plus a bulk pipeline-status read. This resolver
		// runs on every task-scoped tool call, so the ordinary case has to cost
		// zero of them.
		wantLookups int
	}{
		{
			name:        "no run repository: the task's own repository answers",
			lookupRepo:  taskRepo,
			want:        taskRepo,
			wantLookups: 1,
		},
		{
			// The ordinary case: the run is acting on its own task. One scoped
			// read answers it. lookupRepo is deliberately the WRONG repository —
			// if the fast path ever stops short-circuiting, this case turns into
			// a cross-repository refusal rather than passing quietly.
			name:        "the task is in the run's repository: no board-wide lookup",
			contextRepo: contextRepo,
			taskInRepo:  contextRepo,
			lookupRepo:  taskRepo,
			want:        contextRepo,
			wantLookups: 0,
		},
		{
			// The containment case: a task in another repository is refused,
			// not silently resolved there. The run is bound to one repository
			// and every task-scoped tool — merge, release, delete — resolves
			// through here, so acting on the other repository is exactly what
			// a planted task id would buy an injected prompt.
			name:        "a task in another repository is refused, naming both ids",
			contextRepo: contextRepo,
			lookupRepo:  taskRepo,
			want:        uuid.Nil,
			wantErr:     domain.ErrTaskOutsideRepository,
			wantInMessage: []string{
				taskID.String(),
				contextRepo.String(),
				"different repository",
			},
			wantLookups: 1,
		},
		{
			// The task genuinely does not exist (or the board is unreachable):
			// the caller has to fail with its own not-found error against the
			// repository it was working in, not with this lookup's.
			name:        "an unresolvable task keeps the run's repository",
			contextRepo: contextRepo,
			lookupErr:   notFound,
			want:        contextRepo,
			wantLookups: 1,
		},
		{
			name:        "an empty lookup keeps the run's repository",
			contextRepo: contextRepo,
			lookupRepo:  uuid.Nil,
			want:        contextRepo,
			wantLookups: 1,
		},
		{
			name:        "an unresolvable task with no run repository still errors",
			lookupErr:   notFound,
			want:        uuid.Nil,
			wantErr:     notFound,
			wantLookups: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks := &taskRepoLookup{fakeTaskManager: &fakeTaskManager{taskRepoID: tc.taskInRepo}, repoID: tc.lookupRepo, err: tc.lookupErr}
			kit := &ToolKit{Tasks: tasks}
			ctx := registry.ContextWithRepositoryID(context.Background(), tc.contextRepo)

			got, err := kit.resolveTaskRepositoryID(ctx, taskID)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("repository = %s, want %s", got, tc.want)
			}
			for _, want := range tc.wantInMessage {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("error %v does not name %q", err, want)
				}
			}
			if tasks.calls != tc.wantLookups {
				t.Fatalf("board-wide task lookups = %d, want %d", tasks.calls, tc.wantLookups)
			}
		})
	}
}
