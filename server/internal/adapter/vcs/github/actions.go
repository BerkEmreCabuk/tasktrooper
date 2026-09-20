package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type WorkflowJobDef struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	WorkflowFile string `json:"workflow_file"`
}

func ParseWorkflowJobs(ctx context.Context, token, owner, repo string) ([]WorkflowJobDef, error) {
	var entries []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	err := doJSON(ctx, token, http.MethodGet, "/repos/"+owner+"/"+repo+"/contents/.github/workflows", nil, &entries)
	if err != nil {
		if apiErr, ok := err.(*apiError); ok && apiErr.Status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	var out []WorkflowJobDef
	for _, e := range entries {
		if e.Type != "file" {
			continue
		}
		ext := strings.ToLower(path.Ext(e.Name))
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		content, cerr := getFileContent(ctx, token, owner, repo, e.Path)
		if cerr != nil {
			continue
		}
		jobs := parseWorkflowJobNames(content)
		for _, j := range jobs {
			j.WorkflowFile = e.Name
			out = append(out, j)
		}
	}
	return out, nil
}

func getFileContent(ctx context.Context, token, owner, repo, filePath string) (string, error) {
	var out struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := doJSON(ctx, token, http.MethodGet, "/repos/"+owner+"/"+repo+"/contents/"+filePath, nil, &out); err != nil {
		return "", err
	}
	if out.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	}
	return out.Content, nil
}

func parseWorkflowJobNames(yamlSrc string) []WorkflowJobDef {
	var doc struct {
		Jobs map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(yamlSrc), &doc); err != nil {
		return nil
	}
	var out []WorkflowJobDef
	for key, j := range doc.Jobs {
		name := strings.TrimSpace(j.Name)
		if name == "" {
			name = key
		}
		out = append(out, WorkflowJobDef{Key: key, Name: name})
	}
	return out
}

type WorkflowRun struct {
	ID           int64     `json:"id"`
	RunNumber    int       `json:"run_number"`
	HeadSHA      string    `json:"head_sha"`
	HeadBranch   string    `json:"head_branch"`
	Status       string    `json:"status"`     // queued | in_progress | completed
	Conclusion   string    `json:"conclusion"` // success | failure | cancelled | ...
	Event        string    `json:"event"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    time.Time `json:"created_at"`
	RunStartedAt time.Time `json:"run_started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func ListRunsByHeadSHA(ctx context.Context, token, owner, repo, sha string) ([]WorkflowRun, error) {
	q := url.Values{}
	q.Set("head_sha", sha)
	q.Set("per_page", "100")
	var out struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}
	if err := doJSON(ctx, token, http.MethodGet, "/repos/"+owner+"/"+repo+"/actions/runs?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out.WorkflowRuns, nil
}

func ListWorkflowRunsByFileBranch(ctx context.Context, token, owner, repo, workflowFile, branch string) ([]WorkflowRun, error) {
	return listWorkflowRunsByFileBranchAt(ctx, "", token, owner, repo, workflowFile, branch)
}

func listWorkflowRunsByFileBranchAt(ctx context.Context, base, token, owner, repo, workflowFile, branch string) ([]WorkflowRun, error) {
	q := url.Values{}
	if branch != "" {
		q.Set("branch", branch)
	}
	q.Set("per_page", "20")
	path := "/repos/" + owner + "/" + repo + "/actions/workflows/" + url.PathEscape(workflowFile) + "/runs?" + q.Encode()
	var out struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}
	if err := doJSONAt(ctx, base, token, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.WorkflowRuns, nil
}

type RunJob struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`     // queued | in_progress | completed
	Conclusion  string     `json:"conclusion"` // success | failure | ...
	HTMLURL     string     `json:"html_url"`   // job page on github.com
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

func ListRunJobs(ctx context.Context, token, owner, repo string, runID int64) ([]RunJob, error) {
	var out struct {
		Jobs []RunJob `json:"jobs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs?per_page=100", owner, repo, runID)
	if err := doJSON(ctx, token, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

func GetJobLogs(ctx context.Context, token, owner, repo string, jobID int64) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &apiError{Status: resp.StatusCode, Message: strings.TrimSpace(string(data))}
	}
	return string(data), nil
}

func DispatchWorkflow(ctx context.Context, token, owner, repo, workflowFile, ref string) error {
	return dispatchWorkflowAt(ctx, "", token, owner, repo, workflowFile, ref)
}

func dispatchWorkflowAt(ctx context.Context, base, token, owner, repo, workflowFile, ref string) error {
	path := "/repos/" + owner + "/" + repo + "/actions/workflows/" + url.PathEscape(workflowFile) + "/dispatches"
	return doJSONAt(ctx, base, token, http.MethodPost, path, map[string]any{"ref": ref}, nil)
}

func CreateTag(ctx context.Context, token, owner, repo, tag, sha string) error {
	return createTagAt(ctx, "", token, owner, repo, tag, sha)
}

func createTagAt(ctx context.Context, base, token, owner, repo, tag, sha string) error {
	path := repoPath(owner, repo) + "/git/refs"
	body := map[string]string{"ref": "refs/tags/" + tag, "sha": sha}
	return doJSONAt(ctx, base, token, http.MethodPost, path, body, nil)
}

func IsCIUnavailable(err error) bool {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Status {
	case http.StatusPaymentRequired:
		return true
	case http.StatusForbidden:
		return ciUnavailableMessage(apiErr.Message)
	}
	return false
}

func IsCIUnavailableText(text string) bool {
	lowered := strings.ToLower(text)
	if strings.Contains(lowered, "github api: 402") {
		return true
	}
	return ciUnavailableMessage(lowered)
}

func ciUnavailableMessage(message string) bool {
	msg := strings.ToLower(message)
	for _, needle := range []string{
		"billing", "spending limit", "quota", "payment",
		"actions is disabled", "actions are disabled", "upgrade",
		"has been disabled", "not allowed to run",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func IsRefAlreadyExists(err error) bool {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusUnprocessableEntity &&
		strings.Contains(strings.ToLower(apiErr.Message), "already exists")
}
