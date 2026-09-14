package evolution

import (
	"context"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// evaluateImpacts classifies pending evolution events whose observation window
// has elapsed by comparing score events before vs after the change.
func (s *Service) evaluateImpacts(ctx context.Context) {
	cutoff := time.Now().Add(-s.cfg.ImpactWindow)
	pending, err := s.store.ListEventsByImpact(ctx, domain.EvolutionImpactPending, cutoff, 100)
	if err != nil {
		log.Warn().Err(err).Msg("impact evaluation: list pending failed")
		return
	}
	for _, e := range pending {
		before, err := s.perf.EventsInWindow(ctx, e.AgentID, e.CreatedAt.Add(-s.cfg.ImpactWindow), e.CreatedAt)
		if err != nil {
			continue
		}
		after, err := s.perf.EventsInWindow(ctx, e.AgentID, e.CreatedAt, e.CreatedAt.Add(s.cfg.ImpactWindow))
		if err != nil {
			continue
		}
		impact := ClassifyImpact(before, after, s.cfg.MinEventsForImpact)
		if err := s.store.UpdateEventImpact(ctx, e.ID, impact); err != nil {
			log.Warn().Err(err).Str("event", e.ID.String()).Msg("impact update failed")
			continue
		}
		log.Info().Str("event", e.ID.String()).Str("change", e.ChangeType).Str("impact", impact).Msg("evolution impact evaluated")
	}
}

// ClassifyImpact compares score-event windows around an evolution change.
// effective: revision rate dropped ≥0.15 OR net delta improved ≥5.
// regressed: the opposite by the same margins. Otherwise neutral.
// Too little after-data → insufficient_data.
func ClassifyImpact(before, after []domain.AgentScoreEvent, minEvents int) string {
	if len(after) < minEvents {
		return domain.EvolutionImpactInsufficientData
	}
	beforeRev, beforeNet := windowStats(before)
	afterRev, afterNet := windowStats(after)

	if len(before) < minEvents {
		// Not enough baseline: judge on net delta alone.
		switch {
		case afterNet >= 5:
			return domain.EvolutionImpactEffective
		case afterNet <= -5:
			return domain.EvolutionImpactRegressed
		default:
			return domain.EvolutionImpactNeutral
		}
	}
	switch {
	case afterRev <= beforeRev-0.15 || afterNet >= beforeNet+5:
		return domain.EvolutionImpactEffective
	case afterRev >= beforeRev+0.15 || afterNet <= beforeNet-5:
		return domain.EvolutionImpactRegressed
	default:
		return domain.EvolutionImpactNeutral
	}
}

func windowStats(events []domain.AgentScoreEvent) (revisionRate, netDelta float64) {
	if len(events) == 0 {
		return 0, 0
	}
	neg := 0
	for _, e := range events {
		netDelta += e.Delta
		if e.Delta < 0 {
			neg++
		}
	}
	return float64(neg) / float64(len(events)), netDelta
}
