package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Deploy environments are the "where" side of the pipeline deploy categories: a
// category says which workflow to dispatch, a target says which provider that
// workflow ships to and with which variables.
const (
	// DeployEnvLocal has no pipeline category or trigger: nothing in CI ships to
	// a developer's own machine. It exists purely as a recorded address + health
	// check, the same as any other environment.
	DeployEnvLocal   = "local"
	DeployEnvStage   = "stage"
	DeployEnvPreProd = "preprod"
	DeployEnvProd    = "prod"
)

// DeployEnvs lists every environment in promotion order.
func DeployEnvs() []string {
	return []string{DeployEnvLocal, DeployEnvStage, DeployEnvPreProd, DeployEnvProd}
}

func ValidDeployEnv(env string) bool {
	switch env {
	case DeployEnvLocal, DeployEnvStage, DeployEnvPreProd, DeployEnvProd:
		return true
	}
	return false
}

// DeployEnvCategory maps an environment to the pipeline category whose mapped
// workflow file actually performs the deploy.
func DeployEnvCategory(env string) string {
	switch env {
	case DeployEnvStage:
		return PipelineCategoryStageDeploy
	case DeployEnvPreProd:
		return PipelineCategoryPreProdDeploy
	case DeployEnvProd:
		return PipelineCategoryProdDeploy
	}
	return ""
}

// DeployEnvTrigger maps an environment to the pipeline trigger that runs it.
func DeployEnvTrigger(env string) PipelineTrigger {
	switch env {
	case DeployEnvStage:
		return PipelineTriggerStageDeploy
	case DeployEnvPreProd:
		return PipelineTriggerPreProdDeploy
	case DeployEnvProd:
		return PipelineTriggerProdDeploy
	}
	return ""
}

// Deploy providers. The provider decides which template applies, which vars are
// required, and how a rollback is expressed.
const (
	DeployProviderGCPCloudRun = "gcp_cloud_run"
	DeployProviderGCPGKE      = "gcp_gke"
	DeployProviderAWSECS      = "aws_ecs"
	DeployProviderAWSLambda   = "aws_lambda"
	DeployProviderVercel      = "vercel"
	DeployProviderFly         = "fly"
	DeployProviderCustom      = "custom"
	DeployProviderAppStore    = "app_store"
	DeployProviderGooglePlay  = "google_play"
)

func ValidDeployProvider(p string) bool {
	switch p {
	case DeployProviderGCPCloudRun, DeployProviderGCPGKE, DeployProviderAWSECS,
		DeployProviderAWSLambda, DeployProviderVercel, DeployProviderFly, DeployProviderCustom,
		DeployProviderAppStore, DeployProviderGooglePlay:
		return true
	}
	return false
}

// DeployTarget is one repository's shipping definition for one environment: the
// provider, the template it was scaffolded from, the substitution vars, and the
// URL that proves the environment is alive after the deploy.
type DeployTarget struct {
	ID           uuid.UUID `json:"id"`
	RepositoryID uuid.UUID `json:"repository_id"`
	// SubProjectPath scopes this target to one monorepo sub-project ("" = the
	// repository itself, the only value that existed before sub-projects).
	SubProjectPath string `json:"sub_project_path,omitempty"`
	Env            string `json:"env"`
	Provider       string `json:"provider"`
	// TemplateID records which recipe produced the workflow, so a later template
	// revision can be diffed against what the repo actually runs.
	TemplateID string            `json:"template_id,omitempty"`
	Vars       map[string]string `json:"vars,omitempty"`
	// HealthURL is polled by the production monitor; empty disables probing.
	HealthURL string `json:"health_url,omitempty"`
	// LogsURL is an endpoint the application itself serves that returns its
	// recent logs — the other half of HealthURL: health answers "is it up",
	// while a deploy that came up and is logging a failed migration on boot is
	// invisible to it. It is read on demand by the deploy watch, never polled,
	// and re-validated against the destination guard on EVERY fetch because it
	// is agent-writable. It is an HTTP endpoint on purpose — no cluster, no
	// cloud log API (see CLAUDE.md).
	LogsURL string `json:"logs_url,omitempty"`
	// BaseURL is where this environment actually answers (the DNS entry an API
	// consumer or a QA agent hits); health checks live under it, but the two are
	// separate — one proves liveness, the other is the address.
	BaseURL string `json:"base_url,omitempty"`
	// AppPackage is the Android package this environment's build installs as
	// (e.g. ai.tasktrooper.app.stage), and the guard for the device tools:
	// mobile_launch_app can only open a package recorded here, so an agent
	// cannot be talked into opening something else on somebody's phone.
	AppPackage string `json:"app_package,omitempty"`
	// AppURL is where the installable artifact (.apk CI published) lives; empty
	// means "assume it is already on the device", the normal case once a build
	// has been installed once.
	AppURL string `json:"app_url,omitempty"`
	// AutoRollback lets the incident engine propose a redeploy of the last good
	// ref as an executable step instead of a suggestion only.
	AutoRollback bool      `json:"auto_rollback"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SaveDeployTargetRequest upserts one (repository, sub-project, env) target.
type SaveDeployTargetRequest struct {
	// SubProjectPath: see DeployTarget.SubProjectPath.
	SubProjectPath string            `json:"sub_project_path,omitempty"`
	Env            string            `json:"env"`
	Provider       string            `json:"provider"`
	TemplateID     string            `json:"template_id,omitempty"`
	Vars           map[string]string `json:"vars,omitempty"`
	HealthURL      string            `json:"health_url,omitempty"`
	LogsURL        string            `json:"logs_url,omitempty"`
	BaseURL        string            `json:"base_url,omitempty"`
	AppPackage     string            `json:"app_package,omitempty"`
	AppURL         string            `json:"app_url,omitempty"`
	AutoRollback   bool              `json:"auto_rollback"`
}

// DeployTemplateVar is one substitution the template needs before its workflow
// can run (region, service name, cluster, …).
type DeployTemplateVar struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Example  string `json:"example,omitempty"`
	Required bool   `json:"required"`
}

// DeployTemplate is an embedded, provider-specific deploy recipe — the deploy
// counterpart of a skill: the agent loads it and writes the workflow it
// describes, so shipping becomes as repeatable as build and test.
type DeployTemplate struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	// Kinds are the repo kinds this template fits (empty = any kind).
	Kinds        []string            `json:"kinds,omitempty"`
	Envs         []string            `json:"envs,omitempty"`
	RequiredVars []DeployTemplateVar `json:"required_vars,omitempty"`
	// WorkflowFile is the suggested .github/workflows file name, also the value
	// the pipeline mapping should point at once the workflow is pushed.
	WorkflowFile string `json:"workflow_file"`
	RollbackHint string `json:"rollback_hint,omitempty"`
	// Body is the full markdown instruction set (including the workflow YAML)
	// handed to the agent that authors the deploy.
	Body string `json:"body,omitempty"`
}

// MissingVars returns the required var keys that vars does not fill, sorted so
// the message is stable.
func (t DeployTemplate) MissingVars(vars map[string]string) []string {
	var missing []string
	for _, v := range t.RequiredVars {
		if !v.Required {
			continue
		}
		if strings.TrimSpace(vars[v.Key]) == "" {
			missing = append(missing, v.Key)
		}
	}
	sort.Strings(missing)
	return missing
}

// SupportsKind reports whether the template applies to a repo kind; a template
// with no declared kinds fits every repo.
func (t DeployTemplate) SupportsKind(kind string) bool {
	if len(t.Kinds) == 0 {
		return true
	}
	for _, k := range t.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}
