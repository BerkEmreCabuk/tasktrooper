package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const attachTaskFileToolName = "attach_task_file"

type attachTaskFileArgs struct {
	TaskID       string `json:"task_id"`
	AttachmentID string `json:"attachment_id"`
}

type attachTaskFileTool struct {
	kit *ToolKit
}

func newAttachTaskFileTool(kit *ToolKit) port.ToolExecutor {
	return &attachTaskFileTool{kit: kit}
}

func (t *attachTaskFileTool) Name() string {
	return attachTaskFileToolName
}

func (t *attachTaskFileTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        attachTaskFileToolName,
			Description: "Attach an already-uploaded file (image/document) to a board task. Use when the conversation/task context references an uploaded file that belongs on the task.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Board task UUID or its board key (e.g. \"T-1\" for a task, \"B-1\" for a bug, \"A-1\" for an analysis).",
					},
					"attachment_id": map[string]interface{}{
						"type":        "string",
						"description": "UUID of the already-uploaded attachment.",
					},
				},
				"required": []string{"task_id", "attachment_id"},
			},
		},
	}
}

func (t *attachTaskFileTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args attachTaskFileArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(attachTaskFileToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(attachTaskFileToolName, err.Error())
	}
	attachmentID, err := uuid.Parse(strings.TrimSpace(args.AttachmentID))
	if err != nil {
		return toolError(attachTaskFileToolName, "invalid attachment_id: pass the attachment UUID")
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(attachTaskFileToolName, err.Error())
	}
	meta, err := t.kit.Attachments.LinkTask(ctx, repositoryID, taskID, attachmentID)
	if err != nil {
		return toolError(attachTaskFileToolName, err.Error())
	}
	return toolJSON(attachTaskFileToolName, meta)
}
