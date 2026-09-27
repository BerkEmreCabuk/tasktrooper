package ops

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{
		listIncidentsToolName,
		getIncidentToolName,
		proposeRemedyToolName,
		resolveIncidentToolName,
		listDeployTemplatesToolName,
		loadDeployTemplateToolName,
		deployTargetToolName,
		updateDeployTargetToolName,
		recordLocalDeployToolName,
	} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
