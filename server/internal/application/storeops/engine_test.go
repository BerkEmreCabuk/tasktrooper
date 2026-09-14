package storeops_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops/pipeline"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// engineFixture bundles what an engine test scripts (the two probes) and what
// it asserts against (the parker, the starter's record, the audit log).
type engineFixture struct {
	repoID uuid.UUID
	taskID uuid.UUID
	repos  *fakeRepositoryResolver
	apps   *fakeMobileStoreAppStore
	audit  *fakeOpsAuditStore
	parker *fakeReleaseParker
	tasks  *fakeTaskCreator

	// actionsErr / localHost script the probes; actionsCalls and localCalls
	// count them so a test can prove auto asked Actions first and stopped
	// asking once it had an answer.
	actionsErr   error
	localHost    storeops.LocalRunnerHost
	localErr     error
	actionsCalls int
	localCalls   int
	// probedWorkflow is the workflow file name the Actions probe was asked
	// about, which is the whole question on a monorepo.
	probedWorkflow string

	// started records every release the starter was asked to launch.
	started []startedRelease
	// startErr scripts a starter failure.
	startErr error
}

type startedRelease struct {
	Engine    string
	Artifacts []pipeline.Artifact
}

// testWorkflowFile is what pipeline.Render names the workflow for a
// repository-root app — the exact file StartBuild asks the probe about.
const testWorkflowFile = "mobile-release.yml"

// newEngineTestService wires a service with one repository that has an iOS and
// an Android app linked and live, an onboarding task on file (so a park has
// something to park), and both probes answering "available" until a test says
// otherwise.
func newEngineTestService(t *testing.T) (*storeops.Service, *engineFixture) {
	t.Helper()

	repoID, taskID := uuid.New(), uuid.New()
	repos := newFakeRepositoryResolver()
	// The build targets stand in for what import detection read off the working
	// copy. Without them every StartBuild here would be refused, which is the
	// point of the fixture carrying them: a repository that states nothing
	// cannot be released.
	repos.set(domain.Repository{
		ID: repoID, Name: "trooper", Kind: domain.RepoKindMobile,
		DetectedBuildTargets: domain.BuildTargets{XcodeScheme: "Trooper", GradleModule: "app"},
	})

	apps := newFakeMobileStoreAppStore()
	for platform, identifier := range map[string]string{
		domain.MobileStorePlatformIOS:     "com.example.ios",
		domain.MobileStorePlatformAndroid: "com.example.android",
	} {
		if _, err := apps.Upsert(context.Background(), domain.MobileStoreApp{
			RepositoryID:     repoID,
			Platform:         platform,
			Identifier:       identifier,
			StoreAppID:       "asc-app-1",
			AppName:          "Trooper",
			State:            domain.MobileStoreStateLive,
			OnboardingTaskID: &taskID,
		}); err != nil {
			t.Fatal(err)
		}
	}

	f := &engineFixture{
		repoID:    repoID,
		taskID:    taskID,
		repos:     repos,
		apps:      apps,
		audit:     newFakeOpsAuditStore(),
		parker:    &fakeReleaseParker{},
		tasks:     newFakeTaskCreator(),
		localHost: storeops.LocalRunnerHost{Paired: true, MacOS: true},
	}

	svc := storeops.NewService(storeops.Deps{
		Credentials: newFakeCredentialStore(),
		Apps:        apps,
		Repos:       repos,
		Tasks:       f.tasks,
	})
	svc.SetAuditor(f.audit)
	svc.SetReleaseParker(f.parker)
	svc.SetEngineProbes(
		func(_ context.Context, _ domain.Repository, _, workflowFile string) error {
			f.actionsCalls++
			f.probedWorkflow = workflowFile
			return f.actionsErr
		},
		func(context.Context) (storeops.LocalRunnerHost, error) {
			f.localCalls++
			return f.localHost, f.localErr
		},
	)
	svc.SetReleaseStarter(func(_ context.Context, _ domain.Repository, _ domain.MobileStoreApp, engine string, artifacts []pipeline.Artifact) error {
		f.started = append(f.started, startedRelease{Engine: engine, Artifacts: artifacts})
		return f.startErr
	})
	return svc, f
}

