// Package deploywatch answers one question about one task: what happened to
// the commit its pull request merge produced.
//
// It exists because "did this task reach production" had three different
// answers depending on the repository, and the release path knew none of them.
// A backend deploys through a GitHub Actions job; a frontend on a
// push-to-deploy host runs no workflow at all and reports through a commit
// status; a repository with neither simply has no deploy signal, which is an
// answer too. The board needs ONE state to act on, keyed on ONE thing — the
// merge commit (board_tasks.merge_commit_sha, migration 104) — because that is
// the only identifier that means "this task's change" rather than "whatever the
// default branch is carrying today".
//
// The package deliberately owns no clock loop of its own. A pending deploy is
// parked on (domain.ResourceDeployWatch) and re-dispatched by
// application/board.DeploySweeper; nothing here sleeps, and no tool call waits.
package deploywatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// DefaultHealthWindow is how long after a successful deploy an incident on that
// environment is still attributed to the task that just released.
//
// Fifteen minutes rather than the remedy engine's generic 45: this window is
// keyed on a specific commit and is used to BLAME a specific card (and, with
// auto_rollback on, to roll it back), so it has to be short enough that a
// coincidence does not get a task reverted. The generic 45-minute correlation
// in prodops/remedy.go is unchanged and still produces its advisory rollback
// suggestion — that one only ever writes words.
const DefaultHealthWindow = 15 * time.Minute

// resolveTimeout bounds the GitHub reads one Resolve makes. The caller is
// either a tool call inside an agent's turn or a sweeper pass; neither may be
// held open by a wedged API.
const resolveTimeout = 30 * time.Second

// logFetchTimeout bounds a logs_url fetch. Same reasoning, tighter number: an
// application's own log endpoint that cannot answer in ten seconds is not
// going to answer usefully.
const logFetchTimeout = 10 * time.Second

// maxLogBytes caps what is read from an application's logs_url. The Actions job
// log has its own cap in the adapter (1 MiB); both are then tail-truncated to
// something a model can actually read before they leave this package.
const maxLogBytes = 1 << 20

// TaskStore is the slice of the board this package reads.
//
// The names match port.BoardTaskStore's exactly so the store satisfies this
// without an adapter — this is a narrowing, not a translation.
type TaskStore interface {
	Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
	// FindTaskByMergeCommit resolves the task whose merge produced sha. It is
	// the reverse of the watch: production is unhappy, which card put this
	// commit there.
	FindTaskByMergeCommit(ctx context.Context, repositoryID uuid.UUID, sha string) (domain.BoardTask, error)
}

