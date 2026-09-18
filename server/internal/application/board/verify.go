package board

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/toolchain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
	"github.com/rs/zerolog/log"
)

// defaultStageTimeout bounds one verification command. Stages that need longer
// (dependency installs) carry their own Timeout.
const defaultStageTimeout = 5 * time.Minute

func hasNPMScript(dir, script string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	_, ok := pkg.Scripts[script]
	return ok
}

// verifyAndFix runs build/vet checks in the task workspace after the agent
// finishes. On failure the error output is fed back to the agent for up to
// verifyFixAttempts fix rounds; a persistent failure pushes the task back to
// in_progress with an explanatory comment.
//
// The second return value is the verdict of that last round, and the hand-off
// depends on it. Without it the two halves contradicted each other: a run whose
// checks were still red after every fix round was pushed back to in_progress
// here, and advanceToCodeReview — which re-reads the task and finds it in the
// column the run started in — moved it straight to code_review anyway. The
// reviewer got a red build for a task the system had just judged unfinished.
// A round that could not be evaluated (a fix round whose agent loop errored)
// counts as not verified: the last thing anything measured was a failure.
//
// The third return value is a usage-limit block from a fix round: neither
// true nor false describes it, since nothing was actually judged, so the
// caller must check it before reading the bool at all — a fix round the CLI
// never finished must park the task, not fail it or report a stale verdict.
func (r *Runner) verifyAndFix(
	ctx context.Context,
	job RunJob,
	agentRec domain.Agent,
	history []domain.Message,
	resp domain.AgentResponse,
	model string,
	policy domain.ToolPolicy,
	workspace string,
) (domain.AgentResponse, bool, *domain.QuotaBlock) {
	attempts := r.verifyFixAttempts
	if attempts <= 0 {
		attempts = 2
	}
	var repo domain.Repository
	if r.projects != nil {
		fetched, err := r.projects.ResolveRepository(ctx, job.RepositoryID)
		if err != nil {
			log.Warn().Err(err).Str("repository_id", job.RepositoryID.String()).Msg("resolve repository for verification failed; using defaults")
		} else {
			repo = fetched
		}
	}
	rec := activity.FromContext(ctx)
	for attempt := 0; ; attempt++ {
		// The build gate runs after the agent has stopped talking, and it is
		// minutes of silence: no tool call, no message, while the run row still
		// says running. The activity panel had nothing newer to show than the
		// agent's last tool call, so a finished plan sat under a frozen
		// "run_terminal running…" and looked stuck.
		if rec != nil {
			rec.Step("build_verification_start", map[string]any{"attempt": attempt + 1})
		}
		ok, failReport := runVerification(ctx, workspace, repo)
		// Coverage runs only once the code compiles: instrumenting a build that
		// does not build reports 0% and would send the agent to write tests for
		// a package the compiler has not accepted yet.
		//
		// It reports; it does not gate. A thin suite is worth saying out loud —
		// the figure travels with the hand-off, so review and QA read it — but
		// holding the task in its column over a percentage stops the work
		// instead of raising the coverage, and the lines that are missing are
		// usually in code the task never touched. Mutation score rides along on
		// the same run and now runs whatever coverage came back: it is the same
		// kind of note, and it was only conditional because coverage blocked.
		// Scoped to the repository ("") because that is what this run verifies:
		// the workspace is the whole checkout and the stages run at its root.
		// The gates themselves resolve per sub-project (see
		// domain.Repository.EffectiveCoverageGate) for the day a run is scoped
		// to one.
		if ok {
			for _, note := range []string{coverageReport(ctx, workspace, repo, ""), runMutation(ctx, workspace, repo, "")} {
				if note != "" {
					failReport = strings.TrimSpace(failReport + "\n" + note)
				}
			}
		}
		if ok {
			if attempt > 0 {
				log.Info().Str("task_id", job.Task.ID.String()).Int("fix_rounds", attempt).Msg("verification passed after fixes")
			}
			if rec != nil {
				rec.Step("build_verification_passed", map[string]any{
					"attempt": attempt + 1, "unverified": failReport,
				})
			}
			// Nothing failed, but something could not be checked. The note is
			// pushed into the run's own summary because that summary is what
			// review and QA read: a mobile change whose analyzer was never
			// installed would otherwise reach them indistinguishable from one
			// that passed every check.
			// Two different notes travel this path: what could not be checked,
			// and what was measured. They need different words — telling an
			// agent that a 92% coverage report means "not everything could be
			// checked" is how a passing gate gets relayed as a caveat.
			if failReport != "" {
				note := "\n\n[verification] " + failReport
				if strings.Contains(failReport, "[unverified]") {
					note += "\nSay in your hand-off what was not checked instead of reporting a clean build."
				}
				// A shortfall the gate no longer acts on still has to be read
				// as a warning rather than a failure, or the agent starts a fix
				// round of its own for a hand-off nothing is holding.
				if strings.Contains(failReport, coverageWarningMarker) {
					note += "\nThe coverage warning does not hold this hand-off: report the figure in your summary and continue."
				}
				resp.Message.Content = strings.TrimSpace(resp.Message.Content + note)
			}
			return resp, true, nil
		}
		if rec != nil {
			rec.Step("build_verification_failed", map[string]any{
				"attempt": attempt + 1, "report": truncateTail(failReport, 2000),
			})
		}
		if attempt >= attempts {
			r.reportVerificationFailure(ctx, job, failReport)
			resp.Message.Content = strings.TrimSpace(resp.Message.Content +
				"\n\n[verification] Build/vet checks still failing after " + fmt.Sprint(attempts) + " fix attempts; task moved back to in_progress.")
			return resp, false, nil
		}
		history = append(history, domain.Message{Role: domain.RoleAssistant, Content: resp.Message.Content})
		// The fix round runs on the PRE-RUN history: everything the finished run
		// learned — which files matter, which command failed and how — died with
		// the loop, so the agent that had just spent thirty turns in this
		// codebase started the fix blind and explored it again. The run's own
		// activity trace is the only surviving record of that work, so it is
		// folded into one bounded message here.
		//
		// The summary is left out of the digest on purpose: the assistant turn
		// directly above carries it in full, and repeating it would spend the
		// digest's budget on text the model can already read.
		history = withFindingsDigest(history, agent.DigestFromSteps(rec.Steps(ctx), "", 0))
		history = append(history,
			domain.Message{Role: domain.RoleUser, Content: "Automated verification failed in the task workspace. Fix these errors, then re-check your work. " +
				"Do not post an add_task_comment about the fix or the task being done — the system publishes your closing summary to the card once these checks pass:\n\n" + failReport},
		)
		// Same reasoning as the main run's call in runner.go: the fix round's
		// own utility calls stay on the agent's plain model, whatever model
		// this fix round itself is running on.
		//
		// r.agentLoop is the router, so an agent on a host-executed provider
		// gets its fix round on that host's CLI — in the same tt-<key>
		// workspace, which is already on the context. Before that it could not
		// get one at all: the red gate bounced the task back to in_progress
		// without a single fix attempt having been made.
		fixed, err := r.agentLoop.RunTask(ctx, history, model, agentRec.ProviderType, policy,
			agent.WithLightModel(agentRec.Model),
			agent.WithCLILabel(job.Task.Key+" verify-fix", job.Task.Title))
		if err != nil {
			if quotaErr, ok := domain.QuotaBlockOf(err); ok {
				return resp, false, quotaErr
			}
			r.reportVerificationFailure(ctx, job, failReport)
			return resp, false, nil
		}
		resp = fixed
	}
}

