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

var ErrNotConnected = errors.New("Vercel is not connected — connect it from Settings")

var ErrInvalidInput = errors.New("invalid input")

const (
	apiTimeout    = 20 * time.Second
	detectTimeout = 60 * time.Second
)

type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

type Service struct {
	links  port.HostingLinkStore
	repos  RepositoryResolver
	creds  port.VercelCredentialStore
	vercel port.VercelAPI

	targets port.DeployTargetStore

	collect func(ctx context.Context, root string) repofacts.Facts
}

func NewService(links port.HostingLinkStore, repos RepositoryResolver, creds port.VercelCredentialStore, api port.VercelAPI) *Service {
	return &Service{links: links, repos: repos, creds: creds, vercel: api, collect: repofacts.Collect}
}

func (s *Service) SetDeployTargets(t port.DeployTargetStore) { s.targets = t }

func (s *Service) SetFactsCollector(fn func(ctx context.Context, root string) repofacts.Facts) {
	if fn != nil {
		s.collect = fn
	}
}

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

func (s *Service) Unlink(ctx context.Context, repositoryID uuid.UUID, area string) error {
	area = domain.ParseHostingArea(area)
	if !domain.ValidHostingArea(area) {
		return fmt.Errorf("%w: unknown area %q", ErrInvalidInput, area)
	}
	return s.links.Delete(ctx, repositoryID, area)
}

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

		if _, gerr := urlguard.Default().Precheck(link.ProductionURL); gerr == nil {
			target.HealthURL = link.ProductionURL
		}
	}
	if _, err := s.targets.Save(ctx, target); err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("hosting link: prod target sync failed")
	}
}
