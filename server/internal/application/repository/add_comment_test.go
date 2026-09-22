package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestAddCommentStampsActorUserID(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	repos := &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}}
	tasks := &fakeReleaseTaskStore{task: domain.BoardTask{ID: taskID, RepositoryID: repoID}}

	t.Run("actor uid in context is stamped on the comment", func(t *testing.T) {
		comments := &fakeReleaseComments{}
		svc := &Service{repos: repos, tasks: tasks, comments: comments}
		ctx := registry.ContextWithActorUserID(context.Background(), "firebase-uid-123")

		created, err := svc.AddComment(ctx, repoID, taskID, domain.CreateTaskCommentRequest{Content: "looks good"})
		if err != nil {
			t.Fatalf("AddComment: %v", err)
		}
		if created.ActorUserID == nil || *created.ActorUserID != "firebase-uid-123" {
			t.Fatalf("want actor_user_id %q, got %v", "firebase-uid-123", created.ActorUserID)
		}
		if len(comments.comments) != 1 || comments.comments[0].ActorUserID == nil || *comments.comments[0].ActorUserID != "firebase-uid-123" {
			t.Fatalf("comment store did not receive actor_user_id: %+v", comments.comments)
		}
	})

	t.Run("no actor in context leaves actor_user_id nil", func(t *testing.T) {
		comments := &fakeReleaseComments{}
		svc := &Service{repos: repos, tasks: tasks, comments: comments}

		created, err := svc.AddComment(context.Background(), repoID, taskID, domain.CreateTaskCommentRequest{Content: "still works"})
		if err != nil {
			t.Fatalf("AddComment: %v", err)
		}
		if created.ActorUserID != nil {
			t.Fatalf("want nil actor_user_id, got %v", *created.ActorUserID)
		}
	})
}
