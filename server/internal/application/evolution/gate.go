package evolution

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// proposesCatalogChange reports whether the reflection wants to touch skills or
// rules at all. Memory-only output changes nothing the golden suite measures,
// so the gate stays out of its way (and does not pay for two suite runs).
func proposesCatalogChange(output domain.ReflectionOutput) bool {
	return len(output.Skills) > 0 || len(output.Rules) > 0
}

// enforceGoldenGate grades the applied change set on the before/after golden
// runs and rolls the whole set back when the verdict is revert. It returns the
// line appended to the reflection summary.
//
// All-or-nothing on purpose: the changes were reasoned about together (a new
// rule that only makes sense with the skill it points at), so keeping half of
// a rejected set is the one outcome nobody asked for.
func (s *Service) enforceGoldenGate(
	ctx context.Context,
	agentRec domain.Agent,
	reflection domain.AgentReflection,
	before, after goldenRun,
	applyLog []string,
	events []domain.AgentEvolutionEvent,
	outcomes []domain.ReflectionChangeOutcome,
) (string, *domain.ReflectionGateResult) {
	catalogEvents := catalogChangeEvents(events)
	if len(catalogEvents) == 0 {
		return "", nil
	}

	verdict := s.judgeGoldenGate(ctx, agentRec, before, after, gateSummaryChanges(applyLog))
	line := fmt.Sprintf("\n\nGolden gate: %.0f%% → %.0f%% | verdict %s — %s",
		before.Rate*100, after.Rate*100, keepWord(verdict.Keep), verdict.Reason)
	result := &domain.ReflectionGateResult{BeforeRate: before.Rate, AfterRate: after.Rate, Keep: verdict.Keep, Reason: verdict.Reason}
	if verdict.Keep {
		log.Info().Str("agent", agentRec.Name).Float64("before", before.Rate).Float64("after", after.Rate).
			Msg("golden gate kept the changes")
		return line, result
	}

	revertedIDs := s.revertChangeSet(ctx, agentRec, reflection, catalogEvents, verdict.Reason)
	result.RolledBack = len(revertedIDs)
	markOutcomesRolledBack(outcomes, revertedIDs)
	log.Warn().Str("agent", agentRec.Name).Float64("before", before.Rate).Float64("after", after.Rate).
		Int("reverted", len(revertedIDs)).Msg("golden gate reverted the changes")
	if len(revertedIDs) == 0 {
		return line + "\nRollback failed: the changes are still live — inspect them by hand.", result
	}
	return line + fmt.Sprintf("\nRolled back %d change(s); the agent is back on its pre-reflection skills and rules.", len(revertedIDs)), result
}

// markOutcomesRolledBack flips the outcome of every applied change the gate
// just reverted, matched by the evolution event id the apply step attached to
// it — the same id revertChangeSet reports as successfully undone. outcomes is
// mutated in place: it shares a backing array with the slice
// ReflectionDecision.Changes will be set to, so the caller sees the update
// without a second pass.
func markOutcomesRolledBack(outcomes []domain.ReflectionChangeOutcome, revertedEventIDs []uuid.UUID) {
	if len(revertedEventIDs) == 0 {
		return
	}
	reverted := make(map[uuid.UUID]bool, len(revertedEventIDs))
	for _, id := range revertedEventIDs {
		reverted[id] = true
	}
	for i := range outcomes {
		if outcomes[i].EventID != nil && reverted[*outcomes[i].EventID] {
			outcomes[i].Outcome = domain.ReflectionOutcomeRolledBack
		}
	}
}

func keepWord(keep bool) string {
	if keep {
		return "KEEP"
	}
	return "REVERT"
}

// catalogChangeEvents filters out memory writes and reverts — neither is
// something the gate can or should undo. A revert the reflection itself asked
// for was a deliberate repair, not part of the change set under test.
func catalogChangeEvents(events []domain.AgentEvolutionEvent) []domain.AgentEvolutionEvent {
	var out []domain.AgentEvolutionEvent
	for _, e := range events {
		if e.ChangeType == domain.EvolutionChangeRevert {
			continue
		}
		if e.TargetKind == domain.EvolutionTargetSkill || e.TargetKind == domain.EvolutionTargetRule {
			out = append(out, e)
		}
	}
	return out
}

// revertChangeSet undoes events newest-first, so a skill created and then
// updated within the same reflection unwinds in the order it was built.
func (s *Service) revertChangeSet(
	ctx context.Context,
	agentRec domain.Agent,
	reflection domain.AgentReflection,
	events []domain.AgentEvolutionEvent,
	reason string,
) []uuid.UUID {
	revertCtx := catalog.WithVersionSource(ctx, catalog.VersionSource{
		Source:       domain.CatalogVersionSourceEvolution,
		Reason:       "golden gate rollback: " + reason,
		ReflectionID: &reflection.ID,
	})
	var reverted []uuid.UUID
	for i := len(events) - 1; i >= 0; i-- {
		original := events[i]
		if _, err := s.revertEvent(revertCtx, agentRec, original); err != nil {
			log.Warn().Err(err).Str("event", original.ID.String()).Msg("golden gate rollback failed for one change")
			continue
		}
		// The rollback is itself an evolution event: the history has to show
		// that the change was applied and then taken back, or the next
		// reflection reads the catalog and cannot tell what was tried.
		if _, err := s.store.CreateEvent(ctx, domain.AgentEvolutionEvent{
			ReflectionID: &reflection.ID, AgentID: agentRec.ID,
			ChangeType: domain.EvolutionChangeRevert, TargetKind: original.TargetKind,
			TargetID: original.TargetID, TargetName: original.TargetName,
			Before: original.After, After: original.Before, RevertedEventID: &original.ID,
			ScoreAtChange: original.ScoreAtChange, Impact: domain.EvolutionImpactNeutral,
		}); err != nil {
			log.Warn().Err(err).Msg("golden gate revert event persist failed")
		}
		// The original is already known to have hurt — the gate measured it —
		// so it skips the impact window instead of waiting a week to say so.
		if err := s.store.UpdateEventImpact(ctx, original.ID, domain.EvolutionImpactRegressed); err != nil {
			log.Warn().Err(err).Msg("golden gate impact update failed")
		}
		reverted = append(reverted, original.ID)
	}
	return reverted
}

// gateSummaryChanges is the change list handed to the judge, trimmed so a long
// batch cannot dominate its prompt.
func gateSummaryChanges(applyLog []string) []string {
	if len(applyLog) <= 20 {
		return applyLog
	}
	return append(applyLog[:20:20], fmt.Sprintf("…and %d more", len(applyLog)-20))
}
