package tenantboot

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// fakeSeeds stands in for install_state: once the board is seeded it stays
// seeded.
type fakeSeeds struct {
	mu      sync.Mutex
	already bool
	seeded  int
}

func (m *fakeSeeds) SeedBoardOnce(context.Context, string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.already {
		return false, nil
	}
	m.already = true
	m.seeded++
	return true, nil
}

// stepRecorder captures the tenant each step run was scoped to. That is the
// whole property under test: the steps used to run on the boot context, which
// carries no identity, so every one of them raised tenant.ErrNoTenant and wrote
// nothing for anybody.
type stepRecorder struct {
	mu    sync.Mutex
	seen  []uuid.UUID
	fail  error
	after chan struct{}
}

func newStepRecorder() *stepRecorder {
	return &stepRecorder{after: make(chan struct{}, 8)}
}

func (r *stepRecorder) run(ctx context.Context) error {
	r.mu.Lock()
	id, _ := tenant.ID(ctx)
	r.seen = append(r.seen, id)
	err := r.fail
	r.mu.Unlock()
	r.after <- struct{}{}
	return err
}

func (r *stepRecorder) tenants() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uuid.UUID(nil), r.seen...)
}

func (r *stepRecorder) waitFor(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-r.after:
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d of %d step runs happened", i, n)
		}
	}
}

func newService() (*Service, *fakeSeeds) {
	seeds := &fakeSeeds{}
	return NewService(seeds), seeds
}

// A step runs for the tenant on the request, and it runs once per tenant per
// process however busy that tenant is.
func TestStepsRunOncePerTenantScopedToIt(t *testing.T) {
	svc, _ := newService()
	rec := newStepRecorder()
	svc.AddStep("probe", rec.run)

	first := uuid.New()
	second := uuid.New()
	for i := 0; i < 3; i++ {
		if err := svc.Sight(context.Background(), tenant.Identity{TenantID: first, Role: tenant.RoleOwner}); err != nil {
			t.Fatalf("Sight: %v", err)
		}
	}
	if err := svc.Sight(context.Background(), tenant.Identity{TenantID: second, Role: tenant.RoleMember}); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	rec.waitFor(t, 2)

	seen := rec.tenants()
	if len(seen) != 2 {
		t.Fatalf("step ran %d times, want once per tenant", len(seen))
	}
	got := map[uuid.UUID]bool{seen[0]: true, seen[1]: true}
	if !got[first] || !got[second] {
		t.Fatalf("steps ran for %v, want %v and %v", seen, first, second)
	}
}

// The identity is what the whole mechanism exists to carry. A step that reads
// no tenant off its context is the bug, not a degraded mode.
func TestStepContextCarriesTheTenant(t *testing.T) {
	svc, _ := newService()
	rec := newStepRecorder()
	svc.AddStep("probe", rec.run)

	id := uuid.New()
	if err := svc.Sight(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleOwner}); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	rec.waitFor(t, 1)

	if seen := rec.tenants(); len(seen) != 1 || seen[0] != id {
		t.Fatalf("step saw %v, want a context scoped to %v", seen, id)
	}
}

// A failing step is logged and skipped, never fatal: one tenant whose mcp
// catalog did not seed still has a working board, and the request that happened
// to be first must not fail because of a seed nobody asked for.
func TestAFailingStepDoesNotFailTheRequest(t *testing.T) {
	svc, _ := newService()
	bad := newStepRecorder()
	bad.fail = errors.New("nope")
	good := newStepRecorder()
	svc.AddStep("bad", bad.run)
	svc.AddStep("good", good.run)

	if err := svc.Sight(context.Background(), tenant.Identity{TenantID: uuid.New(), Role: tenant.RoleOwner}); err != nil {
		t.Fatalf("Sight must not surface a step failure: %v", err)
	}
	bad.waitFor(t, 1)
	good.waitFor(t, 1)
}

// The board seed is the durable, once-ever half; the steps are the
// once-per-process half. A tenant seeded by an earlier build still gets the
// steps, which is how new seed data reaches tenants that already exist.
func TestStepsRunForAnAlreadySeededTenant(t *testing.T) {
	svc, seeds := newService()
	rec := newStepRecorder()
	svc.AddStep("probe", rec.run)

	id := uuid.New()
	seeds.already = true // install_state says the board already exists

	if err := svc.Sight(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleOwner}); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	rec.waitFor(t, 1)

	seeds.mu.Lock()
	seeded := seeds.seeded
	seeds.mu.Unlock()
	if seeded != 0 {
		t.Fatalf("the board was re-seeded %d times for a tenant that already had one", seeded)
	}
	if seen := rec.tenants(); len(seen) != 1 {
		t.Fatalf("step ran %d times for an existing tenant, want 1", len(seen))
	}
}
