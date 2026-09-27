package runtime

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{
		getEnvironmentToolName,
		queryRuntimeLogsToolName,
		listRuntimeErrorsToolName,
		listDeploymentsToolName,
	} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
