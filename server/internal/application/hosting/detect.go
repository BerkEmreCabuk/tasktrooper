package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repofacts"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Detect answers, for every web-hosted area of the repository, where the tree
// says it ships and which Vercel projects could be it. It reads the working
// copy and the Vercel API; it writes nothing.
func (s *Service) Detect(ctx context.Context, repositoryID uuid.UUID) (domain.HostingDetection, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.HostingDetection{}, err
	}
	det := domain.HostingDetection{RepositoryID: repo.ID, Kind: repo.Kind, Areas: []domain.HostingAreaDetection{}}

	fctx, cancel := context.WithTimeout(ctx, detectTimeout)
	facts := s.collect(fctx, repo.RootPath)
	cancel()
	det.Warnings = append(det.Warnings, facts.Warnings...)
	if det.Kind == "" {
		det.Kind = facts.Kind
	}

	areas := areasFor(det.Kind, repo.SubRepoKinds, facts)
	if len(areas) == 0 {
		det.Warnings = append(det.Warnings, "repository kind "+orUnknown(det.Kind)+" has no web-hosted area (only backend and frontend projects are linked)")
	}

	existing := map[string]domain.HostingLink{}
	if links, lerr := s.links.ListByRepository(ctx, repo.ID); lerr == nil {
		for _, l := range links {
			existing[l.Area] = l
		}
	} else {
		det.Warnings = append(det.Warnings, "stored links could not be read: "+lerr.Error())
	}

	// Vercel side, fetched once for every area.
	var projects []domain.VercelProject
	var tok, scope string
	if t, terr := s.token(ctx); terr == nil {
		tok = t
		det.VercelConnected = true
		scope, _ = s.scope(ctx, nil)
		actx, acancel := context.WithTimeout(ctx, apiTimeout)
		projects, err = s.vercel.Projects(actx, tok, scope)
		acancel()
		if err != nil {
			det.Warnings = append(det.Warnings, "Vercel projects could not be listed: "+err.Error())
			projects = nil
		}
	} else if !errors.Is(terr, ErrNotConnected) {
		det.Warnings = append(det.Warnings, "Vercel credential could not be read: "+terr.Error())
	}

	slug := remoteSlug(facts, repo.RemoteURL)
	for _, a := range areas {
		a.Hints = hintsFor(a, facts)
		if l, ok := existing[a.Area]; ok {
			link := l
			a.Existing = &link
		}
		if det.VercelConnected {
			linked := s.linkedProject(ctx, tok, scope, repo.RootPath, a, projects)
			a.Candidates = candidatesFor(a, repo.Name, slug, projects, linked)
		}
		a.Confidence = confidenceOf(a)
		det.Areas = append(det.Areas, a)
	}
	return det, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// areasFor lists the areas worth asking about: the root of a backend or
// frontend repository, or the backend/frontend halves of a monorepo (mobile
// and worker halves ship through stores and queues, not web hosts).
func areasFor(kind string, subKinds []string, facts repofacts.Facts) []domain.HostingAreaDetection {
	switch kind {
	case domain.RepoKindBackend, domain.RepoKindFrontend:
		return []domain.HostingAreaDetection{{Area: domain.HostingAreaRoot, Kind: kind}}
	case domain.RepoKindMonorepo:
		subs := subKinds
		if len(subs) == 0 {
			subs = facts.SubKinds
		}
		var out []domain.HostingAreaDetection
		for _, want := range []string{domain.RepoKindFrontend, domain.RepoKindBackend} {
			for _, sk := range subs {
				if sk == want {
					out = append(out, domain.HostingAreaDetection{Area: sk, Kind: sk, Directory: dirForKind(facts, sk)})
					break
				}
			}
		}
		return out
	}
	return nil
}