// withFindingsDigest carries what the run has already done into the next fix
// round, replacing the digest an earlier round appended.
//
// Replacing rather than appending matters at the second fix round: the first
// round's digest describes a state of the workspace that the first fix has
// already changed, and two digests that disagree are worse than none. The fresh
// one covers the whole run anyway — it is rendered from the same trace, which by
// then also holds the first fix round's calls.
func withFindingsDigest(history []domain.Message, digest string) []domain.Message {
	if digest == "" {
		return history
	}
	out := make([]domain.Message, 0, len(history)+1)
	for _, m := range history {
		if m.Role == domain.RoleSystem && agent.IsFindingsDigest(m.Content) {
			continue
		}
		out = append(out, m)
	}
	return append(out, domain.Message{Role: domain.RoleSystem, Content: digest})
}

// reportPlanVerificationFailure puts the orchestration verifier's rejection on
// the card, in its own words.
//
// It does not move the task. The build gate's failure does, because a build that
// does not build is not a state anyone can review; this verdict is about whether
// the work met the goal, and the run is already staying put — the hand-off is
// refused by the same flag that brought us here. Moving as well would fight
// whatever column the agent legitimately left the task in.
func (r *Runner) reportPlanVerificationFailure(ctx context.Context, job RunJob, verdict domain.VerificationResult) {
	if r.taskUpdater == nil {
		return
	}
	var sb strings.Builder
	sb.WriteString("Otomatik doğrulama başarısız: bu run'ın sonucu hedefi karşılamıyor, bu yüzden görev code_review'a devredilmedi.\n")
	if summary := strings.TrimSpace(verdict.Summary); summary != "" {
		sb.WriteString("\n" + summary + "\n")
	}
	if len(verdict.Issues) > 0 {
		sb.WriteString("\nAçık bulgular:\n")
		for i, issue := range verdict.Issues {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(issue)))
		}
	}
	sb.WriteString("\nBir sonraki run bu maddeleri kapatmalı; kapanmadan görev ilerlemez.")
	if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    truncateTail(sb.String(), 3000),
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("plan verification failure comment failed")
	}
}

