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

type fakeRegistry struct {
	mu           sync.Mutex
	needs        map[uuid.UUID]bool
	bootstrapped []uuid.UUID
}

func (r *fakeRegistry) EnsureTenant(_ context.Context, id uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.needs[id], nil
}

func (r *fakeRegistry) MarkBootstrapped(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bootstrapped = append(r.bootstrapped, id)
	return nil
}

type fakeMembers struct {
	mu      sync.Mutex
	seeded  int
	members []Member
}

func (m *fakeMembers) UpsertMember(_ context.Context, member Member) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.members = append(m.members, member)
	return nil
}

func (m *fakeMembers) ListMembers(context.Context) ([]Member, error) { return nil, nil }

func (m *fakeMembers) Seed(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seeded++
	return nil
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

func newService() (*Service, *fakeRegistry, *fakeMembers) {
	reg := &fakeRegistry{needs: map[uuid.UUID]bool{}}
	members := &fakeMembers{}
	return NewService(reg, members), reg, members
}

// A step runs for the tenant on the request, and it runs once per tenant per
// process however busy that tenant is.
func TestStepsRunOncePerTenantScopedToIt(t *testing.T) {
	svc, _, _ := newService()
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
	svc, _, _ := newService()
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
	svc, _, _ := newService()
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

// The mirror is the set task assignment validates against, so it takes a
// caller only when there is a person behind it. A control-scope call is the
// control plane acting on its own behalf and names none, whatever it puts in
// the actor header — it used to put the tenant's own uuid there, and every
// tenant ended up with a nameless "member" that a card could be assigned to.
func TestSightDoesNotMirrorAControlPlaneCall(t *testing.T) {
	svc, _, members := newService()

	id := uuid.New()
	if err := svc.Sight(context.Background(), tenant.Identity{
		TenantID:     id,
		Role:         tenant.RoleOwner,
		UserID:       id.String(),
		ControlPlane: true,
	}); err != nil {
		t.Fatalf("Sight: %v", err)
	}

	members.mu.Lock()
	defer members.mu.Unlock()
	if len(members.members) != 0 {
		t.Fatalf("a control-scope call was mirrored into the roster: %+v", members.members)
	}
}

// And the ordinary case still works, which is what makes the test above mean
// something: a proxied request is how a teammate becomes assignable at all.
func TestSightMirrorsAProxiedCaller(t *testing.T) {
	svc, _, members := newService()

	if err := svc.Sight(context.Background(), tenant.Identity{
		TenantID: uuid.New(), Role: tenant.RoleAdmin, UserID: "firebase-uid-7",
	}); err != nil {
		t.Fatalf("Sight: %v", err)
	}

	members.mu.Lock()
	defer members.mu.Unlock()
	if len(members.members) != 1 {
		t.Fatalf("roster writes = %d, want 1", len(members.members))
	}
	if got := members.members[0]; got.UserID != "firebase-uid-7" || got.Role != tenant.RoleAdmin {
		t.Fatalf("mirrored %+v, want the caller's uid and role", got)
	}
}

// The board seed is the durable, once-ever half; the steps are the
// once-per-process half. A tenant seeded by an earlier build still gets the
// steps, which is how new seed data reaches tenants that already exist.
func TestStepsRunForAnAlreadySeededTenant(t *testing.T) {
	svc, reg, members := newService()
	rec := newStepRecorder()
	svc.AddStep("probe", rec.run)

	id := uuid.New()
	reg.needs[id] = false // the registry says this tenant's board already exists

	if err := svc.Sight(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleOwner}); err != nil {
		t.Fatalf("Sight: %v", err)
	}
	rec.waitFor(t, 1)

	members.mu.Lock()
	seeded := members.seeded
	members.mu.Unlock()
	if seeded != 0 {
		t.Fatalf("the board was re-seeded %d times for a tenant that already had one", seeded)
	}
	if seen := rec.tenants(); len(seen) != 1 {
		t.Fatalf("step ran %d times for an existing tenant, want 1", len(seen))
	}
}
