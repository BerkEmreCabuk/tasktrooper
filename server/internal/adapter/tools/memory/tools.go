package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/google/uuid"
	appmemory "github.com/makifbaysal/tasktrooper/server/internal/application/memory"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	saveMemoryToolName   = "save_memory"
	searchMemoryToolName = "search_memory"
	deleteMemoryToolName = "delete_memory"
)

// SkillPromoter decides whether a to-be-saved memory is really reusable
// know-how and, when it is, writes it into the skill catalog instead.
// agentID == uuid.Nil means the save was aimed at team memory.
type SkillPromoter interface {
	MaybePromoteMemory(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID, content, category string) (bool, string, error)
}

type ToolKit struct {
	Memories *appmemory.Service

	// promoter is set after tool registration (the evolution service that
	// implements it is wired later in startup), so access is lock-guarded.
	mu       sync.RWMutex
	promoter SkillPromoter
}

// SetPromoter installs the memory→skill classifier once it exists.
func (k *ToolKit) SetPromoter(p SkillPromoter) {
	k.mu.Lock()
	k.promoter = p
	k.mu.Unlock()
}

func (k *ToolKit) getPromoter() SkillPromoter {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.promoter
}

func NewExecutors(kit *ToolKit) []port.ToolExecutor {
	if kit == nil || kit.Memories == nil {
		return nil
	}
	return []port.ToolExecutor{
		&saveMemoryTool{kit: kit},
		&searchMemoryTool{kit: kit},
		&deleteMemoryTool{kit: kit},
	}
}

func agentFromContext(ctx context.Context) (uuid.UUID, error) {
	agentID := registry.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("memory tools require an agent context")
	}
	return agentID, nil
}

// repositoryFromContext returns the repository the run is working in, if any.
// Chat sessions without a repository and background jobs have none, and those
// runs can only touch global memory.
func repositoryFromContext(ctx context.Context) *uuid.UUID {
	if id := registry.RepositoryIDFromContext(ctx); id != uuid.Nil {
		return &id
	}
	return nil
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

// duplicate looks for a memory that already says what is about to be saved,
// inside the same bucket the save would land in. Search is semantic when an
// embedding model is configured and recency-ordered when it is not; either way
// the decision is made on the words (durability.go), so a missing embedder
// weakens the check without breaking it.
func (k *ToolKit) duplicate(ctx context.Context, agentID uuid.UUID, repoID *uuid.UUID, content string) (domain.AgentMemory, bool) {
	// A private save is compared against the team's memories too: recall serves
	// both buckets into the same run, so re-saying what the team already
	// remembers duplicates it exactly where it costs.
	owner := domain.MemoryOwnerAll
	if agentID == uuid.Nil {
		owner = domain.MemoryOwnerTeam
	}
	query := domain.MemoryQuery{
		AgentID:      agentID,
		Owner:        owner,
		RepositoryID: repoID,
		Repo:         domain.MemoryRepoScopeVisible,
	}
	existing, err := k.Memories.Search(ctx, query, content, 10)
	if err != nil {
		// Never block a save on a failed read: a lost lesson costs more than a
		// duplicated one.
		log.Warn().Err(err).Msg("duplicate-memory check skipped")
		return domain.AgentMemory{}, false
	}
	return domain.MemoryDuplicateOf(existing, content)
}

type saveMemoryTool struct {
	kit *ToolKit
}

func (t *saveMemoryTool) Name() string { return saveMemoryToolName }

func (t *saveMemoryTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: saveMemoryToolName,
			Description: "Save a fact a LATER run — on a different task, weeks from now — will need and could not work out for itself. " +
				"Before saving, apply that test: if the note stops being true once this task is finished, it is not a memory. " +
				"Progress on the card you are on, what you verified, which commit fixed what, why a check went red, what you moved where: that is the task's story and belongs in its comments (add_task_comment), which is where people and later runs look for it. " +
				"A memory never names a task key, a PR number, a commit SHA or a column move — the durable version of the same lesson is that sentence with the card taken out of it. " +
				"Reusable know-how (a procedure you would follow again) is a skill, not a memory; this tool routes those to the skill catalog for you. " +
				"Two independent choices decide where it lands: scope (project = only valid inside the repository you are working in, e.g. its build command, its architecture quirks; global = valid everywhere, e.g. a user preference or a habit you want to keep) and shared (false = your own memory, true = team memory every agent reads). " +
				"Default to scope=project while working on a repository — a lesson learned in one codebase is usually wrong in another. " +
				"Team memory (shared=true) is read by every agent on every run, so it holds only what the whole team would act on: raise the bar there, do not narrate.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"content"},
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "One concise, self-contained fact or lesson, stated so it still reads true a month from now: no task key, no PR number, no commit SHA, no \"this task\", no column move. A note that fails that is rejected with the reason.",
					},
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Optional category (e.g. preference, lesson, convention, feedback)",
					},
					"scope": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"project", "global"},
						"description": "project = bound to the current repository (default when one is in play); global = valid across every repository",
					},
					"shared": map[string]interface{}{
						"type":        "boolean",
						"description": "true = team memory visible to all agents; false (default) = your own memory",
					},
				},
			},
		},
	}
}