func (r *Runner) reportVerificationFailure(ctx context.Context, job RunJob, failReport string) {
	if r.taskUpdater == nil {
		return
	}
	if len(failReport) > 3000 {
		failReport = truncateHead(failReport, 3000) + "\n…(truncated)"
	}
	if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    "Automated verification failed — build/vet errors:\n\n```\n" + failReport + "\n```",
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("verification failure comment failed")
	}
	col := domain.TaskColumnInProgress
	if _, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &col,
		SystemReason: domain.MoveReasonVerificationFailed,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("verification failure task move failed")
	}
}

// runVerification executes the resolved verify stages in the workspace; returns
// ok and a combined failure report (tail-truncated) when something breaks.
func runVerification(ctx context.Context, dir string, repo domain.Repository) (bool, string) {
	stages := ResolveVerifyStages(dir, repo)
	if len(stages) == 0 {
		return true, ""
	}
	// The verify gate must judge the agent's work with the toolchain the repo
	// declares, not whatever the host PATH resolves — otherwise the fix loop
	// trains the agent against the wrong compiler.
	overlay := toolchain.Default.Overlay(dir)
	var failures []string
	// Checks the environment cannot run at all, kept apart from checks that
	// ran and said no.
	//
	// A missing binary is not a broken diff. Treating "flutter: executable file
	// not found" as a build failure bounced the task, spent a fix round on a
	// compiler error that did not exist, and taught the agent its correct code
	// was wrong. Reporting it as unverified is the honest outcome: the gate has
	// nothing to say, and the run is told so rather than being failed or,
	// worse, passed silently.
	var unverified []string
	for _, stage := range stages {
		args := stage.Command
		timeout := stage.Timeout
		if timeout <= 0 {
			timeout = defaultStageTimeout
		}
		cmdCtx, cancel := context.WithTimeout(ctx, timeout)
		cmd := exec.CommandContext(cmdCtx, args[0], args[1:]...)
		cmd.Dir = dir
		// A verify stage is a command the repository under test declares for
		// itself — its own `npm test` script, its own Makefile target. Handing it
		// os.Environ() gave any repo the pod's DATABASE_URL, INTERNAL_AUTH_KEY and
		// MCP_SECRETS_KEY without it ever having to touch run_terminal, so the
		// scrub that closed the terminal door has to close this one too. This is
		// set unconditionally: an empty overlay used to leave cmd.Env nil, and
		// exec reads nil as "inherit the parent's environment".
		cmd.Env = verifyEnv(os.Environ(), overlay.Env)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		cancel()
		if err == nil {
			continue
		}
		if errors.Is(err, exec.ErrNotFound) {
			unverified = append(unverified, fmt.Sprintf("%s (%s is not installed in this environment)",
				stage.Name, args[0]))
			log.Warn().Str("stage", stage.Name).Str("tool", args[0]).
				Msg("verify stage skipped: the tool is not installed")
			continue
		}
		out := buf.String()
		if len(out) > 6000 {
			out = truncateTail(out, 6000)
		}
		// A workspace-preparation stage (dependency install) that fails says
		// nothing about the agent's diff, and the checks after it would only
		// fail for the same reason. Abandon the verdict instead of inventing
		// one — a false "build broken" bounce costs the task a whole round.
		if stage.Setup {
			log.Warn().Str("stage", stage.Name).Str("workspace", dir).
				Msg("verification setup stage failed; skipping the checks that depend on it")
			return true, ""
		}
		failures = append(failures, fmt.Sprintf("$ %s\n%s", strings.Join(args, " "), strings.TrimSpace(out)))
	}
	if len(failures) == 0 {
		if len(unverified) > 0 {
			// Reported through the ok=true path: nothing failed, so the run is
			// not bounced, but the note travels with it so the agent states
			// what was not checked instead of claiming a green build.
			return true, "[unverified] " + strings.Join(unverified, "; ")
		}
		return true, ""
	}
	if len(unverified) > 0 {
		failures = append(failures, "[unverified] "+strings.Join(unverified, "; "))
	}
	// A version mismatch the resolver could not repair is the most likely
	// explanation for an otherwise surprising failure — tell the agent.
	if len(overlay.Warnings) > 0 {
		failures = append(failures, "[toolchain] "+strings.Join(overlay.Warnings, "\n[toolchain] "))
	}
	return false, strings.Join(failures, "\n\n")
}

// dotnetQuietEnv keeps the dotnet CLI from printing its first-run banner and
// telemetry notice into stage output an agent has to read, and from phoning
// home from a verify run nobody started by hand.
var dotnetQuietEnv = []string{"DOTNET_NOLOGO=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1"}

// verifyEnv is the environment a verify stage runs with: the scrubbed parent,
// the repository's toolchain overlay, and npm_config_yes=false. The last one
// stops `npx` from installing a package the project does not have, so a missing
// local binary fails with npm's own "missing packages" error instead of running
// whatever the registry serves under that name.
func verifyEnv(parent, overlay []string) []string {
	extra := make([]string, 0, len(overlay)+1+len(dotnetQuietEnv))
	extra = append(extra, overlay...)
	extra = append(extra, "npm_config_yes=false")
	extra = append(extra, dotnetQuietEnv...)
	return childenv.For(parent, extra)
}
