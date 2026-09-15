package catalog

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
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

// The seed runs inside a boot step with a deadline, on a launch where the
// embedder may still be downloading its model and is paced even when it is not.
// It must create every role agent without asking it anything.
func TestEnsureRoleAgents_NeverWaitsOnTheEmbedder(t *testing.T) {
	store := newMemCatalogStore()
	llm := &notReadyEmbedLLM{}
	svc := NewService(store, llm, "")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := svc.EnsureRoleAgents(ctx); err != nil {
		t.Fatalf("EnsureRoleAgents: %v", err)
	}
	if got := llm.calls.Load(); got != 0 {
		t.Fatalf("seed asked the embedder %d times, want none", got)
	}
	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if want := len(roleAgentDefinitions()); len(agents) != want {
		t.Fatalf("seeded %d agents, want %d", len(agents), want)
	}
}

// The backfill fills exactly the skills the seed left without a vector, and a
// second pass finds nothing left to do.
func TestBackfillSkillEmbeddings_FillsOnlyTheSkillsWithoutAVector(t *testing.T) {
	ctx := context.Background()
	store := newMemCatalogStore()
	if err := NewService(store, &notReadyEmbedLLM{}, "").EnsureRoleAgents(ctx); err != nil {
		t.Fatalf("EnsureRoleAgents: %v", err)
	}

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