// dirForKind reads the classified sub-project directory out of the facts'
// evidence lines ("apps/web → frontend"). "" when the collector did not say.
func dirForKind(facts repofacts.Facts, kind string) string {
	for _, ev := range facts.KindEvidence {
		dir, k, ok := strings.Cut(ev, " → ")
		if ok && strings.TrimSpace(k) == kind {
			return strings.Trim(strings.TrimSpace(dir), "/")
		}
	}
	return ""
}

// hostingHintProviders maps the collector's integration names onto the keys
// the UI and the link provider field use.
var hostingHintProviders = map[string]string{
	"Vercel":                      domain.DeployProviderVercel,
	"Fly.io":                      domain.DeployProviderFly,
	"Netlify":                     "netlify",
	"Render":                      "render",
	"Heroku-style buildpack host": "heroku",
	"AWS Amplify":                 "aws_amplify",
	"Cloudflare Workers/Pages":    "cloudflare",
	"Firebase":                    "firebase",
	"static host":                 "static",
}

// hintsFor attributes the tree's hosting markers and deploy workflows to one
// area. A marker under the area's directory belongs to it; a marker at the
// repository root is reported to every area, because a root vercel.json on a
// monorepo says "something here is on Vercel" without saying which half.
func hintsFor(a domain.HostingAreaDetection, facts repofacts.Facts) []domain.HostingHint {
	var out []domain.HostingHint
	for _, in := range facts.Integrations {
		if in.Category != "hosting" {
			continue
		}
		if !evidenceBelongs(in.Evidence, a.Directory) {
			continue
		}
		provider := hostingHintProviders[in.Name]
		if provider == "" {
			provider = domain.DeployProviderCustom
		}
		out = append(out, domain.HostingHint{Provider: provider, Name: in.Name, Detail: in.Detail, Evidence: in.Evidence})
	}
	for _, d := range facts.Deploys {
		if d.Provider != "GitHub Actions" {
			continue
		}
		detail := d.Trigger
		if d.Environment != "" {
			detail = d.Environment + " · " + detail
		}
		out = append(out, domain.HostingHint{Provider: "github_actions", Name: "GitHub Actions deploy workflow", Detail: detail, Evidence: d.Evidence})
	}
	return out
}

// evidenceBelongs decides whether a marker path speaks for an area: root-level
// markers speak for every area, nested ones only for the directory they sit in.
func evidenceBelongs(evidence, dir string) bool {
	evidence = strings.TrimPrefix(filepath.ToSlash(evidence), "./")
	parent := path.Dir(evidence)
	if parent == "." || parent == ".vercel" {
		return true
	}
	if dir == "" {
		return true
	}
	return strings.HasPrefix(evidence, dir+"/")
}

// vercelLink is what `vercel link` writes to .vercel/project.json.
type vercelLink struct {
	ProjectID string `json:"projectId"`
	OrgID     string `json:"orgId"`
}

// linkedProject reads .vercel/project.json for the area (its own directory
// first, then the repository root) and resolves the project it names — from
// the listed scope when it is there, from the file's own org otherwise. The
// file is untracked and normally gitignored, so it only ever exists on a
// working copy where somebody ran `vercel link`; that is exactly the case
// where it beats every other signal.
func (s *Service) linkedProject(ctx context.Context, tok, scope, root string, a domain.HostingAreaDetection, projects []domain.VercelProject) *domain.VercelProject {
	if root == "" {
		return nil
	}
	var candidates []string
	if a.Directory != "" {
		candidates = append(candidates, filepath.Join(root, filepath.FromSlash(a.Directory), ".vercel", "project.json"))
	}
	candidates = append(candidates, filepath.Join(root, ".vercel", "project.json"))
	for _, file := range candidates {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var link vercelLink
		if json.Unmarshal(raw, &link) != nil || link.ProjectID == "" {
			continue
		}
		for i := range projects {
			if projects[i].ID == link.ProjectID {
				p := projects[i]
				return &p
			}
		}
		// Not in the default scope: the file says which org it belongs to.
		orgScope := ""
		if strings.HasPrefix(link.OrgID, "team_") {
			orgScope = link.OrgID
		}
		if orgScope == scope {
			continue
		}
		actx, cancel := context.WithTimeout(ctx, apiTimeout)
		p, perr := s.vercel.Project(actx, tok, orgScope, link.ProjectID)
		cancel()
		if perr == nil {
			return &p
		}
	}
	return nil
}

