package vercelops

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var ErrNotConnected = errors.New("vercelops: Vercel is not connected yet")

var ErrListingUnavailable = errors.New("vercelops: the connected Vercel token cannot list projects")

var ErrInvalidInput = errors.New("vercelops: invalid input")

var ErrProjectUnreachable = errors.New("vercelops: this Vercel project could not be read with the connected token")

const (
	apiTimeout  = 20 * time.Second
	listTimeout = 45 * time.Second
)

const detailDeployments = 10

type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

type Deps struct {
	Links       port.VercelProjectLinkStore
	Creds       port.VercelCredentialStore
	API         port.VercelAPI
	Deployments port.VercelDeploymentsAPI
	Repos       RepositoryResolver
}

type Service struct {
	links       port.VercelProjectLinkStore
	creds       port.VercelCredentialStore
	api         port.VercelAPI
	deployments port.VercelDeploymentsAPI
	repos       RepositoryResolver
}

func NewService(d Deps) *Service {
	return &Service{
		links:       d.Links,
		creds:       d.Creds,
		api:         d.API,
		deployments: d.Deployments,
		repos:       d.Repos,
	}
}

func (s *Service) token(ctx context.Context) (string, error) {
	if s.creds == nil {
		return "", ErrNotConnected
	}
	tok, err := s.creds.VercelToken(ctx)
	if err != nil {

		if errors.Is(err, port.ErrNotFound) {
			return "", ErrNotConnected
		}
		return "", fmt.Errorf("vercelops: loading the Vercel token: %w", err)
	}
	if strings.TrimSpace(tok) == "" {
		return "", ErrNotConnected
	}
	return tok, nil
}

func (s *Service) defaultTeam(ctx context.Context) string {
	if s.creds == nil {
		return ""
	}
	team, err := s.creds.VercelTeam(ctx)
	if err != nil {

		log.Warn().Err(err).Msg("vercelops: reading the pinned Vercel team failed")
		return ""
	}
	return team
}

