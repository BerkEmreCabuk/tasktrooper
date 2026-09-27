package prodops

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

const deployCorrelationWindow = 45 * time.Minute

type DeployRecord struct {
	Env        string
	Status     domain.PipelineStatus
	FinishedAt time.Time
	TaskKey    string
}

type RemedyContext struct {
	Incident domain.Incident
	Target   domain.DeployTarget
	Rollback string
	Deploys  []DeployRecord
	History  []domain.Incident
}

func Suggest(c RemedyContext) domain.Remedy {
	if r, ok := rollbackRemedy(c); ok {
		return r
	}
	if r, ok := repeatRemedy(c); ok {
		return r
	}
	return signatureRemedy(c)
}

func rollbackRemedy(c RemedyContext) (domain.Remedy, bool) {
	onset := c.Incident.FirstSeenAt
	if onset.IsZero() {
		onset = c.Incident.LastSeenAt
	}
	var suspect *DeployRecord
	for i := range c.Deploys {
		d := c.Deploys[i]
		if d.Env != "" && c.Incident.Env != "" && d.Env != c.Incident.Env {
			continue
		}
		if d.FinishedAt.IsZero() || d.FinishedAt.After(onset) {
			continue
		}
		if onset.Sub(d.FinishedAt) > deployCorrelationWindow {
			continue
		}
		if suspect == nil || d.FinishedAt.After(suspect.FinishedAt) {
			suspect = &c.Deploys[i]
		}
	}
	if suspect == nil {
		return domain.Remedy{}, false
	}

	gap := humanDuration(onset.Sub(suspect.FinishedAt).Round(time.Minute))
	confidence := 70
	summary := rollbackSummaryRecent.Render(envGapInput{Env: c.Incident.Env, Gap: gap})
	if suspect.Status == domain.PipelineStatusFailed {
		confidence = 85
		summary = rollbackSummaryFailed.Render(envGapInput{Env: c.Incident.Env, Gap: gap})
	}
	steps := []string{}
	if c.Rollback != "" {
		steps = append(steps, rollbackStepHint.Render(hintInput{Hint: c.Rollback}))
	} else {
		steps = append(steps, prompt.Text(rollbackStepGeneric))
	}
	steps = append(steps,
		prompt.Text(rollbackStepConfirm),
		prompt.Text(rollbackStepDiff))
	if !c.Target.AutoRollback {
		steps = append(steps, prompt.Text(rollbackStepAutoOff))
	}
	evidence := []string{rollbackEvidenceDeploy.Render(statusGapInput{Status: string(suspect.Status), Gap: gap})}
	if suspect.TaskKey != "" {
		evidence = append(evidence, rollbackEvidenceTask.Render(taskKeyInput{TaskKey: suspect.TaskKey}))
	}
	return domain.Remedy{
		Kind:       domain.RemedyKindRollback,
		Summary:    summary,
		Steps:      steps,
		Confidence: confidence,
		Rollback:   true,
		Evidence:   evidence,
	}, true
}

func repeatRemedy(c RemedyContext) (domain.Remedy, bool) {
	for _, past := range c.History {
		if (past.ID != uuid.Nil && past.ID == c.Incident.ID) || strings.TrimSpace(past.Remedy) == "" {
			continue
		}
		when := "previously"
		if past.ResolvedAt != nil {
			when = past.ResolvedAt.Format("2006-01-02")
		}
		kind := past.RemedyKind
		if kind == "" {
			kind = domain.RemedyKindUnknown
		}
		return domain.Remedy{
			Kind:    kind,
			Summary: repeatSummary.Render(whenInput{When: when}),
			Steps: []string{
				repeatStepFix.Render(fixInput{Fix: strings.TrimSpace(past.Remedy)}),
				prompt.Text(repeatStepFollowup),
			},
			Confidence: 65,
			Evidence: []string{
				repeatEvidence.Render(whenOccurrencesInput{When: when, Occurrences: past.Occurrences}),
			},
		}, true
	}
	return domain.Remedy{}, false
}

