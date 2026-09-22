package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func colPtr(c domain.TaskColumn) *domain.TaskColumn { return &c }

func TestValidateAgentSelfMove(t *testing.T) {
	agentID := uuid.New()
	otherID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), AssigneeAgentID: &agentID}

	cases := []struct {
		name    string
		req     domain.UpdateBoardTaskRequest
		prev    domain.TaskColumn
		wantErr bool
	}{
		{
			name:    "assignee cannot self-move to need_revision",
			req:     domain.UpdateBoardTaskRequest{Actor: domain.TaskActorAgent, ActorAgentID: &agentID, Column: colPtr(domain.TaskColumnNeedRevision)},
			prev:    domain.TaskColumnInProgress,
			wantErr: true,
		},
		{
			name:    "assignee cannot self-move back to todo",
			req:     domain.UpdateBoardTaskRequest{Actor: domain.TaskActorAgent, ActorAgentID: &agentID, Column: colPtr(domain.TaskColumnTodo)},
			prev:    domain.TaskColumnInProgress,
			wantErr: true,
		},
		{
			name: "assignee may self-move to blocked (question about own task)",
			req:  domain.UpdateBoardTaskRequest{Actor: domain.TaskActorAgent, ActorAgentID: &agentID, Column: colPtr(domain.TaskColumnBlocked)},
			prev: domain.TaskColumnInProgress,
		},
		{
			name: "assignee moves forward to code_review",
			req:  domain.UpdateBoardTaskRequest{Actor: domain.TaskActorAgent, ActorAgentID: &agentID, Column: colPtr(domain.TaskColumnCodeReview)},
			prev: domain.TaskColumnInProgress,
		},
		{
			name: "reviewer (different agent) hands back to need_revision",
			req:  domain.UpdateBoardTaskRequest{Actor: domain.TaskActorAgent, ActorAgentID: &otherID, Column: colPtr(domain.TaskColumnNeedRevision)},
			prev: domain.TaskColumnCodeReview,
		},
		{
			name: "human move is never guarded",
			req:  domain.UpdateBoardTaskRequest{Actor: domain.TaskActorHuman, Column: colPtr(domain.TaskColumnNeedRevision)},
			prev: domain.TaskColumnCodeReview,
		},
		{
			name: "agent move without actor id is not guarded (legacy path)",
			req:  domain.UpdateBoardTaskRequest{Actor: domain.TaskActorAgent, Column: colPtr(domain.TaskColumnNeedRevision)},
			prev: domain.TaskColumnCodeReview,
		},
		{
			name: "same-column update is a no-op",
			req:  domain.UpdateBoardTaskRequest{Actor: domain.TaskActorAgent, ActorAgentID: &agentID, Column: colPtr(domain.TaskColumnInProgress)},
			prev: domain.TaskColumnInProgress,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAgentSelfMove(task, tc.req, tc.prev)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
