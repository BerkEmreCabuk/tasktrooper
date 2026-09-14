// Package vercelops is the frontend counterpart of application/storeops: pick
// a project out of the connected Vercel account, bind it to a repository (or
// to one sub-project of a monorepo), and read back what Vercel currently says
// about it.
//
// It sits beside application/hosting rather than inside it because the two are
// keyed differently and that difference is the reason this package exists. A
// hosting link is per AREA — ”, frontend, backend — which is enough to say
// "the frontend is on Vercel" and not enough to say WHICH Vercel project
// web/ and admin/ each are. This one is keyed by sub-project path, the same
// key domain.RepoSubProject and repository_deploy_targets already use.
//
// The connection itself is not re-implemented here: the token is the one
// Settings stored (hosting.Connect), read through port.VercelCredentialStore.
// Token only — there is no OAuth path and none is to be added.
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

// ErrNotConnected means no Vercel token has been stored yet. It is the first
// state every tenant is in, not a failure, and it is answered differently from
// ErrListingUnavailable: that one means the token IS there and Vercel refuses
// it, which sends the operator to Settings to paste a fresh one. Neither is a
// 5xx — see the reason dictionary in the HTTP layer.
var ErrNotConnected = errors.New("vercelops: Vercel is not connected yet")

// ErrListingUnavailable means a token is stored but the projects behind it
// cannot be enumerated — Vercel refused the token on every scope it was asked
// about (revoked, expired, or scoped to nothing). Retrying will not fix it, so
// it must not be reported as a server failure.
var ErrListingUnavailable = errors.New("vercelops: the connected Vercel token cannot list projects")

// ErrInvalidInput wraps the caller's own mistakes: a blank project id, a
// sub-project path this repository does not have, a path on a repository that
// is not a monorepo.
var ErrInvalidInput = errors.New("vercelops: invalid input")

// ErrProjectUnreachable marks a link request naming a project the connected
// token cannot read in any scope it can act in — a typo, a project that was
// deleted between listing and picking, or one in a team this token left. The
// binding is refused rather than written and left to fail on every later read.
var ErrProjectUnreachable = errors.New("vercelops: this Vercel project could not be read with the connected token")

// apiTimeout bounds one Vercel round trip, listTimeout the whole scope walk
// ListProjects does (one call per team the token belongs to).
const (
	apiTimeout  = 20 * time.Second
	listTimeout = 45 * time.Second
)

// detailDeployments is how many recent deployments ProjectDetails reads. The
// head answers "what shipped last"; the rest let it also answer "what failed
// last" without a second query.
const detailDeployments = 10

// RepositoryResolver reads the repository a link belongs to — the same narrow
// shape storeops and hosting declare for themselves, kept local so this
// package imports no other application package.
type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// Deps wires the collaborators of the Vercel project service.
type Deps struct {
	Links       port.VercelProjectLinkStore
	Creds       port.VercelCredentialStore
	API         port.VercelAPI
	Deployments port.VercelDeploymentsAPI
	Repos       RepositoryResolver
}

// Service owns each repository's (and sub-project's) Vercel project binding.
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

// --- connection -------------------------------------------------------------

