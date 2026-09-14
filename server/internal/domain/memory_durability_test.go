package domain

import (
	"testing"

	"github.com/google/uuid"
)

// The rejected set is what agents actually filed into team memory: per-card
// status notes. The kept set is the durable form of the same knowledge plus
// ordinary engineering facts that contain hyphenated names and numbers, which
// the task-key rule must not mistake for board keys.
func TestMemoryRunLogReason(t *testing.T) {
	rejected := []string{
		"T-28 PR #8 head b06f146 fixed the gap I flagged in the prior code_review round",
		"B-3 (PR #6, head 667b896) hit the same org GitHub Actions billing block",
		"Moved code_review→ready_for_qa despite gh pr checks showing red",
		"T-24 bounced in_qa -> need_revision even though all 5 AC were approved",
		"Verified this task locally: go build and go test both clean",
		"Pushed the fix to feature/t-28 and re-requested review",
		"The merge for PR #7 was refused because GitHub reported the PR state unstable",
	}
	for _, text := range rejected {
		if MemoryRunLogReason(text) == "" {
			t.Errorf("expected rejection, accepted: %q", text)
		}
	}

	kept := []string{
		"This org's GitHub Actions billing is blocked: check runs fail within seconds with a billing annotation, and only a human clearing the billing unblocks them — no code change helps.",
		"agent-server's tests need the agent-catalog submodule: run git submodule update --init before go test or the board package fails to build.",
		"gh pr create --fill takes the title from the branch name on a multi-commit branch; pass --title explicitly to keep the intended subject.",
		"The web build refuses to start without VITE_FIREBASE_* — the vite config fails the build rather than shipping an unauthenticatable bundle.",
		"Timestamps in this API are RFC-3339 with a UTF-8 body; SHA-256 signs the internal auth header.",
		"The QA agent needs a seeded database: run make seed before the first scenario.",
	}
	for _, text := range kept {
		if reason := MemoryRunLogReason(text); reason != "" {
			t.Errorf("expected keep, rejected %q: %s", text, reason)
		}
	}
}

func TestMemoryDuplicateOf(t *testing.T) {
	existing := []AgentMemory{
		{ID: uuid.New(), Content: "This org's GitHub Actions billing is blocked, so every check run fails within seconds with a billing annotation and only a human can clear it."},
		{ID: uuid.New(), Content: "The frontend build needs VITE_FIREBASE_* variables or it refuses to start."},
	}

	if _, found := MemoryDuplicateOf(existing, "GitHub Actions billing for this org is blocked, so every check run fails within seconds with a billing annotation; only a human can clear it."); !found {
		t.Error("near-identical memory was not recognised as a duplicate")
	}

	if mem, found := MemoryDuplicateOf(existing, "When Actions is unavailable the board's pipeline gate opens on a timeout and marks the pipeline skipped with gate_reason ci_unavailable."); found {
		t.Errorf("distinct memory rejected as duplicate of %q", mem.Content)
	}
}
