// Package repoprofile maintains the per-repository "project profile": the
// durable brief injected into every repo-scoped agent run so agents stop
// re-deriving the basics on each dispatch.
//
// The profile is built in two halves that must not be mixed.
//
// The DERIVED half is collected by application/repofacts — language mix,
// manifests and their scripts, CI workflows and their triggers, hosting
// markers, migrations, test layout, git conventions, churn. A parser owns it
// because a model does not: asked for "build/test/run commands; deploy shape",
// a profiling run answered "npm run build" and "deploy using Vercel" for a Go
// monorepo that ships to GKE through .github/workflows/deploy.yml. Facts a
// parser can establish are never left to a model.
//
// The AGENT half is judgment a parser cannot produce — purpose, conventions,
// invariants, change recipes, danger zones — and every claim in it must carry
// an evidence path that exists in the working copy. Claims without one are
// rejected at the tool boundary, which is what stops the priors from coming
// back in through the other door.
//
// Sections are stored individually with the source files their truth depends
// on, so a push refreshes exactly the sections whose sources moved.
package repoprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repofacts"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// UpdateProfileToolName is the tool agents call to write the judgment half of
// a repository's profile. Declared here (not in the adapter) because the
// refresh policy and the role policies both need the name without importing
// the adapter.
const UpdateProfileToolName = "update_project_profile"

// profileMaxAge is the full-rebuild gate. Scoped refreshes keep the profile
// current between rebuilds, so this window is a backstop for drift a path
// diff cannot see (a dependency that changed meaning, a convention that
// eroded) rather than the primary freshness mechanism it used to be.
const profileMaxAge = 7 * 24 * time.Hour

// refreshTimeout bounds one background refresh run end to end.
const refreshTimeout = 15 * time.Minute

// factsTimeout bounds the deterministic collection pass. It walks the tree and
// shells out to git; on a huge working copy that is seconds, but it must never
// be able to eat the whole refresh budget.
const factsTimeout = 90 * time.Second

// architectAgentName is the catalog row whose model/provider the refresh run
// borrows. The profile prompt itself is fixed (below), not the agent's.
const architectAgentName = "system-architect"

// ErrRefreshInFlight reports that this repository's profile is already being
// rebuilt; the caller's request is a no-op.
var ErrRefreshInFlight = errors.New("a profile refresh is already running for this repository")

// RepositoryStore is the narrow slice of port.RepositoryStore the service
// needs: read the repo, write the rendered profile cache, and apply the
// settings a profiling pass can answer for itself.
type RepositoryStore interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	UpdateProfile(ctx context.Context, id uuid.UUID, profileMD string) (domain.Repository, error)
	UpdateMeta(ctx context.Context, id uuid.UUID, kind *string, subRepoKinds *[]string, autoReleaseOnDone *bool) (domain.Repository, error)
	Update(ctx context.Context, id uuid.UUID, name, description string, verifyCommand, buildCommand, testCommand *string, requireHumanReview *bool) (domain.Repository, error)
}

// AgentLoop is the LLM↔tool cycle the refresh borrows — satisfied by
// *agent.Loop; an interface so tests can stub the run. The variadic opts
// param exists only so this signature keeps matching *agent.Loop.Run's; the
// refresh itself never passes one, so its runs use plain model throughout.
type AgentLoop interface {
	Run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...agent.RunOption) (domain.AgentResponse, error)
}

// PipelineJobStore is the slice of port.RepositoryPipelineJobStore needed to
// apply a pipeline-slot proposal without clobbering the other slots.
type PipelineJobStore interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepositoryPipelineJob, error)
	ReplaceForRepository(ctx context.Context, repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error)
}

type Service struct {
	repos     RepositoryStore
	profiles  port.RepositoryProfileStore
	loop      AgentLoop
	agents    func(ctx context.Context) ([]domain.Agent, error)
	pipelines PipelineJobStore

	mu       sync.Mutex
	inflight map[uuid.UUID]*refreshState

	// workflows/roles: see board.Dispatcher's own fields of the same name.
	workflows port.WorkflowReader
	roles     port.RoleResolver
}

func (s *Service) SetWorkflows(w port.WorkflowReader)  { s.workflows = w }
func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }

// refreshState is the per-repo in-flight ledger entry. written flips when an
// agent section lands during the run — the success signal the loop's own
// return value cannot carry.
type refreshState struct {
	written bool
}

