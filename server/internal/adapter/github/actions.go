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

// WorkflowJobDef is a job declared in a workflow YAML file. Name is the job's
// display name (its `name:` if set, else the job key) — this is what the
// Actions runs API reports as the job name, so it is what auto-detect and
// status-reads match against.
type WorkflowJobDef struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	WorkflowFile string `json:"workflow_file"`
}

// ParseWorkflowJobs lists .github/workflows/*.yml|yaml and returns every job
// declared across them. A repo with no workflows dir returns an empty slice
// and no error (the caller treats "no workflows" as "pipeline gate disabled").
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
			continue // skip unreadable workflow, don't fail the whole listing
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

// WorkflowRun is a single Actions run.
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

// ListRunsByHeadSHA returns Actions runs whose head commit is sha (both push
// and pull_request event runs for that commit).
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

// ListWorkflowRunsByFileBranch returns recent runs of a specific workflow
// file. An empty branch lists runs across every branch — used both to locate
// the run created by a workflow_dispatch (the dispatch API returns no run
// id) and, with no branch, by a monitor that has no single branch to filter.
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

// RunJob is a job within an Actions run.
type RunJob struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`     // queued | in_progress | completed
	Conclusion  string     `json:"conclusion"` // success | failure | ...
	HTMLURL     string     `json:"html_url"`   // job page on github.com
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

// ListRunJobs returns the jobs of an Actions run.
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

// GetJobLogs returns the plain-text logs of an Actions job (the endpoint
// redirects to a text blob). The caller is responsible for tail-truncation.
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

// DispatchWorkflow triggers a workflow_dispatch run of workflowFile on ref.
// The workflow must declare `on: workflow_dispatch` for this to succeed.
func DispatchWorkflow(ctx context.Context, token, owner, repo, workflowFile, ref string) error {
	return dispatchWorkflowAt(ctx, "", token, owner, repo, workflowFile, ref)
}

func dispatchWorkflowAt(ctx context.Context, base, token, owner, repo, workflowFile, ref string) error {
	path := "/repos/" + owner + "/" + repo + "/actions/workflows/" + url.PathEscape(workflowFile) + "/dispatches"
	return doJSONAt(ctx, base, token, http.MethodPost, path, map[string]any{"ref": ref}, nil)
}

// CreateTag points refs/tags/<tag> at sha. Rollback needs it because
// workflow_dispatch refuses a bare SHA — only a branch or tag name.
func CreateTag(ctx context.Context, token, owner, repo, tag, sha string) error {
	return createTagAt(ctx, "", token, owner, repo, tag, sha)
}

func createTagAt(ctx context.Context, base, token, owner, repo, tag, sha string) error {
	path := repoPath(owner, repo) + "/git/refs"
	body := map[string]string{"ref": "refs/tags/" + tag, "sha": sha}
	return doJSONAt(ctx, base, token, http.MethodPost, path, body, nil)
}

// IsCIUnavailable reports whether a GitHub API error means Actions cannot run
// on this repository AT ALL, as opposed to a transient failure worth retrying.
//
// The distinction is the difference between waiting and giving up. The board
// defers a task's code review until CI reports; if CI can never report, that
// deferral is a deadlock, and this is the function that recognises the case.
//
//	402 Payment Required — the account is out of Actions minutes, or the
//	  spending limit is reached. This is the exact state the user hit: three
//	  cards waiting on workflows the repository had no budget to run.
//	403 Forbidden — overloaded on GitHub's side. It covers "Actions is disabled
//	  for this repository" and "upgrade your plan", which are permanent, and
//	  plain secondary rate limiting, which is not. Only the permanent readings
//	  count, hence the message check: treating a rate limit as "CI unavailable"
//	  would open the review gate on a build that was about to succeed.
//
// A 404 is deliberately NOT here. It means the token cannot see the repository,
// which is a configuration error worth surfacing as one, and the caller's
// timeout path handles it without claiming to know why.
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

// IsCIUnavailableText answers the same question as IsCIUnavailable for a
// failure that reaches its reader as TEXT rather than as an error value.
//
// The deploy path is the one that needs it: a workflow dispatch that fails is
// recorded on the pipeline job as "dispatch failed: <error>", and the error
// value itself is gone by the time anything decides what that failure MEANS. A
// deploy that could not be dispatched because the account is out of Actions
// minutes is not a broken change — bouncing the task to need_revision for it
// sends a developer to fix code that is fine, which is the loop
// PipelineBounceGuard already exists for on the build side.
//
// It reads the 402 the way a human does, out of the "github api: 402 …" text
// apiError.Error() writes, and then applies the same permanent-403 wordings.
func IsCIUnavailableText(text string) bool {
	lowered := strings.ToLower(text)
	if strings.Contains(lowered, "github api: 402") {
		return true
	}
	return ciUnavailableMessage(lowered)
}

// ciUnavailableMessage matches the GitHub wordings that mean Actions cannot run
// on this repository at all, as opposed to a secondary rate limit that clears
// by itself.
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

// IsRefAlreadyExists reports whether a CreateTag failure is GitHub saying the
// ref is already there.
//
// It matters because the two tag callers want opposite things from it. A
// ROLLBACK tag carries a timestamp, so a collision would be a genuine surprise.
// A RELEASE tag is derived from the commit (domain.ReleaseTagForCommit), so a
// collision is the normal outcome of releasing the same commit twice — and
// treating it as a failure would drop the release back onto the default branch,
// which is exactly the drift this system was fixing.
//
// GitHub answers 422 with "Reference already exists" for this. The status alone
// is not enough (422 also covers an invalid SHA), so both are checked.
func IsRefAlreadyExists(err error) bool {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusUnprocessableEntity &&
		strings.Contains(strings.ToLower(apiErr.Message), "already exists")
}
