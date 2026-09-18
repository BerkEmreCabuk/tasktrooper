package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TaskCreator opens the board task that authors a deploy workflow. It is the
// board's CreateTask narrowed to what this package needs.
type TaskCreator interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
}

// RepositoryResolver reads the repo whose deploy is being defined.
type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// Service owns the deploy definition of every repository: which template ships
// each environment, with which variables, and the task that writes the
// resulting workflow.
type Service struct {
	targets port.DeployTargetStore
	repos   RepositoryResolver
	tasks   TaskCreator
	// storeOnboarder kicks off (or continues) storeops' onboarding lifecycle
	// once a store-shipped target (App Store / Google Play) is saved. Late-set
	// like SetTaskCreator to avoid an import cycle: storeops depends on
	// deploy's domain types, not the other way around.
	storeOnboarder func(ctx context.Context, repositoryID uuid.UUID, provider, identifier, appName string) error
	// storeIdentifierGuard vets a store target's bundle ID / package name
	// BEFORE it is persisted. It is deliberately separate from
	// storeOnboarder: onboarding runs after the save and its failures are
	// tolerated, which is the right call for a transient store-API outage but
	// exactly wrong for "this identifier may not be saved at all".
	storeIdentifierGuard func(ctx context.Context, repositoryID uuid.UUID, provider, identifier string) error
	// workflows/roles: see board.Dispatcher's own fields of the same name.
	workflows port.WorkflowReader
	roles     port.RoleResolver
}

func (s *Service) SetWorkflows(w port.WorkflowReader)  { s.workflows = w }
func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }

func NewService(targets port.DeployTargetStore, repos RepositoryResolver) *Service {
	return &Service{targets: targets, repos: repos}
}

// SetTaskCreator enables the "scaffold this deploy" task; without it the
// service still serves templates and targets.
func (s *Service) SetTaskCreator(tasks TaskCreator) { s.tasks = tasks }

// SetStoreOnboarder wires storeops.Service.Onboard behind SaveTarget: saving a
// store-shipped target (App Store / Google Play) starts the app's onboarding
// lifecycle instead of leaving it to a separate manual step. Nil (the
// pre-wiring default) leaves the target save-only, matching every other
// deploy provider.
func (s *Service) SetStoreOnboarder(fn func(ctx context.Context, repositoryID uuid.UUID, provider, identifier, appName string) error) {
	s.storeOnboarder = fn
}

// SetStoreIdentifierGuard wires storeops.Service.EnsureIdentifierAllowed in
// front of SaveTarget, so a store target whose identifier the store lifecycle
// refuses is rejected before anything is written — the caller gets the error
// instead of a 200 and a silently diverged pair of records. Nil (the
// pre-wiring default) leaves saves ungated, matching every other provider.
func (s *Service) SetStoreIdentifierGuard(fn func(ctx context.Context, repositoryID uuid.UUID, provider, identifier string) error) {
	s.storeIdentifierGuard = fn
}

// ConfigView is the deploy settings payload: the repo's saved targets, the
// templates that fit its kind, and which required vars are still empty.
type ConfigView struct {
	Kind      string                  `json:"kind"`
	Targets   []domain.DeployTarget   `json:"targets"`
	Templates []domain.DeployTemplate `json:"templates"`
	Missing   map[string][]string     `json:"missing,omitempty"`
	Envs      []string                `json:"envs"`
	// DetectedAppIdentity is what the working copy says this app's store
	// identifiers are, for prefilling a store target's bundle_id /
	// package_name. Always serialised, both halves "" when nothing was read —
	// a form that prefills has to be able to tell "unknown" from "not sent".
	//
	// Scoped like Kind: a sub-project's own identifiers when one is addressed,
	// the repository's otherwise. Never inherited across that line — a
	// monorepo's mobile app is not the repository, and prefilling one form
	// with another project's bundle id is worse than prefilling nothing.
	DetectedAppIdentity domain.AppIdentity `json:"detected_app_identity"`
}

