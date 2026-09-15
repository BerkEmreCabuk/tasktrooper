package prodops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TaskBoard is the slice of the board this package needs: open the remediation
// task, and comment on it as the incident evolves.
type TaskBoard interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

// RepositoryResolver reads the repo an incident belongs to (for its kind and
// its incident policy).
type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// DeployHistory returns the repository's recent deploy pipelines, newest first.
// It is what lets the engine blame a release for an incident.
type DeployHistory interface {
	RecentDeploys(ctx context.Context, repositoryID uuid.UUID, limit int) ([]domain.TaskPipeline, error)
}

// Notifier pushes an alert to the user's devices.
// ctx is carried for its values: the send is backgrounded and still reads the
// device list through the store.
type Notifier interface {
	Alert(ctx context.Context, title, body string)
}

// Deps wires the collaborators of the production operations service.
type Deps struct {
	Incidents port.IncidentStore
	Targets   port.DeployTargetStore
	Repos     RepositoryResolver
	Tasks     TaskBoard
	Deploys   DeployHistory
	Agents    func(ctx context.Context) ([]domain.Agent, error)
	Notifier  Notifier
}

type Service struct {
	incidents port.IncidentStore
	targets   port.DeployTargetStore
	repos     RepositoryResolver
	tasks     TaskBoard
	deploys   DeployHistory
	agents    func(ctx context.Context) ([]domain.Agent, error)
	notifier  Notifier

	// attributor / rollbacks are the release-loop half: which task's commit
	// production is running, and what to do about it when that commit is what
	// broke. Both late-set (SetReleaseAttributor / SetReleaseRollbackDispatcher)
	// because application/deploywatch and application/board are wired after this
	// service and would otherwise be an import cycle. Nil leaves the incident
	// path exactly as it was.
	attributor ReleaseAttributor
	rollbacks  ReleaseRollbackDispatcher

	// Re-notify throttle for long-running critical incidents: the probe
	// re-ingests every sweep, and a page per sweep is an alert storm.
	notifyMu   sync.Mutex
	lastNotify map[uuid.UUID]time.Time
}

func NewService(deps Deps) *Service {
	return &Service{
		incidents: deps.Incidents,
		targets:   deps.Targets,
		repos:     deps.Repos,
		tasks:     deps.Tasks,
		deploys:   deps.Deploys,
		agents:    deps.Agents,
		notifier:  deps.Notifier,
	}
}

// taskSeverityFloor is the severity from which an incident is worth opening a
// board task for. Below it the incident is recorded and visible, but nobody is
// interrupted.
const taskSeverityFloor = domain.IncidentSeverityHigh