func (s *Service) ListProjects(ctx context.Context) ([]domain.VercelProject, error) {
	tok, err := s.token(ctx)
	if err != nil {
		return nil, err
	}
	if s.api == nil {
		return nil, errors.New("vercelops: Vercel API client not configured")
	}
	actx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	var (
		out       []domain.VercelProject
		seen      = map[string]bool{}
		failures  []error
		slugOf    = map[string]string{}
		teams     []domain.VercelTeam
		teamsErr  error
		personalP []domain.VercelProject
	)

	personalP, err = s.api.Projects(actx, tok, "")
	if err != nil {
		failures = append(failures, err)
	}
	teams, teamsErr = s.api.Teams(actx, tok)
	if teamsErr != nil {
		failures = append(failures, teamsErr)
	}
	for _, t := range teams {
		slugOf[t.ID] = t.Slug
	}

	appendScope := func(teamID string, projects []domain.VercelProject) {
		for _, p := range projects {
			if p.ID == "" || seen[p.ID] {
				continue
			}
			seen[p.ID] = true

			p.TeamID = teamID
			p.TeamSlug = slugOf[teamID]
			out = append(out, p)
		}
	}
	appendScope("", personalP)
	for _, t := range teams {
		projects, perr := s.api.Projects(actx, tok, t.ID)
		if perr != nil {
			failures = append(failures, perr)
			log.Warn().Err(perr).Str("team", t.Slug).Msg("vercelops: listing a team's Vercel projects failed")
			continue
		}
		appendScope(t.ID, projects)
	}

	if len(out) == 0 && len(failures) > 0 {
		return nil, listingFailure(failures)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func listingFailure(failures []error) error {
	for _, err := range failures {
		if errors.Is(err, port.ErrVercelUnauthorized) {
			return fmt.Errorf("vercelops: %v: %w", err, ErrListingUnavailable)
		}
	}
	return fmt.Errorf("vercelops: listing Vercel projects: %w", failures[0])
}

func (s *Service) LinkProject(ctx context.Context, repositoryID uuid.UUID, subProjectPath, projectID string) (domain.VercelProjectLink, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.VercelProjectLink{}, fmt.Errorf("%w: a Vercel project id is required", ErrInvalidInput)
	}
	subProjectPath, err := s.resolveSubProject(ctx, repositoryID, subProjectPath)
	if err != nil {
		return domain.VercelProjectLink{}, err
	}
	tok, err := s.token(ctx)
	if err != nil {
		return domain.VercelProjectLink{}, err
	}
	if s.api == nil {
		return domain.VercelProjectLink{}, errors.New("vercelops: Vercel API client not configured")
	}
	actx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	project, teamID, err := s.findProject(actx, tok, projectID)
	if err != nil {
		return domain.VercelProjectLink{}, err
	}

	link := domain.VercelProjectLink{
		RepositoryID:   repositoryID,
		SubProjectPath: subProjectPath,
		ProjectID:      project.ID,
		ProjectName:    project.Name,
		TeamID:         teamID,
		TeamSlug:       s.scopeSlug(actx, tok, teamID),
		Framework:      project.Framework,
		RootDirectory:  project.RootDirectory,
		ProductionURL:  project.ProductionURL,
	}
	saved, err := s.links.Save(ctx, link)
	if err != nil {
		return domain.VercelProjectLink{}, fmt.Errorf("vercelops: persisting the Vercel project link: %w", err)
	}
	return saved, nil
}

func (s *Service) resolveSubProject(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (string, error) {
	subProjectPath = strings.TrimSpace(subProjectPath)
	if s.repos == nil {
		return "", errors.New("vercelops: repository resolver not configured")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return "", err
	}
	if subProjectPath == "" {
		return "", nil
	}
	for _, sp := range repo.SubProjects {
		if sp.Path == subProjectPath {
			return subProjectPath, nil
		}
	}
	return "", fmt.Errorf("%w: %s has no sub-project at %q", ErrInvalidInput, repo.Name, subProjectPath)
}

func (s *Service) findProject(ctx context.Context, tok, projectID string) (domain.VercelProject, string, error) {
	tried := map[string]bool{}
	attempt := func(teamID string) (domain.VercelProject, bool) {
		if tried[teamID] {
			return domain.VercelProject{}, false
		}
		tried[teamID] = true
		project, err := s.api.Project(ctx, tok, teamID, projectID)
		if err != nil {
			return domain.VercelProject{}, false
		}
		return project, true
	}
	pinned := s.defaultTeam(ctx)
	if project, ok := attempt(pinned); ok {
		return project, pinned, nil
	}
	if project, ok := attempt(""); ok {
		return project, "", nil
	}
	teams, err := s.api.Teams(ctx, tok)
	if err != nil {

		log.Warn().Err(err).Msg("vercelops: listing Vercel teams while linking failed")
		return domain.VercelProject{}, "", fmt.Errorf("vercelops: %s: %w", projectID, ErrProjectUnreachable)
	}
	for _, t := range teams {
		if project, ok := attempt(t.ID); ok {
			return project, t.ID, nil
		}
	}
	return domain.VercelProject{}, "", fmt.Errorf("vercelops: %s: %w", projectID, ErrProjectUnreachable)
}

func (s *Service) scopeSlug(ctx context.Context, tok, teamID string) string {
	if teamID == "" {
		user, err := s.api.User(ctx, tok)
		if err != nil {
			return ""
		}
		return user.Username
	}
	teams, err := s.api.Teams(ctx, tok)
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

func (s *Service) Links(ctx context.Context, repositoryID uuid.UUID) ([]domain.VercelProjectLink, error) {
	links, err := s.links.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("vercelops: listing Vercel project links: %w", err)
	}
	if links == nil {
		links = []domain.VercelProjectLink{}
	}
	return links, nil
}

func (s *Service) Unlink(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) error {
	if err := s.links.Delete(ctx, repositoryID, strings.TrimSpace(subProjectPath)); err != nil {
		return fmt.Errorf("vercelops: deleting the Vercel project link: %w", err)
	}
	return nil
}

func (s *Service) ProjectDetails(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (domain.VercelProjectDetails, error) {
	link, err := s.links.Get(ctx, repositoryID, strings.TrimSpace(subProjectPath))
	if err != nil {
		return domain.VercelProjectDetails{}, err
	}
	details := domain.VercelProjectDetails{
		Link:          link,
		ProductionURL: link.ProductionURL,
		Framework:     link.Framework,
		RootDirectory: link.RootDirectory,
	}

	tok, err := s.token(ctx)
	if err != nil {
		if errors.Is(err, ErrNotConnected) {
			details.Warnings = append(details.Warnings,
				"Vercel is not connected, so only the values recorded when the project was linked are shown")
			return details, nil
		}
		return domain.VercelProjectDetails{}, err
	}
	actx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	if s.api != nil {
		project, perr := s.api.Project(actx, tok, link.TeamID, link.ProjectID)
		switch {
		case perr != nil:
			details.Warnings = append(details.Warnings, "the project could not be re-read from Vercel: "+perr.Error())
		default:

			if project.ProductionURL != "" {
				details.ProductionURL = project.ProductionURL
			}
			if project.Framework != "" {
				details.Framework = project.Framework
			}
			if project.RootDirectory != "" {
				details.RootDirectory = project.RootDirectory
			}
		}
	}

	s.fillDeployments(actx, tok, link, &details)
	return details, nil
}

func (s *Service) fillDeployments(ctx context.Context, tok string, link domain.VercelProjectLink, details *domain.VercelProjectDetails) {
	if s.deployments == nil {
		return
	}
	deployments, err := s.deployments.Deployments(ctx, tok, link.TeamID, link.ProjectID, domain.VercelTargetProduction, detailDeployments)
	if err != nil {
		details.Warnings = append(details.Warnings, "the deployment history could not be read from Vercel: "+err.Error())
		return
	}
	if len(deployments) == 0 {
		anyTarget, aerr := s.deployments.Deployments(ctx, tok, link.TeamID, link.ProjectID, "", detailDeployments)
		if aerr != nil {
			details.Warnings = append(details.Warnings, "the deployment history could not be read from Vercel: "+aerr.Error())
			return
		}
		deployments = anyTarget
	}
	if len(deployments) == 0 {
		details.Warnings = append(details.Warnings, "this Vercel project has no deployments yet")
		return
	}
	latest := deployments[0]
	details.LatestDeployment = &latest
	for i := range deployments {
		if deployments[i].Failed() {
			failed := deployments[i]
			details.LastFailedDeployment = &failed
			break
		}
	}
}
