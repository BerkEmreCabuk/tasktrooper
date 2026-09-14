package repoprofile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repofacts"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeRepoStore struct {
	mu      sync.Mutex
	repo    domain.Repository
	updates []string
	getErr  error
}

func (f *fakeRepoStore) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return domain.Repository{}, f.getErr
	}
	if id != f.repo.ID {
		return domain.Repository{}, errors.New("not found")
	}
	return f.repo, nil
}

func (f *fakeRepoStore) UpdateProfile(_ context.Context, id uuid.UUID, profileMD string) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != f.repo.ID {
		return domain.Repository{}, errors.New("not found")
	}
	now := time.Now()
	f.repo.ProfileMD = profileMD
	f.repo.ProfileUpdatedAt = &now
	f.updates = append(f.updates, profileMD)
	return f.repo, nil
}

func (f *fakeRepoStore) UpdateMeta(_ context.Context, id uuid.UUID, kind *string, subRepoKinds *[]string, autoRelease *bool) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != f.repo.ID {
		return domain.Repository{}, errors.New("not found")
	}
	if kind != nil {
		f.repo.Kind = *kind
	}
	if subRepoKinds != nil {
		f.repo.SubRepoKinds = *subRepoKinds
	}
	if autoRelease != nil {
		f.repo.AutoReleaseOnDone = *autoRelease
	}
	return f.repo, nil
}

func (f *fakeRepoStore) Update(_ context.Context, id uuid.UUID, name, description string, verify, build, test *string, _ *bool) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != f.repo.ID {
		return domain.Repository{}, errors.New("not found")
	}
	f.repo.Name, f.repo.Description = name, description
	if verify != nil {
		f.repo.VerifyCommand = *verify
	}
	if build != nil {
		f.repo.BuildCommand = *build
	}
	if test != nil {
		f.repo.TestCommand = *test
	}
	return f.repo, nil
}

func (f *fakeRepoStore) updateCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.updates)
}

// fakeProfileStore is an in-memory stand-in for the sections/proposals tables.
type fakeProfileStore struct {
	mu        sync.Mutex
	sections  map[string]domain.ProfileSection
	proposals []domain.ProfileProposal
}

func newFakeProfileStore() *fakeProfileStore {
	return &fakeProfileStore{sections: map[string]domain.ProfileSection{}}
}

func (f *fakeProfileStore) ListSections(context.Context, uuid.UUID) ([]domain.ProfileSection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.ProfileSection, 0, len(f.sections))
	for _, s := range f.sections {
		out = append(out, s)
	}
	domain.SortProfileSections(out)
	return out, nil
}

func (f *fakeProfileStore) UpsertSections(_ context.Context, repositoryID uuid.UUID, sections []domain.ProfileSection) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range sections {
		s.RepositoryID = repositoryID
		s.UpdatedAt = time.Now()
		s.Stale = false
		f.sections[s.Section] = s
	}
	return nil
}

func (f *fakeProfileStore) DeleteSections(_ context.Context, _ uuid.UUID, sections []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range sections {
		delete(f.sections, s)
	}
	return nil
}

func (f *fakeProfileStore) MarkStale(_ context.Context, _ uuid.UUID, changed []string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for name, sec := range f.sections {
		for _, src := range sec.SourcePaths {
			for _, c := range changed {
				if src == c && !sec.Stale {
					sec.Stale = true
					f.sections[name] = sec
					out = append(out, name)
				}
			}
		}
	}
	return out, nil
}

func (f *fakeProfileStore) ListProposals(context.Context, uuid.UUID) ([]domain.ProfileProposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.ProfileProposal(nil), f.proposals...), nil
}

func (f *fakeProfileStore) ReplaceProposals(_ context.Context, _ uuid.UUID, proposals []domain.ProfileProposal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.proposals = append([]domain.ProfileProposal(nil), proposals...)
	for i := range f.proposals {
		if f.proposals[i].ID == uuid.Nil {
			f.proposals[i].ID = uuid.New()
		}
	}
	return nil
}

func (f *fakeProfileStore) GetProposal(_ context.Context, id uuid.UUID) (domain.ProfileProposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.proposals {
		if p.ID == id {
			return p, nil
		}
	}
	return domain.ProfileProposal{}, errors.New("not found")
}

func (f *fakeProfileStore) SetProposalStatus(_ context.Context, id uuid.UUID, status domain.ProfileProposalStatus) (domain.ProfileProposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range f.proposals {
		if p.ID == id {
			f.proposals[i].Status = status
			return f.proposals[i], nil
		}
	}
	return domain.ProfileProposal{}, errors.New("not found")
}

