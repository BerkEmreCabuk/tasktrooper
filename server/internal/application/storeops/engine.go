// Release engine selection: which machine actually builds and uploads a
// mobile release. There are exactly two — GitHub Actions and a paired local
// runner — and when neither can run, the release is BLOCKED rather than
// quietly downgraded. See domain.ReleaseEngine* for why that is a product
// rule and not a defensive default.
package storeops

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops/pipeline"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// auditActionStoreBuild is the ops_audit_log action for a release build.
// ops_audit_log.action is free text (migration 062 lists the vocabulary in a
// comment, not a CHECK), so this lives here rather than widening the
// domain.OpsAction* set, which nothing else in this change touches.
const auditActionStoreBuild = "store_build"

// ErrInvalidEngine marks a caller-supplied release engine that is not one of
// domain.ReleaseEngine* — a 400 at the HTTP edge, not a 500.
var ErrInvalidEngine = errors.New("storeops: unknown release engine")

// ErrEngineUnavailable means the engine the repository PINS cannot run this
// release: `github_actions` on a repository whose Actions are billing-blocked
// or that has no release workflow, or `local` with no paired host that can
// build the platform. A pin is a statement, so it is refused rather than
// silently satisfied by the other engine — that fallback is what
// domain.ReleaseEngineAuto is for.
var ErrEngineUnavailable = errors.New("storeops: the pinned release engine is not available")

// ErrActionsNoWorkflow is the probe's way of saying the repository declares no
// mobile release workflow for Actions to dispatch. Counts as "Actions cannot
// run", not as a failure to determine that.
var ErrActionsNoWorkflow = errors.New("storeops: the repository has no mobile release workflow")

// ErrActionsBillingBlocked is the probe's way of saying GitHub refused on
// billing/quota grounds. A wiring that can classify the refusal itself (the
// adapter holds the typed API error) should wrap this instead of relying on
// the message match below.
var ErrActionsBillingBlocked = errors.New("storeops: GitHub Actions is blocked for billing or quota reasons")

// ErrBuildTargetUnknown means the working copy never stated what to build: no
// shared Xcode scheme, or no Gradle module applying com.android.application,
// was readable when the repository was imported
// (repository.DetectBuildTargets). The release is refused rather than run
// against a conventional name, because a scheme that does not exist fails
// inside xcodebuild with nothing pointing at the cause, and a module that does
// not exist fails inside Gradle the same way.
//
// The fix is always in the user's own repository, never here, so the wrapping
// error carries the per-platform instructions (see buildTargetRemedy) and this
// sentinel stays the thing callers match on.
var ErrBuildTargetUnknown = errors.New("storeops: the repository does not state what a release build should archive")

// ActionsProbe answers "can GitHub Actions build and upload this repository's
// mobile release right now". A nil error means yes.
//
// workflowFile is the base name of the workflow the dispatch will ask for —
// the EXACT file, not a family of them. A repository can carry several
// mobile-release workflows (one per app in a monorepo), and answering "yes"
// because some other app's workflow exists sends the release to a dispatch
// GitHub answers 404, which is the one failure this probe is there to prevent.
//
// A narrow function type declared here rather than a port interface, matching
// the SetStoreOnboarder idiom in application/deploy: the only implementation
// is a closure in platform/runtime that reaches adapter/github, and this
// package must not import an adapter to describe it.
type ActionsProbe func(ctx context.Context, repo domain.Repository, platform, workflowFile string) error

// LocalRunnerHost is what a paired local runner can build. The zero value is
// "nothing paired", which is a different answer from "paired but cannot build
// iOS" and has to stay distinguishable — the second one is the state an
// install with a Linux runner and an iOS app is actually in.
type LocalRunnerHost struct {
	Paired bool
	// MacOS reports whether the paired host is a Mac. Signalled by its ability
	// to run iOS simulators (runner.MobileHost.SupportsIOSSimulators), which is
	// the only capability the tunnel reports that a non-Mac can never have.
	MacOS bool
}