func (f *engineFixture) repo() domain.Repository {
	repo, err := f.repos.Get(context.Background(), f.repoID)
	if err != nil {
		panic(err)
	}
	return repo
}

func (f *engineFixture) withEngine(engine string) domain.Repository {
	repo := f.repo()
	repo.ReleaseEngine = engine
	return repo
}

// Auto's whole order in one test: Actions when it can run, the local runner
// when it cannot, and a hard stop when neither can. The third case is the one
// that matters — there is no third engine, so anything other than
// ErrNoReleaseEngine here would be a silent downgrade.
func TestResolveEngineAutoPrefersActionsThenLocalThenBlocks(t *testing.T) {
	svc, f := newEngineTestService(t)
	ctx := context.Background()

	engine, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
	if err != nil || engine != domain.ReleaseEngineActions {
		t.Fatalf("engine = %q, err = %v; want github_actions", engine, err)
	}
	if f.localCalls != 0 {
		t.Fatal("the local runner was probed even though Actions could run")
	}

	f.actionsErr = errors.New("github api: 402 Payment Required")
	engine, err = svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
	if err != nil || engine != domain.ReleaseEngineLocal {
		t.Fatalf("engine = %q, err = %v; want local", engine, err)
	}

	f.localHost = storeops.LocalRunnerHost{}
	if _, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile); !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine", err)
	}
}

// The billing refusal this whole classification exists for: an org whose
// Actions are blocked for non-payment is a permanent state auto answers by
// falling back, and it must be told apart from GitHub simply being down.
func TestResolveEngineTellsABillingRefusalFromAnOutage(t *testing.T) {
	svc, f := newEngineTestService(t)
	ctx := context.Background()
	f.localHost = storeops.LocalRunnerHost{Paired: true, MacOS: true}

	for _, refusal := range []error{
		errors.New("github api: 402 Payment Required"),
		errors.New("github api: 403 The spending limit for this account has been reached"),
		errors.New("github api: 403 Actions is disabled for this repository"),
		storeops.ErrActionsNoWorkflow,
		storeops.ErrActionsBillingBlocked,
	} {
		f.actionsErr = refusal
		engine, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
		if err != nil || engine != domain.ReleaseEngineLocal {
			t.Fatalf("%v: engine = %q, err = %v; want a fallback to local", refusal, engine, err)
		}
	}

	// A plain server error is NOT "Actions cannot run" — falling back on it
	// would hide an outage behind a laptop that may be shut.
	f.actionsErr = errors.New("github api: 500 Internal Server Error")
	if _, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile); err == nil ||
		errors.Is(err, domain.ErrNoReleaseEngine) || strings.Contains(err.Error(), "local") {
		t.Fatalf("err = %v, want the transient failure propagated", err)
	}
}

// An iOS archive needs Xcode, which needs macOS. A paired Linux runner is no
// runner at all for iOS — and with Actions out, that is a blocked release, not
// an Android-style "any host will do".
func TestResolveEngineRefusesANonMacHostForIOS(t *testing.T) {
	svc, f := newEngineTestService(t)
	ctx := context.Background()
	// A billing refusal, not a missing workflow: only the first is an
	// objection this release cannot answer for itself (see the workflow test
	// below), so it is the one that makes "no engine" mean no engine.
	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.localHost = storeops.LocalRunnerHost{Paired: true, MacOS: false}

	if _, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformIOS, testWorkflowFile); !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine for iOS on a non-Mac host", err)
	}
	// The same host builds Android perfectly well.
	engine, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
	if err != nil || engine != domain.ReleaseEngineLocal {
		t.Fatalf("engine = %q, err = %v; want local for android", engine, err)
	}
}

