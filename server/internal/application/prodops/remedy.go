package prodops

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

// deployCorrelationWindow is how long after a deploy a new incident is still
// blamed on that deploy. Long enough to cover a slow rollout, short enough that
// an unrelated outage hours later is not attributed to it.
const deployCorrelationWindow = 45 * time.Minute

// DeployRecord is one finished deploy of the affected repository, flattened
// from the pipeline history so the engine does not depend on pipeline types.
type DeployRecord struct {
	Env        string
	Status     domain.PipelineStatus
	FinishedAt time.Time
	TaskKey    string
}

// RemedyContext is everything the suggestion engine reasons over. It is
// assembled by the service; the engine itself is pure so its verdicts are
// reproducible and testable.
type RemedyContext struct {
	Incident domain.Incident
	Target   domain.DeployTarget
	Rollback string // provider rollback hint from the deploy template
	Deploys  []DeployRecord
	History  []domain.Incident
}

// Suggest produces the incident's remedy: what most likely broke, and the
// concrete steps to take. It always returns something actionable — an unknown
// cause yields a diagnostic checklist rather than silence, because "I don't
// know how to fix this" is the failure mode this engine exists to remove.
func Suggest(c RemedyContext) domain.Remedy {
	if r, ok := rollbackRemedy(c); ok {
		return r
	}
	if r, ok := repeatRemedy(c); ok {
		return r
	}
	return signatureRemedy(c)
}

// rollbackRemedy fires when a deploy to the same environment finished shortly
// before the incident: the newest change is the first suspect, and undoing it
// is the fastest way back to a working production.
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

	gap := onset.Sub(suspect.FinishedAt).Round(time.Minute)
	confidence := 70
	summary := fmt.Sprintf("A deploy to %s finished %s before this incident started — treat it as the cause and roll back first, diagnose after.",
		c.Incident.Env, humanDuration(gap))
	if suspect.Status == domain.PipelineStatusFailed {
		confidence = 85
		summary = fmt.Sprintf("The %s deploy %s before this incident FAILED — production is likely running a half-applied release. Roll back to the last good revision.",
			c.Incident.Env, humanDuration(gap))
	}
	steps := []string{}
	if c.Rollback != "" {
		steps = append(steps, "Roll back: "+c.Rollback)
	} else {
		steps = append(steps, "Roll back the last release of this environment (redeploy the previously running revision/image).")
	}
	steps = append(steps,
		"Confirm recovery against the environment health URL before touching code.",
		"Then diff the released commits and reproduce the failure locally or in stage.")
	if !c.Target.AutoRollback {
		steps = append(steps, "Auto-rollback is off for this target — the rollback has to be dispatched by hand.")
	}
	evidence := []string{fmt.Sprintf("deploy status=%s, finished %s before onset", suspect.Status, humanDuration(gap))}
	if suspect.TaskKey != "" {
		evidence = append(evidence, "released task: "+suspect.TaskKey)
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

// repeatRemedy reuses what actually fixed the same fingerprint last time. A
// recurring incident is the cheapest possible diagnosis — as long as the repeat
// is surfaced instead of being rediscovered from scratch.
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
			Summary: fmt.Sprintf("This exact failure was seen and resolved before (%s). Apply the fix that worked, then decide whether it needs to be made permanent.", when),
			Steps: []string{
				"Previous fix:\n" + strings.TrimSpace(past.Remedy),
				"If this is the third time or more, the recurrence itself is the bug — open a follow-up task for the permanent fix.",
			},
			Confidence: 65,
			Evidence: []string{
				fmt.Sprintf("same fingerprint resolved %s (%d occurrences then)", when, past.Occurrences),
			},
		}, true
	}
	return domain.Remedy{}, false
}

// signatureClass is one keyword → hypothesis rule.
type signatureClass struct {
	kind     string
	keywords []string
	summary  string
	steps    []string
}

// signatures are checked in order; the first match wins, so the more specific
// classes come first.
var signatures = []signatureClass{
	{
		kind:     domain.RemedyKindConfig,
		keywords: []string{"permission denied", "unauthorized", "forbidden", "401", "403", "invalid credential", "missing env", "secret", "no such file or directory: /etc", "config", "certificate", "x509"},
		summary:  "The signature points at configuration or credentials, not at code: something the environment provides is missing, expired or wrong.",
		steps: []string{
			"Compare the failing environment's env vars/secrets against a working environment.",
			"Check for a recently rotated key, expired certificate or renamed config entry.",
			"Fix the configuration first; only change code if the config is provably correct.",
		},
	},
	{
		kind:     domain.RemedyKindDependency,
		keywords: []string{"connection refused", "connection reset", "dial tcp", "no such host", "timeout", "timed out", "upstream", "502", "503", "504", "unreachable", "database is not available", "too many connections", "deadlock"},
		summary:  "The signature points at a dependency: a downstream service, database or network path is not answering.",
		steps: []string{
			"Check the dependency's own health/status before touching this service.",
			"Verify connection limits, pool exhaustion and network policy/firewall changes.",
			"If the dependency is healthy, look for a client-side change: timeouts, retries, connection pooling.",
		},
	},
	{
		kind:     domain.RemedyKindCapacity,
		keywords: []string{"oom", "out of memory", "memory limit", "cpu throttl", "429", "rate limit", "quota", "disk full", "no space left", "evicted", "backoff", "crashloop", "scale"},
		summary:  "The signature points at capacity: the workload is being starved, throttled or evicted rather than failing logically.",
		steps: []string{
			"Check memory/CPU limits and the recent request rate against them.",
			"Scale (replicas or limits) to stop the bleeding, then find what changed the resource profile.",
			"If it is a rate limit or quota, confirm whether traffic grew or a retry loop is amplifying it.",
		},
	},
	{
		kind:     domain.RemedyKindCodeFix,
		keywords: []string{"panic", "nil pointer", "segmentation fault", "unhandled exception", "stack trace", "nullpointerexception", "index out of range", "500", "internal server error"},
		summary:  "The signature points at a code defect reaching production traffic.",
		steps: []string{
			"Reproduce with a failing test that matches the stack trace before changing anything.",
			"Fix the root cause (not the symptom) and keep the reproducing test as a regression guard.",
			"Ship through the normal pipeline — a hotfix that skips tests is how the next incident starts.",
		},
	},
}

// signatureRemedy classifies by error signature when there is no deploy to
// blame and no history to copy.
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
				Summary:    sig.summary,
				Steps:      sig.steps,
				Confidence: confidence,
				Evidence:   []string{fmt.Sprintf("matched signature %q in the alert text", kw)},
			}
		}
	}
	steps := []string{
		"Read the last 15 minutes of logs for this service around the first occurrence.",
		"Check whether anything was deployed, scaled or reconfigured today.",
		"Compare the failing environment against the last environment where it worked.",
	}
	if c.Target.HealthURL != "" {
		// Redacted: this text is handed to an agent, and a health URL with a
		// token in its query string would otherwise land in model context.
		steps = append(steps, "Probe the health endpoint directly: "+urlguard.LogRaw(c.Target.HealthURL))
	}
	return domain.Remedy{
		Kind:       domain.RemedyKindUnknown,
		Summary:    "No known signature matched — this needs diagnosis before a fix can be proposed.",
		Steps:      steps,
		Confidence: 25,
		Evidence:   []string{fmt.Sprintf("%d occurrence(s), severity %s, source %s", c.Incident.Occurrences, c.Incident.Severity, c.Incident.Source)},
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
