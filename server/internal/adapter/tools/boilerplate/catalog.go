// Package boilerplate implements search_boilerplate_catalog: a tool that
// lets an agent check a shared boilerplate repository (see the
// boilerplate_catalog_repo setting) for an existing starter stack before
// generating a new project from scratch. Named boilerplate, not catalog,
// because internal/application/catalog already means something unrelated
// (the agent skill/rule seed catalog).
package boilerplate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

const ToolName = "search_boilerplate_catalog"

const catalogPath = ".ai/catalog.yaml"

// candidateBranches is tried in order; most repos default to main, some
// older ones still default to master.
var candidateBranches = []string{"main", "master"}

// SettingsReader is the narrow slice of port.SettingsStore this tool needs —
// read fresh on every call, since the catalog repo is a UI-editable setting
// that can change without a restart.
type SettingsReader interface {
	Get(ctx context.Context) (domain.AppSettings, error)
}

const defaultRawBaseURL = "https://raw.githubusercontent.com"
const defaultAPIBaseURL = "https://api.github.com"

type catalogTool struct {
	settings   SettingsReader
	httpClient *http.Client
	maxBytes   int64
	rawBaseURL string
	apiBaseURL string
}

func New(settings SettingsReader) port.ToolExecutor {
	return &catalogTool{
		settings:   settings,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		maxBytes:   1048576,
		rawBaseURL: defaultRawBaseURL,
		apiBaseURL: defaultAPIBaseURL,
	}
}

type args struct {
	Query     string `json:"query"`
	PathQuery string `json:"path_query"`
}

type catalogFile struct {
	Version      int            `yaml:"version"`
	Boilerplates []catalogEntry `yaml:"boilerplates"`
}

type catalogEntry struct {
	ID          string   `yaml:"id" json:"id"`
	Path        string   `yaml:"path" json:"path"`
	Type        string   `yaml:"type" json:"type"`
	Language    string   `yaml:"language" json:"language"`
	Framework   string   `yaml:"framework" json:"framework"`
	Tags        []string `yaml:"tags" json:"tags"`
	Description string   `yaml:"description" json:"description"`
	Status      string   `yaml:"status" json:"status"`
}

func (t *catalogTool) Name() string {
	return ToolName
}

func (t *catalogTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: ToolName,
			Description: "Search the shared boilerplate catalog for an existing starter stack " +
				"(backend/frontend/mobile/worker) BEFORE writing new project code from scratch. " +
				"If a matching entry is returned, copy its 'path' directory as the starting point " +
				"instead of generating files by hand — this saves time and tokens. Call with no " +
				"query to list every available boilerplate.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Optional free-text filter: language, framework, or keyword (e.g. \"go rest api\", \"flutter\", \"kafka worker\"). Leave empty to list the full catalog.",
					},
					"path_query": map[string]interface{}{
						"type":        "string",
						"description": "Optional file-path filter searched ACROSS all boilerplates (e.g. \"sonar-project\", \"ci.yml\", \"Dockerfile\", \"middleware\"). Returns which boilerplate contains which matching file — use it to find where a config or pattern already exists before writing one.",
					},
				},
			},
		},
	}
}

func (t *catalogTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a args
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &a); err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err))
		}
	}

	settings, err := t.settings.Get(ctx)
	if err != nil {
		return toolError(fmt.Sprintf("read boilerplate_catalog_repo setting: %v", err))
	}

	if strings.TrimSpace(settings.BoilerplateCatalogRepo) == "" {
		return toolError("no boilerplate catalog is configured: set boilerplate_catalog_repo (Settings) to a GitHub repository whose .ai/catalog.yaml lists your starter stacks")
	}
	owner, repo, err := parseGitHubRepo(settings.BoilerplateCatalogRepo)
	if err != nil {
		return toolError(err.Error())
	}

	raw, branch, err := t.fetchCatalog(ctx, owner, repo)
	if err != nil {
		return toolError(err.Error())
	}

	var doc catalogFile
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return toolError(fmt.Sprintf("parse catalog.yaml: %v", err))
	}

	matches := filterEntries(doc.Boilerplates, a.Query)

	var files map[string][]string
	if strings.TrimSpace(a.PathQuery) != "" {
		paths, treeErr := t.fetchTree(ctx, owner, repo, branch)
		if treeErr != nil {
			return toolError(fmt.Sprintf("list repository files for path_query: %v", treeErr))
		}
		files = groupTreeMatches(paths, a.PathQuery)
	}

	payload := struct {
		Repo      string              `json:"repo"`
		Branch    string              `json:"branch"`
		Query     string              `json:"query,omitempty"`
		PathQuery string              `json:"path_query,omitempty"`
		Count     int                 `json:"count"`
		Entries   []catalogEntry      `json:"entries"`
		Files     map[string][]string `json:"files,omitempty"`
		Hint      string              `json:"hint"`
	}{
		Repo:      fmt.Sprintf("%s/%s", owner, repo),
		Branch:    branch,
		Query:     a.Query,
		PathQuery: a.PathQuery,
		Count:     len(matches),
		Entries:   matches,
		Files:     files,
		Hint:      "To use an entry, copy its 'path' directory from the repo above as your project's starting point, then adapt names/config — don't regenerate its files from scratch.",
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return toolError(fmt.Sprintf("marshal response: %v", err))
	}

	return domain.ToolResult{
		Name:    ToolName,
		Content: string(out),
		IsError: false,
	}
}

