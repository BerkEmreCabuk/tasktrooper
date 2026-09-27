package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	appmemory "github.com/makifbaysal/tasktrooper/server/internal/application/memory"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeMemoryStore is just enough of port.AgentMemoryStore to drive
// saveMemoryTool.Execute's duplicate-detection path.
type fakeMemoryStore struct{ rows []domain.AgentMemory }

func (f *fakeMemoryStore) Create(_ context.Context, m domain.AgentMemory) (domain.AgentMemory, error) {
	m.ID = uuid.New()
	f.rows = append(f.rows, m)
	return m, nil
}
func (f *fakeMemoryStore) Get(_ context.Context, id uuid.UUID) (domain.AgentMemory, error) {
	return domain.AgentMemory{}, nil
}
func (f *fakeMemoryStore) List(_ context.Context, q domain.MemoryQuery) ([]domain.AgentMemory, error) {
	return f.rows, nil
}
func (f *fakeMemoryStore) Update(_ context.Context, m domain.AgentMemory) (domain.AgentMemory, error) {
	return m, nil
}
func (f *fakeMemoryStore) Delete(context.Context, uuid.UUID) error { return nil }
func (f *fakeMemoryStore) CountInScope(context.Context, uuid.UUID, *uuid.UUID) (int, error) {
	return len(f.rows), nil
}
func (f *fakeMemoryStore) DeleteOldestInScope(context.Context, uuid.UUID, *uuid.UUID, int) error {
	return nil
}

type fakePromoter struct {
	promoted  bool
	skillName string
}

func (p fakePromoter) MaybePromoteMemory(context.Context, uuid.UUID, *uuid.UUID, string, string) (bool, string, error) {
	return p.promoted, p.skillName, nil
}

// TestScopeProjectRequiredMessagesUnchanged pins save_memory's and
// search_memory's refusal for scope=project with no repository in context,
// ahead of moving that wording into catalog/system/guards.
func TestScopeProjectRequiredMessagesUnchanged(t *testing.T) {
	kit := testKit()
	ctx := registry.ContextWithAgentID(context.Background(), uuid.New())

	execs := NewExecutors(kit)
	saveRes := execs[0].Execute(ctx, `{"content":"some durable fact","scope":"project"}`)
	if want := "scope=project needs a repository in context; this run has none — use scope=global"; saveRes.Content != want {
		t.Errorf("save_memory scope=project message = %q, want %q", saveRes.Content, want)
	}

	searchRes := execs[1].Execute(ctx, `{"scope":"project"}`)
	if want := "scope=project needs a repository in context; this run has none"; searchRes.Content != want {
		t.Errorf("search_memory scope=project message = %q, want %q", searchRes.Content, want)
	}
}

// TestDuplicateSaveNoteUnchanged pins save_memory's "note" field when the
// content is already remembered in the same bucket.
func TestDuplicateSaveNoteUnchanged(t *testing.T) {
	agentID := uuid.New()
	store := &fakeMemoryStore{rows: []domain.AgentMemory{{
		ID: uuid.New(), AgentID: agentID, Content: "the billing webhook retries five times before giving up",
	}}}
	kit := &ToolKit{Memories: appmemory.NewService(store, nil, "", 0)}
	ctx := registry.ContextWithAgentID(context.Background(), agentID)

	res := NewExecutors(kit)[0].Execute(ctx, `{"content":"the billing webhook retries five times before giving up"}`)

	want := "this is already remembered. If your version adds something the stored one lacks, delete that memory and save the fuller sentence; otherwise nothing needs saving."
	if !strings.Contains(res.Content, want) {
		t.Errorf("save_memory duplicate note = %q, want it to contain %q", res.Content, want)
	}
}

// TestPromotedToSkillNoteUnchanged pins save_memory's "note" field when the
// promoter classifies the content as reusable know-how.
func TestPromotedToSkillNoteUnchanged(t *testing.T) {
	agentID := uuid.New()
	kit := &ToolKit{Memories: appmemory.NewService(&fakeMemoryStore{}, nil, "", 0)}
	kit.SetPromoter(fakePromoter{promoted: true, skillName: "deploy checklist"})
	ctx := registry.ContextWithAgentID(context.Background(), agentID)

	res := NewExecutors(kit)[0].Execute(ctx, `{"content":"always run migrations before restarting the API"}`)

	want := "this was reusable know-how, so it was stored in the skill catalog instead of memory"
	if !strings.Contains(res.Content, want) {
		t.Errorf("save_memory promoted note = %q, want it to contain %q", res.Content, want)
	}
}
