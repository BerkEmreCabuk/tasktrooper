// Package repoprofile exposes the update_project_profile tool: the write path
// for the judgment half of a repository's project profile.
//
// The tool takes sections with evidence rather than one markdown blob. The
// blob shape is still accepted (an older agent, or a run that ignores the
// schema, sends `content`) but it lands in the notes section instead of
// replacing the profile: without evidence there is nothing to verify, and a
// profile every future run inherits is the wrong place to accept unverifiable
// claims.
package repoprofile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	apprepoprofile "github.com/makifbaysal/tasktrooper/server/internal/application/repoprofile"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// maxSectionChars caps one section. Sections are briefs; past this length the
// model is pasting code, which the reader can already read.
const maxSectionChars = 4000

// maxSections caps one call. There are fewer writable sections than this, so
// hitting it means the same section is being sent repeatedly.
const maxSections = 12

// ProfileWriter is the write-through into the repoprofile service.
type ProfileWriter interface {
	ApplySections(ctx context.Context, repositoryID uuid.UUID, writes []apprepoprofile.SectionWrite) ([]apprepoprofile.SectionResult, error)
	UpdateProfile(ctx context.Context, repositoryID uuid.UUID, content string) (domain.Repository, error)
}

type ToolKit struct {
	Profiles ProfileWriter
}

func NewExecutors(kit *ToolKit) []port.ToolExecutor {
	if kit == nil || kit.Profiles == nil {
		return nil
	}
	return []port.ToolExecutor{&updateProfileTool{kit: kit}}
}

func toolError(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message, IsError: true}
}

func toolJSON(name string, payload any) domain.ToolResult {
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolError(name, fmt.Sprintf("marshal response: %v", err))
	}
	return domain.ToolResult{Name: name, Content: string(raw), IsError: false}
}

type updateProfileTool struct {
	kit *ToolKit
}

func (t *updateProfileTool) Name() string { return apprepoprofile.UpdateProfileToolName }

func (t *updateProfileTool) Definition() domain.ToolDefinition {
	writable := domain.AgentWritableProfileSections()
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: apprepoprofile.UpdateProfileToolName,
			Description: "Store judgment sections of the repository's project profile — the durable brief every future agent run inherits. " +
				"Each section you send REPLACES that section and leaves the others alone. " +
				"Every section must carry evidence: repo-relative paths that exist in the working copy; a section whose evidence does not resolve is REJECTED. " +
				"Stack, layout, build/test/run commands, CI, deploy path, integrations, git workflow and test map are derived from the tree by the platform — they cannot be written here. " +
				"Update the profile whenever you learn something durable that the code alone does not make obvious.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"sections"},
				"properties": map[string]interface{}{
					"sections": map[string]interface{}{
						"type":        "array",
						"description": "The sections to store. Send only the ones you are changing.",
						"items": map[string]interface{}{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"section", "body_md", "evidence"},
							"properties": map[string]interface{}{
								"section": map[string]interface{}{
									"type":        "string",
									"enum":        writable,
									"description": "Which section this replaces",
								},
								"body_md": map[string]interface{}{
									"type":        "string",
									"description": fmt.Sprintf("The section body in markdown (max %d chars). Specific claims only — anything equally true of another repo with the same stack does not belong here.", maxSectionChars),
								},
								"evidence": map[string]interface{}{
									"type":        "array",
									"description": "Files backing this section. At least one must exist in the repository or the section is rejected.",
									"items": map[string]interface{}{
										"type":                 "object",
										"additionalProperties": false,
										"required":             []string{"path"},
										"properties": map[string]interface{}{
											"path": map[string]interface{}{"type": "string", "description": "Repo-relative file path"},
											"line": map[string]interface{}{"type": "integer", "description": "Line number, when it sharpens the claim"},
											"note": map[string]interface{}{"type": "string", "description": "What this file shows"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

type sectionArg struct {
	Section  string                   `json:"section"`
	BodyMD   string                   `json:"body_md"`
	Evidence []domain.ProfileEvidence `json:"evidence"`
}

func (t *updateProfileTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := apprepoprofile.UpdateProfileToolName
	var args struct {
		Sections []sectionArg `json:"sections"`
		// Content is the legacy single-blob shape.
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}

	// Repository resolved from the run context, same as the board tools'
	// context fallback: board runs, chats and the refresh run all carry it.
	repositoryID := registry.RepositoryIDFromContext(ctx)
	if repositoryID == uuid.Nil {
		return toolError(name, "update_project_profile needs a repository in context; this run has none")
	}

	if len(args.Sections) == 0 {
		if strings.TrimSpace(args.Content) == "" {
			return toolError(name, "sections is required: send at least one {section, body_md, evidence} entry")
		}
		if _, err := t.kit.Profiles.UpdateProfile(ctx, repositoryID, args.Content); err != nil {
			return toolError(name, err.Error())
		}
		return toolJSON(name, map[string]any{
			"stored":  []string{domain.ProfileSectionNotes},
			"warning": "free-form content was stored as the notes section. Use the sections argument with evidence paths so your claims are verifiable and can be kept fresh.",
		})
	}

	if len(args.Sections) > maxSections {
		args.Sections = args.Sections[:maxSections]
	}
	writes := make([]apprepoprofile.SectionWrite, 0, len(args.Sections))
	for _, s := range args.Sections {
		body := strings.TrimSpace(s.BodyMD)
		if len(body) > maxSectionChars {
			body = domain.TruncateHead(body, maxSectionChars)
		}
		writes = append(writes, apprepoprofile.SectionWrite{
			Section:  strings.TrimSpace(s.Section),
			BodyMD:   body,
			Evidence: s.Evidence,
		})
	}

	results, err := t.kit.Profiles.ApplySections(ctx, repositoryID, writes)
	if err != nil {
		return toolError(name, err.Error())
	}

	var stored, rejected []string
	for _, r := range results {
		if r.Accepted {
			stored = append(stored, r.Section)
		} else {
			rejected = append(rejected, r.Section+": "+r.Reason)
		}
	}
	payload := map[string]any{"stored": stored, "results": results}
	if len(rejected) > 0 {
		payload["rejected"] = rejected
		payload["next_step"] = "Fix the evidence paths for the rejected sections and call the tool again with just those."
	}
	// A call where nothing landed is an error result, not a quiet success:
	// the run has to see the failure to retry it.
	if len(stored) == 0 {
		return toolError(name, "no section was stored — "+strings.Join(rejected, "; "))
	}
	return toolJSON(name, payload)
}
