package prodops_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeIncidents struct {
	incidents map[uuid.UUID]*domain.Incident
	events    map[uuid.UUID][]domain.IncidentEvent
}

func newFakeIncidents() *fakeIncidents {
	return &fakeIncidents{
		incidents: make(map[uuid.UUID]*domain.Incident),
		events:    make(map[uuid.UUID][]domain.IncidentEvent),
	}
}

func (f *fakeIncidents) Upsert(ctx context.Context, in domain.IncidentInput) (domain.Incident, bool, error) {
	if live, err := f.FindLive(ctx, in.RepositoryID, in.Env, in.Fingerprint); err == nil {
		stored := f.incidents[live.ID]
		stored.Occurrences++
		stored.LastSeenAt = time.Now()
		stored.Detail = in.Detail
		return *stored, false, nil
	}
	inc := domain.Incident{
		ID:           uuid.New(),
		RepositoryID: in.RepositoryID,
		Env:          in.Env,
		Source:       in.Source,
		Fingerprint:  in.Fingerprint,
		Title:        in.Title,
		Detail:       in.Detail,
		Severity:     in.Severity,
		Status:       domain.IncidentStatusOpen,
		Payload:      in.Payload,
		Occurrences:  1,
		FirstSeenAt:  time.Now(),
		LastSeenAt:   time.Now(),
	}
	f.incidents[inc.ID] = &inc
	return inc, true, nil
}

func (f *fakeIncidents) Get(_ context.Context, id uuid.UUID) (domain.Incident, error) {
	stored, ok := f.incidents[id]
	if !ok {
		return domain.Incident{}, domain.ErrIncidentNotFound
	}
	inc := *stored
	inc.Events = f.events[id]
	return inc, nil
}

func (f *fakeIncidents) List(_ context.Context, _ domain.IncidentFilter) ([]domain.Incident, error) {
	out := make([]domain.Incident, 0, len(f.incidents))
	for _, inc := range f.incidents {
		out = append(out, *inc)
	}
	return out, nil
}

func (f *fakeIncidents) FindLive(_ context.Context, repositoryID uuid.UUID, env, fingerprint string) (domain.Incident, error) {
	for _, inc := range f.incidents {
		if inc.RepositoryID == repositoryID && inc.Env == env && inc.Fingerprint == fingerprint &&
			!inc.Status.IsTerminal() {
			return *inc, nil
		}
	}
	return domain.Incident{}, domain.ErrIncidentNotFound
}

func (f *fakeIncidents) History(context.Context, uuid.UUID, string, int) ([]domain.Incident, error) {
	return nil, nil
}

func (f *fakeIncidents) UpdateStatus(_ context.Context, id uuid.UUID, status domain.IncidentStatus) (domain.Incident, error) {
	stored, ok := f.incidents[id]
	if !ok {
		return domain.Incident{}, domain.ErrIncidentNotFound
	}
	stored.Status = status
	return *stored, nil
}

func (f *fakeIncidents) UpdateRemedy(_ context.Context, id uuid.UUID, remedy, remedyKind, author string, confidence int) (domain.Incident, error) {
	stored, ok := f.incidents[id]
	if !ok {
		return domain.Incident{}, domain.ErrIncidentNotFound
	}
	stored.Remedy = remedy
	stored.RemedyKind = remedyKind
	stored.RemedyAuthor = author
	stored.Confidence = confidence
	return *stored, nil
}

func (f *fakeIncidents) AttachTask(_ context.Context, id, taskID uuid.UUID) (domain.Incident, error) {
	stored, ok := f.incidents[id]
	if !ok {
		return domain.Incident{}, domain.ErrIncidentNotFound
	}
	stored.TaskID = &taskID
	return *stored, nil
}

func (f *fakeIncidents) ByTask(_ context.Context, taskID uuid.UUID) (domain.Incident, error) {
	for _, inc := range f.incidents {
		if inc.TaskID != nil && *inc.TaskID == taskID {
			return *inc, nil
		}
	}
	return domain.Incident{}, domain.ErrIncidentNotFound
}

func (f *fakeIncidents) AppendEvent(_ context.Context, incidentID uuid.UUID, kind, message string) error {
	f.events[incidentID] = append(f.events[incidentID], domain.IncidentEvent{
		ID: uuid.New(), IncidentID: incidentID, Kind: kind, Message: message, CreatedAt: time.Now(),
	})
	return nil
}

func (f *fakeIncidents) ListEvents(_ context.Context, incidentID uuid.UUID) ([]domain.IncidentEvent, error) {
	return f.events[incidentID], nil
}

type RemedyAuthorshipSuite struct {
	suite.Suite
	ctx       context.Context
	incidents *fakeIncidents
	svc       *prodops.Service
	repoID    uuid.UUID
}

func TestRemedyAuthorshipSuite(t *testing.T) { suite.Run(t, new(RemedyAuthorshipSuite)) }