// Ingest is the single entry point for every production signal. It dedupes,
// derives a remedy, opens (or updates) the remediation task and notifies —
// in that order, so an alert storm produces one incident and one task.
func (s *Service) Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error) {
	if in.RepositoryID == uuid.Nil {
		return domain.Incident{}, errors.New("incident needs a repository")
	}
	if in.Env == "" {
		in.Env = domain.DeployEnvProd
	}
	if in.Source == "" {
		in.Source = domain.IncidentSourceWebhook
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = "Production alert"
	}
	if in.Fingerprint == "" {
		in.Fingerprint = domain.IncidentFingerprint(in.Title, in.Env)
	}
	if in.Severity == "" {
		in.Severity = domain.IncidentSeverityMedium
	}

	if in.Resolved {
		return s.recover(ctx, in)
	}

	incident, created, err := s.incidents.Upsert(ctx, in)
	if err != nil {
		return domain.Incident{}, err
	}
	if created {
		s.event(ctx, incident.ID, domain.IncidentEventDetected,
			fmt.Sprintf("%s incident detected via %s (%s)", incident.Env, incident.Source, incident.Severity))
	} else if incident.Occurrences <= 5 || incident.Occurrences%25 == 0 {
		// The probe re-ingests every sweep of an ongoing outage; recording
		// each recurrence as its own event row grows the timeline unboundedly.
		s.event(ctx, incident.ID, domain.IncidentEventRecurred,
			fmt.Sprintf("recurred (%d occurrences)", incident.Occurrences))
	}

	policy := s.policy(ctx, incident.RepositoryID)
	remedy := s.buildRemedy(ctx, incident)
	// The first-pass remedy is triage, not truth. Once someone has written a
	// proposal, a recurrence of the same fingerprint must not overwrite it with
	// rules-engine boilerplate.
	if created || machineMayOverwriteRemedy(incident) {
		if updated, err := s.incidents.UpdateRemedy(ctx, incident.ID, remedy.Text(), remedy.Kind,
			domain.RemedyAuthorAutoTriage, remedy.Confidence); err != nil {
			log.Warn().Err(err).Str("incident_id", incident.ID.String()).Msg("persist incident remedy failed")
		} else {
			incident = updated
		}
		if created {
			s.event(ctx, incident.ID, domain.IncidentEventTriaged,
				fmt.Sprintf("first-pass remedy: %s (confidence %d)", remedy.Kind, remedy.Confidence))
		}
	}

	// Attribution runs BEFORE the remediation task is opened, deliberately.
	// A generic "production is down" diagnosis task and a specific "DE-12's
	// release broke production, roll it back" are not the same piece of work,
	// and opening the first one and then discovering the second leaves two
	// cards for one outage. It is also the point where auto_rollback stops
	// being decorative.
	incident = s.attributeAndMaybeRollBack(ctx, incident)

	if policy != domain.IncidentPolicyOff && incident.TaskID == nil &&
		incident.Severity.Rank() >= taskSeverityFloor.Rank() {
		if updated, err := s.openRemediationTask(ctx, incident, remedy, policy); err != nil {
			log.Warn().Err(err).Str("incident_id", incident.ID.String()).Msg("open remediation task failed")
		} else {
			incident = updated
		}
	}

	if (created || incident.Severity == domain.IncidentSeverityCritical) && s.shouldRenotify(incident.ID) {
		s.notify(ctx, incident, remedy)
	}
	return incident, nil
}

// machineMayOverwriteRemedy answers whether the automatic first pass is allowed
// to replace the proposal already on the incident. It keys off the recorded
// author: anything a human or an agent wrote is off limits no matter where the
// status was moved afterwards. Incidents from before remedy_author existed
// carry no author at all, so for those — and for incidents nobody has written
// to yet — it falls back to the status proxy this guard used to be.
func machineMayOverwriteRemedy(incident domain.Incident) bool {
	switch incident.RemedyAuthor {
	case domain.RemedyAuthorAutoTriage:
		return true
	case "":
		return incident.Status == domain.IncidentStatusOpen || incident.Status == domain.IncidentStatusTriaging
	default:
		return false
	}
}

// renotifyCooldown spaces the repeat pages of one ongoing critical incident.
const renotifyCooldown = 30 * time.Minute

func (s *Service) shouldRenotify(incidentID uuid.UUID) bool {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	if s.lastNotify == nil {
		s.lastNotify = make(map[uuid.UUID]time.Time)
	}
	if last, ok := s.lastNotify[incidentID]; ok && time.Since(last) < renotifyCooldown {
		return false
	}
	if len(s.lastNotify) > 256 {
		for id, last := range s.lastNotify {
			if time.Since(last) >= renotifyCooldown {
				delete(s.lastNotify, id)
			}
		}
	}
	s.lastNotify[incidentID] = time.Now()
	return true
}

// recover closes the live incident a recovery signal refers to. An unknown
// fingerprint is not an error: alerting systems routinely send a resolve for
// something we never opened.
func (s *Service) recover(ctx context.Context, in domain.IncidentInput) (domain.Incident, error) {
	live, err := s.incidents.FindLive(ctx, in.RepositoryID, in.Env, in.Fingerprint)
	if err != nil {
		if errors.Is(err, domain.ErrIncidentNotFound) {
			return domain.Incident{}, nil
		}
		return domain.Incident{}, err
	}
	resolved, err := s.incidents.UpdateStatus(ctx, live.ID, domain.IncidentStatusResolved)
	if err != nil {
		return domain.Incident{}, err
	}
	s.event(ctx, resolved.ID, domain.IncidentEventResolved, "recovery signal received from "+in.Source)
	if resolved.TaskID != nil && s.tasks != nil {
		if _, err := s.tasks.AddComment(ctx, resolved.RepositoryID, *resolved.TaskID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    "Production recovered — the incident stopped firing. Confirm the root cause is actually fixed before closing this task.",
		}); err != nil {
			log.Warn().Err(err).Msg("incident recovery comment failed")
		}
	}
	if s.notifier != nil {
		s.notifier.Alert(ctx, "Recovered · "+resolved.Env, resolved.Title)
	}
	return resolved, nil
}

