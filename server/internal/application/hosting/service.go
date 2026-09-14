// Package hosting answers "where does this part of the repository actually
// live" and records the answer as a link to the provider's own object.
//
// It sits beside application/deploy rather than inside it: a deploy target is
// per environment and describes a recipe (which workflow ships prod, with which
// vars), while a hosting link is per AREA — the whole repository, or the
// frontend / backend half of a monorepo — and names a provider project. The
// two meet exactly once, in syncProdTarget: linking a single-kind repository
// to a Vercel project fills the prod target's empty fields so the deploy page,
// the production monitor and the deploy watch learn the address without a
// second form.
//
// Detection is deliberately evidence-first. A `.vercel/project.json` names the
// project outright; a Vercel project git-linked to this repository's remote
// (and, on a monorepo, built from this area's directory) is as good; a name
// match is a hint. Anything short of one decisive candidate is reported as
// ambiguous or none, and the UI asks the person — it never guesses a link.
package hosting

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repofacts"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ErrNotConnected means no Vercel token is stored: nothing provider-side can
// be looked up until Settings has one.
var ErrNotConnected = errors.New("Vercel is not connected — connect it from Settings")

// ErrInvalidInput wraps caller mistakes (bad area, bad provider, missing id).
var ErrInvalidInput = errors.New("invalid input")

// apiTimeout bounds one Vercel round trip; detectTimeout bounds the whole
// working-copy scan, which walks the tree and runs a few git commands.
const (
	apiTimeout    = 20 * time.Second
	detectTimeout = 60 * time.Second
)

// RepositoryResolver reads the repo whose hosting is being defined.
type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// Service owns the Vercel connection and every repository's hosting links.
type Service struct {
	links  port.HostingLinkStore
	repos  RepositoryResolver
	creds  port.VercelCredentialStore
	vercel port.VercelAPI
	// targets is optional: set, a root-area Vercel link also fills the prod
	// deploy target (see syncProdTarget).
	targets port.DeployTargetStore
	// collect is the working-copy scanner; a seam so tests can hand in facts
	// without a tree on disk.
	collect func(ctx context.Context, root string) repofacts.Facts
}

func NewService(links port.HostingLinkStore, repos RepositoryResolver, creds port.VercelCredentialStore, api port.VercelAPI) *Service {
	return &Service{links: links, repos: repos, creds: creds, vercel: api, collect: repofacts.Collect}
}

// SetDeployTargets enables the prod-target sync on root-area Vercel links.
func (s *Service) SetDeployTargets(t port.DeployTargetStore) { s.targets = t }

// SetFactsCollector replaces the working-copy scanner (tests).
func (s *Service) SetFactsCollector(fn func(ctx context.Context, root string) repofacts.Facts) {
	if fn != nil {
		s.collect = fn
	}
}

// --- connection -----------------------------------------------------------