// A pin is a statement about where releases may run. Satisfying it with the
// other machine would ship from somewhere nobody approved, so an unavailable
// pinned engine is refused rather than swapped.
func TestResolveEngineNeverSubstitutesAPinnedEngine(t *testing.T) {
	svc, f := newEngineTestService(t)
	ctx := context.Background()

	f.actionsErr = storeops.ErrActionsBillingBlocked
	if _, err := svc.ResolveEngine(ctx, f.withEngine(domain.ReleaseEngineActions), domain.MobileStorePlatformAndroid, testWorkflowFile); !errors.Is(err, storeops.ErrEngineUnavailable) {
		t.Fatalf("err = %v, want ErrEngineUnavailable", err)
	}
	if f.localCalls != 0 {
		t.Fatal("a repository pinned to Actions fell through to the local runner")
	}

	f.actionsErr = nil
	f.localHost = storeops.LocalRunnerHost{}
	if _, err := svc.ResolveEngine(ctx, f.withEngine(domain.ReleaseEngineLocal), domain.MobileStorePlatformAndroid, testWorkflowFile); !errors.Is(err, storeops.ErrEngineUnavailable) {
		t.Fatalf("err = %v, want ErrEngineUnavailable", err)
	}
}

func TestResolveEngineRejectsAnUnknownEngine(t *testing.T) {
	svc, f := newEngineTestService(t)
	if _, err := svc.ResolveEngine(context.Background(), f.withEngine("jenkins"), domain.MobileStorePlatformAndroid, testWorkflowFile); !errors.Is(err, storeops.ErrInvalidEngine) {
		t.Fatalf("err = %v, want ErrInvalidEngine", err)
	}
}

// With no engine to run on, the release does not fail quietly into a log line:
// the card is parked on human_decision, which has no sweeper on purpose —
// paying the bill or opening a Mac is a person's job.
func TestStartBuildParksTheTaskWhenNoEngineCanRun(t *testing.T) {
	svc, f := newEngineTestService(t)
	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.localHost = storeops.LocalRunnerHost{}

	_, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif")
	if !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine", err)
	}
	if len(f.parker.Calls) != 1 {
		t.Fatalf("parks = %d, want the card parked exactly once", len(f.parker.Calls))
	}
	park := f.parker.Calls[0]
	if park.TaskID != f.taskID || park.Resource != domain.ResourceHumanDecision {
		t.Fatalf("park = %+v, want the onboarding task on human_decision", park)
	}
	if len(f.started) != 0 {
		t.Fatal("a release was started despite having no engine to start it on")
	}
	entries, _ := f.audit.List(context.Background(), &f.repoID, 0)
	if len(entries) != 1 || entries[0].Outcome != domain.OpsOutcomeError {
		t.Fatalf("audit = %+v, want one failed store_build row", entries)
	}
}

// The body's engine overrides the repository's setting for one run, and the
// starter is handed the engine that was actually resolved — never the string
// the caller sent.
func TestStartBuildHonoursTheRequestedEngine(t *testing.T) {
	svc, f := newEngineTestService(t)
	f.actionsErr = errors.New("github api: 402 Payment Required")

	start, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, domain.ReleaseEngineLocal, "akif")
	if err != nil {
		t.Fatal(err)
	}
	if start.Engine != domain.ReleaseEngineLocal || len(f.started) != 1 || f.started[0].Engine != domain.ReleaseEngineLocal {
		t.Fatalf("start = %+v, started = %+v; want the local engine", start, f.started)
	}
	if f.actionsCalls != 0 {
		t.Fatal("Actions was probed for a run pinned to the local runner")
	}
	// The generated files are what the engine runs: the release script and the
	// workflow that calls it, in that order.
	if len(start.Artifacts) != 2 || !strings.HasSuffix(start.Artifacts[0], "mobile-release.sh") {
		t.Fatalf("artifacts = %v, want the script first", start.Artifacts)
	}
}

