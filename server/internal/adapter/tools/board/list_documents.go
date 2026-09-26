package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/htmldoc"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const listTaskDocumentsToolName = "list_task_documents"

// listDocumentsTool is the read half of task documents, and it exists for
// exactly the reason listCommentsTool does: the kit shipped add_task_document
// with no counterpart.
//
// That gap became load-bearing the moment an analysis stopped committing its
// spec to the repository. The architect attaches the spec and the plan to the
// analiz task; the implementation task it then opens names that analysis with a
// derived_from relation; and the developer picking the task up was told a
// document exists somewhere with no call that could return it. The run context
// puts the documents in front of that developer anyway — but a run that needs to
// re-read a long plan halfway through, or to look at an analysis it was not
// handed, needs a tool, and a model reaching for one and finding only
// add_task_document writes a document instead of reading one.
type listDocumentsTool struct {
	kit *ToolKit
}

type listDocumentsArgs struct {
	TaskID     string `json:"task_id"`
	DocumentID string `json:"document_id"`
	Raw        bool   `json:"raw"`
	Offset     *int   `json:"offset"`
	Limit      *int   `json:"limit"`
}

// rawWindowChars keeps one raw html window under the native loop's tool-output
// cap (16000 by default), for the reason read_file's does: the loop cuts the
// MIDDLE of an oversized result, and an architect revising a report from a
// copy with a hole in it writes the hole back.
const (
	rawWindowChars = 12000
	maxWindowChars = 100000
)

// DocumentLister is the read side of TaskManager's documents, kept separate for
// the same reason CommentLister is: a build whose task service predates it
// registers no tool rather than failing to compile.
type DocumentLister interface {
	ListDocuments(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskDocument, error)
}

func newListDocumentsTool(kit *ToolKit) port.ToolExecutor {
	return &listDocumentsTool{kit: kit}
}

func (t *listDocumentsTool) Name() string { return listTaskDocumentsToolName }

func (t *listDocumentsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listTaskDocumentsToolName,
			Description: "READ the documents attached to a board task, in order. " +
				"This is where an analysis lives: the analysis report an analiz task produced is a document on THAT task, never a file in the repository, so pass the analiz task's key (e.g. \"A-12\") to read it. " +
				"Your own task's `relations` name it with relation_type \"derived_from\" — and the same documents are already in your run context, so use this to re-read a long plan or to look at an analysis you were not handed. " +
				"An html document (`format: \"html\"`) comes back as readable TEXT by default; pass `raw: true` to get its HTML source — which is what you need before revising it with update_task_document. " +
				"Raw HTML is returned in windows of `limit` characters (default 12000): when `next_offset` is present, call again with that `offset` and the same `document_id` for the rest. " +
				"This tool only reads; add_task_document is what writes one.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"document_id": map[string]interface{}{
						"type":        "string",
						"description": "Only this document (UUID). Required with `offset` when a task has more than one document.",
					},
					"raw": map[string]interface{}{
						"type":        "boolean",
						"description": "Return html documents as their HTML source instead of the text rendition. Needed to revise one.",
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Character offset to start the returned content at (from a previous call's next_offset).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum characters of content per document. Defaults to 12000 for raw html, unlimited otherwise; at most 100000.",
					},
				},
			},
		},
	}
}

func (t *listDocumentsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args listDocumentsArgs
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(listTaskDocumentsToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(listTaskDocumentsToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(listTaskDocumentsToolName, err.Error())
	}
	lister, ok := t.kit.Tasks.(DocumentLister)
	if !ok {
		return toolError(listTaskDocumentsToolName, "this build cannot read task documents")
	}
	docs, err := lister.ListDocuments(ctx, repositoryID, taskID)
	if err != nil {
		return toolError(listTaskDocumentsToolName, err.Error())
	}
	if id := strings.TrimSpace(args.DocumentID); id != "" {
		parsed, perr := uuid.Parse(id)
		if perr != nil {
			return toolError(listTaskDocumentsToolName, "document_id is not a UUID: "+id)
		}
		var only []domain.TaskDocument
		for _, doc := range docs {
			if doc.ID == parsed {
				only = append(only, doc)
			}
		}
		if len(only) == 0 {
			return toolError(listTaskDocumentsToolName, fmt.Sprintf("no document %s on this task (titles: %s)", id, documentTitles(docs)))
		}
		docs = only
	}
	if args.Offset != nil && *args.Offset > 0 && len(docs) > 1 {
		return toolError(listTaskDocumentsToolName, "offset applies to one document — pass document_id as well")
	}
	out := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		out = append(out, renderDocument(doc, args))
	}
	return toolJSON(listTaskDocumentsToolName, map[string]interface{}{
		"task_id":   taskID.String(),
		"count":     len(docs),
		"documents": out,
	})
}

func renderDocument(doc domain.TaskDocument, args listDocumentsArgs) map[string]interface{} {
	out := map[string]interface{}{}
	if raw, err := json.Marshal(doc); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	if doc.Format == "" {
		out["format"] = string(domain.DocumentFormatMarkdown)
	}
	content := doc.Content
	limit := 0
	isHTML := doc.Format == domain.DocumentFormatHTML
	switch {
	case isHTML && args.Raw:
		limit = rawWindowChars
	case isHTML:
		content = htmldoc.Text(doc.Content)
		out["rendered_as"] = "text"
	}
	if args.Limit != nil && *args.Limit > 0 {
		limit = min(*args.Limit, maxWindowChars)
	}
	offset := 0
	if args.Offset != nil && *args.Offset > 0 {
		offset = *args.Offset
	}
	if limit == 0 && offset == 0 {
		out["content"] = content
		return out
	}
	runes := []rune(content)
	total := len(runes)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	out["content"] = string(runes[offset:end])
	out["content_offset"] = offset
	out["content_total_chars"] = total
	if end < total {
		out["next_offset"] = end
	}
	return out
}