// stubLoop routes every Run through fn; calls/messages are recorded so tests
// can assert the retry appended its reminder.
type stubLoop struct {
	mu    sync.Mutex
	calls [][]domain.Message
	fn    func(ctx context.Context, messages []domain.Message) (domain.AgentResponse, error)
}

func (s *stubLoop) Run(ctx context.Context, messages []domain.Message, _ string, _ domain.LLMProviderType, _ domain.ToolPolicy, _ ...agent.RunOption) (domain.AgentResponse, error) {
	s.mu.Lock()
	s.calls = append(s.calls, messages)
	s.mu.Unlock()
	return s.fn(ctx, messages)
}

func (s *stubLoop) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// newFixture builds a repo whose RootPath is a real temp dir, so evidence
// validation has something to validate against.
func newFixture(t *testing.T, profileMD string, updatedAt *time.Time) (*fakeRepoStore, *fakeProfileStore, uuid.UUID) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("seed working copy: %v", err)
	}
	id := uuid.New()
	return &fakeRepoStore{repo: domain.Repository{
		ID: id, Name: "demo", RootPath: root, Kind: domain.RepoKindBackend,
		ProfileMD: profileMD, ProfileUpdatedAt: updatedAt,
	}}, newFakeProfileStore(), id
}

func writeSection(ctx context.Context, svc *Service, id uuid.UUID, body string) ([]SectionResult, error) {
	return svc.ApplySections(ctx, id, []SectionWrite{{
		Section:  domain.ProfileSectionConventions,
		BodyMD:   body,
		Evidence: []domain.ProfileEvidence{{Path: "main.go"}},
	}})
}

// TestRefreshDedup pins the in-flight ledger: while one refresh runs, a second
// synchronous Refresh answers ErrRefreshInFlight and RefreshAsync is a no-op;
// once the run ends the slot is free again.
func TestRefreshDedup(t *testing.T) {
	store, profiles, id := newFixture(t, "", nil)
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var svc *Service
	loop := &stubLoop{fn: func(ctx context.Context, _ []domain.Message) (domain.AgentResponse, error) {
		if _, err := writeSection(ctx, svc, id, "conventions v1"); err != nil {
			t.Errorf("tool write-through failed: %v", err)
		}
		started <- struct{}{}
		<-release
		return domain.AgentResponse{}, nil
	}}
	svc = NewService(store, profiles, loop)

	done := make(chan error, 1)
	go func() { done <- svc.Refresh(context.Background(), id, "manual") }()
	<-started

	if err := svc.Refresh(context.Background(), id, "manual"); !errors.Is(err, ErrRefreshInFlight) {
		t.Fatalf("second Refresh while running = %v, want ErrRefreshInFlight", err)
	}
	if svc.RefreshAsync(context.Background(), id, "manual") {
		t.Fatal("RefreshAsync while running must report already_running (false)")
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first refresh failed: %v", err)
	}
	// Slot released: a new refresh may start.
	if err := svc.Refresh(context.Background(), id, "manual"); err != nil {
		t.Fatalf("refresh after completion failed: %v", err)
	}
}

// TestRefreshSucceedsWhenSectionLands: one pass, a section stored, no retry,
// and the run context carries the repo root plus the repository id.
func TestRefreshSucceedsWhenSectionLands(t *testing.T) {
	store, profiles, id := newFixture(t, "", nil)
	var svc *Service
	loop := &stubLoop{fn: func(ctx context.Context, _ []domain.Message) (domain.AgentResponse, error) {
		if got := registry.EffectiveWorkspaceDir(ctx); got != store.repo.RootPath {
			t.Errorf("workspace dir in run context = %q, want %q", got, store.repo.RootPath)
		}
		if got := registry.RepositoryIDFromContext(ctx); got != id {
			t.Errorf("repository id in run context = %s, want %s", got, id)
		}
		if _, err := writeSection(ctx, svc, id, "handlers always take a ctx first"); err != nil {
			t.Errorf("section write failed: %v", err)
		}
		return domain.AgentResponse{}, nil
	}}
	svc = NewService(store, profiles, loop)

	if err := svc.Refresh(context.Background(), id, "import"); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if got := loop.callCount(); got != 1 {
		t.Fatalf("loop calls = %d, want 1 (no retry after a successful write)", got)
	}
	if !strings.Contains(store.repo.ProfileMD, "handlers always take a ctx first") {
		t.Fatalf("rendered profile missing the section body: %q", store.repo.ProfileMD)
	}
}