// LocalRunnerProbe reads the paired runner host for the acting member. It
// takes no platform: what a host can build is a fact about the machine, and
// the per-platform rule (iOS needs macOS) belongs here in the application
// layer where it is testable, not in the wiring.
type LocalRunnerProbe func(ctx context.Context) (LocalRunnerHost, error)

// ReleaseStarter DELIVERS the generated release files into the repository and
// then starts them on the engine that will run them: a workflow dispatch for
// Actions, a runner job for local. Engine is already resolved — the starter
// never re-decides it.
//
// Delivery is half the contract, not an implementation detail of one engine.
// Neither machine can run a file that is not in the repository: Actions
// dispatches a workflow by name out of the default branch, and a local runner
// executes a script out of a checkout. A starter that only dispatched would
// run whatever the tree happened to hold, which is the previous binding's
// release for as long as nobody noticed.
//
// A starter that cannot start the engine it was handed reports it by wrapping
// domain.ErrNoReleaseEngine, so StartBuild parks the card instead of returning
// an error nobody can act on (see StartBuild).
type ReleaseStarter func(ctx context.Context, repo domain.Repository, app domain.MobileStoreApp, engine string, artifacts []pipeline.Artifact) error

// ReleaseParker parks a board task on a resource. Same shape as
// board.ResourceParker, declared locally so this package does not depend on a
// sibling application package.
type ReleaseParker interface {
	BlockOnResource(ctx context.Context, repositoryID, taskID uuid.UUID, resource, detail string) (domain.TaskColumn, error)
}

// SetEngineProbes wires the two availability probes. Late-set, matching
// SetAuditor: without them ResolveEngine reports both engines unavailable
// rather than panicking, which is the honest answer for a deployment that
// wired neither.
func (s *Service) SetEngineProbes(actions ActionsProbe, local LocalRunnerProbe) {
	s.actionsProbe = actions
	s.localProbe = local
}

// SetReleaseStarter wires what actually launches a resolved release.
func (s *Service) SetReleaseStarter(start ReleaseStarter) { s.startRelease = start }

// SetReleaseParker wires the board-side park used when no engine can run.
func (s *Service) SetReleaseParker(parker ReleaseParker) { s.parker = parker }

