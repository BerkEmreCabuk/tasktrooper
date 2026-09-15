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
//  2. **Run the seeds that used to be boot-time work.** See Step. A process
//     that serves every tenant has no tenant at boot, so about a dozen
//     "ensure the defaults exist" calls in buildHandler were running unscoped
//     and doing nothing at all. They are registered here instead and run once
//     per tenant per process, which is the same cost they always had.
//
// Both are idempotent and both are cheap after the first time: the board seed
// is gated by install_state.board_seeded_at, and the steps by a map lookup.
package tenantboot

import (
	"context"
	_ "embed"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// seedSQL is the default board, run once per install.
//
// It is a .sql file rather than Go string literals so a schema change and its
// seed can be read side by side, and so this file can be diffed against the
// migrations it replaced.
//
//go:embed seed.sql
var seedSQL string

// SeedStore runs the board seed unless it already ran on this install, and
// reports whether it ran.
type SeedStore interface {
	SeedBoardOnce(ctx context.Context, sql string) (bool, error)
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
	seeds SeedStore

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
	// tenant does not pay a database round trip per request. It caches a fact
	// that only ever goes one way (a board is never un-seeded), so a cold
	// process simply asks the database once more.
	seeded sync.Map
	// booted is the same idea for the steps above, and it is a SEPARATE map
	// because the two answer different questions: seeded is "does the board
	// exist" (durable, in install_state), booted is "has THIS process run its
	// ensure-steps for this tenant" (local, and correctly re-done by the next
	// pod).
	booted sync.Map
	// running counts the step runs in flight in this process, so a caller can
	// tell an empty roster from one that is still being written.
	running atomic.Int32
}

func NewService(seeds SeedStore) *Service {
	return &Service{seeds: seeds}
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
// request. It is deliberately the whole per-request cost at the edge: a map
// lookup.
func (s *Service) Sight(ctx context.Context, id tenant.Identity) error {
	if s == nil || s.seeds == nil {
		return nil
	}
	if err := s.ensureSeeded(ctx, id.TenantID); err != nil {
		return err
	}
	s.runSteps(ctx, id)
	return nil
}

// Booting reports whether this process is still running the registered steps.
func (s *Service) Booting() bool {
	if s == nil {
		return false
	}
	return s.running.Load() > 0
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
	s.running.Add(1)
	// Least privilege: a seed has no human behind it and makes no role
	// decisions, so if one ever grows a role check it gets the answer a stranger
	// would rather than the answer this caller happens to have.
	stepCtx := tenant.With(context.WithoutCancel(ctx), tenant.Identity{
		TenantID: id.TenantID,
		Role:     tenant.RoleMember,
	})
	go func() {
		stepCtx, cancel := context.WithTimeout(stepCtx, stepTimeout)
		defer cancel()
		defer s.running.Add(-1)
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
	ran, err := s.seeds.SeedBoardOnce(ctx, seedSQL)
	if err != nil {
		return fmt.Errorf("seed board: %w", err)
	}
	s.seeded.Store(id, struct{}{})
	if ran {
		log.Info().Msg("default board seeded")
	}
	return nil
}
