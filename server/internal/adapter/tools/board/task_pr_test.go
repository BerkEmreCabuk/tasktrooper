package board

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeTaskPullRequests records what the tools ask for and returns whatever the
// test scripted, so these tests exercise argument handling and result shaping
// without git or GitHub.
type fakeTaskPullRequests struct {
	pr          domain.TaskPullRequest
	commit      domain.TaskCommitResult
	comment     domain.PullRequestComment
	err         error
	gotRepoID   uuid.UUID
	gotTaskID   uuid.UUID
	gotDiff     bool
	gotMessage  string
	gotBody     string
	gotReplyTo  int64
	commitCalls int
	mergeCalls  int
	mergeResult domain.TaskPRMergeResult
	// mergeErr is set independently of err: a merge refusal is a different kind
	// of answer from "the PR could not be read".
	mergeErr error
}

func (f *fakeTaskPullRequests) PullRequest(_ context.Context, repositoryID, taskID uuid.UUID, includeDiff bool) (domain.TaskPullRequest, error) {
	f.gotRepoID, f.gotTaskID, f.gotDiff = repositoryID, taskID, includeDiff
	return f.pr, f.err
}

func (f *fakeTaskPullRequests) CommitTaskChanges(_ context.Context, repositoryID, taskID uuid.UUID, message string) (domain.TaskCommitResult, error) {
	f.gotRepoID, f.gotTaskID, f.gotMessage = repositoryID, taskID, message
	f.commitCalls++
	return f.commit, f.err
}

func (f *fakeTaskPullRequests) CommentOnPullRequest(_ context.Context, repositoryID, taskID uuid.UUID, body string, replyTo int64) (domain.PullRequestComment, error) {
	f.gotRepoID, f.gotTaskID, f.gotBody, f.gotReplyTo = repositoryID, taskID, body, replyTo
	return f.comment, f.err
}

func (f *fakeTaskPullRequests) MergeTaskPullRequest(_ context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPRMergeResult, error) {
	f.gotRepoID, f.gotTaskID = repositoryID, taskID
	f.mergeCalls++
	return f.mergeResult, f.mergeErr
}

func prToolKit(prs *fakeTaskPullRequests, taskRepoID uuid.UUID) *ToolKit {
	return &ToolKit{Tasks: &fakeTaskManager{taskRepoID: taskRepoID}, PullRequests: prs}
}

// "There is no PR yet" must arrive as a normal result. An error result reads to a
// model as a broken system, and it then tells the human the PR could not be
// reached instead of that none exists.
func TestGetTaskPullRequestReportsNoPRWithoutErroring(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	prs := &fakeTaskPullRequests{pr: domain.TaskPullRequest{
		Note: "No pull request has been opened for this task yet.",
	}}
	tool := newGetTaskPullRequestTool(prToolKit(prs, repoID))

	res := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`"}`)

	if res.IsError {
		t.Fatalf("expected a normal result, got tool error: %s", res.Content)
	}
	var out domain.TaskPullRequest
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out.Known {
		t.Errorf("known = true, want false")
	}
	if out.Note == "" {
		t.Errorf("a task with no PR must carry a note saying so")
	}
	if !prs.gotDiff {
		t.Errorf("include_diff defaults to true")
	}
}

// The task comes from the run context when the argument is omitted: in a chat whose
// whole subject is one task, making the model repeat the id every turn is both
// noise and a way to reach the wrong task.
func TestTaskPRToolsResolveTheTaskFromTheRunContext(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	prs := &fakeTaskPullRequests{pr: domain.TaskPullRequest{Known: true, Number: 7}}
	tool := newGetTaskPullRequestTool(prToolKit(prs, repoID))

	ctx := registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
	res := tool.Execute(ctx, `{"include_diff":false}`)

	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if prs.gotTaskID != taskID {
		t.Errorf("task id = %s, want the one in context (%s)", prs.gotTaskID, taskID)
	}
	if prs.gotRepoID != repoID {
		t.Errorf("repository id = %s, want the one in context (%s)", prs.gotRepoID, repoID)
	}
	if prs.gotDiff {
		t.Errorf("include_diff:false must be honoured")
	}
}

// Without a task in the argument or the context there is nothing to act on, and the
// error has to name both accepted spellings so the model can retry correctly.
func TestTaskPRToolsRefuseWithNoTaskAnywhere(t *testing.T) {
	tool := newGetTaskPullRequestTool(prToolKit(&fakeTaskPullRequests{}, uuid.New()))

	res := tool.Execute(context.Background(), `{}`)

	if !res.IsError {
		t.Fatalf("expected a tool error, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "board key") {
		t.Errorf("the refusal must say how to name a task, got: %s", res.Content)
	}
}

// Nothing-to-commit comes back as a result, not an error — an error makes the model
// retry a no-op until its iteration budget is gone.
func TestCommitTaskChangesToolReturnsTheNoOpAsAResult(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	prs := &fakeTaskPullRequests{commit: domain.TaskCommitResult{
		Committed: false,
		Message:   "Nothing to commit — the task workspace matches what is already on the branch.",
	}}
	tool := newCommitTaskChangesTool(prToolKit(prs, repoID))

	ctx := registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
	res := tool.Execute(ctx, `{"message":"apply the review feedback"}`)

	if res.IsError {
		t.Fatalf("expected a normal result, got tool error: %s", res.Content)
	}
	if prs.gotMessage != "apply the review feedback" {
		t.Errorf("commit message = %q", prs.gotMessage)
	}
	var out domain.TaskCommitResult
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out.Committed {
		t.Errorf("committed = true, want false")
	}
	if out.Message == "" {
		t.Errorf("a no-op must explain itself")
	}
}

// A reply must reach the reviewer's own thread; the id has to survive the argument
// decoding as a number, not be dropped by it.
func TestCommentOnPullRequestToolPassesTheReplyTarget(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	prs := &fakeTaskPullRequests{comment: domain.PullRequestComment{ID: 99, Body: "fixed"}}
	tool := newCommentOnPullRequestTool(prToolKit(prs, repoID))

	ctx := registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
	res := tool.Execute(ctx, `{"body":"fixed in bbb2222","reply_to_comment_id":12345}`)

	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if prs.gotReplyTo != 12345 {
		t.Errorf("reply_to_comment_id = %d, want 12345", prs.gotReplyTo)
	}
	if prs.gotBody != "fixed in bbb2222" {
		t.Errorf("body = %q", prs.gotBody)
	}
}

// The three tools only exist when the PR use case is wired; a build with no GitHub
// token store must not advertise tools that can only report "not connected".
func TestPRToolsAreRegisteredOnlyWithThePullRequestDependency(t *testing.T) {
	without := NewExecutors(&ToolKit{Tasks: &fakeTaskManager{}})
	with := NewExecutors(&ToolKit{Tasks: &fakeTaskManager{}, PullRequests: &fakeTaskPullRequests{}})

	if names := toolNames(without); hasName(names, getTaskPullRequestToolName) {
		t.Errorf("%s must not be registered without the PR dependency", getTaskPullRequestToolName)
	}
	names := toolNames(with)
	for _, want := range []string{getTaskPullRequestToolName, commitTaskChangesToolName, commentOnPullRequestToolName} {
		if !hasName(names, want) {
			t.Errorf("%s is not registered", want)
		}
	}
}

func toolNames(execs []port.ToolExecutor) []string {
	out := make([]string, 0, len(execs))
	for _, e := range execs {
		out = append(out, e.Name())
	}
	return out
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
