package catalog

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Embedder whose model has not loaded: every call waits until the caller gives up, as a client retrying a 503 would.
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

// Templates are no longer boot-time seeded, so each test states its own.
func seedTemplatesWithSkills(t *testing.T, templates *memTemplateStore, names ...string) {
	t.Helper()
	ctx := context.Background()
	for _, name := range names {
		_, err := templates.UpsertByName(ctx, domain.AgentTemplate{
			Name: name, Description: "built-in role", BuiltIn: true,
			Skills: []domain.TemplateSkill{
				{Name: name + "-skill-1", Description: "d", Content: "body", Enabled: true},
				{Name: name + "-skill-2", Description: "d", Content: "body", Enabled: true},
			},
		})
		require.NoError(t, err, "%s", name)
	}
}

func TestCreateAgentFromTemplate_NeverWaitsOnTheEmbedder(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	llm := &notReadyEmbedLLM{}
	svc := NewService(store, llm, "")
	svc.SetTemplateStore(templates)
	// Safety net: if creation ever waited on the embedder, ctx.Done() unblocks instead of hanging the suite.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	seedTemplatesWithSkills(t, templates, "qa-agent", "backend-developer")
	for _, tpl := range templates.templates {
		if _, err := svc.CreateAgentFromTemplate(ctx, tpl.ID, domain.CreateAgentRequest{}); err != nil {
			t.Fatalf("CreateAgentFromTemplate %s: %v", tpl.Name, err)
		}
	}

	if got := llm.calls.Load(); got != 0 {
		t.Fatalf("creating the role agents asked the embedder %d times, want none", got)
	}
	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if want := len(templates.templates); len(agents) != want {
		t.Fatalf("created %d agents, want %d", len(agents), want)
	}
}

func TestBackfillSkillEmbeddings_FillsOnlyTheSkillsWithoutAVector(t *testing.T) {
	ctx := context.Background()
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	setupSvc := NewService(store, &notReadyEmbedLLM{}, "")
	setupSvc.SetTemplateStore(templates)
	seedTemplatesWithSkills(t, templates, "qa-agent", "backend-developer")
	for _, tpl := range templates.templates {
		if _, err := setupSvc.CreateAgentFromTemplate(ctx, tpl.ID, domain.CreateAgentRequest{}); err != nil {
			t.Fatalf("CreateAgentFromTemplate %s: %v", tpl.Name, err)
		}
	}

	ready := &readyEmbedLLM{}
	svc := NewService(store, ready, "")
	updated, err := svc.BackfillSkillEmbeddings(ctx)
	if err != nil {
		t.Fatalf("BackfillSkillEmbeddings: %v", err)
	}
	want := 4
	if updated != want || int(ready.calls.Load()) != updated {
		t.Fatalf("updated %d skills with %d embed calls; want %d calls", updated, ready.calls.Load(), want)
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
