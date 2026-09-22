package prodops_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type RemedySuite struct {
	suite.Suite
	now time.Time
}

func TestRemedySuite(t *testing.T) { suite.Run(t, new(RemedySuite)) }

func (s *RemedySuite) SetupTest() {
	s.now = time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
}

func (s *RemedySuite) incident(title, detail string) domain.Incident {
	return domain.Incident{
		Env:         domain.DeployEnvProd,
		Title:       title,
		Detail:      detail,
		Severity:    domain.IncidentSeverityCritical,
		Source:      domain.IncidentSourceProbe,
		Occurrences: 1,
		FirstSeenAt: s.now,
		LastSeenAt:  s.now,
	}
}

func (s *RemedySuite) TestRecentDeployIsBlamedFirst() {
	finished := s.now.Add(-10 * time.Minute)

	r := prodops.Suggest(prodops.RemedyContext{
		Incident: s.incident("prod health check failing", "HTTP 503"),
		Rollback: "gcloud run services update-traffic api --to-revisions PREV=100",
		Deploys: []prodops.DeployRecord{
			{Env: domain.DeployEnvProd, Status: domain.PipelineStatusSuccess, FinishedAt: finished, TaskKey: "TT-42"},
		},
	})

	s.Equal(domain.RemedyKindRollback, r.Kind)
	s.True(r.Rollback)
	s.GreaterOrEqual(r.Confidence, 70)
	s.Contains(strings.Join(r.Steps, "\n"), "update-traffic", "the provider's rollback command must be handed over verbatim")
	s.Contains(strings.Join(r.Evidence, "\n"), "TT-42")
}

func (s *RemedySuite) TestFailedDeployRaisesConfidence() {
	good := prodops.Suggest(prodops.RemedyContext{
		Incident: s.incident("prod down", ""),
		Deploys:  []prodops.DeployRecord{{Env: domain.DeployEnvProd, Status: domain.PipelineStatusSuccess, FinishedAt: s.now.Add(-5 * time.Minute)}},
	})
	bad := prodops.Suggest(prodops.RemedyContext{
		Incident: s.incident("prod down", ""),
		Deploys:  []prodops.DeployRecord{{Env: domain.DeployEnvProd, Status: domain.PipelineStatusFailed, FinishedAt: s.now.Add(-5 * time.Minute)}},
	})

	s.Greater(bad.Confidence, good.Confidence)
}

func (s *RemedySuite) TestOldOrOtherEnvDeployIsNotBlamed() {
	r := prodops.Suggest(prodops.RemedyContext{
		Incident: s.incident("checkout returns 500", "unhandled exception in payment handler"),
		Deploys: []prodops.DeployRecord{
			{Env: domain.DeployEnvProd, Status: domain.PipelineStatusSuccess, FinishedAt: s.now.Add(-6 * time.Hour)},
			{Env: domain.DeployEnvStage, Status: domain.PipelineStatusSuccess, FinishedAt: s.now.Add(-2 * time.Minute)},
		},
	})

	s.NotEqual(domain.RemedyKindRollback, r.Kind, "a stage deploy and a six-hour-old prod deploy are not the cause")
	s.Equal(domain.RemedyKindCodeFix, r.Kind)
}

func (s *RemedySuite) TestDeployAfterOnsetIsNotBlamed() {
	r := prodops.Suggest(prodops.RemedyContext{
		Incident: s.incident("prod alert", "connection refused"),
		Deploys:  []prodops.DeployRecord{{Env: domain.DeployEnvProd, Status: domain.PipelineStatusSuccess, FinishedAt: s.now.Add(2 * time.Minute)}},
	})

	s.Equal(domain.RemedyKindDependency, r.Kind)
}

func (s *RemedySuite) TestHistoryReusesThePreviousFix() {
	resolvedAt := s.now.Add(-72 * time.Hour)

	r := prodops.Suggest(prodops.RemedyContext{
		Incident: s.incident("worker stopped consuming", "no signature here"),
		History: []domain.Incident{{
			Remedy:      "Restarted the consumer group and raised the visibility timeout to 5m",
			RemedyKind:  domain.RemedyKindConfig,
			Occurrences: 4,
			ResolvedAt:  &resolvedAt,
		}},
	})

	s.Equal(domain.RemedyKindConfig, r.Kind)
	s.Contains(strings.Join(r.Steps, "\n"), "visibility timeout")
	s.Contains(strings.Join(r.Evidence, "\n"), "same fingerprint")
}

func (s *RemedySuite) TestSignatureClassification() {
	cases := []struct {
		name   string
		title  string
		detail string
		want   string
	}{
		{"config", "deploy alert", "Error: permission denied while reading secret", domain.RemedyKindConfig},
		{"dependency", "api failing", "dial tcp 10.0.0.5:5432: connection refused", domain.RemedyKindDependency},
		{"capacity", "pods restarting", "container killed: OOMKilled", domain.RemedyKindCapacity},
		{"code", "checkout broken", "panic: runtime error: index out of range", domain.RemedyKindCodeFix},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			r := prodops.Suggest(prodops.RemedyContext{Incident: s.incident(tc.title, tc.detail)})
			s.Equal(tc.want, r.Kind)
			s.NotEmpty(r.Steps)
		})
	}
}

func (s *RemedySuite) TestUnknownStillReturnsAChecklist() {
	r := prodops.Suggest(prodops.RemedyContext{
		Incident: s.incident("something odd happened", "no recognisable signature"),
		Target:   domain.DeployTarget{HealthURL: "https://api.example.com/health"},
	})

	s.Equal(domain.RemedyKindUnknown, r.Kind)
	s.NotEmpty(r.Steps)
	s.Less(r.Confidence, 50, "an unknown cause must not claim confidence")
	s.Contains(strings.Join(r.Steps, "\n"), "https://api.example.com/health")
}

func (s *RemedySuite) TestTextRendersStepsAndEvidence() {
	text := domain.Remedy{
		Summary:  "Roll back",
		Steps:    []string{"step one"},
		Evidence: []string{"deploy 5 min before"},
	}.Text()

	s.Contains(text, "Roll back")
	s.Contains(text, "- step one")
	s.Contains(text, "- deploy 5 min before")
}