// Config returns the deploy configuration view for a repository. Template
// bodies are stripped: the list is a picker, and the full recipe is fetched
// per template when it is actually applied.
// subProjectPath filters the view to one sub-project's own targets ("" = the
// repository itself, the pre-sub-project-settings behavior).
func (s *Service) Config(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (ConfigView, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return ConfigView{}, err
	}
	kind := repo.Kind
	identity := repo.DetectedAppIdentity
	if subProjectPath != "" {
		kind = domain.RepoKindBackend
		identity = domain.AppIdentity{}
		for _, sp := range repo.SubProjects {
			if sp.Path == subProjectPath {
				kind = sp.Kind
				identity = sp.DetectedAppIdentity
				break
			}
		}
	}
	view := ConfigView{
		Kind:                kind,
		Envs:                domain.DeployEnvs(),
		Missing:             map[string][]string{},
		DetectedAppIdentity: identity,
	}
	for _, tpl := range TemplatesForKind(kind) {
		tpl.Body = ""
		view.Templates = append(view.Templates, tpl)
	}
	allTargets, err := s.targets.ListByRepository(ctx, repositoryID)
	if err != nil {
		return ConfigView{}, err
	}
	targets := make([]domain.DeployTarget, 0, len(allTargets))
	for _, t := range allTargets {
		if t.SubProjectPath == subProjectPath {
			targets = append(targets, t)
		}
	}
	view.Targets = targets
	for _, t := range targets {
		tpl, ok := Template(t.TemplateID)
		if !ok {
			if tpl, ok = Template(t.Provider); !ok {
				continue
			}
		}
		if missing := tpl.MissingVars(EffectiveVars(tpl, t)); len(missing) > 0 {
			view.Missing[t.Env] = missing
		}
	}
	return view, nil
}