func (s *Service) token(ctx context.Context) (string, error) {
	if s.creds == nil {
		return "", ErrNotConnected
	}
	tok, err := s.creds.VercelToken(ctx)
	if err != nil {
		// A missing row is the unconnected state, not a store failure. Anything
		// else — a cipher that is not configured, a decrypt that fails — is a
		// real fault and has to stay a 500.
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

// defaultTeam is the scope Settings pinned; "" is the personal account.
func (s *Service) defaultTeam(ctx context.Context) string {
	if s.creds == nil {
		return ""
	}
	team, err := s.creds.VercelTeam(ctx)
	if err != nil {
		// Cosmetic: the scope walk below covers the personal account and every
		// team anyway, so a failure here costs an ordering preference at most.
		log.Warn().Err(err).Msg("vercelops: reading the pinned Vercel team failed")
		return ""
	}
	return team
}

// --- listing ----------------------------------------------------------------

// ListProjects returns every project the connected token can see, across the
// personal account and each team it belongs to.
//
// It walks all scopes rather than only the pinned one because the picker's job
// is to show what is pickable, and a token whose default scope is a team would
// otherwise hide the personal projects a repository is just as likely to be on.
//
// A scope that fails is SKIPPED, not fatal: a token that can read three of
// four teams should still produce a picker for the three. The only failure
// that surfaces is one where no scope produced anything AND at least one was
// refused — reported as ErrListingUnavailable if Vercel refused the token
// itself, and as a real error otherwise.
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
			// The scope is stamped from the query, not from the payload:
			// /v9/projects does not echo the team back, so a project listed
			// under a team would otherwise carry an empty TeamID and be
			// addressed against the personal account on the next call.
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

// listingFailure classifies a listing that produced nothing. A token Vercel
// refused is a stable answer the console turns into "reconnect Vercel";
// anything else — an outage, a network fault, a decode error — is this
// server's problem and keeps its 5xx.
func listingFailure(failures []error) error {
	for _, err := range failures {
		if errors.Is(err, port.ErrVercelUnauthorized) {
			return fmt.Errorf("vercelops: %v: %w", err, ErrListingUnavailable)
		}
	}
	return fmt.Errorf("vercelops: listing Vercel projects: %w", failures[0])
}

// --- linking ----------------------------------------------------------------

// LinkProject binds one repository (or one of its sub-projects) to a Vercel
// project, after confirming the connected token can actually read that project.
//
// The confirmation is not a formality: what is stored is what Vercel says the
// project is — its id, name, scope, framework, root directory and production
// address — rather than what the request claimed, so a picker working from a
// stale listing cannot write a link that addresses the wrong project.
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

// resolveSubProject validates the path against the repository's own curated
// sub-project list. "" (the repository as a whole) always passes; anything
// else must be a path the setup dialog actually recorded, because a link on a
// path nothing else knows about is a link no deploy, gate or panel can ever
// find again.
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

// findProject reads the project in whichever scope the token can reach it.
//
// The pinned team is tried first and the personal account second, which is the
// order that resolves in one call for the overwhelmingly common case; the
// remaining teams are only walked when neither answered, so a token in a dozen
// teams costs a dozen calls once, on the link click, and never again.
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
		// Without the team list there is nowhere left to look, so the answer is
		// the same as an exhausted walk: this token cannot read that project.
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

// scopeSlug renders the scope the `vercel --scope` flag and the dashboard URL
// want: a team's slug, or the username for the personal account. Best effort —
// a slug is cosmetic and must not fail a link.
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

// Links lists every Vercel project bound anywhere in this repository.
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

// Unlink forgets one binding. A missing row is not an error.
func (s *Service) Unlink(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) error {
	if err := s.links.Delete(ctx, repositoryID, strings.TrimSpace(subProjectPath)); err != nil {
		return fmt.Errorf("vercelops: deleting the Vercel project link: %w", err)
	}
	return nil
}

// --- details ----------------------------------------------------------------

// ProjectDetails reads the linked project's live state: the address production
// answers on, the last deployment's status / time / commit, the framework, and
// the last build failure if there is one.
//
// Only the link row itself is required. Every live read is best effort and
// records a warning instead of an error, because the link is durable and worth
// rendering on its own: a Vercel outage, a token that was revoked after the
// link was made, or a project that has never deployed must each leave a page
// that still shows which project this is, not an error screen.
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
			// Only overwrite what Vercel actually answered: a project with no
			// production deployment reports no production URL, and blanking the
			// cached one would lose the only address the page had.
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

// fillDeployments reads the project's recent production deployments and picks
// the two the view asks about.
//
// The production filter is dropped on an empty answer rather than reported as
// "no deployments": a project that has only ever had preview builds has a
// perfectly real last deployment, and the returned row carries its own Target
// so the caller can see it is not production.
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