// ResolveEngine decides where repo's platform release is built.
//
// A repository that PINS an engine gets that engine or an error: a pin is a
// statement about where releases are allowed to run, and satisfying it with
// the other machine would ship from somewhere nobody approved.
//
// ReleaseEngineAuto tries GitHub Actions FIRST — it is the machine the whole
// pipeline is written for, and it does not depend on somebody's laptop being
// open — then the local runner, then gives up with domain.ErrNoReleaseEngine.
// "Gives up" is the whole point: there is no third path, so the caller parks
// the work instead of inventing one.
//
// workflowFile is the exact workflow this release will dispatch, which is what
// the Actions probe has to look for. Its ABSENCE from the repository is the one
// Actions objection that does not count against it, on either the pinned or the
// auto path: the starter writes that file before dispatching, so a repository
// that has never released is not one Actions cannot run — it is one Actions has
// not been given the file for yet. It still ranks below a paired local runner
// under auto, which can build with nothing to write first.
func (s *Service) ResolveEngine(ctx context.Context, repo domain.Repository, platform, workflowFile string) (string, error) {
	if !validPlatform(platform) {
		return "", fmt.Errorf("storeops: resolving release engine: unsupported platform %q: %w", platform, ErrInvalidPlatform)
	}
	engine := strings.TrimSpace(repo.ReleaseEngine)
	if engine == "" {
		engine = domain.ReleaseEngineAuto
	}
	if !domain.ValidReleaseEngine(engine) {
		return "", fmt.Errorf("storeops: %q: %w", engine, ErrInvalidEngine)
	}

	switch engine {
	case domain.ReleaseEngineActions:
		if err := s.actionsReady(ctx, repo, platform, workflowFile); err != nil {
			// A pinned engine is not held to the "the workflow is missing"
			// objection at all: the starter writes that file before it
			// dispatches, so the repository the pin describes is the one this
			// release is about to create.
			if !errors.Is(err, ErrActionsNoWorkflow) {
				return "", err
			}
		}
		return domain.ReleaseEngineActions, nil
	case domain.ReleaseEngineLocal:
		ok, err := s.localReady(ctx, platform)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf(
				"storeops: this repository pins the local runner and no paired host can build %s: %w", platform, ErrEngineUnavailable)
		}
		return domain.ReleaseEngineLocal, nil
	}

	actionsErr := s.actionsReady(ctx, repo, platform, workflowFile)
	switch {
	case actionsErr == nil:
		return domain.ReleaseEngineActions, nil
	case !errors.Is(actionsErr, ErrEngineUnavailable):
		// Not "Actions cannot run" but "we could not find out" — a 5xx, a
		// timeout, a token that failed to resolve. Falling through to the
		// local runner on that would hide an outage behind a laptop that may
		// be shut, and would make the next successful sweep look like a
		// regression, so it propagates instead.
		return "", actionsErr
	}
	ok, err := s.localReady(ctx, platform)
	if err != nil {
		return "", err
	}
	if ok {
		return domain.ReleaseEngineLocal, nil
	}
	if errors.Is(actionsErr, ErrActionsNoWorkflow) {
		// The only thing Actions lacked is the workflow THIS release
		// generates, and the starter commits it before dispatching. A
		// repository that has never released is the ordinary way into this
		// branch, and blocking it would mean the file that makes the system
		// work can never arrive by the system's own hand. It ranks below the
		// local runner rather than above it because a paired Mac is a machine
		// that can build today, with nothing to write first.
		return domain.ReleaseEngineActions, nil
	}
	return "", domain.ErrNoReleaseEngine
}

// actionsReady returns nil when Actions can run, an ErrEngineUnavailable-
// wrapped error when it definitely cannot, and the probe's own error when the
// question could not be answered. Those three are deliberately distinct: only
// the middle one may cause a fallback.
func (s *Service) actionsReady(ctx context.Context, repo domain.Repository, platform, workflowFile string) error {
	if s.actionsProbe == nil {
		return fmt.Errorf("storeops: no GitHub Actions probe is wired: %w", ErrEngineUnavailable)
	}
	err := s.actionsProbe(ctx, repo, platform, workflowFile)
	if err == nil {
		return nil
	}
	if actionsUnavailable(err) {
		// Both causes are wrapped, not flattened: the caller has to be able to
		// tell a missing workflow (which this release writes) from a billing
		// refusal (which it cannot) long after this sentence has been read.
		return fmt.Errorf("storeops: GitHub Actions cannot run this release (%w): %w", err, ErrEngineUnavailable)
	}
	return fmt.Errorf("storeops: checking GitHub Actions availability: %w", err)
}

// localReady applies the one per-platform rule: an iOS archive needs Xcode,
// which needs macOS, so a paired Linux host is no host at all for iOS. The
// Android toolchain is portable and runs on any paired machine.
func (s *Service) localReady(ctx context.Context, platform string) (bool, error) {
	if s.localProbe == nil {
		return false, nil
	}
	host, err := s.localProbe(ctx)
	if err != nil {
		return false, fmt.Errorf("storeops: checking the paired local runner: %w", err)
	}
	if !host.Paired {
		return false, nil
	}
	if platform == domain.MobileStorePlatformIOS {
		return host.MacOS, nil
	}
	return true, nil
}