// IngestDeployFailure turns a failed stage/preprod/prod deploy into an
// incident. A deploy that fails halfway is a production event even when no
// external alert fires.
func (s *Service) IngestDeployFailure(ctx context.Context, repositoryID uuid.UUID, env, taskKey, detail string) (domain.Incident, error) {
	severity := domain.IncidentSeverityHigh
	if env == domain.DeployEnvProd {
		severity = domain.IncidentSeverityCritical
	}
	return s.Ingest(ctx, domain.IncidentInput{
		RepositoryID: repositoryID,
		Env:          env,
		Source:       domain.IncidentSourceDeploy,
		Severity:     severity,
		Title:        fmt.Sprintf("%s deploy failed", env),
		Detail:       strings.TrimSpace(taskKey + "\n" + detail),
		Fingerprint:  domain.IncidentFingerprint("deploy-failed", env),
		Payload:      map[string]any{"task": taskKey, "detail": detail},
	})
}

// buildRemedy assembles the engine's context (deploy history, prior
// occurrences, the environment's deploy target) and runs the rules.
func (s *Service) buildRemedy(ctx context.Context, incident domain.Incident) domain.Remedy {
	rc := RemedyContext{Incident: incident}
	if s.targets != nil {
		// Deliberately left as "err == nil, else skip" rather than switched to
		// errors.Is(err, port.ErrNotFound): unlike mobileStoreGate this is
		// best-effort context enrichment for a remedy suggestion, not a
		// deploy/release gate, and buildRemedy has no error return to
		// propagate a real infra failure through anyway — the only
		// observable effect of any Get failure, not-found or otherwise, is a
		// remedy proposal missing its rollback hint. See Task 12 review round
		// 2 fix report for the mobileStoreGate case where the distinction
		// does matter.
		if target, err := s.targets.Get(ctx, incident.RepositoryID, "", incident.Env); err == nil {
			rc.Target = target
			if tpl, ok := deploy.Template(firstNonEmpty(target.TemplateID, target.Provider)); ok {
				rc.Rollback = tpl.RollbackHint
			}
		}
	}
	if s.deploys != nil {
		if pipelines, err := s.deploys.RecentDeploys(ctx, incident.RepositoryID, 10); err == nil {
			for _, p := range pipelines {
				rc.Deploys = append(rc.Deploys, DeployRecord{
					Env:        deployEnvOfTrigger(p.Trigger),
					Status:     p.Status,
					FinishedAt: valueOrZero(p.FinishedAt),
					TaskKey:    p.Note,
				})
			}
		}
	}
	if s.incidents != nil {
		if history, err := s.incidents.History(ctx, incident.RepositoryID, incident.Fingerprint, 3); err == nil {
			rc.History = history
		}
	}
	return Suggest(rc)
}