func (s *RemedyAuthorshipSuite) SetupTest() {
	s.ctx = context.Background()
	s.incidents = newFakeIncidents()
	s.svc = prodops.NewService(prodops.Deps{Incidents: s.incidents})
	s.repoID = uuid.New()
}

func (s *RemedyAuthorshipSuite) alert() domain.IncidentInput {
	return domain.IncidentInput{
		RepositoryID: s.repoID,
		Env:          domain.DeployEnvProd,
		Source:       domain.IncidentSourceWebhook,
		Fingerprint:  "fp-authorship",
		Title:        "api 5xx",
		Detail:       "burst of 503s",
		Severity:     domain.IncidentSeverityMedium,
	}
}

func (s *RemedyAuthorshipSuite) TestAutoTriageIsRecordedAsMachineAuthorship() {
	incident, err := s.svc.Ingest(s.ctx, s.alert())
	s.Require().NoError(err)
	s.NotEmpty(incident.Remedy, "ingest writes a first-pass remedy")
	s.Equal(domain.RemedyAuthorAutoTriage, incident.RemedyAuthor,
		"the first pass is the rules engine, and must say so")
}

func (s *RemedyAuthorshipSuite) TestProposalOverwritesMachineTriage() {
	incident, err := s.svc.Ingest(s.ctx, s.alert())
	s.Require().NoError(err)

	proposed, err := s.svc.ProposeRemedy(s.ctx, incident.ID, domain.Remedy{
		Kind:       domain.RemedyKindConfig,
		Summary:    "connection pool exhausted; raise max_conns to 200",
		Confidence: 85,
	}, domain.RemedyAuthorAgent)
	s.Require().NoError(err)
	s.Equal(domain.RemedyAuthorAgent, proposed.RemedyAuthor)
	s.Contains(proposed.Remedy, "raise max_conns to 200")
}

func (s *RemedyAuthorshipSuite) TestRecurrenceDoesNotOverwriteAHumanRemedy() {
	incident, err := s.svc.Ingest(s.ctx, s.alert())
	s.Require().NoError(err)

	proposed, err := s.svc.ProposeRemedy(s.ctx, incident.ID, domain.Remedy{
		Kind:       domain.RemedyKindCodeFix,
		Summary:    "nil deref in the order handler; guard the empty cart case",
		Confidence: 90,
	}, domain.RemedyAuthorHuman)
	s.Require().NoError(err)

	_, err = s.incidents.UpdateStatus(s.ctx, incident.ID, domain.IncidentStatusTriaging)
	s.Require().NoError(err)

	recurred, err := s.svc.Ingest(s.ctx, s.alert())
	s.Require().NoError(err)
	s.Equal(2, recurred.Occurrences, "the recurrence still folds into the same incident")
	s.Equal(domain.RemedyAuthorHuman, recurred.RemedyAuthor)
	s.Equal(proposed.Remedy, recurred.Remedy, "machine triage must not clobber a human's write")
}

func (s *RemedyAuthorshipSuite) TestRecurrenceRefreshesItsOwnTriage() {
	incident, err := s.svc.Ingest(s.ctx, s.alert())
	s.Require().NoError(err)
	s.Require().Equal(domain.RemedyAuthorAutoTriage, incident.RemedyAuthor)

	_, err = s.incidents.UpdateRemedy(s.ctx, incident.ID, "stale hypothesis",
		domain.RemedyKindUnknown, domain.RemedyAuthorAutoTriage, 10)
	s.Require().NoError(err)

	recurred, err := s.svc.Ingest(s.ctx, s.alert())
	s.Require().NoError(err)
	s.NotEqual("stale hypothesis", recurred.Remedy)
	s.Equal(domain.RemedyAuthorAutoTriage, recurred.RemedyAuthor)
}

func (s *RemedyAuthorshipSuite) TestUnknownAuthorshipFallsBackToTheStatusProxy() {
	incident, err := s.svc.Ingest(s.ctx, s.alert())
	s.Require().NoError(err)
	legacy, err := s.incidents.UpdateRemedy(s.ctx, incident.ID, "written before authorship was recorded",
		domain.RemedyKindConfig, "", 60)
	s.Require().NoError(err)
	s.Require().Empty(legacy.RemedyAuthor)
	_, err = s.incidents.UpdateStatus(s.ctx, incident.ID, domain.IncidentStatusProposed)
	s.Require().NoError(err)

	recurred, err := s.svc.Ingest(s.ctx, s.alert())
	s.Require().NoError(err)
	s.Equal("written before authorship was recorded", recurred.Remedy,
		"a proposed legacy incident is still off limits")

	_, err = s.incidents.UpdateStatus(s.ctx, incident.ID, domain.IncidentStatusOpen)
	s.Require().NoError(err)
	retriaged, err := s.svc.Ingest(s.ctx, s.alert())
	s.Require().NoError(err)
	s.NotEqual("written before authorship was recorded", retriaged.Remedy,
		"an open legacy incident is fair game, as it was before")
	s.Equal(domain.RemedyAuthorAutoTriage, retriaged.RemedyAuthor)
}
