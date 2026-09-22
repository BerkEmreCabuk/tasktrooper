package prodops_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

func TestProbeRefusesInternalHealthURLs(t *testing.T) {
	var hits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("go_goroutines 42"))
	}))
	defer internal.Close()

	for _, healthURL := range []string{
		internal.URL + "/metrics",
		"http://169.254.169.254/computeMetadata/v1/",
		"http://10.4.0.9/healthz",
		"http://[::1]:8080/healthz",
		"http://100.64.0.1/healthz",
	} {
		targets := &fakeTargets{targets: []domain.DeployTarget{{
			ID:           uuid.New(),
			RepositoryID: uuid.New(),
			Env:          domain.DeployEnvProd,
			Provider:     domain.DeployProviderGCPCloudRun,
			HealthURL:    healthURL,
		}}}
		ingester := &fakeIngester{}
		monitor := prodops.NewMonitor(targets, ingester)

		monitor.Sweep(context.Background())
		monitor.Sweep(context.Background())

		if len(ingester.ingested) != 1 {
			t.Fatalf("%s: expected one incident, got %d", healthURL, len(ingester.ingested))
		}
		detail := ingester.ingested[0].Detail
		for _, leak := range []string{"loopback", "private", "link-local", "carrier", "connection refused", "no such host"} {
			if strings.Contains(strings.ToLower(detail), leak) {
				t.Fatalf("%s: the incident detail leaks why it was blocked: %q", healthURL, detail)
			}
		}
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("the probe reached the pod's own endpoint %d times", got)
	}
}

func TestProbeIncidentDropsTheQueryString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	targets := &fakeTargets{targets: []domain.DeployTarget{{
		ID:           uuid.New(),
		RepositoryID: uuid.New(),
		Env:          domain.DeployEnvProd,
		Provider:     domain.DeployProviderGCPCloudRun,
		HealthURL:    srv.URL + "/healthz?probe_token=s3cret",
	}}}
	ingester := &fakeIngester{}
	monitor := prodops.NewMonitor(targets, ingester)
	monitor.SetURLPolicy(loopbackPolicy())

	monitor.Sweep(context.Background())
	monitor.Sweep(context.Background())

	if len(ingester.ingested) != 1 {
		t.Fatalf("expected one incident, got %d", len(ingester.ingested))
	}
	in := ingester.ingested[0]
	if strings.Contains(in.Detail, "s3cret") {
		t.Fatalf("the incident detail carries the probe token: %q", in.Detail)
	}
	if got, _ := in.Payload["health_url"].(string); strings.Contains(got, "s3cret") {
		t.Fatalf("the incident payload carries the probe token: %q", got)
	}
	if !strings.Contains(in.Detail, "HTTP 503") {
		t.Fatalf("a real failure must still be diagnosable: %q", in.Detail)
	}
}

type fakeTargets struct{ targets []domain.DeployTarget }

func (f *fakeTargets) ListByRepository(context.Context, uuid.UUID) ([]domain.DeployTarget, error) {
	return f.targets, nil
}
func (f *fakeTargets) ListAll(context.Context) ([]domain.DeployTarget, error) { return f.targets, nil }
func (f *fakeTargets) Get(context.Context, uuid.UUID, string, string) (domain.DeployTarget, error) {
	if len(f.targets) == 0 {
		return domain.DeployTarget{}, domain.ErrIncidentNotFound
	}
	return f.targets[0], nil
}
func (f *fakeTargets) Save(_ context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	return t, nil
}
func (f *fakeTargets) Delete(context.Context, uuid.UUID, string, string) error { return nil }

type fakeIngester struct{ ingested []domain.IncidentInput }

func (f *fakeIngester) Ingest(_ context.Context, in domain.IncidentInput) (domain.Incident, error) {
	f.ingested = append(f.ingested, in)
	return domain.Incident{ID: uuid.New()}, nil
}

type ProbeSuite struct {
	suite.Suite
	healthy  atomic.Bool
	server   *httptest.Server
	targets  *fakeTargets
	ingester *fakeIngester
	monitor  *prodops.Monitor
}

func TestProbeSuite(t *testing.T) { suite.Run(t, new(ProbeSuite)) }

func (s *ProbeSuite) SetupTest() {
	s.healthy.Store(true)
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if s.healthy.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	s.targets = &fakeTargets{targets: []domain.DeployTarget{{
		ID:           uuid.New(),
		RepositoryID: uuid.New(),
		Env:          domain.DeployEnvProd,
		Provider:     domain.DeployProviderGCPCloudRun,
		HealthURL:    s.server.URL,
	}}}
	s.ingester = &fakeIngester{}
	s.monitor = prodops.NewMonitor(s.targets, s.ingester)

	s.monitor.SetURLPolicy(loopbackPolicy())
}

func loopbackPolicy() urlguard.Policy {
	p := urlguard.PublicOnly()
	p.AllowLoopback = true
	return p
}

func (s *ProbeSuite) TearDownTest() { s.server.Close() }

func (s *ProbeSuite) TestHealthySweepsIngestNothing() {
	s.monitor.Sweep(context.Background())
	s.monitor.Sweep(context.Background())

	s.Empty(s.ingester.ingested)
}

func (s *ProbeSuite) TestIncidentOpensOnlyAfterTwoFailures() {
	s.healthy.Store(false)

	s.monitor.Sweep(context.Background())
	s.Empty(s.ingester.ingested)

	s.monitor.Sweep(context.Background())
	s.Require().Len(s.ingester.ingested, 1)

	in := s.ingester.ingested[0]
	s.Equal(domain.IncidentSourceProbe, in.Source)
	s.Equal(domain.IncidentSeverityCritical, in.Severity, "prod probes are critical")
	s.Contains(in.Detail, "HTTP 503")
	s.False(in.Resolved)
	s.NotEmpty(in.Fingerprint)
}

func (s *ProbeSuite) TestRecoveryResolvesTheIncident() {
	s.healthy.Store(false)
	s.monitor.Sweep(context.Background())
	s.monitor.Sweep(context.Background())
	s.Require().Len(s.ingester.ingested, 1)

	s.healthy.Store(true)
	s.monitor.Sweep(context.Background())

	s.Require().Len(s.ingester.ingested, 2)
	s.True(s.ingester.ingested[1].Resolved)
	s.Equal(s.ingester.ingested[0].Fingerprint, s.ingester.ingested[1].Fingerprint,
		"the recovery must carry the same fingerprint or it closes nothing")
}

func (s *ProbeSuite) TestFailureCounterResetsOnSuccess() {
	s.healthy.Store(false)
	s.monitor.Sweep(context.Background())
	s.healthy.Store(true)
	s.monitor.Sweep(context.Background())
	s.healthy.Store(false)
	s.monitor.Sweep(context.Background())

	s.Empty(s.ingester.ingested)
}

func (s *ProbeSuite) TestStageProbeIsHighNotCritical() {
	s.targets.targets[0].Env = domain.DeployEnvStage
	s.healthy.Store(false)

	s.monitor.Sweep(context.Background())
	s.monitor.Sweep(context.Background())

	s.Require().Len(s.ingester.ingested, 1)
	s.Equal(domain.IncidentSeverityHigh, s.ingester.ingested[0].Severity)
}

func (s *ProbeSuite) TestTargetsWithoutHealthURLAreSkipped() {
	s.targets.targets[0].HealthURL = ""
	s.healthy.Store(false)

	s.monitor.Sweep(context.Background())
	s.monitor.Sweep(context.Background())

	s.Empty(s.ingester.ingested)
}