func TestStartBuildRejectsAnUnknownEngine(t *testing.T) {
	svc, f := newEngineTestService(t)
	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "jenkins", "akif"); !errors.Is(err, storeops.ErrInvalidEngine) {
		t.Fatalf("err = %v, want ErrInvalidEngine", err)
	}
}

// Nothing can be built for a repository whose store app was never linked —
// there is no identifier to sign, upload or name a concurrency group with.
func TestStartBuildRefusesAnUnlinkedApp(t *testing.T) {
	svc, f := newEngineTestService(t)
	app, err := f.apps.Get(context.Background(), f.repoID, domain.MobileStorePlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	app.Identifier = ""
	if _, err := f.apps.Upsert(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); !errors.Is(err, storeops.ErrAppNotReady) {
		t.Fatalf("err = %v, want ErrAppNotReady", err)
	}
}

// The generated script builds what the WORKING COPY named, not what a
// convention would have: the Gradle module here is the detected one, and the
// release script says so.
func TestStartBuildShipsTheDetectedBuildTargets(t *testing.T) {
	svc, f := newEngineTestService(t)
	repo := f.repo()
	repo.DetectedBuildTargets = domain.BuildTargets{XcodeScheme: "Trooper", GradleModule: "androidApp"}
	f.repos.set(repo)

	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); err != nil {
		t.Fatal(err)
	}
	if len(f.started) != 1 {
		t.Fatalf("started = %d, want one release", len(f.started))
	}
	script := f.started[0].Artifacts[0].Body
	if !strings.Contains(script, "MODULE='androidApp'") {
		t.Fatalf("the release script does not build the detected module:\n%s", script)
	}
}

// A mobile sub-project's targets are its own and are never inherited from the
// repository row: a monorepo's app has its own Gradle build, and bundling the
// repository-level module inside it would ship a different binary.
func TestStartBuildTakesTheSubProjectsOwnBuildTargets(t *testing.T) {
	svc, f := newEngineTestService(t)
	repo := f.repo()
	repo.Kind = domain.RepoKindMonorepo
	repo.SubProjects = []domain.RepoSubProject{{
		Path:                 "apps/mobile",
		Kind:                 domain.RepoKindMobile,
		DetectedAppIdentity:  domain.AppIdentity{PackageName: "com.example.android"},
		DetectedBuildTargets: domain.BuildTargets{GradleModule: "shell"},
	}}
	f.repos.set(repo)

	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); err != nil {
		t.Fatal(err)
	}
	script := f.started[0].Artifacts[0].Body
	if !strings.Contains(script, "MODULE='shell'") {
		t.Fatalf("the release script did not use the sub-project's own module:\n%s", script)
	}
}

// A working copy that never stated what to build does not get a release built
// against a convention. The refusal names what to go and do, because the fix
// is a commit in the user's own repository.
func TestStartBuildRefusesWhenNoBuildTargetWasDetected(t *testing.T) {
	for _, tc := range []struct {
		platform string
		targets  domain.BuildTargets
		remedy   string
	}{
		{domain.MobileStorePlatformIOS, domain.BuildTargets{GradleModule: "app"}, "share the app's scheme in Xcode"},
		{domain.MobileStorePlatformAndroid, domain.BuildTargets{XcodeScheme: "Trooper"}, "settings.gradle includes the app module"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			svc, f := newEngineTestService(t)
			repo := f.repo()
			repo.DetectedBuildTargets = tc.targets
			f.repos.set(repo)

			_, err := svc.StartBuild(context.Background(), f.repoID, tc.platform, "", "akif")
			if !errors.Is(err, storeops.ErrBuildTargetUnknown) {
				t.Fatalf("err = %v, want ErrBuildTargetUnknown", err)
			}
			if !strings.Contains(err.Error(), tc.remedy) {
				t.Fatalf("err = %v, want it to say %q", err, tc.remedy)
			}
			if len(f.started) != 0 {
				t.Fatal("a release was started for a target nobody could name")
			}
			entries, _ := f.audit.List(context.Background(), &f.repoID, 0)
			if len(entries) != 1 || entries[0].Outcome != domain.OpsOutcomeError {
				t.Fatalf("audit = %+v, want one failed store_build row", entries)
			}
		})
	}
}