func (t *catalogTool) fetchCatalog(ctx context.Context, owner, repo string) ([]byte, string, error) {
	var lastErr error
	for _, branch := range candidateBranches {
		url := fmt.Sprintf("%s/%s/%s/%s/%s", t.rawBaseURL, owner, repo, branch, catalogPath)
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if reqErr != nil {
			return nil, "", fmt.Errorf("build request: %w", reqErr)
		}
		req.Header.Set("User-Agent", "local-llm-bridge/1.0")

		resp, doErr := t.httpClient.Do(req)
		if doErr != nil {
			lastErr = doErr
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, t.maxBytes))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return body, branch, nil
		}
		lastErr = fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}
	log.Debug().Str("owner", owner).Str("repo", repo).Err(lastErr).Msg("boilerplate catalog fetch failed")
	return nil, "", fmt.Errorf("fetch %s from %s/%s (tried branches %v): %w", catalogPath, owner, repo, candidateBranches, lastErr)
}

// parseGitHubRepo accepts "owner/repo", "github.com/owner/repo", or a full
// "https://github.com/owner/repo(.git)" URL — whatever shape a user pastes
// into the boilerplate_catalog_repo setting.
func parseGitHubRepo(value string) (owner, repo string, err error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", "", fmt.Errorf("boilerplate_catalog_repo setting is empty")
	}
	v = strings.TrimPrefix(v, "https://")
	v = strings.TrimPrefix(v, "http://")
	v = strings.TrimPrefix(v, "github.com/")
	v = strings.TrimSuffix(v, ".git")
	v = strings.Trim(v, "/")

	parts := strings.Split(v, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("boilerplate_catalog_repo %q must look like \"owner/repo\" or a github.com URL", value)
	}
	return parts[0], parts[1], nil
}

// fetchTree lists every file path in the catalog repo via the Git Trees API
// (unauthenticated; the catalog repo is public by construction, since the
// catalog itself is fetched from raw.githubusercontent.com).
func (t *catalogTool) fetchTree(ctx context.Context, owner, repo, branch string) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", t.apiBaseURL, owner, repo, branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "local-llm-bridge/1.0")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, t.maxBytes*8))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}
	var out struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse tree response: %w", err)
	}
	paths := make([]string, 0, len(out.Tree))
	for _, e := range out.Tree {
		if e.Type == "blob" {
			paths = append(paths, e.Path)
		}
	}
	return paths, nil
}

const maxPathMatches = 200

// groupTreeMatches filters paths by a case-insensitive substring and groups
// the hits by their top-level directory — i.e. by boilerplate.
func groupTreeMatches(paths []string, query string) map[string][]string {
	q := strings.ToLower(strings.TrimSpace(query))
	out := map[string][]string{}
	total := 0
	for _, p := range paths {
		if total >= maxPathMatches {
			break
		}
		if !strings.Contains(strings.ToLower(p), q) {
			continue
		}
		top := p
		if idx := strings.Index(p, "/"); idx > 0 {
			top = p[:idx]
		} else {
			top = "(repo root)"
		}
		out[top] = append(out[top], p)
		total++
	}
	return out
}

func filterEntries(entries []catalogEntry, query string) []catalogEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return entries
	}
	var out []catalogEntry
	for _, e := range entries {
		haystack := strings.ToLower(strings.Join([]string{
			e.ID, e.Type, e.Language, e.Framework, e.Description, strings.Join(e.Tags, " "),
		}, " "))
		if strings.Contains(haystack, q) {
			out = append(out, e)
		}
	}
	return out
}

func toolError(message string) domain.ToolResult {
	return domain.ToolResult{
		Name:    ToolName,
		Content: message,
		IsError: true,
	}
}
