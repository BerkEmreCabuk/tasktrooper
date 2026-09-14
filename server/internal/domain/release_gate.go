package domain

import (
	"errors"
	"strings"
)

// Release-target identity errors.
//
// TriggerRelease dispatches the production deploy for a task the board says is
// done — but "done" is a state, not a commit. Between the sign-off and the
// dispatch the task's branch can move (a follow-up agent run commits more work,
// someone force-pushes), and the deploy would then ship code that no reviewer,
// no QA round and no PM ever saw. BoardTask.VerifiedSHA records the commit the
// task carried when it reached done; the release gate re-resolves the branch at
// dispatch time so the two can actually be compared instead of trusted.
var (
	// ErrReleaseTargetMoved is the hard block: the branch is no longer at the
	// commit that was signed off. Shipping unreviewed code to production is
	// worse than a stalled release, so this never degrades to a warning. The
	// way out is the same one the migration gate uses — send the task back
	// through review; returning it to done re-stamps the verified commit.
	ErrReleaseTargetMoved = errors.New("release blocked: this task's branch has moved since it was verified, so a production deploy would ship commits nobody reviewed")
	// ErrReleaseTargetUnverified covers "there is nothing to compare against":
	// no commit was ever stamped for this task, or the branch cannot be
	// resolved right now. It fails closed on purpose — a release target that
	// cannot be proven is exactly the case this gate exists for, and a check
	// that passes when its input is missing is not a check.
	ErrReleaseTargetUnverified = errors.New("release blocked: the commit this task was verified at is unknown, so the code a production deploy would ship cannot be proven to be the code that was reviewed")
)

// VerifiedCommitMatches decides the release-identity question — "is the commit
// about to be acted on the commit that was signed off?" — and nothing else.
//
// It returns a BARE sentinel so each caller can wrap it in wording that fits
// what it is about to do: the release gate says "a production deploy would
// ship…", the merge gate says "merging would land…". The DECISION is here so
// the two can never drift apart, which is the whole risk with a check that is
// written twice: an irreversible action guarded by a second, subtly different
// copy of the rule is guarded by nothing.
//
// Case-insensitive on purpose (see releaseTargetGate): a SHA can arrive from
// git, from GitHub's API or from a stamp written by an older path, and hex
// casing must never be what allows or blocks an irreversible action.
//
// Both empty inputs fail closed. "There is nothing to compare" is exactly the
// case these gates exist for — a check that passes when its input is missing is
// not a check.
func VerifiedCommitMatches(verified, current string) error {
	verified = strings.TrimSpace(verified)
	current = strings.TrimSpace(current)
	if current == "" {
		return ErrReleaseTargetUnverified
	}
	if verified == "" {
		return ErrReleaseTargetUnverified
	}
	if !strings.EqualFold(verified, current) {
		return ErrReleaseTargetMoved
	}
	return nil
}

// ShortSHA renders a commit the way a human reads one in a git log, while
// leaving anything that is not a full hex SHA untouched so a malformed stamp is
// visible in the error rather than silently trimmed into something plausible.
func ShortSHA(sha string) string {
	if len(sha) < 12 {
		return sha
	}
	return sha[:12]
}
