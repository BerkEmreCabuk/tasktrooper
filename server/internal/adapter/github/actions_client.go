package github

import (
	"context"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ActionsAPI adapts the package's free functions to port.ActionsClient so
// deployops can be tested against a fake. It adds no behaviour of its own
// beyond resolving the token per call.
//
// Per call, not per construction, and that is the whole point: the GitHub token
// is a TENANT's, stored encrypted on that tenant's settings row, while this
// client is built once for the process. Holding a string meant one tenant's
// token was baked into the client every other tenant then used — and because
// the token could only be resolved at boot, where there is no tenant, what
// actually happened was that the deploy console was gated off a read that
// always failed and never got built at all. The source below is handed the
// caller's context and reads the token belonging to whoever is being served.
type ActionsAPI struct {
	token   TokenSource
	baseURL string
}

// TokenSource resolves the GitHub token for the tenant on ctx. It is satisfied
// directly by pgSettings.GitHubToken.
type TokenSource func(ctx context.Context) (string, error)

// NewActionsAPI builds a client that presents one fixed token. It is for the
// callers that already hold the right one for the work in hand (a tool
// execution, a test) — anything constructed once for the process wants
// NewActionsAPIFor instead.
func NewActionsAPI(token string) *ActionsAPI {
	return &ActionsAPI{token: func(context.Context) (string, error) { return token, nil }}
}

// NewActionsAPIFor builds a client that resolves the acting tenant's token on
// every call. A nil source yields a client that presents no token, which GitHub
// answers for public reads and refuses for everything else — the same outcome
// as an unconnected tenant, and a truthful one.
func NewActionsAPIFor(source TokenSource) *ActionsAPI {
	return &ActionsAPI{token: source}
}

// SetBaseURL redirects requests at a test server. Empty means the real API.
func (a *ActionsAPI) SetBaseURL(u string) { a.baseURL = u }

// resolve reads the acting tenant's token. An error is returned to the caller
// rather than degraded to an empty token: "GitHub is not connected" and "the
// settings row could not be read" are different problems and only one of them
// is the user's to fix.
func (a *ActionsAPI) resolve(ctx context.Context) (string, error) {
	if a == nil || a.token == nil {
		return "", nil
	}
	return a.token(ctx)
}

// ListWorkflowRuns returns recent runs of workflowFile, mapped to
// port.ActionsRun. An empty branch means every branch.
func (a *ActionsAPI) ListWorkflowRuns(ctx context.Context, owner, repo, workflowFile, branch string) ([]port.ActionsRun, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return nil, err
	}
	runs, err := listWorkflowRunsByFileBranchAt(ctx, a.baseURL, token, owner, repo, workflowFile, branch)
	if err != nil {
		return nil, err
	}
	return mapActionsRuns(runs), nil
}

// DispatchWorkflow triggers a workflow_dispatch run of workflowFile on ref.
func (a *ActionsAPI) DispatchWorkflow(ctx context.Context, owner, repo, workflowFile, ref string) error {
	token, err := a.resolve(ctx)
	if err != nil {
		return err
	}
	return dispatchWorkflowAt(ctx, a.baseURL, token, owner, repo, workflowFile, ref)
}

// CreateTag points refs/tags/<tag> at sha.
func (a *ActionsAPI) CreateTag(ctx context.Context, owner, repo, tag, sha string) error {
	token, err := a.resolve(ctx)
	if err != nil {
		return err
	}
	return createTagAt(ctx, a.baseURL, token, owner, repo, tag, sha)
}

// ListRunsForCommit returns every Actions run whose head commit is sha.
func (a *ActionsAPI) ListRunsForCommit(ctx context.Context, owner, repo, sha string) ([]port.ActionsRun, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return nil, err
	}
	runs, err := listRunsByHeadSHAAt(ctx, a.baseURL, token, owner, repo, sha)
	if err != nil {
		return nil, err
	}
	return mapActionsRuns(runs), nil
}

// ListRunJobs returns a run's jobs.
func (a *ActionsAPI) ListRunJobs(ctx context.Context, owner, repo string, runID int64) ([]port.ActionsJob, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return nil, err
	}
	jobs, err := listRunJobsAt(ctx, a.baseURL, token, owner, repo, runID)
	if err != nil {
		return nil, err
	}
	out := make([]port.ActionsJob, len(jobs))
	for i, j := range jobs {
		out[i] = port.ActionsJob{
			ID:          j.ID,
			Name:        j.Name,
			Status:      j.Status,
			Conclusion:  j.Conclusion,
			HTMLURL:     j.HTMLURL,
			StartedAt:   j.StartedAt,
			CompletedAt: j.CompletedAt,
		}
	}
	return out, nil
}