func NewService(repos RepositoryStore, profiles port.RepositoryProfileStore, loop AgentLoop) *Service {
	return &Service{
		repos:    repos,
		profiles: profiles,
		loop:     loop,
		inflight: make(map[uuid.UUID]*refreshState),
	}
}

// SetAgentLister wires the catalog lookup used to borrow the
// system-architect's model/provider.
func (s *Service) SetAgentLister(f func(ctx context.Context) ([]domain.Agent, error)) {
	s.agents = f
}

// SetPipelineJobs wires the pipeline mapping store so a deploy-workflow
// proposal can be applied into the pipeline settings.
func (s *Service) SetPipelineJobs(store PipelineJobStore) { s.pipelines = store }

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// Sections returns the stored profile sections in display order.
func (s *Service) Sections(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProfileSection, error) {
	if s.profiles == nil {
		return nil, nil
	}
	return s.profiles.ListSections(ctx, repositoryID)
}

// Proposals returns every settings proposal (pending, applied and dismissed)
// the profiling passes have produced for the repository.
func (s *Service) Proposals(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProfileProposal, error) {
	if s.profiles == nil {
		return nil, nil
	}
	return s.profiles.ListProposals(ctx, repositoryID)
}

// InjectionProfile renders the profile a run should receive. kind narrows it
// to the sections that matter for the area being worked on; "" returns all.
func (s *Service) InjectionProfile(ctx context.Context, repositoryID uuid.UUID, kind string) string {
	sections, err := s.Sections(ctx, repositoryID)
	if err != nil || len(sections) == 0 {
		return ""
	}
	return domain.RenderProfileMarkdown(domain.SelectProfileSections(sections, kind))
}

// ---------------------------------------------------------------------------
// Writes from the update_project_profile tool
// ---------------------------------------------------------------------------

// SectionWrite is one section an agent is trying to store.
type SectionWrite struct {
	Section  string
	BodyMD   string
	Evidence []domain.ProfileEvidence
}