var remoteSlugRe = regexp.MustCompile(`[:/]([^/:]+)/([^/]+?)(?:\.git)?/?$`)

// remoteSlug is owner/repo of the working copy's origin, lower-cased, from
// the facts when git answered and from the recorded remote URL otherwise.
func remoteSlug(facts repofacts.Facts, remoteURL string) string {
	if facts.Git.RemoteSlug != "" {
		return strings.ToLower(strings.Trim(facts.Git.RemoteSlug, "/"))
	}
	m := remoteSlugRe.FindStringSubmatch(strings.TrimSpace(remoteURL))
	if len(m) != 3 {
		return ""
	}
	return strings.ToLower(m[1] + "/" + m[2])
}

// matchRank orders reasons strongest first.
func matchRank(reason string) int {
	switch reason {
	case domain.HostingMatchProjectJSON:
		return 0
	case domain.HostingMatchGitLinkDir:
		return 1
	case domain.HostingMatchGitLink:
		return 2
	case domain.HostingMatchName:
		return 3
	}
	return 9
}

// candidatesFor scores every project against one area. The link file names
// the project outright; a git-linked project matches by remote, and on a
// monorepo it is only decisive when its root directory is this area's; a name
// match is the weakest signal and never decisive on its own.
func candidatesFor(a domain.HostingAreaDetection, repoName, slug string, projects []domain.VercelProject, linked *domain.VercelProject) []domain.HostingCandidate {
	var out []domain.HostingCandidate
	seen := map[string]bool{}
	add := func(p domain.VercelProject, reason string) {
		if p.ID == "" || seen[p.ID] {
			return
		}
		seen[p.ID] = true
		out = append(out, domain.HostingCandidate{Provider: domain.DeployProviderVercel, Project: p, Reason: reason})
	}
	if linked != nil {
		add(*linked, domain.HostingMatchProjectJSON)
	}
	areaName := ""
	if a.Directory != "" {
		areaName = path.Base(a.Directory)
	}
	for _, p := range projects {
		switch {
		case slug != "" && p.Link != nil && p.Link.Slug() == slug:
			if a.Directory != "" && strings.Trim(p.RootDirectory, "/") == a.Directory {
				add(p, domain.HostingMatchGitLinkDir)
			} else {
				add(p, domain.HostingMatchGitLink)
			}
		case strings.EqualFold(p.Name, repoName) || (areaName != "" && strings.EqualFold(p.Name, areaName)):
			add(p, domain.HostingMatchName)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return matchRank(out[i].Reason) < matchRank(out[j].Reason) })
	return out
}

// confidenceOf is the rule the UI relies on to decide whether to propose or
// to ask: exactly one decisive candidate — the link file, a root-directory
// match on a monorepo, or the single git-linked project of a single-kind
// repository — is exact; anything else with candidates is ambiguous.
func confidenceOf(a domain.HostingAreaDetection) string {
	if len(a.Candidates) == 0 {
		return domain.HostingConfidenceNone
	}
	top := a.Candidates[0]
	sameReason := 0
	for _, c := range a.Candidates {
		if c.Reason == top.Reason {
			sameReason++
		}
	}
	switch top.Reason {
	case domain.HostingMatchProjectJSON, domain.HostingMatchGitLinkDir:
		if sameReason == 1 {
			return domain.HostingConfidenceExact
		}
	case domain.HostingMatchGitLink:
		if a.Directory == "" && len(a.Candidates) == 1 {
			return domain.HostingConfidenceExact
		}
	}
	return domain.HostingConfidenceAmbiguous
}
