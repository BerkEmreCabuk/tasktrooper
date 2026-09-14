package board

// The three tools that let an agent hold a conversation about a task's pull
// request and then act on it: read the PR, push a change into it, answer a
// reviewer on it.
//
// They resolve the task from the tool argument OR the run context. Context is what
// makes a task chat work: the human says "fix the null check", and requiring the
// model to repeat the task id on every call in a conversation whose entire subject
// is one task is both noise and a way to get the wrong task.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	getTaskPullRequestToolName   = "get_task_pull_request"
	commitTaskChangesToolName    = "commit_task_changes"
	commentOnPullRequestToolName = "comment_on_pull_request"
)

// TaskPullRequests is the task↔pull-request use case as the tools need it
// (implemented by application/board.TaskPRService). Narrow interface so this
// adapter neither speaks git nor speaks to GitHub.
type TaskPullRequests interface {
	PullRequest(ctx context.Context, repositoryID, taskID uuid.UUID, includeDiff bool) (domain.TaskPullRequest, error)
	CommitTaskChanges(ctx context.Context, repositoryID, taskID uuid.UUID, message string) (domain.TaskCommitResult, error)
	CommentOnPullRequest(ctx context.Context, repositoryID, taskID uuid.UUID, body string, replyTo int64) (domain.PullRequestComment, error)
	// MergeTaskPullRequest lands the change (squash + branch delete) once every
	// gate in the service says so. See merge_pr.go.
	MergeTaskPullRequest(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPRMergeResult, error)
}

// taskRefProperty is the shared argument doc: optional everywhere, because a
// task-scoped run already knows which task it is on.
var taskRefProperty = map[string]interface{}{
	"type":        "string",
	"description": "Board task UUID or its board key (e.g. \"T-1\" for a task, \"B-1\" for a bug, \"A-1\" for an analysis). Optional in a chat that is already about one task — omit it there and the task in context is used.",
}

// resolveTaskArg turns the optional task_id argument into an id, falling back to
// the task the run is bound to.
func (kit *ToolKit) resolveTaskArg(ctx context.Context, ref string) (uuid.UUID, error) {
	if ref != "" {
		return kit.resolveTaskRef(ctx, ref)
	}
	if id := registry.TaskIDFromContext(ctx); id != uuid.Nil {
		return id, nil
	}
	return uuid.Nil, fmt.Errorf("no task_id given and this run is not bound to a task; pass the task UUID or its board key (e.g. T-1, B-1, A-1)")
}

type getTaskPullRequestTool struct {
	kit *ToolKit
}

func newGetTaskPullRequestTool(kit *ToolKit) port.ToolExecutor {
	return &getTaskPullRequestTool{kit: kit}
}

func (t *getTaskPullRequestTool) Name() string { return getTaskPullRequestToolName }

func (t *getTaskPullRequestTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: getTaskPullRequestToolName,
			Description: "Read the pull request opened for a board task: its state (open/draft/merged/mergeable), head and base branch, the changed-file list, the review comments and PR conversation, and — unless you turn it off — a size-capped diff. " +
				"Use it before answering any question about \"the PR\" and before changing code a reviewer commented on. If the task has no PR yet, the result says so instead of failing.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"include_diff": map[string]interface{}{
						"type":        "boolean",
						"description": "Include the unified diff (default true). Set false when you only need the state, files or comments — the diff is by far the largest part of the result.",
					},
				},
			},
		},
	}
}

func (t *getTaskPullRequestTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID      string `json:"task_id"`
		IncludeDiff *bool  `json:"include_diff"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(getTaskPullRequestToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.PullRequests == nil {
		return toolError(getTaskPullRequestToolName, "pull request access is not configured on this deployment")
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(getTaskPullRequestToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(getTaskPullRequestToolName, err.Error())
	}
	includeDiff := args.IncludeDiff == nil || *args.IncludeDiff
	pr, err := t.kit.PullRequests.PullRequest(ctx, repositoryID, taskID, includeDiff)
	if err != nil {
		return toolError(getTaskPullRequestToolName, err.Error())
	}
	return toolJSON(getTaskPullRequestToolName, pr)
}

type commitTaskChangesTool struct {
	kit *ToolKit
}

func newCommitTaskChangesTool(kit *ToolKit) port.ToolExecutor {
	return &commitTaskChangesTool{kit: kit}
}

func (t *commitTaskChangesTool) Name() string { return commitTaskChangesToolName }

func (t *commitTaskChangesTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: commitTaskChangesToolName,
			Description: "Commit everything you changed in the task's working copy, push it to the task branch, make sure the pull request exists, and return the branch, commit and PR. " +
				"This is the ONLY way an edit you made reaches the pull request — describing a change or writing the file is not enough. Call it once the change is complete and builds. " +
				"If the working copy has no changes, it says so instead of creating an empty commit.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"message"},
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Commit message describing what changed and why, in the imperative (\"fix the null check on the deploy target lookup\"). A reviewer reads this next to the diff. Always English, whatever language the conversation is in — the repository history is English even when the chat is not.",
					},
				},
			},
		},
	}
}

func (t *commitTaskChangesTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID  string `json:"task_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(commitTaskChangesToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.PullRequests == nil {
		return toolError(commitTaskChangesToolName, "committing the task branch is not configured on this deployment")
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(commitTaskChangesToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(commitTaskChangesToolName, err.Error())
	}
	result, err := t.kit.PullRequests.CommitTaskChanges(ctx, repositoryID, taskID, args.Message)
	if err != nil {
		return toolError(commitTaskChangesToolName, err.Error())
	}
	// Nothing to commit is reported as a normal result, not an error: an error
	// result makes a model retry, and retrying a no-op is how a run burns its
	// iteration budget on a branch that was already up to date.
	return toolJSON(commitTaskChangesToolName, result)
}

type commentOnPullRequestTool struct {
	kit *ToolKit
}

func newCommentOnPullRequestTool(kit *ToolKit) port.ToolExecutor {
	return &commentOnPullRequestTool{kit: kit}
}

func (t *commentOnPullRequestTool) Name() string { return commentOnPullRequestToolName }

func (t *commentOnPullRequestTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: commentOnPullRequestToolName,
			Description: "Post a comment on the task's pull request, or answer one review comment inside its own thread by passing that comment's id. " +
				"Reply in-thread whenever you are responding to a reviewer — a new top-level comment leaves their thread unanswered. Get the ids from get_task_pull_request.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"body"},
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Comment text (markdown). Say what you changed and where, not that you will change it.",
					},
					"reply_to_comment_id": map[string]interface{}{
						"type":        "integer",
						"description": "Id of the review comment to answer, from get_task_pull_request's review_comments. Omit to start a new PR conversation comment.",
					},
				},
			},
		},
	}
}

func (t *commentOnPullRequestTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID  string `json:"task_id"`
		Body    string `json:"body"`
		ReplyTo int64  `json:"reply_to_comment_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(commentOnPullRequestToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.PullRequests == nil {
		return toolError(commentOnPullRequestToolName, "pull request access is not configured on this deployment")
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(commentOnPullRequestToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(commentOnPullRequestToolName, err.Error())
	}
	comment, err := t.kit.PullRequests.CommentOnPullRequest(ctx, repositoryID, taskID, args.Body, args.ReplyTo)
	if err != nil {
		return toolError(commentOnPullRequestToolName, err.Error())
	}
	return toolJSON(commentOnPullRequestToolName, comment)
}