// actionsUnavailable classifies a probe failure as "Actions cannot run on this
// repository at all" versus a transient failure worth reporting as one.
//
// The two definite readings are a missing release workflow and a
// billing/quota refusal. A wiring that holds the typed GitHub error should
// wrap ErrActionsNoWorkflow / ErrActionsBillingBlocked and this never has to
// guess. The message match below is the fallback for the case where it
// cannot: `application` must not import `adapter/github`, so the typed
// *apiError — and with it the 402 Payment Required status — has already been
// flattened into text ("github api: 402 …") by the time it arrives here. That
// is the same reason github.IsCIUnavailableText exists on the CI side, and
// the two needle lists are deliberately the same wordings; a refusal GitHub
// rewords will stop being recognised in both places at once.
//
// A plain 5xx matches nothing here and therefore stays transient — which is
// the distinction the whole engine choice turns on. An org whose Actions were
// switched off for non-payment is a permanent state a fallback should answer;
// GitHub having a bad ten minutes is not.
func actionsUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrActionsNoWorkflow) || errors.Is(err, ErrActionsBillingBlocked) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "github api: 402") {
		return true
	}
	for _, needle := range []string{
		"billing", "spending limit", "quota", "payment",
		"actions is disabled", "actions are disabled", "upgrade",
		"has been disabled", "not allowed to run",
		"no workflow", "workflow not found",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// BuildStart is what StartBuild settled on, for the console to render.
type BuildStart struct {
	Platform string `json:"platform"`
	Engine   string `json:"engine"`
	// Artifacts are the repo-relative paths of the files the starter delivered
	// into the repository for this release, in the order pipeline.Render
	// produced them (script before workflow). They are a report of what was
	// written, not of what was rendered — StartBuild returns nothing at all
	// unless the starter's delivery succeeded.
	Artifacts []string `json:"artifacts"`
}

// workflowDirPrefix is where GitHub reads workflows and the only place a
// dispatchable one can sit, so it is how the workflow is picked out of a
// rendered set without depending on the order they came in.
const workflowDirPrefix = ".github/workflows/"

// workflowFileName is the base name of the workflow among artifacts — what
// GitHub's dispatch endpoint is addressed with, and therefore what the Actions
// probe has to look for. "" when the set carries no workflow, which the probe
// reads as "no such workflow".
func workflowFileName(artifacts []pipeline.Artifact) string {
	for _, artifact := range artifacts {
		if strings.HasPrefix(artifact.Path, workflowDirPrefix) {
			return path.Base(artifact.Path)
		}
	}
	return ""
}

// StartBuild builds and uploads the repository's mobile release on whichever
// engine ResolveEngine picks. engine overrides the repository's own setting
// for this one run; "" uses the repository setting.
//
// It is the caller domain.ErrNoReleaseEngine was written for: when neither
// engine can run, the onboarding task is parked on human_decision and the
// error is returned, because paying the Actions bill or opening a Mac is a
// person's job and no sweeper can do it (see domain.ResourceHumanDecision).
func (s *Service) StartBuild(ctx context.Context, repositoryID uuid.UUID, platform, engine, actor string) (BuildStart, error) {
	// Audited before the repository is even loaded, because a refusal here is
	// still an attempt somebody made. The row is the only trace a malformed or
	// probing call leaves, and it is exactly the one worth having.
	if !validPlatform(platform) {
		err := fmt.Errorf("storeops: starting a release build: unsupported platform %q: %w", platform, ErrInvalidPlatform)
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, nil, err)
		return BuildStart{}, err
	}
	engine = strings.TrimSpace(engine)
	if engine != "" && !domain.ValidReleaseEngine(engine) {
		err := fmt.Errorf("storeops: %q: %w", engine, ErrInvalidEngine)
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": engine}, err)
		return BuildStart{}, err
	}

	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return BuildStart{}, err
	}
	if app.Identifier == "" {
		notReady := fmt.Errorf("storeops: no store app is linked for %s: %w", platform, ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, nil, notReady)
		return BuildStart{}, notReady
	}
	if engine != "" {
		// An override is held to the same rule the settings form is: an engine
		// other than auto only means anything on a mobile repository.
		if err := domain.ValidateReleaseEngine(engine, repo.Kind); err != nil {
			return BuildStart{}, fmt.Errorf("%v: %w", err, ErrInvalidEngine)
		}
		repo.ReleaseEngine = engine
	}

	// Generated BEFORE the engine is chosen, because the choice depends on what
	// was generated: the Actions probe asks whether the repository declares the
	// exact workflow this release will dispatch, and only these artifacts know
	// its name (a monorepo scopes it per sub-project).
	spec := s.buildSpec(repo, app)
	if remedy := buildTargetRemedy(spec); remedy != "" {
		unknown := fmt.Errorf("%w (%s): %s", ErrBuildTargetUnknown, app.Identifier, remedy)
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": engine}, unknown)
		return BuildStart{}, unknown
	}
	artifacts, err := pipeline.Render(spec)
	if err != nil {
		wrapped := fmt.Errorf("storeops: generating the release pipeline for %s: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": engine}, wrapped)
		return BuildStart{}, wrapped
	}

	resolved, err := s.ResolveEngine(ctx, repo, platform, workflowFileName(artifacts))
	if err != nil {
		if errors.Is(err, domain.ErrNoReleaseEngine) {
			err = s.parkNoEngine(ctx, repo, app, err)
		}
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": engine}, err)
		return BuildStart{}, err
	}

	if s.startRelease == nil {
		// No starter at all is the same fact as no engine, and it is parked as
		// one: nothing about this deployment can run the release, and a person
		// has to change that.
		wrapped := s.parkNoEngine(ctx, repo, app, fmt.Errorf(
			"storeops: no release starter is wired for the %s engine: %w", resolved, domain.ErrNoReleaseEngine))
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": resolved}, wrapped)
		return BuildStart{}, wrapped
	}
	if err := s.startRelease(ctx, repo, app, resolved, artifacts); err != nil {
		wrapped := fmt.Errorf("storeops: starting the %s release of %s: %w", resolved, app.Identifier, err)
		if errors.Is(err, domain.ErrNoReleaseEngine) {
			// The probes found a machine the starter turns out not to be able
			// to drive — a paired Mac on a deployment whose local runner is not
			// wired is the live case. Parked rather than returned raw: to the
			// person who pressed the button this is the same wall as having no
			// engine at all, and only a human can take it down.
			wrapped = s.parkNoEngine(ctx, repo, app, wrapped)
		}
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": resolved}, wrapped)
		return BuildStart{}, wrapped
	}

	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		paths = append(paths, artifact.Path)
	}
	s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": resolved}, nil)
	return BuildStart{Platform: platform, Engine: resolved, Artifacts: paths}, nil
}

