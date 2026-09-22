package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

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

			name: "still running", action: "in_progress", status: "in_progress", headSHA: "abc",
			wantAccept: false, wantReason: "not completed",
		},
		{

			name: "completed via action only", action: "completed", status: "", headSHA: "abc",
			wantAccept: false, wantReason: "pipeline runner is not configured",
		},
		{
			name: "completed via status only", action: "", status: "completed", headSHA: "abc",
			wantAccept: false, wantReason: "pipeline runner is not configured",
		},
		{

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

func TestHandleGitHubWorkflowEventDoesNotDedupeAnEmptyDeliveryID(t *testing.T) {
	s := &Service{}
	repoID := uuid.New()
	for i := 0; i < 2; i++ {
		if _, reason := s.HandleGitHubWorkflowEvent(context.Background(), repoID, "", "completed", "completed", "abc"); strings.Contains(reason, "duplicate") {
			t.Fatalf("call %d was deduped on an empty delivery id: %q", i, reason)
		}
	}
}