// openRemediationTask puts the incident on the board. Under the suggest policy
// the task is explicitly forbidden from changing code: it must stop at a
// written proposal, and a human decides. Under auto_fix it carries the fix
// through the normal columns, which still gate it with tests and review.
func (s *Service) openRemediationTask(ctx context.Context, incident domain.Incident, remedy domain.Remedy, policy domain.IncidentPolicy) (domain.Incident, error) {
	if s.tasks == nil {
		return incident, errors.New("board is not available")
	}
	priority := domain.TaskPriorityHigh
	if incident.Severity == domain.IncidentSeverityCritical {
		priority = domain.TaskPriorityCritical
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Production incident on **%s** (severity: %s, source: %s, occurrences: %d).\n\n",
		incident.Env, incident.Severity, incident.Source, incident.Occurrences)
	fmt.Fprintf(&b, "**Symptom:** %s\n", incident.Title)
	if strings.TrimSpace(incident.Detail) != "" {
		fmt.Fprintf(&b, "\n```\n%s\n```\n", truncate(incident.Detail, 2000))
	}
	fmt.Fprintf(&b, "\n**First-pass hypothesis (%s, confidence %d):**\n%s\n", remedy.Kind, remedy.Confidence, remedy.Text())
	fmt.Fprintf(&b, "\nIncident id: `%s` — call `get_incident` for the full payload and timeline.\n", incident.ID)
	if policy == domain.IncidentPolicyAutoFix {
		b.WriteString("\n**Policy: auto_fix.** Diagnose, then implement the fix and take it through the normal pipeline. " +
			"Record what you concluded with `propose_incident_remedy` before you start changing code.\n")
	} else {
		b.WriteString("\n**Policy: suggest.** Do NOT change production code. Diagnose only, then call " +
			"`propose_incident_remedy` with the concrete fix (commands, files, config) and move the task to human_uat for the decision.\n")
	}

	task, err := s.tasks.CreateTask(ctx, incident.RepositoryID, domain.CreateBoardTaskRequest{
		Title:           "Incident: " + truncate(incident.Title, 120),
		Description:     b.String(),
		TaskType:        domain.TaskTypeBug,
		Priority:        priority,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.roleAgent(ctx, incident.RepositoryID),
	})
	if err != nil {
		return incident, err
	}
	updated, err := s.incidents.AttachTask(ctx, incident.ID, task.ID)
	if err != nil {
		return incident, err
	}
	s.event(ctx, incident.ID, domain.IncidentEventTaskCreated, "remediation task "+task.Key+" opened ("+string(policy)+")")
	if triaging, err := s.incidents.UpdateStatus(ctx, incident.ID, domain.IncidentStatusTriaging); err == nil {
		triaging.TaskID = updated.TaskID
		updated = triaging
	}
	return updated, nil
}

// ProposeRemedy records the diagnosis an agent (or a human) reached. It is the
// hand-off point: the incident now carries a concrete fix, and the human is
// told about it.
func (s *Service) ProposeRemedy(ctx context.Context, incidentID uuid.UUID, remedy domain.Remedy, author string) (domain.Incident, error) {
	if remedy.Kind == "" {
		remedy.Kind = domain.RemedyKindUnknown
	}
	if strings.TrimSpace(remedy.Summary) == "" {
		return domain.Incident{}, errors.New("remedy needs a summary")
	}
	// An unnamed caller is still not the rules engine: this entry point is only
	// reached from the tool and the API, so record it as a human write rather
	// than leaving the author blank (blank reads as "unknown" and would let the
	// next recurrence overwrite the proposal).
	if strings.TrimSpace(author) == "" {
		author = domain.RemedyAuthorHuman
	}
	incident, err := s.incidents.UpdateRemedy(ctx, incidentID, remedy.Text(), remedy.Kind, author, remedy.Confidence)
	if err != nil {
		return domain.Incident{}, err
	}
	status := domain.IncidentStatusProposed
	if s.policy(ctx, incident.RepositoryID) == domain.IncidentPolicyAutoFix {
		status = domain.IncidentStatusFixing
	}
	if updated, err := s.incidents.UpdateStatus(ctx, incidentID, status); err == nil {
		incident = updated
	}
	s.event(ctx, incidentID, domain.IncidentEventProposed, author+" proposed a "+remedy.Kind+" remedy")
	if s.notifier != nil {
		s.notifier.Alert(ctx, "Fix proposed · "+incident.Env, truncate(remedy.Summary, 200))
	}
	return incident, nil
}

// Triage re-runs the rules engine for an incident on demand (after a deploy,
// or when new occurrences changed the picture). Unlike the ingest path this is
// an explicit request to re-derive, so it overwrites whatever is there — but
// the result is still machine-authored, and is recorded as such.
func (s *Service) Triage(ctx context.Context, incidentID uuid.UUID) (domain.Incident, error) {
	incident, err := s.incidents.Get(ctx, incidentID)
	if err != nil {
		return domain.Incident{}, err
	}
	remedy := s.buildRemedy(ctx, incident)
	updated, err := s.incidents.UpdateRemedy(ctx, incidentID, remedy.Text(), remedy.Kind,
		domain.RemedyAuthorAutoTriage, remedy.Confidence)
	if err != nil {
		return domain.Incident{}, err
	}
	s.event(ctx, incidentID, domain.IncidentEventTriaged, fmt.Sprintf("re-triaged: %s (confidence %d)", remedy.Kind, remedy.Confidence))
	return updated, nil
}