// SaveTarget upserts one environment's deploy definition.
func (s *Service) SaveTarget(ctx context.Context, repositoryID uuid.UUID, req domain.SaveDeployTargetRequest) (domain.DeployTarget, error) {
	env := strings.TrimSpace(req.Env)
	if !domain.ValidDeployEnv(env) {
		return domain.DeployTarget{}, fmt.Errorf("invalid deploy env %q", req.Env)
	}
	provider := strings.TrimSpace(req.Provider)
	if !domain.ValidDeployProvider(provider) {
		return domain.DeployTarget{}, fmt.Errorf("invalid deploy provider %q", req.Provider)
	}
	templateID := strings.TrimSpace(req.TemplateID)
	if templateID != "" {
		tpl, ok := Template(templateID)
		if !ok {
			return domain.DeployTarget{}, fmt.Errorf("unknown deploy template %q", templateID)
		}
		if tpl.Provider != provider {
			return domain.DeployTarget{}, fmt.Errorf("template %q ships to %s, not %s", templateID, tpl.Provider, provider)
		}
		templateID = tpl.ID
	}
	vars := map[string]string{}
	for k, v := range req.Vars {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		vars[k] = strings.TrimSpace(v)
	}

	// health_url used to be TrimSpace'd and nothing else, and prodops.Monitor
	// then GETs it every minute for as long as the target exists. Storing
	// http://127.0.0.1:8080/metrics or the metadata service bought a scheduled
	// scanner that reports its results into an incident an agent reads.
	//
	// This is the offline half of the guard — it rejects the internal literals
	// without resolving, because a health URL is routinely configured before the
	// environment that answers it exists and a write-time DNS lookup would fail
	// on a perfectly good target. Names are judged at dial time in prodops.probe.
	healthURL := strings.TrimSpace(req.HealthURL)
	if healthURL != "" {
		if _, err := urlguard.Default().Precheck(healthURL); err != nil {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).
				Msg("refused a deploy target health_url")
			return domain.DeployTarget{}, fmt.Errorf("health_url is not an allowed destination")
		}
	}

	// logs_url gets the identical offline guard, for the identical reason: it
	// is agent-writable and it is fetched by the deploy watch. The difference
	// from health_url is only in the polling — this one is read on demand
	// rather than every minute — and that difference makes it no safer, so it
	// is checked here on write AND again in deploywatch on every fetch.
	logsURL := strings.TrimSpace(req.LogsURL)
	if logsURL != "" {
		if _, err := urlguard.Default().Precheck(logsURL); err != nil {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).
				Msg("refused a deploy target logs_url")
			return domain.DeployTarget{}, fmt.Errorf("logs_url is not an allowed destination")
		}
	}

	// Store-shipped environments need a bundle ID / package name before
	// anything else can happen — it is what storeops.Onboard registers (or
	// finds) the app under. Validate before saving so a store target that
	// points nowhere never lands.
	var storeIdentifier, storeAppName string
	if domain.IsStoreProvider(provider) {
		storeIdentifier = vars["bundle_id"]
		if storeIdentifier == "" {
			storeIdentifier = vars["package_name"]
		}
		if storeIdentifier == "" {
			return domain.DeployTarget{}, fmt.Errorf("store targets need bundle_id/package_name")
		}
		storeAppName = vars["app_name"]

		// Ask the store lifecycle whether this identifier may be saved at
		// all, BEFORE writing. Re-pointing an app that is already live is
		// refused here rather than inside the post-save onboarding hook,
		// whose errors are swallowed on purpose — a refusal discovered there
		// would return 200 and leave the target pointing at a new bundle ID
		// while the store row stayed live under the old one, with
		// mobileStoreGate (state-keyed, not identifier-keyed) still green.
		if s.storeIdentifierGuard != nil {
			if err := s.storeIdentifierGuard(ctx, repositoryID, provider, storeIdentifier); err != nil {
				return domain.DeployTarget{}, err
			}
		}
	}

	saved, err := s.targets.Save(ctx, domain.DeployTarget{
		RepositoryID:   repositoryID,
		SubProjectPath: strings.TrimSpace(req.SubProjectPath),
		Env:            env,
		Provider:       provider,
		TemplateID:     templateID,
		Vars:           vars,
		HealthURL:      healthURL,
		LogsURL:        logsURL,
		BaseURL:        strings.TrimSpace(req.BaseURL),
		// The device tools' guard: mobile_launch_app opens this package and
		// nothing else. Set by a human here, never by an agent tool — a guard
		// whose subject can be rewritten by the thing it guards is not one.
		//
		// A store target already carries the identifier in vars; default to it
		// so an operator who filled that in does not have to say the same
		// package name twice for the phone to be usable.
		AppPackage:   firstNonEmpty(strings.TrimSpace(req.AppPackage), vars["package_name"]),
		AppURL:       strings.TrimSpace(req.AppURL),
		AutoRollback: req.AutoRollback,
	})
	if err != nil {
		return domain.DeployTarget{}, err
	}

	// Onboard only after a successful save, and only when a store target was
	// actually saved. Onboarding talks to external store APIs (App Store
	// Connect / Play Developer API) and can fail transiently — that must never
	// roll back or fail this save, so the error is logged and otherwise
	// swallowed on purpose: the target is the durable record, onboarding just
	// reacts to it and can be retried independently.
	if domain.IsStoreProvider(provider) && s.storeOnboarder != nil {
		appName := storeAppName
		if appName == "" {
			if repo, rerr := s.repos.Get(ctx, repositoryID); rerr == nil {
				appName = repo.Name
			}
		}
		if oerr := s.storeOnboarder(ctx, repositoryID, provider, storeIdentifier, appName); oerr != nil {
			log.Warn().Err(oerr).Str("repository_id", repositoryID.String()).Str("provider", provider).
				Msg("store onboarding failed; target was saved, onboarding will need to be retried")
		}
	}

	return saved, nil
}

