package domain

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Repository struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	RootPath    string    `json:"root_path"`
	// RemoteURL is the git origin the working copy came from. Persisted so a
	// lost or never-cloned RootPath can be restored without the human supplying
	// the path again — the working copy is a cache, this is the source of truth.
	RemoteURL     string `json:"remote_url,omitempty"`
	VerifyCommand string `json:"verify_command"`
	BuildCommand  string `json:"build_command"`
	TestCommand   string `json:"test_command"`
	// CoverageThreshold is the WHOLE-REPO line coverage bar the run's coverage
	// note is written against. 0 means the default
	// (board.DefaultCoverageThreshold, 90%).
	//
	// It blocks nothing. Coverage is measured, stated in the hand-off and left
	// there for whoever reads the run — holding a finished task over a
	// percentage stops the work without raising the number, and on a whole-repo
	// figure it holds it over code the task never touched. The diff's own
	// figure is reported alongside it; see board.NewCodeCoverageThreshold.
	CoverageThreshold float64 `json:"coverage_threshold,omitempty"`
	// RequireOverallCoverage decides how a shortfall is phrased, not whether it
	// stops anything: on, the note says the repository's own bar was missed;
	// off, it states the figure alone so a run does not go chasing a number
	// nobody set. It survives from when this pair armed a gate.
	RequireOverallCoverage bool `json:"require_overall_coverage"`
	// MutationEnabled arms the mutation-score bar the same way
	// RequireOverallCoverage arms the coverage one: it decides how the run's
	// mutation note is phrased, not whether anything is held. Off by default —
	// a repository with no mutation tooling scores nothing to judge.
	MutationEnabled bool `json:"mutation_enabled"`
	// MutationThreshold is the percentage of mutants a run is expected to kill.
	// 0 means no number was set, in which case the score is reported bare.
	MutationThreshold float64 `json:"mutation_threshold,omitempty"`
	// Kind classifies the repo so pipeline auto-detect uses the right keyword
	// set (e.g. mobile never matches a docker build). One of RepoKind*.
	Kind string `json:"kind"`
	// MobilePlatform is only meaningful when Kind == RepoKindMobile: which
	// platform the app targets, so a run is not sent looking for an Xcode
	// project in an Android-only tree. One of MobilePlatform*, "" = unset.
	MobilePlatform string `json:"mobile_platform,omitempty"`
	// ReleaseEngine is where this repository's mobile releases are built and
	// uploaded: ReleaseEngineAuto (the default), ReleaseEngineActions or
	// ReleaseEngineLocal. Only meaningful when Kind == RepoKindMobile.
	//
	// It is a repository-level answer rather than a per-environment one
	// because the two engines produce the same artifact from the same script
	// — the choice is about which machine is available, not about what ships.
	ReleaseEngine string `json:"release_engine,omitempty"`
	// DetectedAppIdentity is the store identity read off the working copy at
	// import, for prefilling a store deploy target. Detection, not a decision:
	// what actually ships is the target's own bundle_id / package_name vars.
	DetectedAppIdentity AppIdentity `json:"detected_app_identity"`
	// DetectedBuildTargets is the Xcode scheme / Gradle module read off the
	// working copy at import, and it is the ONLY source the generated release
	// script gets them from — unlike DetectedAppIdentity, which merely
	// prefills a form a human still confirms. Either half "" refuses the
	// release rather than falling back to a convention; see BuildTargets.
	DetectedBuildTargets BuildTargets `json:"detected_build_targets"`
	// SubRepoKinds is only meaningful when Kind == RepoKindMonorepo: the set of
	// sub-repo kinds present, each getting its own category → job mapping.
	SubRepoKinds []string `json:"sub_repo_kinds,omitempty"`
	// SubProjects is only meaningful when Kind == RepoKindMonorepo: the
	// addressable, human-curated list of sub-projects detected at import, each
	// with its own path and kind. Distinct from SubRepoKinds (a deduplicated
	// set of kind values used for pipeline job routing) — this is the list a
	// human can retype or remove a row from; neither is derived from the other.
	SubProjects []RepoSubProject `json:"sub_projects,omitempty"`
	// AutoReleaseOnDone, when false, stops the done→prod-deploy auto-trigger so
	// a repo using batched release trains isn't force-deployed per task.
	AutoReleaseOnDone bool `json:"auto_release_on_done"`
	// RequireHumanReview, when true, makes code_review a human approval gate:
	// the reviewing agent still reviews and its verdict is still recorded, but
	// an approval is held instead of advancing the task, so a human moves it
	// forward. A rejection still goes through — approval is what is gated.
	//
	// Only code_review. pm_uat is deliberately not gated: its forward move is
	// into human_uat, which IS the human gate, and holding both made one person
	// approve the same task twice.
	RequireHumanReview bool `json:"require_human_review"`
	// RequireReviewChain, when true, refuses to let a task enter done (or
	// released) until it has passed every review stage its type requires —
	// code review, QA and UAT for a task/bug, analiz review for an analiz.
	// See Workflow.ReviewChain.
	//
	// Opt-in, and off by default, because the requirement is only honest on a
	// board that is actually wired for it: a repo with no QA agent subscribed,
	// or one whose board_columns were customised to drop in_qa or pm_uat, would
	// have every task parked in front of a stage nothing can satisfy. Turning
	// it on is the owner asserting the chain exists.
	RequireReviewChain bool `json:"require_review_chain"`
	// RequireReleaseDeploy, when true, refuses to let a task enter released
	// until a production deploy has actually succeeded for it (a task_pipelines
	// row with a prod_deploy trigger and a success status; a preprod_deploy
	// success counts only on a repo with no prod workflow mapped, which is the
	// same condition under which the pipeline runner itself treats preprod as
	// the release).
	//
	// Opt-in for a sharper reason than RequireReviewChain: a repo with no
	// deploy workflow mapped records its prod deploy as SKIPPED — nothing ran —
	// and a skipped pipeline is not evidence of a deploy. Such a repo must
	// leave this off, or its tasks would never leave done.
	RequireReleaseDeploy bool `json:"require_release_deploy"`
	// RequirePipelineForReview, when true (the default), holds the reviewing
	// architect back until the build/test pipeline has reported for the task
	// sitting in code_review. When false, code_review dispatches its reviewer
	// immediately and the pipeline — if one runs at all — is informational.
	//
	// It defaults TRUE, which is the opposite of RequireReviewChain and
	// RequireReleaseDeploy, and the asymmetry is the point: those two are new
	// requirements a repository opts INTO, while this is behaviour that has
	// always been on for every repository and is now opt-OUT-able. Turning it
	// off is the owner saying "this repository's CI cannot answer" — an
	// exhausted Actions quota, a repo whose checks live somewhere the control
	// plane cannot read, a board that simply does not want review gated on a
	// build.
	//
	// It is not the only thing that can open the gate: PipelineGateSweeper
	// opens it on a timeout or on a refusal from GitHub even here, because a
	// gate whose only key is an event nobody guarantees is a deadlock. This
	// setting is the difference between "wait for CI, but never forever" and
	// "do not wait for CI at all".
	RequirePipelineForReview bool `json:"require_pipeline_for_review"`
	// IncidentPolicy decides what a production incident on this repo triggers:
	// nothing, a diagnosis task that stops at a proposal, or a full board fix.
	IncidentPolicy IncidentPolicy `json:"incident_policy"`
	// TestStrategy decides how a task is verified before it moves on:
	// local (workspace tests only), stage (deploy to staging for QA, default)
	// or per_step (deploy at every reviewed step).
	TestStrategy string `json:"test_strategy"`
	// WebhookInstalled reports whether a GitHub push webhook is registered for
	// this repo (derived from the stored hook id — the secret itself never
	// leaves the store, let alone an API response).
	WebhookInstalled bool `json:"webhook_installed"`
	// Docs points at the four reference docs agents should read before
	// touching this repository's code. "" fields are unset — see
	// RepositoryDocs.
	Docs RepositoryDocs `json:"docs"`
	// DocsTaskID is the board task of the LAST reference-doc bundle asked for
	// (repodocs.Service.CreateDocsBundleTask): one task, one branch, one pull
	// request carrying every requested doc. "" = none outstanding. Overwritten
	// by the next bundle and cleared when its PR is merged.
	DocsTaskID string `json:"docs_task_id,omitempty"`
	// ProfileMD is the agent-maintained project profile: a compact markdown
	// brief of the codebase (stack, layout, commands, conventions) injected
	// into every repo-scoped agent run. "" = never profiled.
	ProfileMD string `json:"profile_md,omitempty"`
	// ProfileUpdatedAt stamps the last profile write; nil = never profiled.
	// The push-webhook trigger refreshes only when it is nil or stale.
	ProfileUpdatedAt *time.Time  `json:"profile_updated_at,omitempty"`
	ProjectIDs       []uuid.UUID `json:"project_ids,omitempty"`
	// GitWarning is the finished sentence the repository card shows above its
	// root path: no working copy at that path, no folder at that path, or a
	// folder that could not be read (see GitPresence.Warning), plus the async
	// git/GitHub setup failure the service records itself.
	//
	// English, and rendered verbatim by the web app — it is a server-composed
	// string, not a translation key, so there is nowhere on the client to look
	// one up. Localising it would mean threading the user's language into
	// every read path that returns a repository and turning Warning() into
	// Warning(lang), the way QuotaBlock.UserMessage takes one. That is a
	// deliberate not-yet, not an oversight.
	GitWarning string `json:"git_warning,omitempty"`
	// GitRestorable reports that the folder is genuinely missing here AND a
	// remote is recorded, i.e. the code can be fetched onto this machine. It is
	// the server's answer to "should the card offer to restore this", so the
	// client never has to reconstruct the rule from the warning sentence.
	GitRestorable bool `json:"git_restorable,omitempty"`
	// GitRestore is the running or last restore attempt, nil when none was ever
	// started on this process. It rides along on every repository read so a
	// reload mid-clone still shows the clone.
	GitRestore *RepositoryRestore `json:"git_restore,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

// Repo kinds. A monorepo carries SubRepoKinds drawn from the single-kind set.
const (
	RepoKindBackend  = "backend"
	RepoKindFrontend = "frontend"
	RepoKindMobile   = "mobile"
	RepoKindWorker   = "worker"
	RepoKindMonorepo = "monorepo"
)

// ValidRepoKind reports whether k is a known repo kind.
func ValidRepoKind(k string) bool {
	switch k {
	case RepoKindBackend, RepoKindFrontend, RepoKindMobile, RepoKindWorker, RepoKindMonorepo:
		return true
	}
	return false
}

// Mobile platforms. Only meaningful on a mobile repo or sub-project; "" is the
// unset value and is always allowed, because the platform is a guess made from
// the working copy and a repository registered before this existed has none.
const (
	MobilePlatformIOS     = "ios"
	MobilePlatformAndroid = "android"
	MobilePlatformCross   = "cross_platform"
)

// AppIdentity is a mobile app's store identity as READ OFF THE WORKING COPY:
// the iOS bundle id and the Android package name. Detected at import
// (repository.DetectAppIdentity), never typed by a human here — the human's
// own answer lives in a deploy target's bundle_id / package_name vars, which
// this only prefills.
//
// Both fields are always serialised, empty included: a consumer prefilling a
// form needs to see "nobody could read one", and an absent key and an empty
// one would spell that two ways.
type AppIdentity struct {
	BundleID    string `json:"bundle_id"`
	PackageName string `json:"package_name"`
}

// IsZero reports whether neither identifier could be read.
func (a AppIdentity) IsZero() bool { return a.BundleID == "" && a.PackageName == "" }

// BuildTargets is what a mobile working copy calls the thing that produces its
// release artifact, READ OFF THAT WORKING COPY: the Xcode scheme to archive and
// the Gradle module to bundle. Detected at import
// (repository.DetectBuildTargets).
//
// Unlike AppIdentity these are not a prefill anybody reviews — they are
// interpolated into the generated release script, so "" has to stay "" all the
// way to the release, where it refuses the build. The conventions they replace
// (scheme = the app's display name, module = "app") are true often enough to
// look right and wrong often enough to archive a target that does not exist.
type BuildTargets struct {
	// XcodeScheme is the iOS half: the SHARED scheme name, since an
	// unshared scheme is not in the repository and CI cannot archive it.
	XcodeScheme string `json:"xcode_scheme"`
	// GradleModule is the Android half, colon-separated without the leading
	// colon ("app", "apps:android") — the <module> in :<module>:bundleRelease.
	GradleModule string `json:"gradle_module"`
}

// IsZero reports whether neither target could be read.
func (b BuildTargets) IsZero() bool { return b.XcodeScheme == "" && b.GradleModule == "" }

// ValidMobilePlatform reports whether p is a known mobile platform. "" is not
// one: callers that allow the unset value test for it themselves, so nothing
// can accidentally treat "no answer" as a platform.
func ValidMobilePlatform(p string) bool {
	switch p {
	case MobilePlatformIOS, MobilePlatformAndroid, MobilePlatformCross:
		return true
	}
	return false
}

// ValidSubRepoKind reports whether k is a valid monorepo sub-repo kind (any
// single-kind repo type; monorepo cannot nest).
func ValidSubRepoKind(k string) bool {
	switch k {
	case RepoKindBackend, RepoKindFrontend, RepoKindMobile, RepoKindWorker:
		return true
	}
	return false
}

// AllSubRepoKinds returns every valid monorepo sub-repo kind. The pipeline
// settings UI computes auto-detect suggestions over all of them so a newly
// ticked sub-project shows its candidate jobs immediately, before the
// selection is saved.
func AllSubRepoKinds() []string {
	return []string{RepoKindBackend, RepoKindFrontend, RepoKindMobile, RepoKindWorker}
}

// RepoSubProject is one project inside a monorepo working copy: where it lives
// and what it is. Detected at import (repository.DetectRepoSubProjects) and
// then curated by the human in the initial-setup dialog, which can retype or
// remove a row before it is saved.
type RepoSubProject struct {
	// Path is repo-relative and slash-separated. "." is the repository root,
	// which a Go-module-at-root + web/ split legitimately produces.
	Path string `json:"path"`
	// Kind is one of RepoKind* except RepoKindMonorepo — monorepos do not nest.
	Kind string `json:"kind"`
	// MobilePlatform is only meaningful when Kind == RepoKindMobile, same
	// meaning as Repository.MobilePlatform but scoped to this sub-project.
	MobilePlatform string `json:"mobile_platform,omitempty"`
	// DetectedAppIdentity is Repository.DetectedAppIdentity scoped to this
	// sub-project's own directory. It rides inside the sub_projects JSON
	// column, so it needed no schema change of its own.
	DetectedAppIdentity AppIdentity `json:"detected_app_identity"`
	// DetectedBuildTargets is Repository.DetectedBuildTargets scoped to this
	// sub-project's own directory, and is never inherited from the repository:
	// a monorepo's mobile app has its own Xcode project and its own Gradle
	// build, and archiving the wrong one is exactly the failure this field
	// exists to prevent. Rides inside the sub_projects JSON column.
	DetectedBuildTargets BuildTargets `json:"detected_build_targets"`
	// Docs points at this sub-project's own four reference docs, same meaning
	// as Repository.Docs but paths are relative to Path, not the repo root.
	Docs RepositoryDocs `json:"docs,omitempty"`
	// The four quality-gate overrides. Nil means "inherit the repository's
	// setting", which is why they are pointers and not plain values: a
	// sub-project that never states an opinion must keep following the repo
	// even after the repo's own number changes, and a zero threshold is a
	// legitimate statement ("no bar") rather than an absent one.
	CoverageEnabled   *bool    `json:"coverage_enabled,omitempty"`
	CoverageThreshold *float64 `json:"coverage_threshold,omitempty"`
	MutationEnabled   *bool    `json:"mutation_enabled,omitempty"`
	MutationThreshold *float64 `json:"mutation_threshold,omitempty"`
}

// QualityGate is one resolved quality bar — whether it is armed and the
// percentage it is armed at. Threshold 0 means no number was set; the caller
// supplies its own default (see board.DefaultCoverageThreshold).
type QualityGate struct {
	Enabled   bool
	Threshold float64
}

// EffectiveCoverageGate resolves the whole-repo line-coverage bar for one
// scope: the sub-project at subProjectPath when it overrides it, otherwise the
// repository's own setting. "" asks for the repository itself.
func (r Repository) EffectiveCoverageGate(subProjectPath string) QualityGate {
	gate := QualityGate{Enabled: r.RequireOverallCoverage, Threshold: r.CoverageThreshold}
	if sp, ok := r.subProject(subProjectPath); ok {
		if sp.CoverageEnabled != nil {
			gate.Enabled = *sp.CoverageEnabled
		}
		if sp.CoverageThreshold != nil {
			gate.Threshold = *sp.CoverageThreshold
		}
	}
	return gate
}

// EffectiveMutationGate is EffectiveCoverageGate for the mutation score.
func (r Repository) EffectiveMutationGate(subProjectPath string) QualityGate {
	gate := QualityGate{Enabled: r.MutationEnabled, Threshold: r.MutationThreshold}
	if sp, ok := r.subProject(subProjectPath); ok {
		if sp.MutationEnabled != nil {
			gate.Enabled = *sp.MutationEnabled
		}
		if sp.MutationThreshold != nil {
			gate.Threshold = *sp.MutationThreshold
		}
	}
	return gate
}

func (r Repository) subProject(path string) (RepoSubProject, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return RepoSubProject{}, false
	}
	for _, sp := range r.SubProjects {
		if sp.Path == path {
			return sp, true
		}
	}
	return RepoSubProject{}, false
}

// RepositoryDocs names the reference docs agents should read before touching
// a repository (or one of its sub-projects): each field is a path relative to
// wherever this RepositoryDocs is scoped, "" meaning not set.
type RepositoryDocs struct {
	CodingStandards string `json:"coding_standards,omitempty"`
	TestStandards   string `json:"test_standards,omitempty"`
	Architecture    string `json:"architecture,omitempty"`
	LocalRun        string `json:"local_run,omitempty"`
}

// Reference-doc kinds. One RepositoryDocs field per kind.
const (
	RepoDocCodingStandards = "coding_standards"
	RepoDocTestStandards   = "test_standards"
	RepoDocArchitecture    = "architecture"
	RepoDocLocalRun        = "local_run"
)

// ValidRepoDocKind reports whether k is a known reference-doc kind.
func ValidRepoDocKind(k string) bool {
	switch k {
	case RepoDocCodingStandards, RepoDocTestStandards, RepoDocArchitecture, RepoDocLocalRun:
		return true
	}
	return false
}

// DefaultRepoDocPath is where a generated doc of this kind lands absent an
// explicit path — the .ai/ convention every repository in this codebase
// already follows for its own reference docs (see e.g. agent-server/.ai,
// web/.ai).
//
// local_run is the exception, and deliberately not a document: what a human
// (or an agent) needs on a fresh machine is a thing they can run, not prose
// they have to translate into commands. So its default is the bootstrap script
// itself.
func DefaultRepoDocPath(kind string) string {
	switch kind {
	case RepoDocCodingStandards:
		return ".ai/coding-standards.md"
	case RepoDocTestStandards:
		return ".ai/test-standards.md"
	case RepoDocArchitecture:
		return ".ai/architecture.md"
	case RepoDocLocalRun:
		return "scripts/dev.sh"
	default:
		return ""
	}
}

// maxSubProjects bounds a PATCH's sub_projects list so a hostile or buggy
// client cannot grow the column without limit; a real monorepo's own project
// count never comes close.
const maxSubProjects = 200

// ValidateSubProjects cleans and validates a client-supplied sub-project list:
// trims whitespace, rejects an empty or absolute path, a ".." path segment, a
// duplicate path, a kind that is not a valid single-kind repo type, a mobile
// platform on something that is not a mobile sub-project, or a quality-gate
// threshold outside 0-100. Returns the cleaned list, safe to persist as-is.
func ValidateSubProjects(in []RepoSubProject) ([]RepoSubProject, error) {
	if len(in) > maxSubProjects {
		return nil, fmt.Errorf("too many sub-projects: %d (max %d)", len(in), maxSubProjects)
	}
	seen := make(map[string]bool, len(in))
	out := make([]RepoSubProject, 0, len(in))
	for _, sp := range in {
		path := strings.TrimSpace(sp.Path)
		kind := strings.TrimSpace(sp.Kind)
		if path == "" {
			return nil, fmt.Errorf("sub-project path must not be empty")
		}
		if filepath.IsAbs(path) {
			return nil, fmt.Errorf("sub-project path must be relative: %s", path)
		}
		for _, seg := range strings.Split(path, "/") {
			if seg == ".." {
				return nil, fmt.Errorf("sub-project path must not contain '..': %s", path)
			}
		}
		if seen[path] {
			return nil, fmt.Errorf("duplicate sub-project path: %s", path)
		}
		if !ValidSubRepoKind(kind) {
			return nil, fmt.Errorf("invalid sub-project kind: %s", kind)
		}
		platform := strings.TrimSpace(sp.MobilePlatform)
		if err := validateMobilePlatform(platform, kind); err != nil {
			return nil, fmt.Errorf("sub-project %s: %w", path, err)
		}
		for label, value := range map[string]*float64{
			"coverage_threshold": sp.CoverageThreshold,
			"mutation_threshold": sp.MutationThreshold,
		} {
			if value != nil && (*value < 0 || *value > 100) {
				return nil, fmt.Errorf("sub-project %s: %s must be between 0 and 100, got %.1f", path, label, *value)
			}
		}
		// Carried through rather than validated: it is a detection result the
		// client is echoing back, not a statement it is making, so a PATCH of
		// some unrelated field must not erase it. Dropped on a sub-project that
		// is no longer mobile for the same reason — nobody stated it, so
		// nothing is being overruled.
		identity := sp.DetectedAppIdentity
		targets := sp.DetectedBuildTargets
		if kind != RepoKindMobile {
			identity = AppIdentity{}
			targets = BuildTargets{}
		}
		seen[path] = true
		out = append(out, RepoSubProject{
			Path:                 path,
			Kind:                 kind,
			MobilePlatform:       platform,
			DetectedAppIdentity:  identity,
			DetectedBuildTargets: targets,
			Docs:                 sp.Docs,
			CoverageEnabled:      sp.CoverageEnabled,
			CoverageThreshold:    sp.CoverageThreshold,
			MutationEnabled:      sp.MutationEnabled,
			MutationThreshold:    sp.MutationThreshold,
		})
	}
	return out, nil
}

// validateMobilePlatform is the one rule both the repository and its
// sub-projects are held to: a platform must be a known one, and it must sit on
// something that is actually mobile. "" always passes — it is the unset value.
func validateMobilePlatform(platform, kind string) error {
	if platform == "" {
		return nil
	}
	if !ValidMobilePlatform(platform) {
		return fmt.Errorf("invalid mobile platform: %s", platform)
	}
	if kind != RepoKindMobile {
		return fmt.Errorf("mobile_platform is only meaningful on a %s project, not %s", RepoKindMobile, kind)
	}
	return nil
}

// ValidateMobilePlatform is validateMobilePlatform for a repository-level
// value, where the kind is the repository's own.
func ValidateMobilePlatform(platform, kind string) error {
	return validateMobilePlatform(strings.TrimSpace(platform), strings.TrimSpace(kind))
}

// validateReleaseEngine mirrors validateMobilePlatform's shape but not its
// unset rule: "" is a legal input here, not just a pass-through, because the
// release_engine column defaults to ReleaseEngineAuto and a client that never
// mentions the field must be read as choosing that default rather than
// stating nothing.
func validateReleaseEngine(engine, kind string) error {
	if engine == "" {
		return nil
	}
	if !ValidReleaseEngine(engine) {
		return fmt.Errorf("invalid release engine: %s", engine)
	}
	if engine != ReleaseEngineAuto && kind != RepoKindMobile {
		return fmt.Errorf("release_engine is only meaningful on a %s project, not %s", RepoKindMobile, kind)
	}
	return nil
}

// ValidateReleaseEngine is validateReleaseEngine for a repository-level
// value, where the kind is the repository's own.
func ValidateReleaseEngine(engine, kind string) error {
	return validateReleaseEngine(strings.TrimSpace(engine), strings.TrimSpace(kind))
}

// Pipeline job mapping categories.
const (
	PipelineCategoryValidate     = "validate"
	PipelineCategoryBuild        = "build"
	PipelineCategoryTest         = "test"
	PipelineCategoryMutationTest = "mutation_test"
	// PipelineCategoryPROpen names the workflow that opens a PR once CI is
	// green. It is listed and selectable but never dispatched and never gated:
	// agent task branches (tt-123) do not match its feature/** trigger, because
	// the board opens their PR itself (EnsurePullRequest).
	PipelineCategoryPROpen        = "pr_open"
	PipelineCategoryStageDeploy   = "stage_deploy"
	PipelineCategoryPreProdDeploy = "preprod_deploy"
	PipelineCategoryProdDeploy    = "prod_deploy"
)

// PipelineJobTargetKind distinguishes a status-read job from a dispatchable workflow.
const (
	PipelineTargetJob      = "job"      // validate/build/test/mutation_test — a run job whose conclusion is read
	PipelineTargetWorkflow = "workflow" // pr_open/stage_deploy/prod_deploy — a workflow file
)

// RepositoryPipelineJob maps one (sub-repo, category) slot to a GitHub Actions
// job (status-read) or workflow file (dispatch). SubRepoKind is "" for
// single-kind repos.
type RepositoryPipelineJob struct {
	ID           uuid.UUID `json:"id"`
	RepositoryID uuid.UUID `json:"repository_id"`
	// SubProjectPath identifies which sub-project this slot belongs to ("" =
	// the repository itself, or a non-monorepo repo). SubRepoKind stays a
	// descriptive field (which kind that sub-project is, for keyword
	// suggestion purposes) — SubProjectPath is what disambiguates two
	// sub-projects that share a kind.
	SubProjectPath string `json:"sub_project_path,omitempty"`
	SubRepoKind    string `json:"sub_repo_kind"`
	Category       string `json:"category"`
	TargetKind     string `json:"target_kind"`
	TargetRef      string `json:"target_ref"`
	AutoDetected   bool   `json:"auto_detected"`
}

type OpenRepositoryRequest struct {
	RootPath    string      `json:"root_path"`
	Description string      `json:"description"`
	ProjectIDs  []uuid.UUID `json:"project_ids,omitempty"`
	// Owner: klasör git'siz ise GitHub reposunun açılacağı hesap/org ("" = token sahibi).
	Owner string `json:"owner,omitempty"`
	// CloneURL, bilinen origin adresidir. Boşsa çalışma kopyasının origin'inden
	// okunur; kalıcı olarak saklanır ki kopya kaybolursa repo geri çekilebilsin.
	CloneURL string `json:"clone_url,omitempty"`
	// Kind classifies the repo (one of RepoKind*). Optional: left empty it is
	// detected from what is on disk, so a repo is never silently registered as
	// a backend just because nobody said otherwise.
	Kind string `json:"kind,omitempty"`
}

type CreateRepositoryRequest struct {
	Name        string      `json:"name"`
	ParentDir   string      `json:"parent_dir"`
	Description string      `json:"description"`
	ProjectIDs  []uuid.UUID `json:"project_ids,omitempty"`
	// Owner: yeni GitHub reposunun açılacağı hesap/org ("" = token sahibi).
	Owner string `json:"owner,omitempty"`
	// Kind classifies the repo (one of RepoKind*); empty means auto-detect.
	Kind string `json:"kind,omitempty"`
}

// ImportGitHubRepositoryRequest, mevcut bir GitHub reposunu çalışma alanına
// klonlayıp kod deposu olarak kaydeder.
type ImportGitHubRepositoryRequest struct {
	Owner       string      `json:"owner"`
	Name        string      `json:"name"`
	CloneURL    string      `json:"clone_url,omitempty"`
	Description string      `json:"description"`
	ProjectIDs  []uuid.UUID `json:"project_ids,omitempty"`
	// Kind classifies the repo (one of RepoKind*); empty means auto-detect
	// from the cloned working copy.
	Kind string `json:"kind,omitempty"`
}

type UpdateRepositoryRequest struct {
	Name           string  `json:"name,omitempty"`
	Description    string  `json:"description,omitempty"`
	VerifyCommand  *string `json:"verify_command,omitempty"`
	BuildCommand   *string `json:"build_command,omitempty"`
	TestCommand    *string `json:"test_command,omitempty"`
	Kind           *string `json:"kind,omitempty"`
	MobilePlatform *string `json:"mobile_platform,omitempty"`
	// ReleaseEngine picks where the repository's mobile releases are built:
	// domain.ReleaseEngine*. A non-nil empty string is a legal statement — it
	// means ReleaseEngineAuto, the column's default (see validateReleaseEngine).
	ReleaseEngine      *string           `json:"release_engine,omitempty"`
	MutationEnabled    *bool             `json:"mutation_enabled,omitempty"`
	MutationThreshold  *float64          `json:"mutation_threshold,omitempty"`
	SubRepoKinds       *[]string         `json:"sub_repo_kinds,omitempty"`
	SubProjects        *[]RepoSubProject `json:"sub_projects,omitempty"`
	AutoReleaseOnDone  *bool             `json:"auto_release_on_done,omitempty"`
	RequireHumanReview *bool             `json:"require_human_review,omitempty"`
	IncidentPolicy     *IncidentPolicy   `json:"incident_policy,omitempty"`
	TestStrategy       *string           `json:"test_strategy,omitempty"`
	// Docs replaces the repository's own reference-doc pointers wholesale when
	// set. Nil leaves them untouched — see RepositoryStore.UpdateDocs.
	Docs *RepositoryDocs `json:"docs,omitempty"`
}

// SavePipelineJobsRequest replaces the full category→target mapping for a repo.
type SavePipelineJobsRequest struct {
	Jobs []RepositoryPipelineJob `json:"jobs"`
}

// TaskGitInfo is the GitHub coordinates of a task workspace's branch, used to
// locate its GitHub Actions runs and dispatch deploy workflows.
type TaskGitInfo struct {
	Owner   string
	Repo    string
	Branch  string
	HeadSHA string
}

type SetRepositoryProjectsRequest struct {
	ProjectIDs []uuid.UUID `json:"project_ids"`
}