func (s *Service) Resolve(ctx context.Context, incidentID uuid.UUID, note string) (domain.Incident, error) {
	incident, err := s.incidents.UpdateStatus(ctx, incidentID, domain.IncidentStatusResolved)
	if err != nil {
		return domain.Incident{}, err
	}
	s.event(ctx, incidentID, domain.IncidentEventResolved, firstNonEmpty(note, "resolved"))
	return incident, nil
}

func (s *Service) Ignore(ctx context.Context, incidentID uuid.UUID, note string) (domain.Incident, error) {
	incident, err := s.incidents.UpdateStatus(ctx, incidentID, domain.IncidentStatusIgnored)
	if err != nil {
		return domain.Incident{}, err
	}
	s.event(ctx, incidentID, domain.IncidentEventIgnored, firstNonEmpty(note, "muted"))
	return incident, nil
}

func (s *Service) List(ctx context.Context, filter domain.IncidentFilter) ([]domain.Incident, error) {
	return s.incidents.List(ctx, filter)
}

func (s *Service) Get(ctx context.Context, incidentID uuid.UUID) (domain.Incident, error) {
	return s.incidents.Get(ctx, incidentID)
}

// ByTask resolves the incident a remediation task belongs to, so an agent
// working the task can read its incident without being told the id.
func (s *Service) ByTask(ctx context.Context, taskID uuid.UUID) (domain.Incident, error) {
	return s.incidents.ByTask(ctx, taskID)
}

// notify interrupts the human for incidents worth interrupting for, with the
// proposed action in the body — the point is that the notification itself
// answers "what do I do now".
func (s *Service) notify(ctx context.Context, incident domain.Incident, remedy domain.Remedy) {
	if s.notifier == nil || incident.Severity.Rank() < taskSeverityFloor.Rank() {
		return
	}
	title := fmt.Sprintf("%s · %s", strings.ToUpper(incident.Env), truncate(incident.Title, 80))
	s.notifier.Alert(ctx, title, truncate(remedy.Summary, 200))
	s.event(ctx, incident.ID, domain.IncidentEventNotified, "pushed to devices")
}

func (s *Service) policy(ctx context.Context, repositoryID uuid.UUID) domain.IncidentPolicy {
	if s.repos == nil {
		return domain.IncidentPolicySuggest
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil || !domain.ValidIncidentPolicy(repo.IncidentPolicy) {
		return domain.IncidentPolicySuggest
	}
	return repo.IncidentPolicy
}

// roleAgent picks the agent that owns incidents for the repo kind.
func (s *Service) roleAgent(ctx context.Context, repositoryID uuid.UUID) *uuid.UUID {
	if s.agents == nil || s.repos == nil {
		return nil
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return nil
	}
	agents, err := s.agents(ctx)
	if err != nil {
		return nil
	}
	want := domain.DeveloperAgentForKind(repo.Kind, repo.SubProjects)
	for i := range agents {
		if agents[i].Name == want {
			id := agents[i].ID
			return &id
		}
	}
	return nil
}

func (s *Service) event(ctx context.Context, incidentID uuid.UUID, kind, message string) {
	if err := s.incidents.AppendEvent(ctx, incidentID, kind, message); err != nil {
		log.Warn().Err(err).Str("incident_id", incidentID.String()).Str("kind", kind).Msg("append incident event failed")
	}
}

// deployEnvOfTrigger maps a deploy pipeline trigger back to its environment.
func deployEnvOfTrigger(trigger domain.PipelineTrigger) string {
	switch trigger {
	case domain.PipelineTriggerStageDeploy:
		return domain.DeployEnvStage
	case domain.PipelineTriggerPreProdDeploy:
		return domain.DeployEnvPreProd
	case domain.PipelineTriggerProdDeploy:
		return domain.DeployEnvProd
	}
	return ""
}

func valueOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
