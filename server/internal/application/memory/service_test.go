package memory

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeStore keeps memories in a slice and applies the same bucket rules the SQL
// store applies, so the service's scope decisions are testable without pgx.
type fakeStore struct {
	rows []domain.AgentMemory
	now  time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{now: time.Unix(0, 0)}
}

func (f *fakeStore) Create(_ context.Context, m domain.AgentMemory) (domain.AgentMemory, error) {
	f.now = f.now.Add(time.Second)
	m.ID = uuid.New()
	m.CreatedAt = f.now
	m.UpdatedAt = f.now
	m.Scope = domain.MemoryScopeOf(m.AgentID, m.RepositoryID)
	f.rows = append(f.rows, m)
	return m, nil
}

func (f *fakeStore) Get(_ context.Context, id uuid.UUID) (domain.AgentMemory, error) {
	for _, m := range f.rows {
		if m.ID == id {
			return m, nil
		}
	}
	return domain.AgentMemory{}, errNotFound
}

func (f *fakeStore) List(_ context.Context, q domain.MemoryQuery) ([]domain.AgentMemory, error) {
	var out []domain.AgentMemory
	for i := len(f.rows) - 1; i >= 0; i-- {
		m := f.rows[i]
		switch {
		case q.Owner == domain.MemoryOwnerTeam || q.AgentID == uuid.Nil:
			if m.AgentID != uuid.Nil {
				continue
			}
		case q.Owner == domain.MemoryOwnerAgent:
			if m.AgentID != q.AgentID {
				continue
			}
		default:
			if m.AgentID != uuid.Nil && m.AgentID != q.AgentID {
				continue
			}
		}
		switch {
		case q.Repo == domain.MemoryRepoScopeAny:
		case q.Repo == domain.MemoryRepoScopeGlobal || q.RepositoryID == nil:
			if m.RepositoryID != nil {
				continue
			}
		case q.Repo == domain.MemoryRepoScopeProject:
			if m.RepositoryID == nil || *m.RepositoryID != *q.RepositoryID {
				continue
			}
		default:
			if m.RepositoryID != nil && *m.RepositoryID != *q.RepositoryID {
				continue
			}
		}
		out = append(out, m)
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	return out, nil
}

func (f *fakeStore) Update(_ context.Context, m domain.AgentMemory) (domain.AgentMemory, error) {
	for i, row := range f.rows {
		if row.ID == m.ID {
			m.CreatedAt = row.CreatedAt
			m.Scope = domain.MemoryScopeOf(m.AgentID, m.RepositoryID)
			f.rows[i] = m
			return m, nil
		}
	}
	return domain.AgentMemory{}, errNotFound
}

func (f *fakeStore) Delete(_ context.Context, id uuid.UUID) error {
	for i, m := range f.rows {
		if m.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return errNotFound
}

func (f *fakeStore) inScope(m domain.AgentMemory, agentID uuid.UUID, repositoryID *uuid.UUID) bool {
	if m.AgentID != agentID {
		return false
	}
	if repositoryID == nil {
		return m.RepositoryID == nil
	}
	return m.RepositoryID != nil && *m.RepositoryID == *repositoryID
}

func (f *fakeStore) CountInScope(_ context.Context, agentID uuid.UUID, repositoryID *uuid.UUID) (int, error) {
	n := 0
	for _, m := range f.rows {
		if f.inScope(m, agentID, repositoryID) {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) DeleteOldestInScope(_ context.Context, agentID uuid.UUID, repositoryID *uuid.UUID, n int) error {
	for i := 0; i < len(f.rows) && n > 0; {
		if f.inScope(f.rows[i], agentID, repositoryID) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			n--
			continue
		}
		i++
	}
	return nil
}

type notFoundError struct{}

func (notFoundError) Error() string { return "memory not found" }

var errNotFound = notFoundError{}

func contents(mems []domain.AgentMemory) []string {
	out := make([]string, len(mems))
	for i, m := range mems {
		out[i] = m.Content
	}
	return out
}

func TestSaveRecordsScope(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil, "", 100)
	repoID := uuid.New()
	agentID := uuid.New()

	agentProject, err := svc.Save(context.Background(), agentID, &repoID, "repo lesson", "lesson", domain.MemorySourceAgent)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if agentProject.Scope != domain.MemoryScopeAgentProject {
		t.Fatalf("scope = %q, want %q", agentProject.Scope, domain.MemoryScopeAgentProject)
	}

	teamGlobal, err := svc.SaveShared(context.Background(), nil, "workspace convention", "convention", domain.MemorySourceUser)
	if err != nil {
		t.Fatalf("save shared: %v", err)
	}
	if teamGlobal.Scope != domain.MemoryScopeTeamGlobal {
		t.Fatalf("scope = %q, want %q", teamGlobal.Scope, domain.MemoryScopeTeamGlobal)
	}

	teamProject, err := svc.SaveShared(context.Background(), &repoID, "repo build command", "convention", domain.MemorySourceUser)
	if err != nil {
		t.Fatalf("save shared project: %v", err)
	}
	if teamProject.Scope != domain.MemoryScopeTeamProject {
		t.Fatalf("scope = %q, want %q", teamProject.Scope, domain.MemoryScopeTeamProject)
	}
}

func TestRecallCombinesProjectAndGlobalAndSkipsOtherRepos(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil, "", 100)
	agentID := uuid.New()
	repoA, repoB := uuid.New(), uuid.New()
	ctx := context.Background()

	mustSave(t, svc, agentID, &repoA, "A: run make verify")
	mustSave(t, svc, agentID, &repoB, "B: run gradle check")
	mustSave(t, svc, agentID, nil, "always write tests first")
	if _, err := svc.SaveShared(ctx, &repoA, "A: staging deploys on merge", "", domain.MemorySourceUser); err != nil {
		t.Fatalf("save shared: %v", err)
	}
	if _, err := svc.SaveShared(ctx, nil, "team: never force-push main", "", domain.MemorySourceUser); err != nil {
		t.Fatalf("save shared: %v", err)
	}

	got := contents(svc.Recall(ctx, agentID, &repoA, 8))
	want := map[string]bool{
		"A: run make verify":          true,
		"A: staging deploys on merge": true,
		"always write tests first":    true,
		"team: never force-push main": true,
	}
	if len(got) != len(want) {
		t.Fatalf("recall = %v, want %d entries", got, len(want))
	}
	for _, c := range got {
		if !want[c] {
			t.Fatalf("recall leaked %q (repo B or unrelated): %v", c, got)
		}
	}
}

func TestRecallWithoutRepositoryReturnsOnlyGlobal(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil, "", 100)
	agentID := uuid.New()
	repoA := uuid.New()

	mustSave(t, svc, agentID, &repoA, "A: run make verify")
	mustSave(t, svc, agentID, nil, "always write tests first")

	got := contents(svc.Recall(context.Background(), agentID, nil, 8))
	if len(got) != 1 || got[0] != "always write tests first" {
		t.Fatalf("recall = %v, want only the global memory", got)
	}
}

