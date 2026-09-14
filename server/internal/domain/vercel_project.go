package domain

import (
	"time"

	"github.com/google/uuid"
)

// A VercelProjectLink answers "which Vercel project is THIS part of the
// repository", keyed by sub-project path.
//
// It is deliberately not a HostingLink (migration 112), which answers the same
// question keyed by AREA — root / frontend / backend. An area cannot name two
// frontends: a monorepo with web/ and admin/ has one area and two Vercel
// projects, and the second one has nowhere to go. SubProjectPath is the key
// domain.RepoSubProject already uses (migration 118), so the two are the same
// list the setup dialog curates.
type VercelProjectLink struct {
	ID           uuid.UUID `json:"id"`
	RepositoryID uuid.UUID `json:"repository_id"`
	// SubProjectPath is one of Repository.SubProjects[].Path; "" is the
	// repository as a whole. Note "" and "." are different keys — "." is a
	// legitimate sub-project path (a Go module at the root beside web/).
	SubProjectPath string `json:"sub_project_path"`
	// ProjectID / ProjectName are Vercel's own identity for the project
	// (prj_…). ProjectID is what every later call is addressed with.
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name,omitempty"`
	// TeamID / TeamSlug is the Vercel scope above the project; "" is the
	// token owner's personal account, which is a real scope and not "unset".
	TeamID   string `json:"team_id"`
	TeamSlug string `json:"team_slug,omitempty"`
	// The three facts read off the project at link time, cached so the panel
	// renders before the live read comes back (and still renders when it
	// fails). Vercel stays the source of truth for all three.
	Framework     string    `json:"framework,omitempty"`
	RootDirectory string    `json:"root_directory,omitempty"`
	ProductionURL string    `json:"production_url,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Vercel deployment states, as /v7/deployments reports them in both `state`
// and `readyState`. Only the two the details view branches on are named.
const (
	VercelDeploymentReady = "READY"
	VercelDeploymentError = "ERROR"
)

// VercelTargetProduction is the `target` value of a production deployment.
const VercelTargetProduction = "production"

// VercelDeployment is one deployment as the details view needs it.
//
// Every field is optional on purpose: Vercel marks only uid/name/url/created/
// createdAt/readyState/type/projectId as required, and the commit fields come
// out of the free-form `meta` object whose keys the API reference does not
// enumerate at all. An absent field is left zero rather than guessed.
type VercelDeployment struct {
	ID    string `json:"id"`
	State string `json:"state,omitempty"`
	// Target is "production", "staging" or "" — the last meaning a preview
	// build, which is what a project that has never shipped production has.
	Target       string    `json:"target,omitempty"`
	URL          string    `json:"url,omitempty"`
	InspectorURL string    `json:"inspector_url,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	ReadyAt      time.Time `json:"ready_at,omitempty"`
	// Commit* are read from the deployment's git-provider metadata. They are
	// empty for a CLI deploy, which carries no commit at all.
	CommitSHA     string `json:"commit_sha,omitempty"`
	CommitRef     string `json:"commit_ref,omitempty"`
	CommitMessage string `json:"commit_message,omitempty"`
	CommitAuthor  string `json:"commit_author,omitempty"`
	// ErrorCode / ErrorMessage are set by Vercel only on a canceled or errored
	// deployment. They are the build failure as the API states it — not a log
	// tail, which needs a second, per-deployment events call.
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// Failed reports whether this deployment is one that did not ship.
func (d VercelDeployment) Failed() bool { return d.State == VercelDeploymentError }

// VercelProjectDetails is the linked project plus what Vercel currently says
// about it.
//
// Warnings, and not an error, is how a partial read is reported: the link row
// is durable and worth rendering on its own, and a Vercel outage must not turn
// "here is your project, its live state could not be read" into a page that
// shows nothing. Every nil pointer below therefore has a matching warning.
type VercelProjectDetails struct {
	Link VercelProjectLink `json:"link"`
	// The three live values, falling back to the link's cached copies when the
	// project could not be re-read.
	ProductionURL string `json:"production_url,omitempty"`
	Framework     string `json:"framework,omitempty"`
	RootDirectory string `json:"root_directory,omitempty"`
	// LatestDeployment is the most recent deployment in the scope that was
	// queried; nil when none exists or none could be read.
	LatestDeployment *VercelDeployment `json:"latest_deployment,omitempty"`
	// LastFailedDeployment is the most recent ERROR deployment seen in the
	// same page. It is reported separately from LatestDeployment because a
	// project whose last build failed and was then fixed still wants the
	// failure visible, and a project that is currently broken has the same
	// deployment in both fields.
	LastFailedDeployment *VercelDeployment `json:"last_failed_deployment,omitempty"`
	Warnings             []string          `json:"warnings,omitempty"`
}