type signatureClass struct {
	kind       string
	keywords   []string
	summaryKey prompt.Key[struct{}]
	stepKeys   []prompt.Key[struct{}]
}

var signatures = []signatureClass{
	{
		kind:       domain.RemedyKindConfig,
		keywords:   []string{"permission denied", "unauthorized", "forbidden", "401", "403", "invalid credential", "missing env", "secret", "no such file or directory: /etc", "config", "certificate", "x509"},
		summaryKey: signatureConfigSummary,
		stepKeys:   []prompt.Key[struct{}]{signatureConfigStep1, signatureConfigStep2, signatureConfigStep3},
	},
	{
		kind:       domain.RemedyKindDependency,
		keywords:   []string{"connection refused", "connection reset", "dial tcp", "no such host", "timeout", "timed out", "upstream", "502", "503", "504", "unreachable", "database is not available", "too many connections", "deadlock"},
		summaryKey: signatureDependencySummary,
		stepKeys:   []prompt.Key[struct{}]{signatureDependencyStep1, signatureDependencyStep2, signatureDependencyStep3},
	},
	{
		kind:       domain.RemedyKindCapacity,
		keywords:   []string{"oom", "out of memory", "memory limit", "cpu throttl", "429", "rate limit", "quota", "disk full", "no space left", "evicted", "backoff", "crashloop", "scale"},
		summaryKey: signatureCapacitySummary,
		stepKeys:   []prompt.Key[struct{}]{signatureCapacityStep1, signatureCapacityStep2, signatureCapacityStep3},
	},
	{
		kind:       domain.RemedyKindCodeFix,
		keywords:   []string{"panic", "nil pointer", "segmentation fault", "unhandled exception", "stack trace", "nullpointerexception", "index out of range", "500", "internal server error"},
		summaryKey: signatureCodeFixSummary,
		stepKeys:   []prompt.Key[struct{}]{signatureCodeFixStep1, signatureCodeFixStep2, signatureCodeFixStep3},
	},
}

func renderTexts(keys []prompt.Key[struct{}]) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = prompt.Text(k)
	}
	return out
}

func signatureRemedy(c RemedyContext) domain.Remedy {
	haystack := strings.ToLower(c.Incident.Title + "\n" + c.Incident.Detail)
	for _, sig := range signatures {
		for _, kw := range sig.keywords {
			if !strings.Contains(haystack, kw) {
				continue
			}
			confidence := 55
			if c.Incident.Occurrences > 3 {
				confidence = 60
			}
			return domain.Remedy{
				Kind:       sig.kind,
				Summary:    prompt.Text(sig.summaryKey),
				Steps:      renderTexts(sig.stepKeys),
				Confidence: confidence,
				Evidence:   []string{signatureEvidence.Render(keywordInput{Keyword: kw})},
			}
		}
	}
	steps := []string{
		prompt.Text(fallbackStep1),
		prompt.Text(fallbackStep2),
		prompt.Text(fallbackStep3),
	}
	if c.Target.HealthURL != "" {

		steps = append(steps, fallbackStepProbe.Render(urlInput{URL: urlguard.LogRaw(c.Target.HealthURL)}))
	}
	return domain.Remedy{
		Kind:       domain.RemedyKindUnknown,
		Summary:    prompt.Text(fallbackSummary),
		Steps:      steps,
		Confidence: 25,
		Evidence:   []string{fallbackEvidence.Render(occurrencesSeveritySourceInput{Occurrences: c.Incident.Occurrences, Severity: string(c.Incident.Severity), Source: string(c.Incident.Source)})},
	}
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return "less than a minute"
	}
	if d < time.Hour {
		return fmt.Sprintf("%d min", int(d.Minutes()))
	}
	return fmt.Sprintf("%.1f h", d.Hours())
}