// TestRefreshRetriesThenGivesUp: a run that never stores a section gets exactly
// one retry carrying the reminder, then reports failure.
func TestRefreshRetriesThenGivesUp(t *testing.T) {
	store, profiles, id := newFixture(t, "old profile", nil)
	loop := &stubLoop{fn: func(context.Context, []domain.Message) (domain.AgentResponse, error) {
		return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "done (it was not)"}}, nil
	}}
	svc := NewService(store, profiles, loop)

	if err := svc.Refresh(context.Background(), id, "push"); err == nil {
		t.Fatal("refresh without a section write must report failure")
	}
	if got := loop.callCount(); got != 2 {
		t.Fatalf("loop calls = %d, want 2 (first pass + one reminder retry)", got)
	}
	retry := loop.calls[1]
	last := retry[len(retry)-1]
	if last.Role != domain.RoleUser || !strings.Contains(last.Content, "update_project_profile") {
		t.Fatalf("retry must end with the reminder, got %q (%s)", last.Content, last.Role)
	}
}

// TestDerivedSectionsSurviveAModelFailure is the point of collecting facts
// before the model runs: the stack/commands/git half must be stored even when
// the LLM pass produces nothing.
func TestDerivedSectionsSurviveAModelFailure(t *testing.T) {
	store, profiles, id := newFixture(t, "", nil)
	if err := os.WriteFile(filepath.Join(store.repo.RootPath, "go.mod"), []byte("module demo\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("seed go.mod: %v", err)
	}
	loop := &stubLoop{fn: func(context.Context, []domain.Message) (domain.AgentResponse, error) {
		return domain.AgentResponse{}, errors.New("provider is down")
	}}
	svc := NewService(store, profiles, loop)

	_ = svc.Refresh(context.Background(), id, "manual")

	sections, _ := profiles.ListSections(context.Background(), id)
	var haveStack, haveCommands bool
	for _, s := range sections {
		switch s.Section {
		case domain.ProfileSectionStack:
			haveStack = s.Origin == domain.ProfileOriginDerived
		case domain.ProfileSectionCommands:
			haveCommands = strings.Contains(s.BodyMD, "go build ./...")
		}
	}
	if !haveStack || !haveCommands {
		t.Fatalf("derived sections must survive a failed model pass; got %d sections (stack=%v commands=%v)", len(sections), haveStack, haveCommands)
	}
}

// TestApplySectionsRejectsUnverifiableClaims: no evidence, evidence that does
// not exist, a derived section and a traversal path are all refused.
func TestApplySectionsRejectsUnverifiableClaims(t *testing.T) {
	store, profiles, id := newFixture(t, "", nil)
	svc := NewService(store, profiles, &stubLoop{fn: func(context.Context, []domain.Message) (domain.AgentResponse, error) {
		return domain.AgentResponse{}, nil
	}})
	ctx := context.Background()

	results, err := svc.ApplySections(ctx, id, []SectionWrite{
		{Section: domain.ProfileSectionConventions, BodyMD: "use typescript for type safety"},
		{Section: domain.ProfileSectionInvariants, BodyMD: "tenant id is checked", Evidence: []domain.ProfileEvidence{{Path: "does/not/exist.go"}}},
		{Section: domain.ProfileSectionStack, BodyMD: "next.js on vercel", Evidence: []domain.ProfileEvidence{{Path: "main.go"}}},
		{Section: domain.ProfileSectionGotchas, BodyMD: "secrets live here", Evidence: []domain.ProfileEvidence{{Path: "../../etc/passwd"}}},
		{Section: domain.ProfileSectionPurpose, BodyMD: "the bridge service", Evidence: []domain.ProfileEvidence{{Path: "main.go", Line: 1}}},
	})
	if err != nil {
		t.Fatalf("ApplySections: %v", err)
	}
	accepted := map[string]bool{}
	for _, r := range results {
		accepted[r.Section] = r.Accepted
	}
	for _, section := range []string{domain.ProfileSectionConventions, domain.ProfileSectionInvariants, domain.ProfileSectionStack, domain.ProfileSectionGotchas} {
		if accepted[section] {
			t.Errorf("section %q must be rejected", section)
		}
	}
	if !accepted[domain.ProfileSectionPurpose] {
		t.Error("a section with a real evidence path must be accepted")
	}

	sections, _ := profiles.ListSections(ctx, id)
	for _, s := range sections {
		if s.Section == domain.ProfileSectionStack && s.Origin == domain.ProfileOriginAgent {
			t.Fatal("an agent must never be able to overwrite a derived section")
		}
	}
}

// TestProposalsAutoApplyOnlyIntoEmptyFields: an empty setting is filled, a
// human-set one is offered instead.
func TestProposalsAutoApplyOnlyIntoEmptyFields(t *testing.T) {
	store, profiles, id := newFixture(t, "", nil)
	store.repo.Kind = domain.RepoKindFrontend // already chosen by a human
	svc := NewService(store, profiles, &stubLoop{fn: func(context.Context, []domain.Message) (domain.AgentResponse, error) {
		return domain.AgentResponse{}, nil
	}})

	facts := repofacts.Facts{
		Kind:        domain.RepoKindBackend,
		CollectedAt: time.Now(),
		Commands:    []repofacts.Command{{Purpose: "test", Cmd: "go test ./...", Source: "go.mod"}},
		Manifests:   []repofacts.Manifest{{Path: "go.mod", Ecosystem: "go"}},
		FileCount:   3,
	}
	svc.syncProposals(context.Background(), store.repo, facts)

	proposals, _ := profiles.ListProposals(context.Background(), id)
	byField := map[string]domain.ProfileProposal{}
	for _, p := range proposals {
		byField[p.Field] = p
	}
	if got := byField[domain.ProposalFieldRepoKind].Status; got != domain.ProposalPending {
		t.Fatalf("kind proposal over a human choice = %q, want pending", got)
	}
	if store.repo.Kind != domain.RepoKindFrontend {
		t.Fatalf("a set kind must not be overwritten, got %q", store.repo.Kind)
	}
	if got := byField[domain.ProposalFieldTestCommand].Status; got != domain.ProposalApplied {
		t.Fatalf("test command proposal into an empty field = %q, want applied", got)
	}
	if store.repo.TestCommand != "go test ./..." {
		t.Fatalf("empty test command must be filled, got %q", store.repo.TestCommand)
	}
}

// TestScopeInstruction: a push that touched only derived sources gives the
// model the small job, one that touched an agent section names it.
func TestScopeInstruction(t *testing.T) {
	if got := scopeInstruction(nil); got != "" {
		t.Fatalf("no stale sections must add no instruction, got %q", got)
	}
	derivedOnly := scopeInstruction([]string{domain.ProfileSectionStack, domain.ProfileSectionCommands})
	if !strings.Contains(derivedOnly, "rebuilt from the tree") {
		t.Fatalf("derived-only scope instruction wrong: %q", derivedOnly)
	}
	agentScoped := scopeInstruction([]string{domain.ProfileSectionStack, domain.ProfileSectionInvariants})
	if !strings.Contains(agentScoped, domain.ProfileSectionInvariants) || strings.Contains(agentScoped, domain.ProfileSectionStack) {
		t.Fatalf("scoped instruction must name only agent sections, got %q", agentScoped)
	}
}

// TestProfileStale pins the full-rebuild window.
func TestProfileStale(t *testing.T) {
	now := time.Now()
	if !ProfileStale(nil, now) {
		t.Fatal("a never-built profile must be stale")
	}
	fresh := now.Add(-24 * time.Hour)
	if ProfileStale(&fresh, now) {
		t.Fatal("a day-old profile is inside the window")
	}
	boundary := now.Add(-profileMaxAge)
	if ProfileStale(&boundary, now) {
		t.Fatal("exactly at the window is still fresh (strictly older required)")
	}
	stale := now.Add(-profileMaxAge - time.Hour)
	if !ProfileStale(&stale, now) {
		t.Fatal("older than the window must be stale")
	}
}

// TestBuildRefreshMessages: the facts block and the previous agent sections
// ride along, and the user message names the reason.
func TestBuildRefreshMessages(t *testing.T) {
	repo := domain.Repository{Name: "demo", Kind: domain.RepoKindFrontend}
	facts := repofacts.Facts{
		FileCount: 12,
		Manifests: []repofacts.Manifest{{Path: "package.json", Ecosystem: "npm", Manager: "pnpm"}},
	}
	existing := []domain.ProfileSection{{
		Section: domain.ProfileSectionConventions, BodyMD: "old brief", Origin: domain.ProfileOriginAgent,
	}}
	msgs := buildRefreshMessages(repo, "push", facts, existing, nil)
	if len(msgs) != 4 {
		t.Fatalf("messages = %d, want system + facts + previous sections + user", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "Repository kind: frontend") {
		t.Fatalf("system prompt must carry the kind, got %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[1].Content, "Verified repository facts") || !strings.Contains(msgs[1].Content, "pnpm") {
		t.Fatalf("facts block missing or empty: %q", msgs[1].Content)
	}
	if !strings.Contains(msgs[2].Content, "old brief") {
		t.Fatalf("previous sections message wrong: %q", msgs[2].Content)
	}
	if !strings.Contains(msgs[3].Content, "Reason: push") {
		t.Fatalf("user message must name the reason, got %q", msgs[3].Content)
	}

	repo.Kind = ""
	msgs = buildRefreshMessages(repo, "import", repofacts.Facts{}, nil, nil)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want system + user when there are no facts and no history", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "Repository kind: unspecified") {
		t.Fatalf("empty kind must read 'unspecified', got %q", msgs[0].Content)
	}
}
