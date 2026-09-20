package github

// The GitHub side of the deploy watch: everything that can say "this commit
// was deployed" for a repository that has no Actions deploy job.
//
// An Actions run is only one of the ways a merge reaches production, and it is
// not the common one for a frontend. A repository connected to a push-to-deploy
// host (Vercel) runs no workflow of its own at all — the host's GitHub App
// watches the default branch, builds it, and reports back through two GitHub
// surfaces this file reads:
//
//	commit statuses  GET /repos/{o}/{r}/commits/{sha}/status
//	                 the combined state (success | pending | failure) plus one
//	                 context per reporter — `vercel[bot]` writes "Vercel". This
//	                 is exactly what `gh api repos/.../commits/main/status`
//	                 returns, and it is the primary signal.
//	deployments      GET /repos/{o}/{r}/deployments?sha=<sha>
//	                 + .../deployments/{id}/statuses — the richer surface, with
//	                 an environment name and a log/target URL. Read second,
//	                 because a repository can carry deployments created by
//	                 something that never finishes them.
//
// No provider credentials are involved anywhere here. That is the point: the
// deploy watch reads GitHub, which every one of these hosts already writes to,
// instead of growing one client per hosting provider.
//
// Every path is built through repoPath (url.PathEscape'd) rather than by
// interpolating owner/repo into a format string — the pattern api.go/pr.go
// established, and the one the older actions.go interpolations are a known
// hazard against.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CombinedStatus is GitHub's rollup of every commit status written against one
// commit.
type CombinedStatus struct {
	// State is "success", "pending", "failure" or "error". GitHub returns
	// "pending" both for a check in flight and for a commit with no statuses
	// at all, so TotalCount is what tells those apart — see HasSignal.
	State      string         `json:"state"`
	SHA        string         `json:"sha"`
	TotalCount int            `json:"total_count"`
	Statuses   []CommitStatus `json:"statuses"`
}

// HasSignal reports whether anything actually wrote a status for this commit.
// A commit nobody reported on comes back {state: "pending", total_count: 0},
// which must read as "no deploy signal" rather than as "a deploy is running" —
// the difference between waiting for a deploy and waiting forever.
func (c CombinedStatus) HasSignal() bool { return c.TotalCount > 0 && len(c.Statuses) > 0 }

// Contexts lists the reporters, newest first as GitHub returns them.
func (c CombinedStatus) Contexts() []string {
	out := make([]string, 0, len(c.Statuses))
	for _, s := range c.Statuses {
		if s.Context != "" {
			out = append(out, s.Context)
		}
	}
	return out
}

// CommitStatus is one reporter's verdict on a commit.
type CommitStatus struct {
	State       string    `json:"state"`
	Context     string    `json:"context"`
	Description string    `json:"description"`
	TargetURL   string    `json:"target_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GetCombinedStatus returns the combined commit status for sha.
func GetCombinedStatus(ctx context.Context, token, owner, repo, sha string) (CombinedStatus, error) {
	return getCombinedStatusAt(ctx, "", token, owner, repo, sha)
}

func getCombinedStatusAt(ctx context.Context, base, token, owner, repo, sha string) (CombinedStatus, error) {
	var out CombinedStatus
	path := repoPath(owner, repo) + "/commits/" + url.PathEscape(sha) + "/status"
	if err := doJSONAt(ctx, base, token, http.MethodGet, path, nil, &out); err != nil {
		return CombinedStatus{}, err
	}
	return out, nil
}

// Deployment is a GitHub Deployment record. A push-to-deploy host opens one per
// build; this repository's own release.yml opens one too (see CLAUDE.md), which
// is why the environment name is carried through rather than assumed.
type Deployment struct {
	ID          int64     `json:"id"`
	SHA         string    `json:"sha"`
	Ref         string    `json:"ref"`
	Environment string    `json:"environment"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// DeploymentStatus is one state transition of a Deployment. The interesting
// ones are "success", "failure", "error" and "in_progress"/"queued"/"pending".
type DeploymentStatus struct {
	State          string    `json:"state"`
	Description    string    `json:"description"`
	EnvironmentURL string    `json:"environment_url"`
	LogURL         string    `json:"log_url"`
	TargetURL      string    `json:"target_url"`
	CreatedAt      time.Time `json:"created_at"`
}

// ListDeploymentsForSHA returns the deployments opened for a commit, newest
// first.
func ListDeploymentsForSHA(ctx context.Context, token, owner, repo, sha string) ([]Deployment, error) {
	return listDeploymentsForSHAAt(ctx, "", token, owner, repo, sha)
}

func listDeploymentsForSHAAt(ctx context.Context, base, token, owner, repo, sha string) ([]Deployment, error) {
	q := url.Values{}
	q.Set("sha", sha)
	q.Set("per_page", "20")
	var out []Deployment
	path := repoPath(owner, repo) + "/deployments?" + q.Encode()
	if err := doJSONAt(ctx, base, token, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListDeploymentStatuses returns a deployment's statuses, newest first.
func ListDeploymentStatuses(ctx context.Context, token, owner, repo string, deploymentID int64) ([]DeploymentStatus, error) {
	return listDeploymentStatusesAt(ctx, "", token, owner, repo, deploymentID)
}

func listDeploymentStatusesAt(ctx context.Context, base, token, owner, repo string, deploymentID int64) ([]DeploymentStatus, error) {
	var out []DeploymentStatus
	path := repoPath(owner, repo) + "/deployments/" + strconv.FormatInt(deploymentID, 10) + "/statuses?per_page=20"
	if err := doJSONAt(ctx, base, token, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// listRunsByHeadSHAAt is ListRunsByHeadSHA with a base override, so the deploy
// watch's client can be pointed at a test server the same way every other
// method on ActionsAPI can. The exported wrapper above it is unchanged.
func listRunsByHeadSHAAt(ctx context.Context, base, token, owner, repo, sha string) ([]WorkflowRun, error) {
	q := url.Values{}
	q.Set("head_sha", sha)
	q.Set("per_page", "100")
	var out struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}
	path := repoPath(owner, repo) + "/actions/runs?" + q.Encode()
	if err := doJSONAt(ctx, base, token, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.WorkflowRuns, nil
}

// listRunJobsAt is ListRunJobs with a base override.
func listRunJobsAt(ctx context.Context, base, token, owner, repo string, runID int64) ([]RunJob, error) {
	var out struct {
		Jobs []RunJob `json:"jobs"`
	}
	path := repoPath(owner, repo) + "/actions/runs/" + strconv.FormatInt(runID, 10) + "/jobs?per_page=100"
	if err := doJSONAt(ctx, base, token, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

// getJobLogsAt is GetJobLogs with a base override. The endpoint answers a
// redirect to a plain-text blob rather than JSON, so it cannot go through
// doJSONAt; the read is capped at 1 MiB here and tail-truncated by the caller,
// which is where the size that matters (what fits in a model's context) is
// known.
func getJobLogsAt(ctx context.Context, base, token, owner, repo string, jobID int64) (string, error) {
	path := repoPath(owner, repo) + "/actions/jobs/" + strconv.FormatInt(jobID, 10) + "/logs"
	return fetchText(ctx, resolveBase(base)+path, token)
}

func fetchText(ctx context.Context, endpoint, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
		return "", &apiError{Status: resp.StatusCode, Message: fmt.Sprintf("job logs: %s", strings.TrimSpace(string(data)))}
	}
	return string(data), nil
}
