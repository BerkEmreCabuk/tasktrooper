package bootseed

import (
	"context"
	_ "embed"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

//go:embed seed.sql
var seedSQL string

type SeedStore interface {
	SeedBoardOnce(ctx context.Context, sql string) (bool, error)
}

type Step struct {
	Name string
	Run  func(context.Context) error
}

type Service struct {
	seeds SeedStore

	steps []Step

	seeded atomic.Bool

	started atomic.Bool

	running atomic.Int32
}

func NewService(seeds SeedStore) *Service {
	return &Service{seeds: seeds}
}

func (s *Service) AddStep(name string, run func(context.Context) error) {
	if s == nil || run == nil || name == "" {
		return
	}
	s.steps = append(s.steps, Step{Name: name, Run: run})
}

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

func (s *Service) Booting() bool {
	if s == nil {
		return false
	}
	return s.running.Load() > 0
}

const stepTimeout = 2 * time.Minute

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