// buildSpec derives the generator's input from the store binding plus what the
// repository states about itself. Nothing here comes from a template variable
// a human typed twice — the identifier and the app name are the ones the store
// console confirmed when the app was linked, and the build targets are the
// ones read off the working copy at import
// (repository.DetectBuildTargets, columns detected_xcode_scheme /
// detected_gradle_module).
//
// The build targets used to be conventions invented right here — the Xcode
// scheme named after the app, the Gradle module called `app`. Both are wrong
// often enough to matter and neither announces itself: xcodebuild fails deep
// in a build log naming a scheme nobody chose. What the tree says is the only
// answer this returns, and where it says nothing the caller refuses the
// release rather than shipping a guess (see buildTargetRemedy).
func (s *Service) buildSpec(repo domain.Repository, app domain.MobileStoreApp) pipeline.Spec {
	spec := pipeline.Spec{
		Platform:   app.Platform,
		Identifier: app.Identifier,
		AppName:    app.AppName,
		StoreAppID: app.StoreAppID,
	}
	if spec.AppName == "" {
		spec.AppName = repo.Name
	}
	// A monorepo ships from the sub-project that actually holds the app, so
	// the generated paths are scoped to it. Matching on the identifier first
	// and the platform second: two mobile sub-projects in one tree is exactly
	// the case a platform-only match would get wrong.
	targets := repo.DetectedBuildTargets
	for _, sub := range repo.SubProjects {
		if sub.Kind != domain.RepoKindMobile {
			continue
		}
		identity := sub.DetectedAppIdentity
		if identity.BundleID == app.Identifier || identity.PackageName == app.Identifier {
			spec.SubProjectPath, targets = sub.Path, sub.DetectedBuildTargets
			break
		}
		if spec.SubProjectPath == "" && subProjectCovers(sub.MobilePlatform, app.Platform) {
			spec.SubProjectPath, targets = sub.Path, sub.DetectedBuildTargets
		}
	}
	// Never inherited across the sub-project line, the same way
	// deploy.Service.Config refuses to inherit the detected identity: a
	// monorepo's mobile app has its own Xcode project, and archiving the
	// repository-level scheme inside it would build the wrong thing.
	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		spec.Scheme = targets.XcodeScheme
	case domain.MobileStorePlatformAndroid:
		spec.Module = targets.GradleModule
	}
	return spec
}

