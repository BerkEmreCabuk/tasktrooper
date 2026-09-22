package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Hosting links answer "where does this part of the repository actually live"
// with the provider's own identity — a Vercel project id, its team, its root
// directory, the address production answers on — as opposed to a deploy target,
// which answers "which workflow ships env X". The two are keyed differently on
// purpose: a target is per environment, a link is per AREA, so a monorepo can
// say its frontend is on Vercel while its backend is elsewhere.

// HostingAreaRoot is the area of a single-kind repository: the whole tree. A
// monorepo uses its sub-repo kinds (RepoKindFrontend, RepoKindBackend…) as
// areas instead.
const HostingAreaRoot = ""

// hostingAreaRootParam is how the root area travels in a URL path segment,
// where "" cannot.
const hostingAreaRootParam = "root"

// HostingAreaParam renders an area for a path segment.
func HostingAreaParam(area string) string {
	if area == HostingAreaRoot {
		return hostingAreaRootParam
	}
	return area
}

// ParseHostingArea reads an area back from a path segment or request body.
// "" and "root" both mean the whole repository.
func ParseHostingArea(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == hostingAreaRootParam {
		return HostingAreaRoot
	}
	return raw
}

// ValidHostingArea reports whether an area is the root or a sub-repo kind.
func ValidHostingArea(area string) bool {
	return area == HostingAreaRoot || ValidSubRepoKind(area)
}

// Who established a link.
const (
	HostingSourceDetected = "detected"
	HostingSourceUser     = "user"
)

// HostingLink is one (repository, area) → provider object binding.
type HostingLink struct {
	ID           uuid.UUID `json:"id"`
	RepositoryID uuid.UUID `json:"repository_id"`
	Area         string    `json:"area"`
	// Provider is one of DeployProvider*. Only DeployProviderVercel carries a
	// verified external identity today; any other value records what the
	// operator said when asked, so the question is not asked again.
	Provider string `json:"provider"`
	// ExternalID / ExternalName identify the provider-side project.
	ExternalID   string `json:"external_id,omitempty"`
	ExternalName string `json:"external_name,omitempty"`
	// ScopeID / ScopeSlug are the provider's tenant above the project (a Vercel
	// team; "" is the personal account).
	ScopeID   string `json:"scope_id,omitempty"`
	ScopeSlug string `json:"scope_slug,omitempty"`
	// RootDirectory is the sub-folder the provider builds from, when the
	// project is a monorepo area.
	RootDirectory string `json:"root_directory,omitempty"`
	// ProductionURL is the address production answers on, as reported by the
	// provider — the same fact a prod deploy target's base_url records.
	ProductionURL string    `json:"production_url,omitempty"`
	Source        string    `json:"source"`
	Evidence      string    `json:"evidence,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// SaveHostingLinkRequest binds one area to a provider object. For Vercel the
// project is resolved and verified against the API before anything is stored;
// for every other provider the row records the operator's answer as given.
type SaveHostingLinkRequest struct {
	Area     string `json:"area"`
	Provider string `json:"provider"`
	// ExternalID is the provider project id (Vercel: prj_…). Required for
	// Vercel, optional otherwise.
	ExternalID string `json:"external_id,omitempty"`
	// ScopeID is the provider tenant the project lives under (Vercel team id;
	// "" = personal account). Omitted, the connection's default team is used.
	ScopeID *string `json:"scope_id,omitempty"`
	// Source defaults to HostingSourceUser; the UI passes "detected" when it is
	// confirming what detection proposed.
	Source string `json:"source,omitempty"`
	// Evidence is free text: the reason the operator (or the detector) picked
	// this project.
	Evidence string `json:"evidence,omitempty"`
}

// Vercel objects, as the rest of the system needs them — domain types, not
// adapter ones, so the application layer can reason about a project without
// importing the HTTP client.

// VercelUser is the account a token belongs to.
type VercelUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
	Name     string `json:"name,omitempty"`
}

// VercelTeam is a scope a token can act in.
type VercelTeam struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// VercelGitLink is the repository a Vercel project deploys from.
type VercelGitLink struct {
	Type             string `json:"type"`
	Org              string `json:"org"`
	Repo             string `json:"repo"`
	ProductionBranch string `json:"production_branch,omitempty"`
}

// Slug renders org/repo, lower-cased, for comparison with a git remote.
func (l VercelGitLink) Slug() string {
	if l.Org == "" || l.Repo == "" {
		return ""
	}
	return strings.ToLower(l.Org + "/" + l.Repo)
}

// VercelProject is one project as the link and detection views need it.
type VercelProject struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Framework     string         `json:"framework,omitempty"`
	RootDirectory string         `json:"root_directory,omitempty"`
	Link          *VercelGitLink `json:"link,omitempty"`
	ProductionURL string         `json:"production_url,omitempty"`
	TeamID        string         `json:"team_id,omitempty"`
	TeamSlug      string         `json:"team_slug,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
}

// VercelConnectionStatus is the settings-page view of the connection.
type VercelConnectionStatus struct {
	Connected bool   `json:"connected"`
	Username  string `json:"username,omitempty"`
	Email     string `json:"email,omitempty"`
	// TeamID / TeamSlug is the default scope links and project listings use;
	// "" means the personal account.
	TeamID   string `json:"team_id"`
	TeamSlug string `json:"team_slug,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// Detection confidence for one area.
const (
	HostingConfidenceExact     = "exact"     // one candidate, proven by a link file or a root-directory match
	HostingConfidenceAmbiguous = "ambiguous" // candidates exist, none proven — ask
	HostingConfidenceNone      = "none"      // nothing matched — ask
)

// Candidate match reasons, strongest first.
const (
	HostingMatchProjectJSON = "project_json" // .vercel/project.json names this project
	HostingMatchGitLinkDir  = "git_link_dir" // git-linked to this repo AND built from this area's directory
	HostingMatchGitLink     = "git_link"     // git-linked to this repo
	HostingMatchName        = "name"         // project name equals the repo/area name
)

// HostingHint is a marker the tree itself carries about where an area ships (a
// vercel.json, a fly.toml, a deploy workflow). It is evidence, not a link.
type HostingHint struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Detail   string `json:"detail,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

// HostingCandidate is a provider project that may be where the area lives.
type HostingCandidate struct {
	Provider string        `json:"provider"`
	Project  VercelProject `json:"project"`
	Reason   string        `json:"reason"`
}

// HostingAreaDetection is the answer for one area: what is recorded, what the
// tree says, which provider projects match, and whether that is decisive.
type HostingAreaDetection struct {
	Area string `json:"area"`
	// Kind is the area's repo kind (backend/frontend); Directory the folder the
	// area lives in on a monorepo ("" at the root).
	Kind       string             `json:"kind"`
	Directory  string             `json:"directory,omitempty"`
	Existing   *HostingLink       `json:"existing,omitempty"`
	Hints      []HostingHint      `json:"hints,omitempty"`
	Candidates []HostingCandidate `json:"candidates,omitempty"`
	Confidence string             `json:"confidence"`
}

// HostingDetection is the whole repository's answer.
type HostingDetection struct {
	RepositoryID uuid.UUID `json:"repository_id"`
	Kind         string    `json:"kind"`
	// VercelConnected says whether candidates could be looked up at all; false
	// means hints only, and the UI should send the user to Settings.
	VercelConnected bool                   `json:"vercel_connected"`
	Areas           []HostingAreaDetection `json:"areas"`
	Warnings        []string               `json:"warnings,omitempty"`
}
