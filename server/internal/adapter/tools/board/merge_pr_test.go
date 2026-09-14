package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// mergeToolKit builds the tool over the two fakes, keeping the task manager
// reachable so the result comment can be asserted.
func mergeToolKit(prs *fakeTaskPullRequests, tasks *fakeTaskManager) *ToolKit {
	return &ToolKit{Tasks: tasks, PullRequests: prs}
}

// A clean merge is reported to the model and to NOBODY else. The merge commit
// is recorded on the task by the service, the board renders it, and a comment
// repeating it is the third copy of a fact that went fine — which is exactly
// what buries the comments that did not.
func TestMergeTaskPullRequestToolDoesNotCommentACleanMerge(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	prs := &fakeTaskPullRequests{mergeResult: domain.TaskPRMergeResult{
		Merged:         true,
		PRNumber:       42,
		MergeCommitSHA: "abc1234567890000000000000000000000000000",
		Branch:         "feature/t-7",
		BranchDeleted:  true,
		Message:        "Merged pull request #42 into main as abc123456789 (squash). Branch feature/t-7 deleted.",
	}}
	tasks := &fakeTaskManager{taskRepoID: repoID}
	tool := newMergeTaskPullRequestTool(mergeToolKit(prs, tasks))

	ctx := registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
	res := tool.Execute(ctx, `{}`)

	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if prs.mergeCalls != 1 {
		t.Errorf("merge calls = %d, want 1", prs.mergeCalls)
	}
	if prs.gotTaskID != taskID {
		t.Errorf("merged task = %s, want the task in context %s", prs.gotTaskID, taskID)
	}
	var out domain.TaskPRMergeResult
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !out.Merged || out.MergeCommitSHA == "" {
		t.Errorf("result does not carry the merge: %+v", out)
	}
	if len(tasks.comments) != 0 {
		t.Fatalf("a clean merge wrote %d comment(s) on the card; it must write none: %+v", len(tasks.comments), tasks.comments)
	}
}

// A merge that half-worked still comments: an undeleted branch or a merge
// commit the task could not record are both things a person has to finish, and
// the card is the only place they would find out.
func TestMergeTaskPullRequestToolCommentsAPartialMerge(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	prs := &fakeTaskPullRequests{mergeResult: domain.TaskPRMergeResult{
		Merged:         true,
		PRNumber:       42,
		MergeCommitSHA: "abc1234567890000000000000000000000000000",
		Branch:         "feature/t-7",
		BranchDeleted:  false,
		Message:        "Merged pull request #42 into main as abc123456789 (squash). The branch feature/t-7 could NOT be deleted (403); delete it by hand.",
	}}
	tasks := &fakeTaskManager{taskRepoID: repoID}
	tool := newMergeTaskPullRequestTool(mergeToolKit(prs, tasks))

	ctx := registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
	if res := tool.Execute(ctx, `{}`); res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if len(tasks.comments) != 1 {
		t.Fatalf("comments on the task = %d, want 1", len(tasks.comments))
	}
	if !strings.Contains(tasks.comments[0].Content, "could NOT be deleted") {
		t.Errorf("the comment does not say what was left undone: %q", tasks.comments[0].Content)
	}
}

// A refusal is an error result — and it says not to retry. The model's default
// answer to an error is another attempt, and every one of these blocks is a
// state only a board action can change; a retry loop would burn the whole run.
func TestMergeTaskPullRequestToolTellsTheModelNotToRetryARefusal(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	for _, refusal := range []error{
		domain.ErrMergeTaskNotDone,
		domain.ErrMergeChecksNotGreen,
		domain.ErrMergeAlreadyMerged,
		domain.ErrMergeClosed,
		domain.ErrMergeNoPullRequest,
		domain.ErrReleaseTargetMoved,
		domain.ErrReleaseTargetUnverified,
		domain.ErrReviewChainIncomplete,
	} {
		t.Run(refusal.Error()[:20], func(t *testing.T) {
			prs := &fakeTaskPullRequests{mergeErr: fmt.Errorf("%w: details", refusal)}
			tasks := &fakeTaskManager{taskRepoID: repoID}
			tool := newMergeTaskPullRequestTool(mergeToolKit(prs, tasks))

			ctx := registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
			res := tool.Execute(ctx, `{}`)

			if !res.IsError {
				t.Fatalf("a refusal must be an error result, got: %s", res.Content)
			}
			if !strings.Contains(res.Content, "Nothing was merged") {
				t.Errorf("refusal does not say nothing merged: %q", res.Content)
			}
			if !strings.Contains(res.Content, "Do not retry") {
				t.Errorf("refusal does not tell the model to stop: %q", res.Content)
			}
			if len(tasks.comments) != 0 {
				t.Errorf("a refusal must not write a merge comment on the card")
			}
		})
	}
}

// A transport failure is NOT a refusal: GitHub being unreachable is worth one
// more attempt, and telling the model to stop would strand a mergeable task.
func TestMergeTaskPullRequestToolLeavesTransportErrorsRetryable(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	prs := &fakeTaskPullRequests{mergeErr: fmt.Errorf("read pull request #42 before merging it: github api: 502 Bad Gateway")}
	tool := newMergeTaskPullRequestTool(mergeToolKit(prs, &fakeTaskManager{taskRepoID: repoID}))

	ctx := registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
	res := tool.Execute(ctx, `{}`)

	if !res.IsError {
		t.Fatal("a failed merge must be an error result")
	}
	if strings.Contains(res.Content, "Do not retry") {
		t.Errorf("a transport failure must stay retryable: %q", res.Content)
	}
}

// Registered with the other PR tools and only with them: a build with no GitHub
// wiring must not advertise a merge button that cannot merge.
func TestMergeToolIsRegisteredWithThePullRequestDependency(t *testing.T) {
	without := toolNames(NewExecutors(&ToolKit{Tasks: &fakeTaskManager{}}))
	with := toolNames(NewExecutors(&ToolKit{Tasks: &fakeTaskManager{}, PullRequests: &fakeTaskPullRequests{}}))

	if hasName(without, mergeTaskPullRequestToolName) {
		t.Errorf("%s must not be registered without the PR dependency", mergeTaskPullRequestToolName)
	}
	if !hasName(with, mergeTaskPullRequestToolName) {
		t.Errorf("%s is not registered", mergeTaskPullRequestToolName)
	}
}
