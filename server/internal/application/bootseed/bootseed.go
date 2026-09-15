// Package bootseed seeds the default board once per install and runs the boot
// steps once per process.
//
//  1. **Seed the board.** The 13 board columns, the task counters, the provider
//     list, the model prices and the single settings rows are not in the
//     migrations; they are seeded here, gated by install_state.board_seeded_at,
//     so a board the user has since edited is never seeded again.
//
//  2. **Run the boot steps.** See Step.
//
// Both are idempotent and cheap after the first time: a successful board seed
// is cached, and the steps are launched once.
package bootseed

import (
	"context"
	_ "embed"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// seedSQL is the default board, run once per install.
//
// It is a .sql file rather than Go string literals so a schema change and its
// seed can be read side by side.
//
//go:embed seed.sql
var seedSQL string

// SeedStore runs the board seed unless it already ran on this install, and
// reports whether it ran.
type SeedStore interface {
	SeedBoardOnce(ctx context.Context, sql string) (bool, error)
}

// Step is "ensure the defaults exist" work that needs the database and must not
// hold up the listener: the mcp server seed, the role agent seed, the llm
// provider bootstrap, the mobile device list.
type Step struct {
	// Name is what a failure is logged under: it has to say which seed did not
	// happen.
	Name string
	Run  func(context.Context) error
}

type Service struct {
	seeds SeedStore

	// steps run once per PROCESS, not once ever.
	//
	// Once-ever would need a durable marker and would then skip an install
	// seeded by an older build every time a step is added — which is how seed
	// data goes missing silently. Every step is an idempotent "ensure", so the
	// cost of repeating it on the next start is one read that finds everything
	// already there.
	steps []Step

	// seeded caches a successful board seed, so a request does not pay a
	// database round trip. A failed seed is not cached: the next Ensure retries.
	seeded atomic.Bool
	// started is set when the steps are launched, so they never launch twice.
	started atomic.Bool
	// running counts the step runs in flight, so a caller can tell an empty
	// roster from one that is still being written.
	running atomic.Int32
}

func NewService(seeds SeedStore) *Service {
	return &Service{seeds: seeds}
}

// AddStep registers boot setup. Called during wiring, before the listener
// accepts anything, so the slice is never written after it is first read.
//
// A nil run is dropped rather than stored: the wiring is full of "this store
// exists only when Postgres does" branches, and a step whose dependency was
// never built must be absent, not a nil dereference.
func (s *Service) AddStep(name string, run func(context.Context) error) {
	if s == nil || run == nil || name == "" {
		return
	}
	s.steps = append(s.steps, Step{Name: name, Run: run})
}

// Ensure seeds the board unless that already succeeded, then launches the steps
// unless they were already launched. Boot calls it; so does every request, which
// is how a seed that failed at boot is retried.
func (s *Service) Ensure(ctx context.Context) error {
	if s == nil || s.seeds == nil {
		return nil
	}
	if err := s.ensureSeeded(ctx); err != nil {
		return err
	}
	s.runSteps(ctx)
	return nil
}

// Booting reports whether this process is still running the registered steps.
func (s *Service) Booting() bool {
	if s == nil {
		return false
	}
	return s.running.Load() > 0
}

// stepTimeout bounds the whole step run. It is generous because the steps are
// seeds rather than requests — the role agent seed alone writes a dozen rows —
// and it exists only so a wedged database cannot leave the goroutine alive for
// the life of the process.
const stepTimeout = 2 * time.Minute

// runSteps runs the registered steps once per process, in the background.
//
// Background, not inline: the role agent seed is a dozen writes, and nothing
// should wait for them. The BOARD seed above is still synchronous — a first page
// load with no columns is a broken page, whereas an mcp catalog that appears a
// second later is a settings screen nobody has opened yet.
//
// The context keeps the caller's values but not its cancellation: the request
// that happened to trigger the steps ends long before they do.
func (s *Service) runSteps(ctx context.Context) {
	if len(s.steps) == 0 || !s.started.CompareAndSwap(false, true) {
		return
	}
	s.running.Add(1)
	stepCtx := context.WithoutCancel(ctx)
	go func() {
		stepCtx, cancel := context.WithTimeout(stepCtx, stepTimeout)
		defer cancel()
		defer s.running.Add(-1)
		for _, step := range s.steps {
			if err := step.Run(stepCtx); err != nil {
				// Logged and continued, never fatal and never retried: one
				// failed seed must not stop the next, and an install whose mcp
				// catalog did not seed still has a working board. The next start
				// tries again.
				log.Warn().Err(err).Str("step", step.Name).Msg("boot step failed")
			}
		}
		log.Info().Int("steps", len(s.steps)).Msg("boot steps done")
	}()
}

func (s *Service) ensureSeeded(ctx context.Context) error {
	if s.seeded.Load() {
		return nil
	}
	ran, err := s.seeds.SeedBoardOnce(ctx, seedSQL)
	if err != nil {
		return fmt.Errorf("seed board: %w", err)
	}
	s.seeded.Store(true)
	if ran {
		log.Info().Msg("default board seeded")
	}
	return nil
}