// buildTargetRemedy reports what the person who pressed the button has to go
// and do, or "" when the working copy already stated the target this platform
// needs.
//
// Checked before the release starts rather than left to pipeline.Render, which
// would also refuse: Render's message describes a malformed Spec to whoever
// wrote the code, while this one is read by somebody whose repository is the
// thing that needs changing. Both sentences name a file in that repository on
// purpose — "share the scheme" is not advice, it is the missing commit.
func buildTargetRemedy(spec pipeline.Spec) string {
	switch spec.Platform {
	case domain.MobileStorePlatformIOS:
		if strings.TrimSpace(spec.Scheme) == "" {
			return "no shared Xcode scheme was found in the working copy: share the app's scheme in Xcode (Product › Scheme › Manage Schemes, tick Shared), commit the .xcscheme file it writes under xcshareddata/xcschemes, and re-import the repository"
		}
	case domain.MobileStorePlatformAndroid:
		if strings.TrimSpace(spec.Module) == "" {
			return "no Gradle module applying com.android.application was found in the working copy: make sure settings.gradle includes the app module and that its build.gradle applies that plugin, then re-import the repository"
		}
	}
	return ""
}

// subProjectCovers reports whether a sub-project's declared mobile platform
// includes the store platform being built. cross_platform covers both; an
// unset platform states nothing and covers neither.
func subProjectCovers(mobilePlatform, storePlatform string) bool {
	switch mobilePlatform {
	case domain.MobilePlatformCross:
		return true
	case domain.MobilePlatformIOS:
		return storePlatform == domain.MobileStorePlatformIOS
	case domain.MobilePlatformAndroid:
		return storePlatform == domain.MobileStorePlatformAndroid
	}
	return false
}

// blockedReleaseRemedy is the one sentence that turns a blocked release into
// something a person can act on. It is the card's text and the error's, so the
// board and the console never say two different things.
const blockedReleaseRemedy = "Enable GitHub Actions for this repository (or settle its billing), or pair a Mac as a local runner, then start the release again."