func (t *saveMemoryTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Content  string `json:"content"`
		Category string `json:"category"`
		Scope    string `json:"scope"`
		Shared   bool   `json:"shared"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(saveMemoryToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	agentID, err := agentFromContext(ctx)
	if err != nil {
		return toolError(saveMemoryToolName, err.Error())
	}
	if args.Shared {
		agentID = uuid.Nil
	}

	// A run log is not a memory. Refused rather than filed: the note is not
	// lost (its home is the task's comments) and a rejection with the reason is
	// what teaches the difference — a silent drop would not.
	if reason := domain.MemoryRunLogReason(args.Content); reason != "" {
		return toolError(saveMemoryToolName, fmt.Sprintf("not saved: %s. %s", reason, domain.MemoryRunLogHint))
	}

	repoID := repositoryFromContext(ctx)
	scope := strings.ToLower(strings.TrimSpace(args.Scope))
	switch scope {
	case "global":
		repoID = nil
	case "project":
		if repoID == nil {
			return toolError(saveMemoryToolName, "scope=project needs a repository in context; this run has none — use scope=global")
		}
	case "":
		// Unset means "wherever I am": project-scoped inside a repository,
		// global otherwise. Keeps the common case correct without the model
		// having to reason about it.
	default:
		return toolError(saveMemoryToolName, fmt.Sprintf("unknown scope %q (want project or global)", args.Scope))
	}

	// Reusable know-how belongs in the skill catalog, not in memory — a
	// classifier gets first refusal. Any error falls through to a normal
	// memory save; losing the note entirely would be worse than misfiling it.
	//
	// The fall-through is right and it stays. What was wrong is that it was
	// SILENT: `err == nil && promoted` discarded the error unread, so a
	// classifier that could not run at all — an agent on the Claude Code CLI,
	// which cannot serve a JSON-schema call — filed every note as a plain
	// memory and left no trace of the step it skipped. The classification is
	// optional; knowing it never happens is not.
	if p := t.kit.getPromoter(); p != nil {
		promoted, skillName, err := p.MaybePromoteMemory(ctx, agentID, repoID, args.Content, args.Category)
		switch {
		case err != nil:
			log.Warn().Err(err).Str("agent_id", agentID.String()).
				Bool("permanent", errors.Is(err, domain.ErrHostExecutedUnservable)).
				Msg("skill-vs-memory classification skipped; saving as a plain memory")
		case promoted:
			return toolJSON(saveMemoryToolName, map[string]any{
				"saved": false, "promoted_to_skill": skillName,
				"note": "this was reusable know-how, so it was stored in the skill catalog instead of memory",
			})
		}
	}

	// The same lesson filed once per task is how team memory filled up with five
	// copies of one billing outage. Recall is what this competes with: a
	// duplicate does not add knowledge, it evicts other knowledge from the
	// handful of memories a future run is shown.
	if existing, found := t.kit.duplicate(ctx, agentID, repoID, args.Content); found {
		return toolJSON(saveMemoryToolName, map[string]any{
			"saved":        false,
			"duplicate_of": existing.ID,
			"existing":     existing.Content,
			"note": "this is already remembered. If your version adds something the stored one lacks, " +
				"delete that memory and save the fuller sentence; otherwise nothing needs saving.",
		})
	}

	mem, err := t.kit.Memories.Save(ctx, agentID, repoID, args.Content, args.Category, domain.MemorySourceAgent)
	if err != nil {
		return toolError(saveMemoryToolName, err.Error())
	}
	return toolJSON(saveMemoryToolName, map[string]any{
		"saved": true, "memory_id": mem.ID, "scope": mem.Scope, "shared": args.Shared,
	})
}

type searchMemoryTool struct {
	kit *ToolKit
}

func (t *searchMemoryTool) Name() string { return searchMemoryToolName }

func (t *searchMemoryTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: searchMemoryToolName,
			Description: "Search the memories you can read: your own plus the team's, from this repository plus the global ones. " +
				"Empty query returns the most recent. Use scope to narrow to project or global memories, and owner to look only at your own or only at the team's.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Optional semantic search query",
					},
					"scope": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"all", "project", "global"},
						"description": "all (default) = this repository's memories plus the global ones; project = only this repository; global = only repository-independent",
					},
					"owner": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"all", "self", "team"},
						"description": "all (default) = your memories plus the team's; self = only yours; team = only the team's",
					},
					"top_k": map[string]interface{}{
						"type":        "integer",
						"description": "Max results (default 5)",
					},
				},
			},
		},
	}
}

func (t *searchMemoryTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Query string `json:"query"`
		Scope string `json:"scope"`
		Owner string `json:"owner"`
		TopK  int    `json:"top_k"`
	}
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(searchMemoryToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	agentID, err := agentFromContext(ctx)
	if err != nil {
		return toolError(searchMemoryToolName, err.Error())
	}

	query := domain.MemoryQuery{AgentID: agentID, RepositoryID: repositoryFromContext(ctx)}
	switch strings.ToLower(strings.TrimSpace(args.Scope)) {
	case "", "all":
		query.Repo = domain.MemoryRepoScopeVisible
	case "project":
		if query.RepositoryID == nil {
			return toolError(searchMemoryToolName, "scope=project needs a repository in context; this run has none")
		}
		query.Repo = domain.MemoryRepoScopeProject
	case "global":
		query.Repo = domain.MemoryRepoScopeGlobal
	default:
		return toolError(searchMemoryToolName, fmt.Sprintf("unknown scope %q (want all, project or global)", args.Scope))
	}
	switch strings.ToLower(strings.TrimSpace(args.Owner)) {
	case "", "all":
		query.Owner = domain.MemoryOwnerAll
	case "self", "agent":
		query.Owner = domain.MemoryOwnerAgent
	case "team", "shared":
		query.Owner = domain.MemoryOwnerTeam
	default:
		return toolError(searchMemoryToolName, fmt.Sprintf("unknown owner %q (want all, self or team)", args.Owner))
	}

	mems, err := t.kit.Memories.Search(ctx, query, args.Query, args.TopK)
	if err != nil {
		return toolError(searchMemoryToolName, err.Error())
	}
	type memOut struct {
		ID       uuid.UUID `json:"id"`
		Content  string    `json:"content"`
		Category string    `json:"category,omitempty"`
		Scope    string    `json:"scope"`
		Source   string    `json:"source"`
	}
	out := make([]memOut, len(mems))
	for i, m := range mems {
		out[i] = memOut{ID: m.ID, Content: m.Content, Category: m.Category, Scope: m.Scope, Source: m.Source}
	}
	return toolJSON(searchMemoryToolName, map[string]any{"memories": out})
}

type deleteMemoryTool struct {
	kit *ToolKit
}

func (t *deleteMemoryTool) Name() string { return deleteMemoryToolName }

func (t *deleteMemoryTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        deleteMemoryToolName,
			Description: "Delete one of your own memories by id (when it is outdated or wrong).",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"memory_id"},
				"properties": map[string]interface{}{
					"memory_id": map[string]interface{}{
						"type":        "string",
						"description": "UUID of the memory to delete",
					},
				},
			},
		},
	}
}

func (t *deleteMemoryTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		MemoryID string `json:"memory_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(deleteMemoryToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	memID, err := uuid.Parse(args.MemoryID)
	if err != nil {
		return toolError(deleteMemoryToolName, "invalid memory_id")
	}
	agentID, err := agentFromContext(ctx)
	if err != nil {
		return toolError(deleteMemoryToolName, err.Error())
	}
	if err := t.kit.Memories.Delete(ctx, agentID, memID); err != nil {
		return toolError(deleteMemoryToolName, err.Error())
	}
	return toolJSON(deleteMemoryToolName, map[string]any{"deleted": true})
}
