package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const queryRuntimeLogsToolName = "query_runtime_logs"

const (
	defaultLogSince = time.Hour
	defaultLogLimit = 100
	maxLogLimit     = 500
)

type queryRuntimeLogsTool struct {
	kit *ToolKit
}

func (t *queryRuntimeLogsTool) Name() string { return queryRuntimeLogsToolName }

func (t *queryRuntimeLogsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: queryRuntimeLogsToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"component":   componentSchema(),
					"environment": environmentSchema(),
					"since": map[string]interface{}{
						"type": "string",
					},
					"min_severity": map[string]interface{}{
						"type": "string",
						"enum": []string{"debug", "info", "warning", "error", "critical"},
					},
					"text": map[string]interface{}{
						"type": "string",
					},
					"limit": map[string]interface{}{
						"type": "integer",
					},
					repositoryIDProperty: repositoryIDSchema(),
				},
			},
		},
	}
}

type queryRuntimeLogsArgs struct {
	Component    string `json:"component"`
	Environment  string `json:"environment"`
	Since        string `json:"since"`
	MinSeverity  string `json:"min_severity"`
	Text         string `json:"text"`
	Limit        int    `json:"limit"`
	RepositoryID string `json:"repository_id"`
}

func validLogSeverity(s string) bool {
	switch domain.LogSeverity(s) {
	case domain.LogDebug, domain.LogInfo, domain.LogWarning, domain.LogError, domain.LogCritical:
		return true
	}
	return false
}

type logEntryView struct {
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Path      string `json:"path,omitempty"`
	Status    int    `json:"status_code,omitempty"`
}

func (t *queryRuntimeLogsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args queryRuntimeLogsArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}
	if args.MinSeverity != "" && !validLogSeverity(args.MinSeverity) {
		return toolError(name, fmt.Sprintf("invalid min_severity %q", args.MinSeverity))
	}

	now := time.Now().UTC()
	since := now.Add(-defaultLogSince)
	if args.Since != "" {
		s, err := resolveSince(args.Since, now)
		if err != nil {
			return toolError(name, err.Error())
		}
		since = s
	}

	limit := args.Limit
	if limit <= 0 {
		limit = defaultLogLimit
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}

	repositoryID, err := resolveRepositoryID(ctx, args.RepositoryID, name)
	if err != nil {
		return toolError(name, err.Error())
	}
	env, comp, err := resolveBoundEnvironment(ctx, t.kit, repositoryID, args.Component, args.Environment)
	if err != nil {
		return toolError(name, err.Error())
	}

	page, err := t.kit.Cloud.Logs(ctx, env.ID, domain.RuntimeLogQuery{
		Since:       since,
		Until:       now,
		MinSeverity: domain.LogSeverity(args.MinSeverity),
		Text:        args.Text,
		Limit:       limit,
	})
	if err != nil {
		return toolError(name, err.Error())
	}

	entries := make([]logEntryView, 0, len(page.Entries))
	for _, e := range page.Entries {
		entries = append(entries, logEntryView{
			Timestamp: e.Timestamp.Format(time.RFC3339),
			Severity:  string(e.Severity),
			Message:   e.Message,
			Path:      e.Path,
			Status:    e.StatusCode,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp > entries[j].Timestamp })

	out := map[string]any{
		"component":   comp.Path,
		"environment": string(env.Environment),
		"entries":     entries,
		"count":       len(entries),
	}
	if page.Truncated {
		out["note"] = prompt.RuntimeLogsTruncatedText()
	}
	return toolJSON(name, out)
}