// parkNoEngine records the refusal where a human will see it — on the board,
// not only in a failed API call — and returns an error that still matches
// domain.ErrNoReleaseEngine. Same shape as repository.blockRelease, with the
// park added: this block has no automatic way out (see
// domain.ResourceHumanDecision), so the card has to stop rather than be
// retried into the same wall.
//
// A park that did not happen is reported, never swallowed. The HTTP layer
// answers 409 with this text and tells the operator the card was parked; if it
// was not, that sentence is the only place they could have learned otherwise.
func (s *Service) parkNoEngine(ctx context.Context, repo domain.Repository, app domain.MobileStoreApp, cause error) error {
	detail := cause.Error() + " (" + app.Platform + ")"

	taskID, err := s.blockedReleaseTask(ctx, repo, app, detail)
	if err != nil {
		return fmt.Errorf("%w; no board card records it: %v", cause, err)
	}
	if s.parker == nil {
		return fmt.Errorf("%w; no board card records it: no release parker is wired", cause)
	}
	if _, err := s.parker.BlockOnResource(ctx, repo.ID, taskID, domain.ResourceHumanDecision, detail); err != nil {
		return fmt.Errorf("%w; parking the board card on human_decision failed: %v", cause, err)
	}
	if s.comments != nil {
		// The comment is the explanation, not the block itself — the card is
		// already parked without it, so a failure here is logged rather than
		// turned into a release that reports itself unparked.
		if _, err := s.comments.AddComment(ctx, repo.ID, taskID, domain.CreateTaskCommentRequest{
			Content:    "Release blocked: " + detail + "\n\n" + blockedReleaseRemedy,
			AuthorType: "system",
		}); err != nil {
			log.Warn().Err(err).Str("repository_id", repo.ID.String()).
				Msg("storeops: commenting the release block failed")
		}
	}
	log.Warn().Err(cause).Str("repository_id", repo.ID.String()).Str("platform", app.Platform).
		Msg("release blocked: no release engine available")
	return cause
}

// blockedReleaseTask returns the board task this block belongs on, opening one
// when the app has none.
//
// An app bound through the store picker has no onboarding task — only Onboard
// (the deploy-target path) ever writes one — so making the park conditional on
// that field meant the whole picker population got a 409 and a log line, with
// nothing on the board. domain.ResourceHumanDecision says the board is exactly
// where a person is supposed to find this, so the card is opened rather than
// the park skipped.
func (s *Service) blockedReleaseTask(ctx context.Context, repo domain.Repository, app domain.MobileStoreApp, detail string) (uuid.UUID, error) {
	if app.OnboardingTaskID != nil {
		return *app.OnboardingTaskID, nil
	}
	if s.tasks == nil {
		return uuid.Nil, errors.New("no board task creator is wired")
	}
	task, err := s.tasks.CreateTask(ctx, repo.ID, domain.CreateBoardTaskRequest{
		Title:       fmt.Sprintf("Release blocked: %s (%s)", app.Identifier, app.Platform),
		Description: detail + "\n\n" + blockedReleaseRemedy,
		Priority:    domain.TaskPriorityHigh,
		Column:      domain.TaskColumnTodo,
		CreatedBy:   "system",
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("opening the board card failed: %w", err)
	}
	// Recorded on the row so the next blocked attempt parks THIS card instead
	// of opening another one. Best-effort: the card exists and is about to be
	// parked either way, and losing the release to a failed bookkeeping write
	// would be the worse trade.
	if err := s.rememberBlockedReleaseTask(ctx, app, task.ID); err != nil {
		log.Warn().Err(err).Str("repository_id", repo.ID.String()).
			Msg("storeops: recording the release block card on the store app row failed")
	}
	return task.ID, nil
}

// rememberBlockedReleaseTask re-reads the row before writing it back, rather
// than writing the copy the caller has been holding since the start of the
// request: Upsert writes every column, and the store round-trips in between
// are long enough for the monitor or a prod deploy to have moved the
// lifecycle on (see port.MobileStoreAppStore.SetTracks).
func (s *Service) rememberBlockedReleaseTask(ctx context.Context, app domain.MobileStoreApp, taskID uuid.UUID) error {
	current, err := s.apps.Get(ctx, app.RepositoryID, app.Platform)
	if err != nil {
		return err
	}
	if current.OnboardingTaskID != nil {
		return nil
	}
	current.OnboardingTaskID = &taskID
	_, err = s.apps.Upsert(ctx, current)
	return err
}
