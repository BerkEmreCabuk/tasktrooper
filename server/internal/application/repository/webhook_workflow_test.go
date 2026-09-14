package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// HandleGitHubWorkflowEvent is the entry point that turns "GitHub finished a run
// on commit X" into the task_pipelines write and the board move it implies. The
// task_pipelines write itself is asserted in board.TestResolveByHeadSHA…; what
// is asserted here is the gate in front of it, because every early return is a
// delivery the board DOES NOT act on, and getting one wrong is silent — either a
// wasted GitHub round-trip per delivery, or a card that stays wedged.
func TestHandleGitHubWorkflowEventGate(t *testing.T) {
	for _, tc := range []struct {
		name       string
		action     string
		status     string
		headSHA    string
		wantAccept bool
		wantReason string
	}{
		{
			// A hook fires on requested/in_progress too. Neither carries a
			// conclusion, so resolving on them would spend a round-trip to learn
			// nothing the completion is about to say.
			name: "still running", action: "in_progress", status: "in_progress", headSHA: "abc",
			wantAccept: false, wantReason: "not completed",
		},
		{
			// check_suite reports only `action`; workflow_run reports both. Either
			// saying completed has to be enough, or half the deliveries are dropped.
			name: "completed via action only", action: "completed", status: "", headSHA: "abc",
			wantAccept: false, wantReason: "pipeline runner is not configured",
		},
		{
			name: "completed via status only", action: "", status: "completed", headSHA: "abc",
			wantAccept: false, wantReason: "pipeline runner is not configured",
		},
		{
			// Nothing to join on. Reached when a payload shape changes under us,
			// which must fail loudly in the reason rather than look like success.
			name: "no head sha", action: "completed", status: "completed", headSHA: "  ",
			wantAccept: false, wantReason: "no head sha",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{}
			accepted, reason := s.HandleGitHubWorkflowEvent(
				context.Background(), uuid.New(), "delivery-"+tc.name, tc.action, tc.status, tc.headSHA)
			if accepted != tc.wantAccept {
				t.Fatalf("accepted = %v, want %v (reason %q)", accepted, tc.wantAccept, reason)
			}
			if !strings.Contains(reason, tc.wantReason) {
				t.Fatalf("reason = %q, want it to mention %q", reason, tc.wantReason)
			}
		})
	}
}

// GitHub retries a delivery it thinks failed, within minutes. A retry must not
// re-resolve: the same dedupe ledger the push path uses covers this one, which is
// the whole reason it is shared rather than reimplemented.
func TestHandleGitHubWorkflowEventIgnoresARetriedDelivery(t *testing.T) {
	s := &Service{}
	repoID := uuid.New()

	if _, reason := s.HandleGitHubWorkflowEvent(context.Background(), repoID, "d-1", "completed", "completed", "abc"); strings.Contains(reason, "duplicate") {
		t.Fatalf("the first delivery was treated as a duplicate: %q", reason)
	}
	_, reason := s.HandleGitHubWorkflowEvent(context.Background(), repoID, "d-1", "completed", "completed", "abc")
	if !strings.Contains(reason, "duplicate delivery") {
		t.Fatalf("reason = %q, want the retry to be recognised as a duplicate", reason)
	}
}

// An empty delivery id (a hand-made request, a proxy that strips headers) is not
// a duplicate of the previous empty one — deduping on "" would drop every
// delivery after the first.
func TestHandleGitHubWorkflowEventDoesNotDedupeAnEmptyDeliveryID(t *testing.T) {
	s := &Service{}
	repoID := uuid.New()
	for i := 0; i < 2; i++ {
		if _, reason := s.HandleGitHubWorkflowEvent(context.Background(), repoID, "", "completed", "completed", "abc"); strings.Contains(reason, "duplicate") {
			t.Fatalf("call %d was deduped on an empty delivery id: %q", i, reason)
		}
	}
}