func TestEvictionIsPerScope(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil, "", 1)
	agentID := uuid.New()
	repoA, repoB := uuid.New(), uuid.New()

	mustSave(t, svc, agentID, &repoA, "A: old")
	mustSave(t, svc, agentID, &repoB, "B: keep me")
	mustSave(t, svc, agentID, nil, "global: keep me")
	mustSave(t, svc, agentID, &repoA, "A: new")

	remaining := contents(mustList(t, svc, domain.MemoryQuery{AgentID: agentID, Repo: domain.MemoryRepoScopeAny}))
	for _, c := range remaining {
		if c == "A: old" {
			t.Fatalf("over-cap repo memory survived: %v", remaining)
		}
	}
	for _, want := range []string{"A: new", "B: keep me", "global: keep me"} {
		found := false
		for _, c := range remaining {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("eviction crossed scopes, %q gone: %v", want, remaining)
		}
	}
}

func TestTeamMemoriesAreNotEvicted(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil, "", 1)
	ctx := context.Background()
	repoID := uuid.New()

	for _, c := range []string{"team one", "team two", "team three"} {
		if _, err := svc.SaveShared(ctx, &repoID, c, "", domain.MemorySourceUser); err != nil {
			t.Fatalf("save shared: %v", err)
		}
	}
	got := mustList(t, svc, domain.MemoryQuery{Owner: domain.MemoryOwnerTeam, RepositoryID: &repoID, Repo: domain.MemoryRepoScopeProject})
	if len(got) != 3 {
		t.Fatalf("team memories evicted: %v", contents(got))
	}
}

func TestUpdateKeepsScope(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil, "", 100)
	agentID := uuid.New()
	repoID := uuid.New()
	ctx := context.Background()

	mem := mustSave(t, svc, agentID, &repoID, "old text")
	updated, err := svc.Update(ctx, agentID, mem.ID, "new text", "lesson")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.RepositoryID == nil || *updated.RepositoryID != repoID {
		t.Fatalf("update dropped the project scope: %+v", updated)
	}
	if updated.Scope != domain.MemoryScopeAgentProject {
		t.Fatalf("scope = %q, want %q", updated.Scope, domain.MemoryScopeAgentProject)
	}
}

func TestAgentCannotUpdateTeamMemory(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil, "", 100)
	ctx := context.Background()

	mem, err := svc.SaveShared(ctx, nil, "team rule", "", domain.MemorySourceUser)
	if err != nil {
		t.Fatalf("save shared: %v", err)
	}
	if _, err := svc.Update(ctx, uuid.New(), mem.ID, "hijacked", ""); err == nil {
		t.Fatal("agent updated a team memory through the agent endpoint")
	}
}

func mustSave(t *testing.T, svc *Service, agentID uuid.UUID, repoID *uuid.UUID, content string) domain.AgentMemory {
	t.Helper()
	mem, err := svc.Save(context.Background(), agentID, repoID, content, "", domain.MemorySourceAgent)
	if err != nil {
		t.Fatalf("save %q: %v", content, err)
	}
	return mem
}

func mustList(t *testing.T, svc *Service, q domain.MemoryQuery) []domain.AgentMemory {
	t.Helper()
	got, err := svc.List(context.Background(), q)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return got
}
