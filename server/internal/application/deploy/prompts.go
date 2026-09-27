package deploy

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

type setupTaskInput struct {
	Env          string
	TemplateName string
	Provider     string
	WorkflowFile string
	EnvCategory  string
	MissingVars  []string
}

var setupTaskKey = prompt.Define[setupTaskInput]("briefs.deploy.setup_task", setupTaskInput{
	Env: "prod", TemplateName: "GCP Cloud Run", Provider: "gcp_cloud_run", WorkflowFile: "deploy-cloud-run.yml", EnvCategory: "prod_deploy",
})

type localSetupTaskInput struct {
	ScriptPath string
	Kind       string
}

var localSetupTaskKey = prompt.Define[localSetupTaskInput]("briefs.deploy.local_setup_task", localSetupTaskInput{
	ScriptPath: "scripts/dev.sh", Kind: "backend",
})