// RecordTargetURLs writes back only where an environment actually answers:
// its base URL and its health URL. Nothing else about the target can move
// through here.
//
// This is the after-the-deploy half of SaveTarget, and it is deliberately not
// SaveTarget with two fields filled in. An agent recording the URL a deploy
// just served must not be able to re-point the provider, the template or the
// vars — and re-saving a store target through SaveTarget would also re-trigger
// store onboarding for a change that has nothing to do with the store.
//
// When the environment has no target yet, a minimal hand-rolled one is created
// (custom provider, no template, no vars): the repo ships by some workflow
// nobody described here, but where it landed is still worth recording.
// health_url and logs_url get the same offline destination guard SaveTarget
// applies, because the production monitor will poll the first and the deploy
// watch will fetch the second.
//
// The fields arrive as a struct rather than as a growing list of *string
// parameters: this call has gained one address field per feature that needed
// one (base, health, app, now logs) and four positional pointers of the same
// type is a call site nobody can read and a compiler cannot check.
func (s *Service) RecordTargetURLs(ctx context.Context, repositoryID uuid.UUID, env string, in RecordTargetURLsInput) (domain.DeployTarget, error) {
	baseURL, healthURL, appURL, logsURL := in.BaseURL, in.HealthURL, in.AppURL, in.LogsURL
	if !domain.ValidDeployEnv(env) {
		return domain.DeployTarget{}, fmt.Errorf("invalid deploy env %q", env)
	}
	if baseURL == nil && healthURL == nil && appURL == nil && logsURL == nil {
		return domain.DeployTarget{}, fmt.Errorf("nothing to record: pass base_url, health_url, logs_url or app_url")
	}
	target, err := s.targets.Get(ctx, repositoryID, "", env)
	if err != nil {
		if !errors.Is(err, port.ErrNotFound) {
			return domain.DeployTarget{}, err
		}
		target = domain.DeployTarget{
			RepositoryID: repositoryID,
			Env:          env,
			Provider:     domain.DeployProviderCustom,
		}
	}
	if baseURL != nil {
		target.BaseURL = strings.TrimSpace(*baseURL)
	}
	// app_url is writable here but app_package is not, and the split is the
	// guard, not an oversight. Which app may be opened on a real phone is a
	// human decision; WHERE this build of it currently lives is a fact CI
	// learns on every merge, and making a person copy that URL by hand is how
	// QA ends up testing last week's APK.
	if appURL != nil {
		trimmed := strings.TrimSpace(*appURL)
		if trimmed != "" {
			if _, perr := urlguard.Default().Precheck(trimmed); perr != nil {
				log.Warn().Err(perr).Str("repository_id", repositoryID.String()).
					Msg("refused an agent-recorded deploy target app_url")
				return domain.DeployTarget{}, fmt.Errorf("app_url is not an allowed destination")
			}
		}
		target.AppURL = trimmed
	}
	if healthURL != nil {
		trimmed := strings.TrimSpace(*healthURL)
		if trimmed != "" {
			if _, perr := urlguard.Default().Precheck(trimmed); perr != nil {
				log.Warn().Err(perr).Str("repository_id", repositoryID.String()).
					Msg("refused an agent-recorded deploy target health_url")
				return domain.DeployTarget{}, fmt.Errorf("health_url is not an allowed destination")
			}
		}
		target.HealthURL = trimmed
	}
	// Same treatment as health_url, and for a sharper reason: the deploy watch
	// GETs this one under the deployment's own network identity right after a
	// deploy, which is exactly the moment an agent has been asked to look at
	// something and is most likely to record whatever a log line suggested.
	if logsURL != nil {
		trimmed := strings.TrimSpace(*logsURL)
		if trimmed != "" {
			if _, perr := urlguard.Default().Precheck(trimmed); perr != nil {
				log.Warn().Err(perr).Str("repository_id", repositoryID.String()).
					Msg("refused an agent-recorded deploy target logs_url")
				return domain.DeployTarget{}, fmt.Errorf("logs_url is not an allowed destination")
			}
		}
		target.LogsURL = trimmed
	}
	return s.targets.Save(ctx, target)
}