// Commenter writes the watch's findings onto the card.
type Commenter interface {
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

// RepositoryResolver reads a repository row.
type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// Deps wires the service's collaborators. Everything is optional except
// Tasks/Targets/Repos/Actions; a nil collaborator disables the feature that
// needs it rather than panicking, which is how a build with no GitHub token or
// no git still starts.
type Deps struct {
	Tasks     TaskStore
	Comments  Commenter
	Targets   port.DeployTargetStore
	Repos     RepositoryResolver
	Runs      port.DeploymentRunStore
	Pipeline  port.RepositoryPipelineJobStore
	Actions   port.ActionsClient
	Rollbacks Rollbacker
	Git       GitReverter
	Incidents IncidentIngester
	// RepoCoordinates resolves a repository's GitHub owner/name. domain.Repository
	// carries neither — only a local RootPath — so this is injected, exactly as
	// deployops.Service.SetRepoResolver does for the same reason.
	RepoCoordinates func(ctx context.Context, repo domain.Repository) (owner, name string, err error)
	// HealthWindow overrides DefaultHealthWindow.
	HealthWindow time.Duration
}

// Service is the deploy watch.
type Service struct {
	tasks     TaskStore
	comments  Commenter
	targets   port.DeployTargetStore
	repos     RepositoryResolver
	runs      port.DeploymentRunStore
	pipeline  port.RepositoryPipelineJobStore
	actions   port.ActionsClient
	rollbacks Rollbacker
	git       GitReverter
	incidents IncidentIngester
	coords    func(ctx context.Context, repo domain.Repository) (string, string, error)

	healthWindow time.Duration
	// policy vets a logs_url before it is dialled, on EVERY fetch. The field is
	// swappable for tests the same way prodops.Monitor's is — and, as there,
	// there is deliberately no way to hand this a bare *http.Client and opt out
	// of the guard altogether.
	policy urlguard.Policy
	now    func() time.Time
}

func New(deps Deps) *Service {
	window := deps.HealthWindow
	if window <= 0 {
		window = DefaultHealthWindow
	}
	return &Service{
		tasks:        deps.Tasks,
		comments:     deps.Comments,
		targets:      deps.Targets,
		repos:        deps.Repos,
		runs:         deps.Runs,
		pipeline:     deps.Pipeline,
		actions:      deps.Actions,
		rollbacks:    deps.Rollbacks,
		git:          deps.Git,
		incidents:    deps.Incidents,
		coords:       deps.RepoCoordinates,
		healthWindow: window,
		policy:       urlguard.Default(),
		now:          time.Now,
	}
}

// SetClock overrides the service's clock; tests use it to make health windows
// assertable.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetURLPolicy overrides what counts as a dialable logs URL.
func (s *Service) SetURLPolicy(p urlguard.Policy) { s.policy = p }

// HealthWindow is how long a successful deploy owns its environment's
// incidents. Read by the attribution and by the status the agent sees.
func (s *Service) HealthWindow() time.Duration { return s.healthWindow }

// ErrNotConfigured is every "this deployment cannot answer that" rolled into
// one sentinel: no GitHub client, no repository coordinates, no board.
var ErrNotConfigured = errors.New("the deploy watch is not configured on this deployment (GitHub is not connected)")

// Status resolves the deploy state of a task's merge commit.
//
// The three signals are consulted in a fixed order and the first one that has
// anything to say wins:
//
//  1. an Actions run for the commit that contains a DEPLOY job. A repository
//     whose CI and CD live in the same workflow ("Backend CI/CD" with a
//     `deploy` job) answers here, and the job — not the run — is what is read,
//     because the run also carries build and test and folding those in would
//     report a red unit test as a failed deploy.
//  2. the commit's combined status. This is the push-to-deploy case: the host's
//     GitHub App writes `success | Vercel` against the commit and there is no
//     workflow anywhere. Read over the GitHub API with no provider credentials.
//  3. a GitHub Deployment opened against the commit (inside the same adapter
//     call as 2, consulted after it).
//
// Nothing found is DeployWatchNoSignal, not a failure and not a wait: a
// repository that does not deploy on merge has to be able to reach the end of
// the watch.
func (s *Service) Status(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.DeployWatchStatus, error) {
	if s.tasks == nil || s.actions == nil || s.coords == nil {
		return domain.DeployWatchStatus{}, ErrNotConfigured
	}
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.DeployWatchStatus{}, err
	}
	return s.statusForTask(ctx, task)
}

// StatusForTask is Status with the task already in hand — the sweeper's entry,
// which has just read the row it is deciding about.
func (s *Service) StatusForTask(ctx context.Context, task domain.BoardTask) (domain.DeployWatchStatus, error) {
	if s.actions == nil || s.coords == nil {
		return domain.DeployWatchStatus{}, ErrNotConfigured
	}
	return s.statusForTask(ctx, task)
}

