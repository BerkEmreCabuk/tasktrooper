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

const UpdateProfileToolName = "update_project_profile"

const profileMaxAge = 7 * 24 * time.Hour

const refreshTimeout = 15 * time.Minute

const factsTimeout = 90 * time.Second

var ErrRefreshInFlight = errors.New("a profile refresh is already running for this repository")

type RepositoryStore interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	UpdateProfile(ctx context.Context, id uuid.UUID, profileMD string) (domain.Repository, error)
	UpdateMeta(ctx context.Context, id uuid.UUID, kind *string, subRepoKinds *[]string, autoReleaseOnDone *bool) (domain.Repository, error)
	Update(ctx context.Context, id uuid.UUID, name, description string, verifyCommand, buildCommand, testCommand *string, requireHumanReview *bool) (domain.Repository, error)
}

type AgentLoop interface {
	Run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...agent.RunOption) (domain.AgentResponse, error)
}

type PipelineJobStore interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepositoryPipelineJob, error)
	ReplaceForRepository(ctx context.Context, repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error)
}

type Service struct {
	repos     RepositoryStore
	profiles  port.RepositoryProfileStore
	loop      AgentLoop
	agents    AgentGetter
	pipelines PipelineJobStore

	mu       sync.Mutex
	inflight map[uuid.UUID]*refreshState

	workflows port.WorkflowReader
	roles     port.RoleResolver
}

func (s *Service) SetWorkflows(w port.WorkflowReader)  { s.workflows = w }
func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }

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

type AgentGetter interface {
	GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error)
}

func (s *Service) SetAgentGetter(g AgentGetter) {
	s.agents = g
}

func (s *Service) SetPipelineJobs(store PipelineJobStore) { s.pipelines = store }

func (s *Service) Sections(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProfileSection, error) {
	if s.profiles == nil {
		return nil, nil
	}
	return s.profiles.ListSections(ctx, repositoryID)
}

func (s *Service) Proposals(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProfileProposal, error) {
	if s.profiles == nil {
		return nil, nil
	}
	return s.profiles.ListProposals(ctx, repositoryID)
}

func (s *Service) InjectionProfile(ctx context.Context, repositoryID uuid.UUID, kind string) string {
	sections, err := s.Sections(ctx, repositoryID)
	if err != nil || len(sections) == 0 {
		return ""
	}
	return domain.RenderProfileMarkdown(domain.SelectProfileSections(sections, kind))
}

type SectionWrite struct {
	Section  string
	BodyMD   string
	Evidence []domain.ProfileEvidence
}

type SectionResult struct {
	Section  string `json:"section"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

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

func (s *Service) renderCache(ctx context.Context, repositoryID uuid.UUID) error {
	sections, err := s.profiles.ListSections(ctx, repositoryID)
	if err != nil {
		return err
	}
	_, err = s.repos.UpdateProfile(ctx, repositoryID, domain.RenderProfileMarkdown(sections))
	return err
}

func (s *Service) Refresh(ctx context.Context, repositoryID uuid.UUID, reason string) error {
	st, ok := s.begin(repositoryID)
	if !ok {
		return ErrRefreshInFlight
	}
	defer s.end(repositoryID)
	return s.run(ctx, repositoryID, reason, nil, st)
}

func (s *Service) RefreshAsync(ctx context.Context, repositoryID uuid.UUID, reason string) bool {
	return s.refreshAsyncScoped(ctx, repositoryID, reason, nil)
}

func (s *Service) refreshAsyncScoped(ctx context.Context, repositoryID uuid.UUID, reason string, only []string) bool {
	st, ok := s.begin(repositoryID)
	if !ok {
		return false
	}
	go func() {
		defer s.end(repositoryID)

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

	runCtx := registry.ContextWithWorkspaceDir(ctx, repo.RootPath)
	runCtx = registry.ContextWithRepositoryID(runCtx, repo.ID)
	if architect.ID != uuid.Nil {
		runCtx = registry.ContextWithAgentID(runCtx, architect.ID)
	}

	policy := refreshToolPolicy()
	existing, _ := s.Sections(ctx, repositoryID)
	messages := buildRefreshMessages(repo, reason, facts, existing, only)

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

func (s *Service) architect(ctx context.Context) domain.Agent {
	if s.agents == nil || s.roles == nil {
		return domain.Agent{}
	}
	id, err := s.roles.AgentForPurpose(ctx, domain.PurposeRepoProfiler, "")
	if err != nil || id == nil {
		if err != nil {
			log.Warn().Err(err).Msg("profile refresh: role resolver lookup failed, using default model")
		} else {
			log.Warn().Msg("profile refresh: repo_profiler purpose has no role/assignment, using default model")
		}
		return domain.Agent{}
	}
	agent, err := s.agents.GetAgent(ctx, *id)
	if err != nil {
		log.Warn().Err(err).Msg("profile refresh: agent catalog lookup failed, using default model")
		return domain.Agent{}
	}
	return agent
}

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

func marshalValue(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return raw
}