// RecordTargetURLsInput is the set of address fields RecordTargetURLs may
// write. Every field is a pointer with the same contract: nil leaves the stored
// value alone, an explicit "" is a deliberate clear.
type RecordTargetURLsInput struct {
	BaseURL   *string
	HealthURL *string
	AppURL    *string
	LogsURL   *string
}

// subProjectPath: "" deletes the repository's own target, unchanged from
// before sub-projects could each have targets of their own.
func (s *Service) DeleteTarget(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) error {
	if !domain.ValidDeployEnv(env) {
		return fmt.Errorf("invalid deploy env %q", env)
	}
	return s.targets.Delete(ctx, repositoryID, subProjectPath, env)
}

func (s *Service) Target(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) (domain.DeployTarget, error) {
	return s.targets.Get(ctx, repositoryID, subProjectPath, env)
}

func (s *Service) Targets(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error) {
	return s.targets.ListByRepository(ctx, repositoryID)
}

// Instructions renders the target's template with its variables filled in —
// the exact text an agent (or a human) needs to author the deploy workflow.
func (s *Service) Instructions(ctx context.Context, repositoryID uuid.UUID, env string) (string, error) {
	target, err := s.targets.Get(ctx, repositoryID, "", env)
	if err != nil {
		return "", err
	}
	tpl, ok := Template(target.TemplateID)
	if !ok {
		if tpl, ok = Template(target.Provider); !ok {
			return "", fmt.Errorf("no deploy template for provider %q", target.Provider)
		}
	}
	return Render(tpl, target), nil
}

