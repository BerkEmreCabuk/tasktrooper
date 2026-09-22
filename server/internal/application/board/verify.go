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

// The second return is the last round's verdict and the hand-off depends on it.
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
		if rec != nil {
			rec.Step("build_verification_start", map[string]any{"attempt": attempt + 1})
		}
		ok, failReport := runVerification(ctx, workspace, repo)
		// Coverage is measured only once the code compiles, and it reports instead of gating.
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
			if failReport != "" {
				note := "\n\n[verification] " + failReport
				if strings.Contains(failReport, "[unverified]") {
					note += "\nSay in your hand-off what was not checked instead of reporting a clean build."
				}
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
		history = withFindingsDigest(history, agent.DigestFromSteps(rec.Steps(ctx), "", 0))
		history = append(history,
			domain.Message{Role: domain.RoleUser, Content: "Automated verification failed in the task workspace. Fix these errors, then re-check your work. " +
				"Do not post an add_task_comment about the fix or the task being done — the system publishes your closing summary to the card once these checks pass:\n\n" + failReport},
		)
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

// Verdict only: the build gate's failure is what moves the task, not this.
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
	col, ok := r.workflowFor(ctx, job.Task.TaskType).WorkColumn()
	if !ok {
		col = domain.TaskColumnInProgress
	}
	if _, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &col,
		SystemReason: domain.MoveReasonVerificationFailed,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("verification failure task move failed")
	}
}

func runVerification(ctx context.Context, dir string, repo domain.Repository) (bool, string) {
	stages := ResolveVerifyStages(dir, repo)
	if len(stages) == 0 {
		return true, ""
	}
	// Judge with the repo's declared toolchain, not the host PATH: otherwise the fix loop trains against the wrong compiler.
	overlay := toolchain.Default.Overlay(dir)
	var failures []string
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
		// The overlay is set unconditionally: empty used to leave cmd.Env nil, and exec reads nil as "inherit the parent".
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
		if stage.Setup {
			log.Warn().Str("stage", stage.Name).Str("workspace", dir).
				Msg("verification setup stage failed; skipping the checks that depend on it")
			return true, ""
		}
		failures = append(failures, fmt.Sprintf("$ %s\n%s", strings.Join(args, " "), strings.TrimSpace(out)))
	}
	if len(failures) == 0 {
		if len(unverified) > 0 {
			return true, "[unverified] " + strings.Join(unverified, "; ")
		}
		return true, ""
	}
	if len(unverified) > 0 {
		failures = append(failures, "[unverified] "+strings.Join(unverified, "; "))
	}
	if len(overlay.Warnings) > 0 {
		failures = append(failures, "[toolchain] "+strings.Join(overlay.Warnings, "\n[toolchain] "))
	}
	return false, strings.Join(failures, "\n\n")
}

func verifyEnv(parent, overlay []string) []string {
	extra := make([]string, 0, len(overlay)+1)
	extra = append(extra, overlay...)
	extra = append(extra, "npm_config_yes=false")
	return childenv.For(parent, extra)
}