// SectionResult reports what happened to one attempted section write.
type SectionResult struct {
	Section  string `json:"section"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

// ApplySections validates and stores agent-written sections. A section is
// refused when it names a derived (parser-owned) section, when it carries no
// evidence, or when none of its evidence paths exist in the working copy.
//
// The rejection is the point. An unverifiable claim in the profile is
// inherited by every future run of every agent on this repository, so the
// cost of accepting a plausible-sounding invention is paid forever, while the
// cost of refusing one is a single retry.
func (s *Service) ApplySections(ctx context.Context, repositoryID uuid.UUID, writes []SectionWrite) ([]SectionResult, error) {
	if s.profiles == nil {
		return nil, errors.New("profile sections are not available on this deployment")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return nil, err
	}

	results := make([]SectionResult, 0, len(writes))
	accepted := make([]domain.ProfileSection, 0, len(writes))
	for _, w := range writes {
		section := strings.TrimSpace(w.Section)
		body := strings.TrimSpace(w.BodyMD)
		switch {
		case !domain.ValidProfileSection(section):
			results = append(results, SectionResult{Section: section, Reason: "unknown section; allowed: " + strings.Join(domain.AgentWritableProfileSections(), ", ")})
			continue
		case !isAgentWritable(section):
			results = append(results, SectionResult{Section: section, Reason: "this section is derived from the tree by the platform and cannot be written by an agent"})
			continue
		case body == "":
			results = append(results, SectionResult{Section: section, Reason: "body_md is empty"})
			continue
		}

		valid, invalid := validateEvidence(repo.RootPath, w.Evidence)
		if len(valid) == 0 {
			reason := "every section needs at least one evidence path that exists in the repository"
			if len(invalid) > 0 {
				reason += "; not found: " + strings.Join(invalid, ", ")
			}
			results = append(results, SectionResult{Section: section, Reason: reason})
			continue
		}

		res := SectionResult{Section: section, Accepted: true}
		if len(invalid) > 0 {
			res.Reason = "stored without unverifiable evidence paths: " + strings.Join(invalid, ", ")
		}
		results = append(results, res)
		accepted = append(accepted, domain.ProfileSection{
			Section:      section,
			BodyMD:       body,
			Evidence:     valid,
			SourcePaths:  evidencePaths(valid),
			SourceCommit: headCommit(ctx, repo.RootPath),
			Origin:       domain.ProfileOriginAgent,
		})
	}

	if len(accepted) > 0 {
		if err := s.profiles.UpsertSections(ctx, repositoryID, accepted); err != nil {
			return nil, err
		}
		s.markWritten(repositoryID)
		if err := s.renderCache(ctx, repositoryID); err != nil {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("profile cache render failed")
		}
	}
	return results, nil
}

// UpdateProfile is the legacy single-markdown write-through, kept so an agent
// (or an older tool call shape) that sends one blob still records something.
// It lands in the notes section rather than replacing the profile: a blob has
// no evidence and no per-fact sources, and letting it overwrite the sectioned
// profile would undo everything this package exists to guarantee.
func (s *Service) UpdateProfile(ctx context.Context, repositoryID uuid.UUID, content string) (domain.Repository, error) {
	if s.profiles == nil {
		return s.repos.UpdateProfile(ctx, repositoryID, content)
	}
	sec := domain.ProfileSection{
		Section: domain.ProfileSectionNotes,
		BodyMD:  strings.TrimSpace(content),
		Origin:  domain.ProfileOriginAgent,
	}
	if err := s.profiles.UpsertSections(ctx, repositoryID, []domain.ProfileSection{sec}); err != nil {
		return domain.Repository{}, err
	}
	s.markWritten(repositoryID)
	if err := s.renderCache(ctx, repositoryID); err != nil {
		return domain.Repository{}, err
	}
	return s.repos.Get(ctx, repositoryID)
}

// renderCache regenerates repositories.profile_md from the stored sections.
// The column stays the single read path for the settings UI and for every
// existing injection site, so nothing else has to learn about sections.
func (s *Service) renderCache(ctx context.Context, repositoryID uuid.UUID) error {
	sections, err := s.profiles.ListSections(ctx, repositoryID)
	if err != nil {
		return err
	}
	_, err = s.repos.UpdateProfile(ctx, repositoryID, domain.RenderProfileMarkdown(sections))
	return err
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

// Refresh rebuilds the profile synchronously. ErrRefreshInFlight when another
// refresh for the same repository is already running.
func (s *Service) Refresh(ctx context.Context, repositoryID uuid.UUID, reason string) error {
	st, ok := s.begin(repositoryID)
	if !ok {
		return ErrRefreshInFlight
	}
	defer s.end(repositoryID)
	return s.run(ctx, repositoryID, reason, nil, st)
}

// RefreshAsync kicks a background rebuild. false = one is already running and
// this request was a no-op ("already_running" to the HTTP caller).
func (s *Service) RefreshAsync(ctx context.Context, repositoryID uuid.UUID, reason string) bool {
	return s.refreshAsyncScoped(ctx, repositoryID, reason, nil)
}

// refreshAsyncScoped takes the CALLER's context and strips its cancellation
// (context.WithoutCancel): the rebuild outlives the request that asked for
// it, so it cannot hold the request's cancellation. It once ran on a bare
// context.Background(), so on a live stack every repository import produced
// "profile refresh failed: tenant: no tenant in context" and no profile.
func (s *Service) refreshAsyncScoped(ctx context.Context, repositoryID uuid.UUID, reason string, only []string) bool {
	st, ok := s.begin(repositoryID)
	if !ok {
		return false
	}
	go func() {
		defer s.end(repositoryID)
		// A panic in a background rebuild must never take the process down —
		// import and push webhooks fire this without anyone watching.
		defer func() {
			if r := recover(); r != nil {
				log.Error().Any("panic", r).Str("repository_id", repositoryID.String()).Msg("profile refresh panicked")
			}
		}()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
		defer cancel()
		if err := s.run(ctx, repositoryID, reason, only, st); err != nil {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("reason", reason).Msg("profile refresh failed")
		}
	}()
	return true
}

// RefreshForChangedPaths is the push-webhook entry point. It marks the
// sections whose sources the push touched and rebuilds only those — a commit
// in apps/mobile no longer costs a full re-profile of the backend, and,
// unlike the old age gate, a commit that changes the deploy workflow is
// picked up immediately instead of up to six hours later.
//
// A profile that was never built, or one older than profileMaxAge, still gets
// the full pass.
func (s *Service) RefreshForChangedPaths(ctx context.Context, repositoryID uuid.UUID, reason string, changedPaths []string) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("profile staleness check: repository lookup failed")
		return
	}
	if ProfileStale(repo.ProfileUpdatedAt, time.Now()) {
		s.RefreshAsync(ctx, repositoryID, reason)
		return
	}
	if s.profiles == nil || len(changedPaths) == 0 {
		return
	}
	stale, err := s.profiles.MarkStale(ctx, repositoryID, changedPaths)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("profile staleness marking failed")
		return
	}
	if len(stale) == 0 {
		return
	}
	log.Info().Str("repository", repo.Name).Strs("sections", stale).Msg("profile sections went stale; scoped refresh queued")
	s.refreshAsyncScoped(ctx, repositoryID, reason, stale)
}

// RefreshAfterPush is what the push webhook calls once a reindex lands. It
// works out for itself which files moved — every section records the commit it
// was written against, so the diff between that commit and HEAD is the exact
// change set — and refreshes only the sections that diff touches.
func (s *Service) RefreshAfterPush(ctx context.Context, repositoryID uuid.UUID, reason string) {
	if s.profiles == nil {
		s.RefreshIfStale(ctx, repositoryID, reason)
		return
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("push profile refresh: repository lookup failed")
		return
	}
	if ProfileStale(repo.ProfileUpdatedAt, time.Now()) {
		s.RefreshAsync(ctx, repositoryID, reason)
		return
	}
	sections, err := s.profiles.ListSections(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("push profile refresh: section lookup failed")
		return
	}
	base := newestSourceCommit(sections)
	if base == "" {
		// Sections predate commit stamping (or the working copy is not a git
		// repo): fall back to the age gate rather than refreshing blindly.
		s.RefreshIfStale(ctx, repositoryID, reason)
		return
	}
	changed := changedPathsSince(ctx, repo.RootPath, base)
	if len(changed) == 0 {
		return
	}
	s.RefreshForChangedPaths(ctx, repositoryID, reason, changed)
}

func newestSourceCommit(sections []domain.ProfileSection) string {
	newest := ""
	var newestAt time.Time
	for _, s := range sections {
		if s.SourceCommit == "" {
			continue
		}
		if newest == "" || s.UpdatedAt.After(newestAt) {
			newest, newestAt = s.SourceCommit, s.UpdatedAt
		}
	}
	return newest
}

// RefreshIfStale keeps the old age-gated entry point for callers with no path
// list (import completion, manual reindex).
func (s *Service) RefreshIfStale(ctx context.Context, repositoryID uuid.UUID, reason string) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("profile staleness check: repository lookup failed")
		return
	}
	if !ProfileStale(repo.ProfileUpdatedAt, time.Now()) {
		return
	}
	s.RefreshAsync(ctx, repositoryID, reason)
}

// ProfileStale reports whether a profile needs a full rebuild: never written,
// or older than the freshness window.
func ProfileStale(updatedAt *time.Time, now time.Time) bool {
	return updatedAt == nil || now.Sub(*updatedAt) > profileMaxAge
}

func (s *Service) begin(repositoryID uuid.UUID) (*refreshState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, running := s.inflight[repositoryID]; running {
		return nil, false
	}
	st := &refreshState{}
	s.inflight[repositoryID] = st
	return st, true
}

func (s *Service) end(repositoryID uuid.UUID) {
	s.mu.Lock()
	delete(s.inflight, repositoryID)
	s.mu.Unlock()
}

func (s *Service) markWritten(repositoryID uuid.UUID) {
	s.mu.Lock()
	if st, ok := s.inflight[repositoryID]; ok {
		st.written = true
	}
	s.mu.Unlock()
}

func (s *Service) wasWritten(st *refreshState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return st.written
}

// run is the synchronous core: collect the facts, store them as the derived
// sections, turn what they imply into settings (applied or proposed), then
// spend the model only on the judgment half.
//
// The deterministic pass is committed BEFORE the model runs. A refresh whose
// LLM call fails still leaves the repository with an accurate stack, command,
// CI and deploy picture — which is most of the profile's day-to-day value and
// used to be lost entirely whenever the model errored.
func (s *Service) run(ctx context.Context, repositoryID uuid.UUID, reason string, only []string, st *refreshState) error {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("profile refresh: %w", err)
	}

	facts := s.collectFacts(ctx, repo)
	if s.profiles != nil {
		if derived := repofacts.DerivedSections(facts); len(derived) > 0 {
			if err := s.profiles.UpsertSections(ctx, repositoryID, derived); err != nil {
				log.Warn().Err(err).Str("repository", repo.Name).Msg("profile refresh: storing derived sections failed")
			} else if err := s.renderCache(ctx, repositoryID); err != nil {
				log.Warn().Err(err).Str("repository", repo.Name).Msg("profile refresh: cache render failed")
			}
		}
		s.syncProposals(ctx, repo, facts)
	}

	architect := s.architect(ctx)

	// The refresh runs against repository.RootPath — the shared working copy
	// the indexer also walks — with READ-ONLY tools, so no branch checkout or
	// private workspace is needed. AgentID makes save_memory attributable.
	runCtx := registry.ContextWithWorkspaceDir(ctx, repo.RootPath)
	runCtx = registry.ContextWithRepositoryID(runCtx, repo.ID)
	if architect.ID != uuid.Nil {
		runCtx = registry.ContextWithAgentID(runCtx, architect.ID)
	}

	policy := refreshToolPolicy()
	existing, _ := s.Sections(ctx, repositoryID)
	messages := buildRefreshMessages(repo, reason, facts, existing, only)

	// s.loop is the router in production, so an architect agent on a
	// host-executed provider profiles the repository with that host's CLI —
	// started in repo.RootPath, which runCtx already carries.
	label := agent.WithCLILabel("profile:"+repo.Name, "repository profile refresh")
	if _, err := s.loop.Run(runCtx, messages, architect.Model, architect.ProviderType, policy, label); err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("profile refresh: first pass errored")
	}
	if s.wasWritten(st) {
		log.Info().Str("repository", repo.Name).Str("reason", reason).Msg("project profile updated")
		return nil
	}

	messages = append(messages, domain.Message{Role: domain.RoleUser, Content: profileReminderMessage})
	if _, err := s.loop.Run(runCtx, messages, architect.Model, architect.ProviderType, policy, label); err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("profile refresh: retry pass errored")
	}
	if s.wasWritten(st) {
		log.Info().Str("repository", repo.Name).Str("reason", reason).Msg("project profile updated on retry")
		return nil
	}

	log.Warn().Str("repository", repo.Name).Str("reason", reason).
		Msg("profile refresh: no agent section landed; the derived half is still current")
	return fmt.Errorf("profile refresh for %q ended without an agent-written section", repo.Name)
}

func (s *Service) collectFacts(ctx context.Context, repo domain.Repository) repofacts.Facts {
	fctx, cancel := context.WithTimeout(ctx, factsTimeout)
	defer cancel()
	facts := repofacts.Collect(fctx, repo.RootPath)
	for _, w := range facts.Warnings {
		log.Debug().Str("repository", repo.Name).Str("warning", w).Msg("profile facts collection")
	}
	return facts
}

// architect resolves the system-architect catalog row; a zero Agent (default
// model/provider, no agent context) keeps the refresh alive when the catalog
// is unavailable or the role was renamed.
func (s *Service) architect(ctx context.Context) domain.Agent {
	if s.agents == nil {
		return domain.Agent{}
	}
	agents, err := s.agents(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("profile refresh: agent catalog lookup failed, using default model")
		return domain.Agent{}
	}
	for _, a := range agents {
		if a.Name == architectAgentName {
			return a
		}
	}
	log.Warn().Str("agent", architectAgentName).Msg("profile refresh: architect agent not found, using default model")
	return domain.Agent{}
}

// refreshToolPolicy is the refresh run's allowlist: the read-only exploration
// tools plus the profile write-through and the memory pair the prompt names.
func refreshToolPolicy() domain.ToolPolicy {
	tools := append([]string(nil), domain.CodeExplorationTools...)
	tools = append(tools, UpdateProfileToolName, "save_memory", "search_memory")
	return domain.ToolPolicy{AllowTools: tools}
}

func isAgentWritable(section string) bool {
	for _, s := range domain.AgentWritableProfileSections() {
		if s == section {
			return true
		}
	}
	return false
}

func evidencePaths(evidence []domain.ProfileEvidence) []string {
	out := make([]string, 0, len(evidence))
	for _, e := range evidence {
		out = append(out, e.Path)
	}
	return out
}

// marshalValue is the small helper the proposal builders share.
func marshalValue(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return raw
}
