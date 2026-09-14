package prodops_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type NormalizeSuite struct{ suite.Suite }

func TestNormalizeSuite(t *testing.T) { suite.Run(t, new(NormalizeSuite)) }

func (s *NormalizeSuite) TestAlertmanagerFiring() {
	raw := []byte(`{
		"status": "firing",
		"alerts": [{
			"status": "firing",
			"labels": {"alertname": "HighErrorRate", "severity": "critical", "service": "api", "env": "prod"},
			"annotations": {"summary": "5xx rate above 10%", "description": "api returned 503 for 12% of requests"},
			"fingerprint": "abc123"
		}]
	}`)

	in, err := prodops.Normalize(raw, prodops.Defaults{})

	s.Require().NoError(err)
	s.Equal("5xx rate above 10%", in.Title)
	s.Equal(domain.IncidentSeverityCritical, in.Severity)
	s.Equal("prod", in.Env)
	s.Equal("abc123", in.Fingerprint, "the sender's own fingerprint must be reused so dedupe identities match")
	s.False(in.Resolved)
	s.Equal(domain.IncidentSourceWebhook, in.Source)
}

func (s *NormalizeSuite) TestAlertmanagerResolvedIsRecovery() {
	raw := []byte(`{"status":"resolved","alerts":[{"status":"resolved","labels":{"alertname":"HighErrorRate"},"annotations":{"summary":"recovered"}}]}`)

	in, err := prodops.Normalize(raw, prodops.Defaults{})

	s.Require().NoError(err)
	s.True(in.Resolved)
}

func (s *NormalizeSuite) TestGoogleCloudMonitoring() {
	raw := []byte(`{"incident":{"incident_id":"0.abc","state":"open","policy_name":"api latency","condition_name":"p99 > 2s","summary":"api latency high","resource":{"labels":{"service_name":"api"}}}}`)

	in, err := prodops.Normalize(raw, prodops.Defaults{})

	s.Require().NoError(err)
	s.Equal("api latency high", in.Title)
	s.Equal(domain.IncidentSeverityHigh, in.Severity)
	s.False(in.Resolved)
	s.NotEmpty(in.Fingerprint)
}

func (s *NormalizeSuite) TestGoogleCloudMonitoringClosedIsRecovery() {
	raw := []byte(`{"incident":{"state":"closed","policy_name":"api latency","condition_name":"p99 > 2s"}}`)

	in, err := prodops.Normalize(raw, prodops.Defaults{})

	s.Require().NoError(err)
	s.True(in.Resolved)
}

func (s *NormalizeSuite) TestSentry() {
	raw := []byte(`{"culprit":"handler.Serve","data":{"issue":{"id":"991","title":"panic: nil map","level":"error","project":"api","environment":"prod"}}}`)

	in, err := prodops.Normalize(raw, prodops.Defaults{})

	s.Require().NoError(err)
	s.Equal("panic: nil map", in.Title)
	s.Equal(domain.IncidentSeverityHigh, in.Severity)
	s.Equal("prod", in.Env)
}

func (s *NormalizeSuite) TestGenericPayloadAndDefaults() {
	raw := []byte(`{"title":"queue backlog","severity":"warning","detail":"5k messages pending"}`)

	in, err := prodops.Normalize(raw, prodops.Defaults{Env: "stage", Source: domain.IncidentSourceManual})

	s.Require().NoError(err)
	s.Equal("queue backlog", in.Title)
	s.Equal(domain.IncidentSeverityMedium, in.Severity)
	s.Equal("stage", in.Env)
	s.Equal(domain.IncidentSourceManual, in.Source)
	s.NotEmpty(in.Fingerprint, "a payload without a fingerprint still needs a dedupe identity")
}

// An unrecognised shape must still produce an ingestable incident: dropping an
// alert because we do not know its vendor is the worst possible outcome.
func (s *NormalizeSuite) TestUnknownShapeStillIngests() {
	raw := []byte(`{"weird":{"nested":true}}`)

	in, err := prodops.Normalize(raw, prodops.Defaults{})

	s.Require().NoError(err)
	s.Equal("Unlabelled production alert", in.Title)
	s.Equal(domain.DeployEnvProd, in.Env)
	s.NotEmpty(in.Fingerprint)
}

func (s *NormalizeSuite) TestInvalidJSONRejected() {
	_, err := prodops.Normalize([]byte(`not json`), prodops.Defaults{})
	s.Error(err)
}

// The same alert must fold onto the same fingerprint, and a different alert
// must not — this is what keeps one outage from opening a hundred tasks.
func (s *NormalizeSuite) TestFingerprintStability() {
	a := domain.IncidentFingerprint("HighErrorRate", "api", "prod")
	b := domain.IncidentFingerprint("highErrorRate", "api", "prod")
	c := domain.IncidentFingerprint("HighErrorRate", "worker", "prod")

	s.Equal(a, b, "fingerprints are case-insensitive")
	s.NotEqual(a, c)
	s.Empty(domain.IncidentFingerprint("", "  "))
}
