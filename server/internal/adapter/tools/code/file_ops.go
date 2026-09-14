package code

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	deleteFileToolName = "delete_file"
	moveFileToolName   = "move_file"
)

// protectedNames are paths no agent edit may remove or move, whatever it was
// asked to clean up. .git IS the task workspace: deleting it destroys the
// branch, the history and the run's only way to hand work back, and it is the
// kind of thing a "remove the old build artifacts" instruction can reach by
// accident.
var protectedNames = map[string]bool{
	".git": true,
}

type deleteFileArgs struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

type moveFileArgs struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Overwrite bool   `json:"overwrite"`
}

type deleteFileTool struct{}
type moveFileTool struct{}

// NewDeleteFileTool and NewMoveFileTool complete the set an agent needs to
// change a repository without shelling out: read_file, write_file, edit_file,
// edit_lines, and these two. Removing and renaming were the last operations
// that still required `rm`/`mv` through run_terminal, where they run against
// the whole filesystem rather than the workspace and leave nothing in the trace
// but a silent exit status.
func NewDeleteFileTool() port.ToolExecutor { return &deleteFileTool{} }

// NewMoveFileTool builds the move/rename tool.
func NewMoveFileTool() port.ToolExecutor { return &moveFileTool{} }

func (t *deleteFileTool) Name() string { return deleteFileToolName }

func (t *deleteFileTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: deleteFileToolName,
			Description: "Delete a file, or an empty directory, inside the workspace. Pass recursive:true to remove a directory and everything in it. " +
				"Use this instead of `rm` through the shell. " +
				"This removes the ENTIRE file. To remove a function, a block or a range of lines from a file that must survive, " +
				"use edit_lines with mode:delete — there is no delete_lines tool.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to delete, relative to the workspace root",
					},
					"recursive": map[string]interface{}{
						"type":        "boolean",
						"description": "Required to delete a directory that is not empty. Deletes everything inside it.",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (t *deleteFileTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args deleteFileArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(deleteFileToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.Path) == "" {
		return toolError(deleteFileToolName, "path is required")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(deleteFileToolName, err.Error())
	}
	abs, err := resolveEditablePath(root, args.Path, deleteFileToolName)
	if err != nil {
		return toolError(deleteFileToolName, err.Error())
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return toolError(deleteFileToolName, fmt.Sprintf(
				"no such path: %s. It is already gone, or the path is wrong — check with get_repo_tree.", args.Path))
		}
		return toolError(deleteFileToolName, fmt.Sprintf("stat %s: %v", args.Path, err))
	}

	if info.IsDir() {
		entries, readErr := os.ReadDir(abs)
		if readErr != nil {
			return toolError(deleteFileToolName, fmt.Sprintf("read %s: %v", args.Path, readErr))
		}
		if len(entries) > 0 && !args.Recursive {
			return toolError(deleteFileToolName, fmt.Sprintf(
				"%s is a directory with %d entries. Pass recursive:true to delete it and everything inside.",
				args.Path, len(entries)))
		}
		if err := os.RemoveAll(abs); err != nil {
			return toolError(deleteFileToolName, fmt.Sprintf("delete %s: %v", args.Path, err))
		}
		return domain.ToolResult{
			Name:    deleteFileToolName,
			Content: fmt.Sprintf("%s: directory deleted (%d entries). Gone from disk — do not check.", args.Path, len(entries)),
		}
	}

	if err := os.Remove(abs); err != nil {
		return toolError(deleteFileToolName, fmt.Sprintf("delete %s: %v", args.Path, err))
	}
	return domain.ToolResult{
		Name:    deleteFileToolName,
		Content: fmt.Sprintf("%s: deleted (%d bytes). Gone from disk — do not check.", args.Path, info.Size()),
	}
}

func (t *moveFileTool) Name() string { return moveFileToolName }

func (t *moveFileTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: moveFileToolName,
			Description: "Move or rename a file or directory inside the workspace. Missing parent directories of the destination are created. " +
				"Use this instead of `mv` through the shell. Renaming a symbol's file does not update the code that imports it — grep_code for the old path afterwards.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"from": map[string]interface{}{
						"type":        "string",
						"description": "Current path, relative to the workspace root",
					},
					"to": map[string]interface{}{
						"type":        "string",
						"description": "New path, relative to the workspace root",
					},
					"overwrite": map[string]interface{}{
						"type":        "boolean",
						"description": "Replace the destination if it already exists. Without it, an existing destination is an error.",
					},
				},
				"required": []string{"from", "to"},
			},
		},
	}
}

func (t *moveFileTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args moveFileArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(moveFileToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.From) == "" || strings.TrimSpace(args.To) == "" {
		return toolError(moveFileToolName, "from and to are both required")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(moveFileToolName, err.Error())
	}
	src, err := resolveEditablePath(root, args.From, moveFileToolName)
	if err != nil {
		return toolError(moveFileToolName, err.Error())
	}
	dst, err := resolveEditablePath(root, args.To, moveFileToolName)
	if err != nil {
		return toolError(moveFileToolName, err.Error())
	}
	if src == dst {
		return toolError(moveFileToolName, "from and to are the same path")
	}

	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return toolError(moveFileToolName, fmt.Sprintf(
				"no such path: %s. Check it with get_repo_tree.", args.From))
		}
		return toolError(moveFileToolName, fmt.Sprintf("stat %s: %v", args.From, err))
	}

	if _, err := os.Stat(dst); err == nil {
		if !args.Overwrite {
			return toolError(moveFileToolName, fmt.Sprintf(
				"%s already exists. Pass overwrite:true to replace it, or pick another destination.", args.To))
		}
		if err := os.RemoveAll(dst); err != nil {
			return toolError(moveFileToolName, fmt.Sprintf("replace %s: %v", args.To, err))
		}
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return toolError(moveFileToolName, fmt.Sprintf("create parent directory for %s: %v", args.To, err))
	}
	if err := os.Rename(src, dst); err != nil {
		return toolError(moveFileToolName, fmt.Sprintf("move %s to %s: %v", args.From, args.To, err))
	}

	return domain.ToolResult{
		Name: moveFileToolName,
		Content: fmt.Sprintf("moved %s to %s. Done on disk — do not check. "+
			"Imports and references to the old path are NOT updated; grep_code for it if the file was code.",
			args.From, args.To),
	}
}

// resolveEditablePath confines a path to the workspace and refuses the ones no
// edit may touch. Containment is the same rule the read tools apply — `path` is
// model output, and filepath.Join cleans rather than confines.
func resolveEditablePath(root, rel, tool string) (string, error) {
	abs, err := workspace.ResolveWithinRoot(root, rel)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	if abs == absRoot {
		return "", fmt.Errorf("%s cannot operate on the workspace root itself", tool)
	}
	relToRoot, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", rel, err)
	}
	for _, segment := range strings.Split(filepath.ToSlash(relToRoot), "/") {
		if protectedNames[segment] {
			return "", fmt.Errorf("%q is protected: %s may not touch it", segment, tool)
		}
	}
	return abs, nil
}
