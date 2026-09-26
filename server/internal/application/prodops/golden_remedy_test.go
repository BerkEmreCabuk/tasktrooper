package prodops_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGoldenSuggestRemedy pins Suggest's exact Summary/Steps/Evidence
// wording, byte-for-byte, before that prose moves into
// catalog/system/prompts/briefs/**.
func TestGoldenSuggestRemedy(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

	t.Run("recent successful deploy is blamed", func(t *testing.T) {
		r := prodops.Suggest(prodops.RemedyContext{
			Incident: domain.Incident{Env: domain.DeployEnvProd, Title: "prod health check failing", Detail: "HTTP 503", FirstSeenAt: now, LastSeenAt: now},
			Target:   domain.DeployTarget{AutoRollback: true},
			Rollback: "gcloud run services update-traffic api --to-revisions PREV=100",
			Deploys: []prodops.DeployRecord{
				{Env: domain.DeployEnvProd, Status: domain.PipelineStatusSuccess, FinishedAt: now.Add(-10 * time.Minute), TaskKey: "TT-42"},
			},
		})
		require.Equal(t, domain.RemedyKindRollback, r.Kind)
		require.Equal(t, 70, r.Confidence)
		require.Equal(t, "A deploy to prod finished 10 min before this incident started — treat it as the cause and roll back first, diagnose after.", r.Summary)
		require.Equal(t, []string{
			"Roll back: gcloud run services update-traffic api --to-revisions PREV=100",
			"Confirm recovery against the environment health URL before touching code.",
			"Then diff the released commits and reproduce the failure locally or in stage.",
		}, r.Steps)
		require.Equal(t, []string{"deploy status=success, finished 10 min before onset", "released task: TT-42"}, r.Evidence)
	})

	t.Run("failed deploy raises confidence and adds the auto-rollback-off step", func(t *testing.T) {
		r := prodops.Suggest(prodops.RemedyContext{
			Incident: domain.Incident{Env: domain.DeployEnvProd, Title: "prod health check failing", FirstSeenAt: now, LastSeenAt: now},
			Target:   domain.DeployTarget{AutoRollback: false},
			Deploys: []prodops.DeployRecord{
				{Env: domain.DeployEnvProd, Status: domain.PipelineStatusFailed, FinishedAt: now.Add(-2 * time.Minute)},
			},
		})
		require.Equal(t, domain.RemedyKindRollback, r.Kind)
		require.Equal(t, 85, r.Confidence)
		require.Equal(t, "The prod deploy 2 min before this incident FAILED — production is likely running a half-applied release. Roll back to the last good revision.", r.Summary)
		require.Equal(t, []string{
			"Roll back the last release of this environment (redeploy the previously running revision/image).",
			"Confirm recovery against the environment health URL before touching code.",
			"Then diff the released commits and reproduce the failure locally or in stage.",
			"Auto-rollback is off for this target — the rollback has to be dispatched by hand.",
		}, r.Steps)
	})

	t.Run("recurrence reuses the prior fix", func(t *testing.T) {
		resolved := now.Add(-48 * time.Hour)
		r := prodops.Suggest(prodops.RemedyContext{
			Incident: domain.Incident{Title: "queue backlog", FirstSeenAt: now, LastSeenAt: now},
			History: []domain.Incident{
				{Remedy: "Scaled the consumer to 4 replicas.", RemedyKind: domain.RemedyKindCapacity, ResolvedAt: &resolved, Occurrences: 3},
			},
		})
		require.Equal(t, domain.RemedyKindCapacity, r.Kind)
		require.Equal(t, 65, r.Confidence)
		require.Equal(t, "This exact failure was seen and resolved before (2026-07-25). Apply the fix that worked, then decide whether it needs to be made permanent.", r.Summary)
		require.Equal(t, []string{
			"Previous fix:\nScaled the consumer to 4 replicas.",
			"If this is the third time or more, the recurrence itself is the bug — open a follow-up task for the permanent fix.",
		}, r.Steps)
		require.Equal(t, []string{"same fingerprint resolved 2026-07-25 (3 occurrences then)"}, r.Evidence)
	})

	signatureCases := []struct {
		name        string
		title       string
		detail      string
		wantKind    string
		wantSummary string
		wantSteps   []string
	}{
		{
			name:        "config signature",
			title:       "deploy failing",
			detail:      "permission denied writing to /var/run",
			wantKind:    domain.RemedyKindConfig,
			wantSummary: "The signature points at configuration or credentials, not at code: something the environment provides is missing, expired or wrong.",
			wantSteps: []string{
				"Compare the failing environment's env vars/secrets against a working environment.",
				"Check for a recently rotated key, expired certificate or renamed config entry.",
				"Fix the configuration first; only change code if the config is provably correct.",
			},
		},
		{
			name:        "dependency signature",
			title:       "api errors",
			detail:      "dial tcp 10.0.0.1:5432: connection refused",
			wantKind:    domain.RemedyKindDependency,
			wantSummary: "The signature points at a dependency: a downstream service, database or network path is not answering.",
			wantSteps: []string{
				"Check the dependency's own health/status before touching this service.",
				"Verify connection limits, pool exhaustion and network policy/firewall changes.",
				"If the dependency is healthy, look for a client-side change: timeouts, retries, connection pooling.",
			},
		},
		{
			name:        "capacity signature",
			title:       "pods restarting",
			detail:      "OOMKilled, crashloop backoff",
			wantKind:    domain.RemedyKindCapacity,
			wantSummary: "The signature points at capacity: the workload is being starved, throttled or evicted rather than failing logically.",
			wantSteps: []string{
				"Check memory/CPU limits and the recent request rate against them.",
				"Scale (replicas or limits) to stop the bleeding, then find what changed the resource profile.",
				"If it is a rate limit or quota, confirm whether traffic grew or a retry loop is amplifying it.",
			},
		},
		{
			name:        "code fix signature",
			title:       "500s on checkout",
			detail:      "panic: nil pointer dereference",
			wantKind:    domain.RemedyKindCodeFix,
			wantSummary: "The signature points at a code defect reaching production traffic.",
			wantSteps: []string{
				"Reproduce with a failing test that matches the stack trace before changing anything.",
				"Fix the root cause (not the symptom) and keep the reproducing test as a regression guard.",
				"Ship through the normal pipeline — a hotfix that skips tests is how the next incident starts.",
			},
		},
	}
	for _, tc := range signatureCases {
		t.Run(tc.name, func(t *testing.T) {
			r := prodops.Suggest(prodops.RemedyContext{
				Incident: domain.Incident{Title: tc.title, Detail: tc.detail, FirstSeenAt: now, LastSeenAt: now, Occurrences: 1},
			})
			require.Equal(t, tc.wantKind, r.Kind)
			require.Equal(t, 55, r.Confidence)
			require.Equal(t, tc.wantSummary, r.Summary)
			require.Equal(t, tc.wantSteps, r.Steps)
		})
	}

	t.Run("no signature matched falls back to diagnosis with a health probe", func(t *testing.T) {
		r := prodops.Suggest(prodops.RemedyContext{
			Incident: domain.Incident{Title: "something odd", Occurrences: 2, Severity: domain.IncidentSeverityHigh, Source: domain.IncidentSourceProbe, FirstSeenAt: now, LastSeenAt: now},
			Target:   domain.DeployTarget{HealthURL: "https://api.example.com/health"},
		})
		require.Equal(t, domain.RemedyKindUnknown, r.Kind)
		require.Equal(t, 25, r.Confidence)
		require.Equal(t, "No known signature matched — this needs diagnosis before a fix can be proposed.", r.Summary)
		require.Equal(t, []string{
			"Read the last 15 minutes of logs for this service around the first occurrence.",
			"Check whether anything was deployed, scaled or reconfigured today.",
			"Compare the failing environment against the last environment where it worked.",
			"Probe the health endpoint directly: https://api.example.com/health",
		}, r.Steps)
		require.Equal(t, []string{"2 occurrence(s), severity high, source probe"}, r.Evidence)
	})
}
