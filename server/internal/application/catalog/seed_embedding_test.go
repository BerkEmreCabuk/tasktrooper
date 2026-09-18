package catalog

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// notReadyEmbedLLM is an embedder whose model has not loaded: every call waits
// until the caller gives up, as a client retrying a 503 would.
type notReadyEmbedLLM struct {
	stubLLMClient
	calls atomic.Int32
}

func (n *notReadyEmbedLLM) Embed(ctx context.Context, _ string, _ string) ([]float32, error) {
	n.calls.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

type readyEmbedLLM struct {
	stubLLMClient
	calls atomic.Int32
}

func (r *readyEmbedLLM) Embed(context.Context, string, string) ([]float32, error) {
	r.calls.Add(1)
	return []float32{0.25, 0.5}, nil
}

// createAllRoleAgentsFromTemplates is what the UI's "set up the ready-made
// team" action does: upsert the built-in templates, then create one agent per
// role from them — the only path that creates a role agent today.
func createAllRoleAgentsFromTemplates(t *testing.T, ctx context.Context, svc *Service, templates *memTemplateStore) {
	t.Helper()
	if err := svc.EnsureRoleTemplates(ctx); err != nil {
		t.Fatalf("EnsureRoleTemplates: %v", err)
	}
	for _, tpl := range templates.templates {
		if _, err := svc.CreateAgentFromTemplate(ctx, tpl.ID, domain.CreateAgentRequest{}); err != nil {
			t.Fatalf("CreateAgentFromTemplate %s: %v", tpl.Name, err)
		}
	}
}

// Creating the role agents from their built-in templates must never wait on
// the embedder: seedSkill (used by CreateAgentFromTemplate to copy a
// template's skills onto the new agent) stores every skill without a vector,
// the same way the old boot-time seed did.
func TestCreateAgentFromTemplate_NeverWaitsOnTheEmbedder(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	llm := &notReadyEmbedLLM{}
	svc := NewService(store, llm, "")
	svc.SetTemplateStore(templates)
	// A safety net, not the point of the test: if this ever regressed into
	// waiting on the embedder, ctx.Done() unblocks notReadyEmbedLLM.Embed
	// instead of hanging the suite.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	createAllRoleAgentsFromTemplates(t, ctx, svc, templates)

	if got := llm.calls.Load(); got != 0 {
		t.Fatalf("creating the role agents asked the embedder %d times, want none", got)
	}
	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if want := len(roleAgentDefinitions()); len(agents) != want {
		t.Fatalf("created %d agents, want %d", len(agents), want)
	}
}

// The backfill fills exactly the skills seedSkill left without a vector, and a
// second pass finds nothing left to do.
func TestBackfillSkillEmbeddings_FillsOnlyTheSkillsWithoutAVector(t *testing.T) {
	ctx := context.Background()
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	setupSvc := NewService(store, &notReadyEmbedLLM{}, "")
	setupSvc.SetTemplateStore(templates)
	createAllRoleAgentsFromTemplates(t, ctx, setupSvc, templates)

	ready := &readyEmbedLLM{}
	svc := NewService(store, ready, "")
	updated, err := svc.BackfillSkillEmbeddings(ctx)
	if err != nil {
		t.Fatalf("BackfillSkillEmbeddings: %v", err)
	}
	if updated == 0 || int(ready.calls.Load()) != updated {
		t.Fatalf("updated %d skills with %d embed calls; want one call per skill and at least one skill", updated, ready.calls.Load())
	}

	agents, _ := store.ListAgents(ctx)
	for _, agent := range agents {
		skills, _ := store.ListSkillsByAgent(ctx, agent.ID)
		for _, sk := range skills {
			if len(sk.Embedding) == 0 {
				t.Fatalf("skill %s of %s still has no vector", sk.Name, agent.Name)
			}
		}
	}

	again, err := svc.BackfillSkillEmbeddings(ctx)
	if err != nil || again != 0 {
		t.Fatalf("second pass updated %d (err %v), want 0", again, err)
	}
}