func (s *Service) statusForTask(ctx context.Context, task domain.BoardTask) (domain.DeployWatchStatus, error) {
	out := domain.DeployWatchStatus{
		TaskID:       task.ID,
		TaskKey:      task.Key,
		RepositoryID: task.RepositoryID,
		Env:          domain.DeployEnvProd,
		MergeSHA:     strings.TrimSpace(task.MergeCommitSHA),
		State:        domain.DeployWatchUnknown,
		Signal:       domain.DeploySignalNone,
		CheckedAt:    s.now(),
	}

	// The target is read first and its failure is tolerated: the health URL,
	// the logs URL and the rollback policy are context for the answer, not the
	// answer. A repository with no prod target still has a deploy to watch.
	if s.targets != nil {
		if target, terr := s.targets.Get(ctx, task.RepositoryID, "", out.Env); terr == nil {
			out.HealthURL = target.HealthURL
			out.LogsURL = target.LogsURL
			out.AutoRollback = target.AutoRollback
		} else if !errors.Is(terr, port.ErrNotFound) {
			log.Warn().Err(terr).Str("task_id", task.ID.String()).Msg("deploy watch: reading deploy target failed")
		}
	}

	if out.MergeSHA == "" {
		out.Detail = "This task has no merge commit recorded — its pull request has not been merged, so nothing of it can be in production yet."
		return out, nil
	}

	repo, err := s.repos.Get(ctx, task.RepositoryID)
	if err != nil {
		return out, fmt.Errorf("deploy watch: loading repository: %w", err)
	}
	owner, name, err := s.coords(ctx, repo)
	if err != nil {
		return out, fmt.Errorf("deploy watch: resolving repository coordinates: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	if resolved, ok, rerr := s.actionsSignal(ctx, task.RepositoryID, owner, name, out.MergeSHA); rerr != nil {
		return out, rerr
	} else if ok {
		return s.finish(mergeStatus(out, resolved)), nil
	}

	signal, err := s.actions.CommitDeployStatus(ctx, owner, name, out.MergeSHA)
	if err != nil {
		return out, fmt.Errorf("deploy watch: reading commit deploy status: %w", err)
	}
	if signal.Kind == "" {
		out.State = domain.DeployWatchNoSignal
		out.Signal = domain.DeploySignalNone
		out.Detail = fmt.Sprintf("Nothing reports a deploy of %s: no Actions run carries a deploy job for it, and no commit status or GitHub Deployment was written against it. "+
			"This repository does not deploy on merge (or its deploy has not started yet and has left no trace).", domain.ShortSHA(out.MergeSHA))
		return out, nil
	}
	out.Signal = signal.Kind
	out.Contexts = signal.Contexts
	switch signal.State {
	case "success":
		out.State = domain.DeployWatchSuccess
	case "failure":
		out.State = domain.DeployWatchFailure
	default:
		out.State = domain.DeployWatchPending
	}
	out.Detail = describeCommitSignal(signal, out.MergeSHA)
	return s.finish(out), nil
}

// actionsSignal folds the deploy JOBS across every Actions run for the commit.
//
// Returning ok=false is the important case: it means the runs exist but none of
// them contains anything that looks like a deploy — a repository whose Actions
// only build and test. The caller then moves on to the commit-status signal
// instead of reporting the CI result as a deploy result, which is the mistake
// that would make a green unit test read as "released".
func (s *Service) actionsSignal(ctx context.Context, repositoryID uuid.UUID, owner, name, sha string) (domain.DeployWatchStatus, bool, error) {
	runs, err := s.actions.ListRunsForCommit(ctx, owner, name, sha)
	if err != nil {
		return domain.DeployWatchStatus{}, false, fmt.Errorf("deploy watch: listing runs for commit: %w", err)
	}
	if len(runs) == 0 {
		return domain.DeployWatchStatus{}, false, nil
	}
	matcher := s.deployJobMatcher(ctx, repositoryID)

	out := domain.DeployWatchStatus{Signal: domain.DeploySignalActionsRun}
	found := false
	pending := false
	for _, run := range runs {
		jobs, jerr := s.actions.ListRunJobs(ctx, owner, name, run.ID)
		if jerr != nil {
			// One unreadable run must not fail the whole watch — but it must
			// not silently count as "no deploy job" either, or a transient
			// error would flip a running deploy to no_signal and end the
			// watch. Treated as pending: try again next sweep.
			log.Warn().Err(jerr).Int64("run_id", run.ID).Msg("deploy watch: listing run jobs failed")
			pending = true
			continue
		}
		for _, job := range jobs {
			if !matcher(job.Name) {
				continue
			}
			found = true
			out.RunID = run.ID
			out.RunURL = run.HTMLURL
			switch {
			case job.Status != "completed":
				pending = true
			case job.Conclusion == "success":
				// Keep looking: a later run of the same commit may have failed.
			default:
				failed := domain.DeployWatchJob{
					ID: job.ID, Name: job.Name, Status: job.Status, Conclusion: job.Conclusion, URL: job.HTMLURL,
				}
				if failed.URL == "" {
					failed.URL = run.HTMLURL
				}
				out.State = domain.DeployWatchFailure
				out.FailedJob = &failed
				out.Detail = fmt.Sprintf("The deploy job %q of %s concluded %q.", job.Name, domain.ShortSHA(sha), job.Conclusion)
				return out, true, nil
			}
		}
	}
	if !found {
		return domain.DeployWatchStatus{}, pending, nil
	}
	if pending {
		out.State = domain.DeployWatchPending
		out.Detail = fmt.Sprintf("The deploy of %s is still running.", domain.ShortSHA(sha))
		return out, true, nil
	}
	out.State = domain.DeployWatchSuccess
	out.Detail = fmt.Sprintf("The deploy job for %s finished successfully.", domain.ShortSHA(sha))
	return out, true, nil
}

// deployJobMatcher decides which job in a run is THE deploy.
//
// The repository's own pipeline mapping is the authority when it has one: a
// prod/preprod deploy category mapped to a job name is a human saying "this job
// is the deploy", and it beats any guess. The name heuristic is the fallback
// for a repository nobody mapped, and it is deliberately narrow — "deploy",
// "release", "publish", "ship" — because a matcher that is too eager turns a
// job called "deployment-docs" into the thing an automatic rollback fires on.
func (s *Service) deployJobMatcher(ctx context.Context, repositoryID uuid.UUID) func(string) bool {
	mapped := map[string]bool{}
	if s.pipeline != nil {
		if jobs, err := s.pipeline.ListByRepository(ctx, repositoryID); err == nil {
			for _, j := range jobs {
				switch j.Category {
				case domain.PipelineCategoryProdDeploy, domain.PipelineCategoryPreProdDeploy:
					if ref := strings.TrimSpace(j.TargetRef); ref != "" {
						mapped[strings.ToLower(ref)] = true
					}
				}
			}
		} else {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("deploy watch: reading pipeline mappings failed")
		}
	}
	return func(jobName string) bool {
		lower := strings.ToLower(strings.TrimSpace(jobName))
		if lower == "" {
			return false
		}
		if mapped[lower] {
			return true
		}
		for _, kw := range []string{"deploy", "release", "publish", "ship"} {
			if strings.Contains(lower, kw) {
				return true
			}
		}
		return false
	}
}

// mergeStatus copies the resolved signal onto the context-carrying shell.
func mergeStatus(base, resolved domain.DeployWatchStatus) domain.DeployWatchStatus {
	base.State = resolved.State
	base.Signal = resolved.Signal
	base.Detail = resolved.Detail
	base.RunID = resolved.RunID
	base.RunURL = resolved.RunURL
	base.FailedJob = resolved.FailedJob
	if len(resolved.Contexts) > 0 {
		base.Contexts = resolved.Contexts
	}
	return base
}

// finish stamps the post-release health window onto a successful deploy.
//
// The window is what makes an incident attributable. Until it closes, an
// incident opened on this environment belongs to THIS task — not to "some
// deploy in the last 45 minutes", which is all the existing correlation could
// say and which is why its rollback suggestion never named a card.
func (s *Service) finish(status domain.DeployWatchStatus) domain.DeployWatchStatus {
	if status.State != domain.DeployWatchSuccess {
		return status
	}
	until := s.now().Add(s.healthWindow)
	status.HealthWindowUntil = &until
	status.Detail = strings.TrimSpace(status.Detail + fmt.Sprintf(
		" Production is now running this task's code; watch %s until %s — an incident opened before then is this release's.",
		healthLabel(status.HealthURL), until.Format(time.RFC3339)))
	return status
}

func healthLabel(healthURL string) string {
	if strings.TrimSpace(healthURL) == "" {
		return "the environment (no health_url is recorded — record one with update_deploy_target)"
	}
	return urlguard.LogRaw(healthURL)
}

func describeCommitSignal(signal port.CommitDeploySignal, sha string) string {
	who := strings.Join(signal.Contexts, ", ")
	if who == "" {
		who = signal.Environment
	}
	if who == "" {
		who = "the deploy provider"
	}
	verb := map[string]string{
		"success": "reported this deploy successful",
		"failure": "reported this deploy FAILED",
	}[signal.State]
	if verb == "" {
		verb = "has not finished this deploy yet"
	}
	out := fmt.Sprintf("%s %s for %s (no Actions deploy job exists — this repository deploys on push).",
		who, verb, domain.ShortSHA(sha))
	if d := strings.TrimSpace(signal.Description); d != "" {
		out += " " + d
	}
	return out
}

// ---------------------------------------------------------------------- logs

// LogSource names which log a fetch wants.
const (
	LogSourceActionsJob = "actions_job"
	LogSourceEndpoint   = "logs_url"
)

// LogResult is one log fetch, already truncated to something a model can read.
type LogResult struct {
	Source    string `json:"source"`
	Reference string `json:"reference,omitempty"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Note      string `json:"note,omitempty"`
}

// JobLogs fetches the Actions job log and returns a SUMMARY of it, not the
// blob.
//
// A deploy job's raw log is tens of thousands of lines of setup, cache
// restores and dependency resolution, and the failure is four of them near the
// bottom. Handing the whole thing to a model costs the run's entire context
// budget to deliver information that a tail plus the error lines carries
// exactly as well — and a truncation from the FRONT would cut off precisely the
// part that matters, which is why this tails.
func (s *Service) JobLogs(ctx context.Context, repositoryID uuid.UUID, jobID int64, maxChars int) (LogResult, error) {
	if s.actions == nil || s.coords == nil {
		return LogResult{}, ErrNotConfigured
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return LogResult{}, err
	}
	owner, name, err := s.coords(ctx, repo)
	if err != nil {
		return LogResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	raw, err := s.actions.JobLogs(ctx, owner, name, jobID)
	if err != nil {
		return LogResult{}, fmt.Errorf("deploy watch: fetching job logs: %w", err)
	}
	content, truncated := SummarizeLog(raw, maxChars)
	return LogResult{
		Source:    LogSourceActionsJob,
		Reference: fmt.Sprintf("job %d", jobID),
		Content:   content,
		Truncated: truncated,
	}, nil
}

// EndpointLogs fetches the environment's own logs_url.
//
// The destination is re-validated here, on every fetch, and not only when the
// URL was written. It is agent-writable, and a name that resolved to a public
// address at save time is free to answer 127.0.0.1 by the time it is dialled —
// the same reasoning prodops.Monitor.probe carries, and the same refusal text,
// which deliberately says nothing about which range was hit: the result is read
// by a model, and "loopback" versus "private address" is a free network map.
func (s *Service) EndpointLogs(ctx context.Context, repositoryID uuid.UUID, env string, maxChars int) (LogResult, error) {
	if s.targets == nil {
		return LogResult{}, ErrNotConfigured
	}
	target, err := s.targets.Get(ctx, repositoryID, "", env)
	if err != nil {
		return LogResult{}, err
	}
	raw := strings.TrimSpace(target.LogsURL)
	if raw == "" {
		return LogResult{}, fmt.Errorf("no logs_url is recorded for %s — record one with update_deploy_target if this application exposes a log endpoint", env)
	}

	ctx, cancel := context.WithTimeout(ctx, logFetchTimeout)
	defer cancel()
	dest, err := s.policy.Validate(ctx, raw)
	if err != nil {
		log.Warn().Err(err).Str("logs_url", urlguard.LogRaw(raw)).Msg("deploy watch: logs URL destination refused")
		return LogResult{}, errors.New("logs_url is not an allowed destination")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dest.URL.String(), nil)
	if err != nil {
		return LogResult{}, errors.New("logs_url is not an allowed destination")
	}
	resp, err := s.policy.ClientFor(dest, logFetchTimeout).Do(req)
	if err != nil {
		if errors.Is(err, urlguard.ErrBlocked) {
			log.Warn().Err(err).Str("logs_url", urlguard.LogValue(dest.URL)).Msg("deploy watch: logs URL destination refused at dial")
			return LogResult{}, errors.New("logs_url is not an allowed destination")
		}
		return LogResult{}, fmt.Errorf("fetching %s: %w", urlguard.LogRaw(raw), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxLogBytes))
	content, truncated := SummarizeLog(string(body), maxChars)
	out := LogResult{
		Source:    LogSourceEndpoint,
		Reference: urlguard.LogRaw(raw),
		Content:   content,
		Truncated: truncated,
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out.Note = fmt.Sprintf("the log endpoint answered HTTP %d — the body below is whatever it returned", resp.StatusCode)
	}
	return out, nil
}

// defaultLogChars is what a log fetch returns when the caller names no cap. It
// sits below tools.max_tool_output_chars so the agent loop never cuts the
// middle out of a report this package already truncated deliberately.
const defaultLogChars = 6000

// SummarizeLog reduces a raw log to the part that explains a failure: the lines
// that look like errors, followed by the tail.
//
// Not a raw dump and not a blind tail. A raw dump costs the run's context for
// nothing; a blind tail is usually right but loses the stack trace when the job
// prints a cleanup summary after it. So the error-looking lines are lifted out
// first (capped, in order, deduped), and the tail follows them — which means
// the two things a human would scroll to are the two things the model gets.
func SummarizeLog(raw string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		maxChars = defaultLogChars
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if len(raw) <= maxChars {
		return strings.TrimSpace(raw), false
	}
	lines := strings.Split(raw, "\n")

	seen := map[string]bool{}
	var errorLines []string
	for _, line := range lines {
		if !looksLikeError(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		errorLines = append(errorLines, trimmed)
		if len(errorLines) >= 40 {
			break
		}
	}

	var head string
	if len(errorLines) > 0 {
		head = "Error lines found in this log:\n" + strings.Join(errorLines, "\n") + "\n\n"
		if len(head) > maxChars/2 {
			head = head[:maxChars/2] + "\n…\n\n"
		}
	}
	budget := maxChars - len(head)
	if budget < 0 {
		budget = 0
	}
	tail := raw
	if len(tail) > budget {
		tail = tail[len(tail)-budget:]
		// Never start mid-line: a truncated first line reads as corrupted
		// output and the model reports the log as unreadable.
		if idx := strings.IndexByte(tail, '\n'); idx >= 0 && idx < len(tail)-1 {
			tail = tail[idx+1:]
		}
	}
	return strings.TrimSpace(head + "…(earlier output truncated)\n" + tail), true
}

// errorMarkers are the substrings that make a log line worth lifting out. Case
// is folded before matching.
var errorMarkers = []string{
	"error", "failed", "failure", "fatal", "panic", "exception",
	"##[error]", "exit code 1", "exit status 1", "cannot ", "not found",
	"denied", "timed out", "refused",
}

func looksLikeError(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range errorMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
