package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const addTaskDocumentToolName = "add_task_document"

type addDocumentArgs struct {
	TaskID  string `json:"task_id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type addDocumentTool struct {
	kit *ToolKit
}

func newAddDocumentTool(kit *ToolKit) port.ToolExecutor {
	return &addDocumentTool{kit: kit}
}

func (t *addDocumentTool) Name() string {
	return addTaskDocumentToolName
}

func (t *addDocumentTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: addTaskDocumentToolName,
			Description: "Add a NEW markdown document to a board task as the current agent. " +
				"Revising something already attached to the task is update_task_document's job, not this one — never write \"Spec v2\" next to \"Spec\". " +
				"Writing a title that already exists on the task rewrites that document in place rather than duplicating it.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Board task UUID or its board key (e.g. \"T-1\" for a task, \"B-1\" for a bug, \"A-1\" for an analysis).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Document title",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Document content (markdown)",
					},
				},
				"required": []string{"task_id", "title"},
			},
		},
	}
}

// requiresRepoGrounding reports whether taskType carries the
// require_repo_grounding type behaviour — what used to be a literal
// TaskTypeAnaliz check here and in update_task_document. Nil-safe and
// permissive on an unreadable snapshot: kit.Workflows is only unset in tests
// that predate B1's wiring, and this gate exists to catch a fabricated
// analysis, not to block every document write over a transient read error the
// board's own dispatch would already have refused to run on.
func (kit *ToolKit) requiresRepoGrounding(ctx context.Context, taskType domain.TaskType) bool {
	if kit.Workflows == nil {
		return false
	}
	wf, err := kit.Workflows.Workflow(ctx, taskType)
	if err != nil {
		return false
	}
	return wf.TypeHas(domain.BehaviourRequireRepoGrounding)
}

func (t *addDocumentTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args addDocumentArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(addTaskDocumentToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(addTaskDocumentToolName, err.Error())
	}
	if args.Title == "" {
		return toolError(addTaskDocumentToolName, "title is required")
	}
	agentID, err := resolveAgentID(ctx)
	if err != nil {
		return toolError(addTaskDocumentToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(addTaskDocumentToolName, err.Error())
	}
	if task, ok := t.kit.findTask(ctx, taskID); ok && t.kit.requiresRepoGrounding(ctx, task.TaskType) {
		if msg := ungroundedAnalysisReason(ctx); msg != "" {
			return toolError(addTaskDocumentToolName, msg)
		}
	}
	// Same title, same task: a revision, not a second document. A model told to
	// change a spec it already wrote reaches for the tool it knows, and without
	// this the card grows a fresh copy on every pass — so the write lands on the
	// existing document and the model is told, in the result, which tool to use
	// next time.
	if updater, ok := t.kit.Tasks.(DocumentUpdater); ok {
		if existing, ferr := t.kit.findDocument(ctx, repositoryID, taskID, "", args.Title); ferr == nil {
			content := args.Content
			updated, uerr := updater.UpdateDocument(ctx, repositoryID, taskID, existing.ID, domain.UpdateTaskDocumentRequest{
				Content: &content,
			})
			if uerr != nil {
				return toolError(addTaskDocumentToolName, uerr.Error())
			}
			return toolJSON(addTaskDocumentToolName, documentResult(updated,
				"A document with this title was already on the task, so it was rewritten in place instead of duplicated. Use update_task_document for revisions."))
		}
	}
	doc, err := t.kit.Tasks.AddDocument(ctx, repositoryID, taskID, domain.CreateTaskDocumentRequest{
		Title:         args.Title,
		Content:       args.Content,
		CreatedByType: "agent",
		CreatedByID:   agentID.String(),
	})
	if err != nil {
		return toolError(addTaskDocumentToolName, err.Error())
	}
	return toolJSON(addTaskDocumentToolName, doc)
}