// A repository with no registry row 404s rather than being reported as a bad
// request — the same split every other storeops route makes.
func TestStartBuildOnAnUnknownRepositoryIsNotFound(t *testing.T) {
	svc, _ := newEngineTestService(t)
	if _, err := svc.StartBuild(context.Background(), uuid.New(), domain.MobileStorePlatformAndroid, "", "akif"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// unlinkOnboardingTask drops the app's onboarding task, which is the state
// every app bound through the store picker is in: only Onboard ever writes
// that field.
func (f *engineFixture) unlinkOnboardingTask(t *testing.T, platform string) {
	t.Helper()
	app, err := f.apps.Get(context.Background(), f.repoID, platform)
	if err != nil {
		t.Fatal(err)
	}
	app.OnboardingTaskID = nil
	if _, err := f.apps.Upsert(context.Background(), app); err != nil {
		t.Fatal(err)
	}
}

// The probe is asked for the ONE file the dispatch will name. A monorepo
// renders mobile-release-<sub-project>.yml, and a repository that still
// carries a plain mobile-release.yml from an earlier life must not be read as
// already having this app's workflow — that answer sends the release to a
// dispatch GitHub replies 404 to.
func TestStartBuildProbesTheExactWorkflowItWillDispatch(t *testing.T) {
	svc, f := newEngineTestService(t)
	repo := f.repo()
	repo.Kind = domain.RepoKindMonorepo
	repo.SubProjects = []domain.RepoSubProject{{
		Path:                 "apps/mobile",
		Kind:                 domain.RepoKindMobile,
		DetectedAppIdentity:  domain.AppIdentity{PackageName: "com.example.android"},
		DetectedBuildTargets: domain.BuildTargets{GradleModule: "app"},
	}}
	f.repos.set(repo)

	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); err != nil {
		t.Fatal(err)
	}
	if f.probedWorkflow != "mobile-release-apps-mobile.yml" {
		t.Fatalf("probed %q, want the sub-project's own workflow file", f.probedWorkflow)
	}
}

// The starter is handed the file BODIES and their modes, not just paths: it is
// the thing that has to write them, and the release script has to land
// executable because a machine execs it directly.
func TestStartBuildHandsTheStarterTheFilesToDeliver(t *testing.T) {
	svc, f := newEngineTestService(t)
	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); err != nil {
		t.Fatal(err)
	}
	artifacts := f.started[0].Artifacts
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %d, want the script and the workflow", len(artifacts))
	}
	if artifacts[0].Body == "" || artifacts[0].Mode != 0o755 {
		t.Fatalf("script = %+v, want a body and mode 0755", artifacts[0])
	}
	if artifacts[1].Body == "" || artifacts[1].Mode != 0o644 {
		t.Fatalf("workflow = %+v, want a body and mode 0644", artifacts[1])
	}
}

// A repository that has never released has no mobile-release workflow, and
// that is not a reason to block: the starter writes that exact file before it
// dispatches. Blocking here is what made a clean mobile repo unreleasable —
// the file the system needs could never arrive by the system's own hand.
func TestResolveEngineTakesActionsWhenTheOnlyGapIsTheWorkflowItWrites(t *testing.T) {
	svc, f := newEngineTestService(t)
	ctx := context.Background()
	f.actionsErr = storeops.ErrActionsNoWorkflow
	f.localHost = storeops.LocalRunnerHost{}

	engine, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
	if err != nil || engine != domain.ReleaseEngineActions {
		t.Fatalf("engine = %q, err = %v; want github_actions", engine, err)
	}

	// A paired machine that can build TODAY still wins: it has nothing to
	// write first.
	f.localHost = storeops.LocalRunnerHost{Paired: true, MacOS: true}
	engine, err = svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile)
	if err != nil || engine != domain.ReleaseEngineLocal {
		t.Fatalf("engine = %q, err = %v; want local", engine, err)
	}

	// A billing refusal is the objection this release CANNOT answer for
	// itself, and it still blocks.
	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.localHost = storeops.LocalRunnerHost{}
	if _, err := svc.ResolveEngine(ctx, f.repo(), domain.MobileStorePlatformAndroid, testWorkflowFile); !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine", err)
	}
}