// JobLogs returns a job's plain-text log, untruncated.
func (a *ActionsAPI) JobLogs(ctx context.Context, owner, repo string, jobID int64) (string, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return "", err
	}
	return getJobLogsAt(ctx, a.baseURL, token, owner, repo, jobID)
}

// CommitDeployStatus reads the two non-Actions deploy signals for a commit:
// the combined commit status first, then the GitHub Deployment.
//
// Order matters and is not arbitrary. The combined status is what a
// push-to-deploy host's bot writes on every build and what `gh api
// repos/.../commits/<sha>/status` returns, so it is present whenever the
// mechanism is present. Deployments are richer (environment name, log URL) but
// a repository can carry deployments opened by something that never closes
// them — an unfinished one would read as an eternal "pending" and park the
// watch forever if it were consulted first.
//
// A failure to read the status is NOT swallowed into "no signal": the caller
// has to be able to tell "nothing deploys this repository" from "GitHub did not
// answer", because only the first of those is a reason to stop watching.
func (a *ActionsAPI) CommitDeployStatus(ctx context.Context, owner, repo, sha string) (port.CommitDeploySignal, error) {
	token, err := a.resolve(ctx)
	if err != nil {
		return port.CommitDeploySignal{}, err
	}
	combined, err := getCombinedStatusAt(ctx, a.baseURL, token, owner, repo, sha)
	if err != nil {
		return port.CommitDeploySignal{}, err
	}
	if combined.HasSignal() {
		signal := port.CommitDeploySignal{
			Kind:     domain.DeploySignalCommitStatus,
			State:    normalizeStatusState(combined.State),
			Contexts: combined.Contexts(),
		}
		// The newest status carries the words a human wrote and the link they
		// pointed at; the combined state above is the verdict.
		if len(combined.Statuses) > 0 {
			signal.Description = combined.Statuses[0].Description
			signal.URL = combined.Statuses[0].TargetURL
		}
		return signal, nil
	}

	deployments, err := listDeploymentsForSHAAt(ctx, a.baseURL, token, owner, repo, sha)
	if err != nil {
		return port.CommitDeploySignal{}, err
	}
	for _, d := range deployments {
		statuses, serr := listDeploymentStatusesAt(ctx, a.baseURL, token, owner, repo, d.ID)
		if serr != nil {
			return port.CommitDeploySignal{}, serr
		}
		if len(statuses) == 0 {
			continue
		}
		latest := statuses[0]
		return port.CommitDeploySignal{
			Kind:        domain.DeploySignalDeploymentState,
			State:       normalizeStatusState(latest.State),
			Environment: d.Environment,
			Description: latest.Description,
			URL:         firstNonEmptyString(latest.EnvironmentURL, latest.LogURL, latest.TargetURL),
		}, nil
	}
	return port.CommitDeploySignal{}, nil
}

func mapActionsRuns(runs []WorkflowRun) []port.ActionsRun {
	out := make([]port.ActionsRun, len(runs))
	for i, r := range runs {
		out[i] = port.ActionsRun{
			ID:           r.ID,
			RunNumber:    r.RunNumber,
			HeadSHA:      r.HeadSHA,
			HeadBranch:   r.HeadBranch,
			Status:       r.Status,
			Conclusion:   r.Conclusion,
			HTMLURL:      r.HTMLURL,
			Event:        r.Event,
			RunStartedAt: r.RunStartedAt,
			UpdatedAt:    r.UpdatedAt,
		}
	}
	return out
}

// normalizeStatusState folds GitHub's two vocabularies onto one. Commit
// statuses use success/pending/failure/error; deployment statuses add
// queued/in_progress/inactive. Anything unrecognised is treated as pending
// rather than as a verdict — inventing "failure" from a word this code has not
// seen before is how an automatic rollback fires on a signal nobody meant as
// one.
func normalizeStatusState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "success":
		return "success"
	case "failure", "error":
		return "failure"
	default:
		return "pending"
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
