// Package tenantboot is what happens the first time this server sees a tenant,
// and what happens on every request after.
//
// Three jobs that look unrelated and are not:
//
//  1. **Seed the board.** Migrations used to seed it — 13 board columns, the
//     task counters, the provider list, the model prices, the single settings
//     rows — because a database WAS a tenant. In a shared database a migration
//     runs once for the whole fleet, so those rows would belong to nobody
//     (migration 114 removes them). The seed had to move somewhere that runs
//     once per TENANT, and the only event that reliably happens once per tenant
//     is its first request.
//
//  2. **Mirror the caller into tenant_members.** The control plane owns the
//     roster; this is the local copy the board renders names from and validates
//     an assignee against. Refreshed from the signed headers on the request
//     being served, so it is always at least as fresh as the caller.
//
//  3. **Run the seeds that used to be boot-time work.** See Step. A process
//     that serves every tenant has no tenant at boot, so about a dozen
//     "ensure the defaults exist" calls in buildHandler were running unscoped
//     and doing nothing at all. They are registered here instead and run once
//     per tenant per process, which is the same cost they always had.
//
// All three are idempotent and all three are cheap after the first time: one
// UPDATE for the member, a gate read that the tenants row answers, and a map
// lookup for the steps.
package tenantboot

import (
	"context"
	_ "embed"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// seedSQL is the per-tenant board seed, run with app.tenant_id already set to
// the new tenant, so every INSERT picks the tenant up from the column default
// (migration 114) exactly as an ordinary write does.
//
// It is a .sql file rather than Go string literals so a schema change and its
// seed can be read side by side, and so this file can be diffed against the
// migrations it replaced.
//
//go:embed seed.sql
var seedSQL string

// Registry is the tenant-registry half of postgres.DB — the only part of the
// database that is not policy-protected, because it is the index of tenants
// rather than one tenant's rows.
type Registry interface {
	EnsureTenant(ctx context.Context, id uuid.UUID) (needsBootstrap bool, err error)
	MarkBootstrapped(ctx context.Context, id uuid.UUID) error
}

// MemberStore mirrors the caller into tenant_members.
type MemberStore interface {
	UpsertMember(ctx context.Context, m Member) error
	ListMembers(ctx context.Context) ([]Member, error)
	Seed(ctx context.Context, sql string) error
}

// Member is one row of the local roster mirror.
type Member struct {
	UserID      string
	Email       string
	DisplayName string
	Role        tenant.Role
}

// Step is per-tenant setup that used to happen once, at process boot, back when
// a process served one tenant.
//
// There were about a dozen of them and they all failed the same way: the mcp
// server seed, the role agent seed, the llm provider bootstrap, the mobile
// device list. Each ran in buildHandler on the bare boot context, which carries
// no identity, so every one raised tenant.ErrNoTenant, logged a Warn and did
// nothing. The server booted, listened and reported healthy — the seeds simply
// never happened, for anybody.
//
// They cannot go back to being boot-time work in any form. A shared process has
// no tenant at boot and, on a cold pod, may not even have a tenant LIST: the
// registry is populated by first sight, so a fan-out at boot would iterate zero
// rows and report success. The only event that reliably happens once per tenant
// is a request from it, which is exactly what this package already hangs the
// board seed on.
type Step struct {
	// Name is what a failure is logged under. It is the sweeper-name of this
	// mechanism: it has to say which seed did not happen.
	Name string
	Run  func(context.Context) error
}

type Service struct {
	registry Registry
	members  MemberStore

	// steps run once per tenant per PROCESS, not once per tenant ever.
	//
	// Once-ever would need a durable marker and would then skip a tenant
	// seeded by an older build every time a step is added — which is how seed
	// data goes missing silently. Every step is an idempotent "ensure", so the
	// cost of repeating it on a new pod is one read that finds everything
	// already there, and that is the same cost profile these calls had when
	// they ran once per process at boot.
	steps []Step

	// seeded remembers the tenants this process has already seeded, so a busy
	// tenant does not pay a registry read per request. It is a cache of a fact
	// that only ever goes one way (a tenant is never un-seeded), so a cold
	// process simply asks the database once more.
	seeded sync.Map
	// booted is the same idea for the steps above, and it is a SEPARATE map
	// because the two answer different questions: seeded is "does this tenant's
	// board exist" (durable, shared by every replica through the registry row),
	// booted is "has THIS process run its ensure-steps for this tenant" (local,
	// and correctly re-done by the next pod).
	booted sync.Map
}

func NewService(registry Registry, members MemberStore) *Service {
	return &Service{registry: registry, members: members}
}

// AddStep registers per-tenant setup. Called during wiring, before the listener
// accepts anything, so the slice is never written after it is first read.
//
// A nil run is dropped rather than stored: the wiring is full of "this store
// exists only when Postgres does" branches, and a step whose dependency was
// never built must be absent, not a nil dereference on the first request.
func (s *Service) AddStep(name string, run func(context.Context) error) {
	if s == nil || run == nil || name == "" {
		return
	}
	s.steps = append(s.steps, Step{Name: name, Run: run})
}

// Sight is called by the HTTP tenant middleware for every authenticated
// request. It is deliberately the whole per-request cost of multi-tenancy at
// the edge: a map lookup, and one member upsert.
func (s *Service) Sight(ctx context.Context, id tenant.Identity) error {
	if s == nil || s.registry == nil {
		return nil
	}
	if err := s.ensureSeeded(ctx, id.TenantID); err != nil {
		return err
	}
	s.runSteps(ctx, id)
	if id.UserID == "" || id.ControlPlane {
		// Nobody to mirror. Either the call names no human — a machine call, or
		// a self-hosted run with no gateway — or it is the control plane acting
		// on its own behalf, whose actor is not a person however the far side
		// filled it in. Not an error: the board simply records no actor, as it
		// did before teams.
		//
		// The scope is checked here rather than trusted to be absent, because
		// this roster is the set assignment validates against: a control-scope
		// call that signed the tenant id as its actor gave every tenant a
		// nameless "member" that a card could be handed to and whose Mac can
		// never attach. Anything that can write this table owes a check at the
		// point of writing.
		return nil
	}
	return s.members.UpsertMember(ctx, Member{UserID: id.UserID, Role: id.Role})
}

// stepTimeout bounds one tenant's whole step run. It is generous because the
// steps are seeds rather than requests — the role agent seed alone writes a
// dozen rows — and it exists only so a wedged database cannot leave a goroutine
// per tenant alive for the life of the pod.
const stepTimeout = 2 * time.Minute

// runSteps runs the registered steps once per tenant per process, in the
// background.
//
// Background, not inline, for the reason the role agent seed was already a
// goroutine at boot: it is a dozen writes, and the request that happened to be
// first must not pay for them. The BOARD seed above is still synchronous — a
// first page load with no columns is a broken page, whereas an mcp catalog that
// appears a second later is a settings screen nobody has opened yet.
//
// The context is detached from the request (which is about to end) but keeps
// its values, so the tenant identity the steps need rides along unchanged. That
// is the whole difference from what these calls did before: same work, same
// once-per-process cost, on a context that names who it is for.
func (s *Service) runSteps(ctx context.Context, id tenant.Identity) {
	if len(s.steps) == 0 {
		return
	}
	if _, done := s.booted.LoadOrStore(id.TenantID, struct{}{}); done {
		return
	}
	// Least privilege, matching tenant.EachTenant: a seed has no human behind
	// it and makes no role decisions, so if one ever grows a role check it gets
	// the answer a stranger would rather than the answer this caller happens to
	// have.
	stepCtx := tenant.With(context.WithoutCancel(ctx), tenant.Identity{
		TenantID: id.TenantID,
		Role:     tenant.RoleMember,
	})
	go func() {
		stepCtx, cancel := context.WithTimeout(stepCtx, stepTimeout)
		defer cancel()
		for _, step := range s.steps {
			if err := step.Run(stepCtx); err != nil {
				// Logged and continued, never fatal and never retried: one
				// failed seed must not stop the next, and a tenant whose mcp
				// catalog did not seed still has a working board. The next pod
				// to see this tenant tries again.
				log.Warn().Err(err).
					Str("tenant_id", id.TenantID.String()).
					Str("step", step.Name).
					Msg("tenant boot step failed")
			}
		}
		log.Info().Str("tenant_id", id.TenantID.String()).
			Int("steps", len(s.steps)).Msg("tenant boot steps done")
	}()
}

func (s *Service) ensureSeeded(ctx context.Context, id uuid.UUID) error {
	if _, done := s.seeded.Load(id); done {
		return nil
	}
	needs, err := s.registry.EnsureTenant(ctx, id)
	if err != nil {
		return err
	}
	if !needs {
		s.seeded.Store(id, struct{}{})
		return nil
	}
	if s.members != nil {
		if err := s.members.Seed(ctx, seedSQL); err != nil {
			return fmt.Errorf("seed tenant board: %w", err)
		}
	}
	if err := s.registry.MarkBootstrapped(ctx, id); err != nil {
		return err
	}
	s.seeded.Store(id, struct{}{})
	log.Info().Str("tenant_id", id.String()).Msg("tenant board seeded")
	return nil
}

// Members returns the local roster for the tenant on ctx. The board uses it to
// render names and to check that an assignee is really in this tenant.
func (s *Service) Members(ctx context.Context) ([]Member, error) {
	if s == nil || s.members == nil {
		return nil, nil
	}
	return s.members.ListMembers(ctx)
}

// IsMember answers "may this card be assigned to this person".
//
// It is a check against the MIRROR, so it can only be wrong in one direction:
// a teammate invited in the control plane who has never opened the app is not
// in it yet and cannot be assigned. That is the safe direction — the other one
// would let a card be assigned to a uid from another tenant.
func (s *Service) IsMember(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return true, nil // unassigning is always allowed
	}
	members, err := s.Members(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range members {
		if m.UserID == userID {
			return true, nil
		}
	}
	return false, nil
}