// The engine the probes picked can turn out to be one this deployment cannot
// drive — a paired Mac with no local runner wired is the live case. To the
// person who pressed the button that is the same wall as having no engine, so
// it parks the card instead of returning a 500 nobody can act on.
func TestStartBuildParksWhenTheStarterCannotDriveTheResolvedEngine(t *testing.T) {
	svc, f := newEngineTestService(t)
	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.startErr = fmt.Errorf("the local release engine is not wired: %w", domain.ErrNoReleaseEngine)

	_, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif")
	if !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine so the HTTP layer answers 409", err)
	}
	if len(f.parker.Calls) != 1 || f.parker.Calls[0].Resource != domain.ResourceHumanDecision {
		t.Fatalf("parks = %+v, want the card parked once on human_decision", f.parker.Calls)
	}
}

// An app bound through the picker has no onboarding task. Parking used to be
// silently skipped for it, so the whole picker population got a 409 and a log
// line with nothing on the board — which is the one place
// domain.ResourceHumanDecision says a person is supposed to find this.
func TestStartBuildOpensACardToParkWhenTheAppHasNoTask(t *testing.T) {
	svc, f := newEngineTestService(t)
	f.unlinkOnboardingTask(t, domain.MobileStorePlatformAndroid)
	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.localHost = storeops.LocalRunnerHost{}

	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine", err)
	}
	if f.tasks.count() != 1 {
		t.Fatalf("cards opened = %d, want exactly one", f.tasks.count())
	}
	if title := f.tasks.Calls[0].Request.Title; !strings.Contains(title, "Release blocked") {
		t.Fatalf("card title = %q, want it to name the block", title)
	}

	// The card is remembered on the row, so pressing the button again parks
	// the same one instead of littering the board.
	app, err := f.apps.Get(context.Background(), f.repoID, domain.MobileStorePlatformAndroid)
	if err != nil {
		t.Fatal(err)
	}
	if app.OnboardingTaskID == nil {
		t.Fatal("the opened card was not recorded on the store app row")
	}
	if len(f.parker.Calls) != 1 || f.parker.Calls[0].TaskID != *app.OnboardingTaskID {
		t.Fatalf("parks = %+v, want the card that was just opened parked", f.parker.Calls)
	}
	if _, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif"); !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatal(err)
	}
	if f.tasks.count() != 1 {
		t.Fatalf("cards opened = %d after a second blocked build, want still one", f.tasks.count())
	}
}

// A park that did not happen must not be reported as one: the handler answers
// 409 saying the card was parked, and this error text is the only place the
// operator could learn otherwise.
func TestStartBuildSaysSoWhenTheParkItselfFails(t *testing.T) {
	svc, f := newEngineTestService(t)
	f.actionsErr = storeops.ErrActionsBillingBlocked
	f.localHost = storeops.LocalRunnerHost{}
	f.parker.Err = errors.New("board is down")

	_, err := svc.StartBuild(context.Background(), f.repoID, domain.MobileStorePlatformAndroid, "", "akif")
	if !errors.Is(err, domain.ErrNoReleaseEngine) {
		t.Fatalf("err = %v, want ErrNoReleaseEngine still matched", err)
	}
	if !strings.Contains(err.Error(), "parking the board card") {
		t.Fatalf("err = %v, want it to say the park failed", err)
	}
}