// CreateSetupTask opens the board task that writes (or updates) the deploy
// workflow for one environment, with the rendered recipe as its description —
// the deploy counterpart of the CI workflow setup task.
func (s *Service) CreateSetupTask(ctx context.Context, repositoryID uuid.UUID, env string) (domain.BoardTask, error) {
	if s.tasks == nil {
		return domain.BoardTask{}, fmt.Errorf("board is not available")
	}
	if !domain.ValidDeployEnv(env) {
		return domain.BoardTask{}, fmt.Errorf("invalid deploy env %q", env)
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	target, err := s.targets.Get(ctx, repositoryID, "", env)
	if err != nil {
		return domain.BoardTask{}, fmt.Errorf("define the %s deploy target first: %w", env, err)
	}
	tpl, ok := Template(target.TemplateID)
	if !ok {
		if tpl, ok = Template(target.Provider); !ok {
			return domain.BoardTask{}, fmt.Errorf("no deploy template for provider %q", target.Provider)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Author the %s deploy for this repository using the %s template.\n\n", env, tpl.Name)
	fmt.Fprintf(&b, "Target: provider=%s, env=%s, workflow file=.github/workflows/%s\n",
		target.Provider, env, tpl.WorkflowFile)
	if missing := tpl.MissingVars(target.Vars); len(missing) > 0 {
		fmt.Fprintf(&b, "\nMissing variables (ask before guessing): %s\n", strings.Join(missing, ", "))
	}
	b.WriteString("\nDefinition of done:\n")
	b.WriteString("- the workflow file exists, is workflow_dispatch-triggerable and passes a manual run\n")
	b.WriteString("- the deploy verifies itself (smoke check) and rolls back on failure\n")
	fmt.Fprintf(&b, "- the pipeline mapping for category %s points at %s\n", domain.DeployEnvCategory(env), tpl.WorkflowFile)
	b.WriteString("\n---\n\n")
	b.WriteString(Render(tpl, target))

	return s.tasks.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
		Title:           fmt.Sprintf("Set up %s deploy (%s)", env, tpl.Name),
		Description:     b.String(),
		TaskType:        domain.TaskTypeTask,
		Priority:        domain.TaskPriorityHigh,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.systemTaskAssignee(ctx, repo.Kind, repo.SubProjects),
	})
}

// CreateLocalSetupTask opens the board task that writes the bootstrap script
// running this repository (or one monorepo sub-project) on a developer's own
// machine. Local has no provider, template or address — SaveTarget and
// Instructions do not apply to it — so this writes its own task body instead
// of rendering a deploy template.
//
// A script, not a document: prose about how to run something is one more thing
// to read and translate into commands, and it goes stale silently. The script
// either works on a fresh machine or it visibly does not.
func (s *Service) CreateLocalSetupTask(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (domain.BoardTask, error) {
	if s.tasks == nil {
		return domain.BoardTask{}, fmt.Errorf("board is not available")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	kind := repo.Kind
	dir := ""
	subProjectPath = strings.TrimSpace(subProjectPath)
	if subProjectPath != "" {
		kind = domain.RepoKindBackend
		for _, sp := range repo.SubProjects {
			if sp.Path == subProjectPath {
				kind = sp.Kind
				break
			}
		}
		dir = subProjectPath + "/"
	}

	scriptPath := dir + "scripts/dev.sh"

	var b strings.Builder
	fmt.Fprintf(&b, "Write %s: a COMPLETE, executable bootstrap script that gets this %s running on a developer's own machine. A script, not a markdown guide — do not write one.\n\n", scriptPath, kind)
	b.WriteString("Definition of done — running it once on a fresh machine leaves the project running, with no other step:\n")
	b.WriteString("- installs every dependency and toolchain the project needs (checks first, installs only what is missing)\n")
	b.WriteString("- prepares env/config: creates the .env (or equivalent) from the example with local defaults, runs the migrations and seeds a first run needs\n")
	b.WriteString("- starts every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process\n")
	b.WriteString("- idempotent: a second run is safe and duplicates nothing\n")
	b.WriteString("- executable (`chmod +x`), `#!/usr/bin/env bash`, `set -euo pipefail`\n")
	b.WriteString("- a short usage header comment at the top: what it does, how to run it, and the port/URL it comes up on\n")
	fmt.Fprintf(&b, "- accepts an optional port argument (`%s [port]`) overriding the default, so it can be rerun when the default port is taken\n", scriptPath)
	b.WriteString("- matches what the repo actually needs today (its real package manager, build tool, ports) — not a generic template\n")
	switch kind {
	case domain.RepoKindMobile:
		b.WriteString("- covers both iOS (simulator) and Android (emulator) if the repo ships both platforms\n")
	case domain.RepoKindWorker:
		b.WriteString("- starts any queue/broker the worker needs locally (or configures a fake/in-memory mode when there is none to start)\n")
	}

	return s.tasks.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
		Title:           fmt.Sprintf("Write the local bootstrap script (%s)", kind),
		Description:     b.String(),
		TaskType:        domain.TaskTypeTask,
		Priority:        domain.TaskPriorityMedium,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.systemTaskAssignee(ctx, kind, repo.SubProjects),
	})
}

// systemTaskAssignee resolves who a system-opened deploy-setup task goes to:
// the developer role's agent for the repo/sub-project's area. Deploy setup
// writes workflow files into a branch, so even a monorepo's goes to a
// developer (RepoArea's per-kind resolution), never to the system architect,
// which refuses to author files.
func (s *Service) systemTaskAssignee(ctx context.Context, kind string, subProjects []domain.RepoSubProject) *uuid.UUID {
	if s.roles == nil {
		return nil
	}
	area := domain.RepoArea(kind, subProjects)
	id, err := s.roles.AgentForPurpose(ctx, domain.PurposeSystemTaskAssignee, area)
	if err != nil {
		return nil
	}
	return id
}

// firstNonEmpty returns the first value that is not blank, or "".
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