func (s *Service) token(ctx context.Context) (string, error) {
	if s.creds == nil {
		return "", ErrNotConnected
	}
	tok, err := s.creds.VercelToken(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(tok) == "" {
		return "", ErrNotConnected
	}
	return tok, nil
}

// Status reports whether a token is stored and still accepted by Vercel, and
// the default team it acts in.
func (s *Service) Status(ctx context.Context) (domain.VercelConnectionStatus, error) {
	if s.creds == nil {
		return domain.VercelConnectionStatus{Detail: "credential store not configured"}, nil
	}
	tok, err := s.creds.VercelToken(ctx)
	if err != nil {
		return domain.VercelConnectionStatus{}, err
	}
	if strings.TrimSpace(tok) == "" {
		return domain.VercelConnectionStatus{}, nil
	}
	actx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	user, err := s.vercel.User(actx, tok)
	if err != nil {
		return domain.VercelConnectionStatus{Detail: "token is invalid or Vercel is unreachable: " + err.Error()}, nil
	}
	teamID, _ := s.creds.VercelTeam(ctx)
	st := domain.VercelConnectionStatus{Connected: true, Username: user.Username, Email: user.Email, TeamID: teamID}
	if teamID != "" {
		st.TeamSlug = s.teamSlug(actx, tok, teamID)
	}
	return st, nil
}

// teamSlug is best-effort: a slug is cosmetic and a Teams failure must not
// turn a working connection into an error.
func (s *Service) teamSlug(ctx context.Context, tok, teamID string) string {
	teams, err := s.vercel.Teams(ctx, tok)
	if err != nil {
		return ""
	}
	for _, t := range teams {
		if t.ID == teamID {
			return t.Slug
		}
	}
	return ""
}

// Connect verifies a token against /v2/user, stores it encrypted, and pins the
// default team (validated against the token's memberships; "" = personal).
func (s *Service) Connect(ctx context.Context, token, teamID string) (domain.VercelConnectionStatus, error) {
	if s.creds == nil {
		return domain.VercelConnectionStatus{}, errors.New("credential store not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return domain.VercelConnectionStatus{}, fmt.Errorf("%w: token is required", ErrInvalidInput)
	}
	actx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	if _, err := s.vercel.User(actx, token); err != nil {
		return domain.VercelConnectionStatus{}, fmt.Errorf("%w: token could not be verified: %v", ErrInvalidInput, err)
	}
	teamID = strings.TrimSpace(teamID)
	if teamID != "" {
		if err := s.assertTeam(actx, token, teamID); err != nil {
			return domain.VercelConnectionStatus{}, err
		}
	}
	if err := s.creds.SetVercelToken(ctx, token); err != nil {
		return domain.VercelConnectionStatus{}, err
	}
	if err := s.creds.SetVercelTeam(ctx, teamID); err != nil {
		return domain.VercelConnectionStatus{}, err
	}
	return s.Status(ctx)
}

// SetTeam changes the default scope of an existing connection.
func (s *Service) SetTeam(ctx context.Context, teamID string) (domain.VercelConnectionStatus, error) {
	tok, err := s.token(ctx)
	if err != nil {
		return domain.VercelConnectionStatus{}, err
	}
	teamID = strings.TrimSpace(teamID)
	if teamID != "" {
		actx, cancel := context.WithTimeout(ctx, apiTimeout)
		defer cancel()
		if err := s.assertTeam(actx, tok, teamID); err != nil {
			return domain.VercelConnectionStatus{}, err
		}
	}
	if err := s.creds.SetVercelTeam(ctx, teamID); err != nil {
		return domain.VercelConnectionStatus{}, err
	}
	return s.Status(ctx)
}

func (s *Service) assertTeam(ctx context.Context, tok, teamID string) error {
	teams, err := s.vercel.Teams(ctx, tok)
	if err != nil {
		return fmt.Errorf("listing Vercel teams: %w", err)
	}
	for _, t := range teams {
		if t.ID == teamID {
			return nil
		}
	}
	return fmt.Errorf("%w: team %q is not one this token belongs to", ErrInvalidInput, teamID)
}

// Disconnect forgets the token and its team. Links are kept: they record a
// fact about the repositories, not about who could look it up.
func (s *Service) Disconnect(ctx context.Context) error {
	if s.creds == nil {
		return nil
	}
	return s.creds.DeleteVercelToken(ctx)
}

func (s *Service) Teams(ctx context.Context) ([]domain.VercelTeam, error) {
	tok, err := s.token(ctx)
	if err != nil {
		return nil, err
	}
	actx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	teams, err := s.vercel.Teams(actx, tok)
	if err != nil {
		return nil, err
	}
	if teams == nil {
		teams = []domain.VercelTeam{}
	}
	return teams, nil
}

// Projects lists the projects in a scope — nil teamID means the connection's
// default team, an explicit "" the personal account.
func (s *Service) Projects(ctx context.Context, teamID *string) ([]domain.VercelProject, error) {
	tok, err := s.token(ctx)
	if err != nil {
		return nil, err
	}
	scope, err := s.scope(ctx, teamID)
	if err != nil {
		return nil, err
	}
	actx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	projects, err := s.vercel.Projects(actx, tok, scope)
	if err != nil {
		return nil, err
	}
	if projects == nil {
		projects = []domain.VercelProject{}
	}
	slug := ""
	if scope != "" {
		slug = s.teamSlug(actx, tok, scope)
	}
	for i := range projects {
		projects[i].TeamID = scope
		projects[i].TeamSlug = slug
	}
	return projects, nil
}

func (s *Service) scope(ctx context.Context, teamID *string) (string, error) {
	if teamID != nil {
		return strings.TrimSpace(*teamID), nil
	}
	if s.creds == nil {
		return "", nil
	}
	return s.creds.VercelTeam(ctx)
}

// --- links ------------------------------------------------------------------

func (s *Service) Links(ctx context.Context, repositoryID uuid.UUID) ([]domain.HostingLink, error) {
	links, err := s.links.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if links == nil {
		links = []domain.HostingLink{}
	}
	return links, nil
}

// Link binds one area to a provider project. A Vercel link is resolved
// against the API first, so what is stored is what Vercel says the project
// is, not what the request claimed.
func (s *Service) Link(ctx context.Context, repositoryID uuid.UUID, req domain.SaveHostingLinkRequest) (domain.HostingLink, error) {
	area := domain.ParseHostingArea(req.Area)
	if !domain.ValidHostingArea(area) {
		return domain.HostingLink{}, fmt.Errorf("%w: unknown area %q", ErrInvalidInput, req.Area)
	}
	provider := strings.TrimSpace(strings.ToLower(req.Provider))
	if !domain.ValidDeployProvider(provider) {
		return domain.HostingLink{}, fmt.Errorf("%w: unknown provider %q", ErrInvalidInput, req.Provider)
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.HostingLink{}, err
	}
	source := strings.TrimSpace(req.Source)
	if source != domain.HostingSourceDetected {
		source = domain.HostingSourceUser
	}
	link := domain.HostingLink{
		RepositoryID: repo.ID,
		Area:         area,
		Provider:     provider,
		ExternalID:   strings.TrimSpace(req.ExternalID),
		Source:       source,
		Evidence:     strings.TrimSpace(req.Evidence),
	}

	if provider == domain.DeployProviderVercel {
		tok, err := s.token(ctx)
		if err != nil {
			return domain.HostingLink{}, err
		}
		if link.ExternalID == "" {
			return domain.HostingLink{}, fmt.Errorf("%w: a Vercel project id is required", ErrInvalidInput)
		}
		scope, err := s.scope(ctx, req.ScopeID)
		if err != nil {
			return domain.HostingLink{}, err
		}
		actx, cancel := context.WithTimeout(ctx, apiTimeout)
		defer cancel()
		project, err := s.vercel.Project(actx, tok, scope, link.ExternalID)
		if err != nil {
			return domain.HostingLink{}, fmt.Errorf("%w: Vercel project %q could not be read: %v", ErrInvalidInput, link.ExternalID, err)
		}
		link.ExternalID = project.ID
		link.ExternalName = project.Name
		link.ScopeID = scope
		link.RootDirectory = project.RootDirectory
		link.ProductionURL = project.ProductionURL
		if scope != "" {
			link.ScopeSlug = s.teamSlug(actx, tok, scope)
		} else if user, uerr := s.vercel.User(actx, tok); uerr == nil {
			// The personal account's scope slug is the username — the value
			// `vercel --scope` and the deploy recipe want.
			link.ScopeSlug = user.Username
		}
	}

	saved, err := s.links.Save(ctx, link)
	if err != nil {
		return domain.HostingLink{}, err
	}
	if provider == domain.DeployProviderVercel && area == domain.HostingAreaRoot {
		s.syncProdTarget(ctx, repo, saved)
	}
	return saved, nil
}

// Unlink forgets the binding. The prod deploy target, if one was filled by
// the link, is left alone: it is visible and editable on the deploy page and
// removing a link must not silently stop the production monitor.
func (s *Service) Unlink(ctx context.Context, repositoryID uuid.UUID, area string) error {
	area = domain.ParseHostingArea(area)
	if !domain.ValidHostingArea(area) {
		return fmt.Errorf("%w: unknown area %q", ErrInvalidInput, area)
	}
	return s.links.Delete(ctx, repositoryID, area)
}

// syncProdTarget carries a root-area Vercel link into the prod deploy target:
// created when there is none, completed when the existing one is Vercel with
// blanks, and never touched when a human pointed prod somewhere else. A
// failure here is logged, not returned — the link is the durable record and
// the target can be edited by hand on the deploy page.
func (s *Service) syncProdTarget(ctx context.Context, repo domain.Repository, link domain.HostingLink) {
	if s.targets == nil {
		return
	}
	target, err := s.targets.Get(ctx, repo.ID, "", domain.DeployEnvProd)
	switch {
	case err == nil:
		if target.Provider != domain.DeployProviderVercel {
			log.Info().Str("repository", repo.Name).Str("provider", target.Provider).
				Msg("hosting link: prod target ships elsewhere, leaving it alone")
			return
		}
	case errors.Is(err, port.ErrNotFound):
		target = domain.DeployTarget{RepositoryID: repo.ID, Env: domain.DeployEnvProd, Provider: domain.DeployProviderVercel}
		if tpl, ok := deploy.Template(domain.DeployProviderVercel); ok && tpl.SupportsKind(repo.Kind) {
			target.TemplateID = tpl.ID
		}
	default:
		log.Warn().Err(err).Str("repository", repo.Name).Msg("hosting link: reading prod target failed")
		return
	}
	if target.Vars == nil {
		target.Vars = map[string]string{}
	}
	fill := func(key, value string) {
		if strings.TrimSpace(target.Vars[key]) == "" && value != "" {
			target.Vars[key] = value
		}
	}
	fill("vercel_scope", link.ScopeSlug)
	fill("vercel_project", link.ExternalName)
	fill("vercel_project_id", link.ExternalID)
	fill("vercel_team_id", link.ScopeID)
	if target.BaseURL == "" {
		target.BaseURL = link.ProductionURL
	}
	if target.HealthURL == "" && link.ProductionURL != "" {
		// Same offline guard deploy.SaveTarget applies: the monitor will poll
		// this every minute.
		if _, gerr := urlguard.Default().Precheck(link.ProductionURL); gerr == nil {
			target.HealthURL = link.ProductionURL
		}
	}
	if _, err := s.targets.Save(ctx, target); err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("hosting link: prod target sync failed")
	}
}
