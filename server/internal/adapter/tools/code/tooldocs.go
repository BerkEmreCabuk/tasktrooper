package code

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{
		codebaseSearchToolName,
		grepCodeToolName,
		getRepoTreeToolName,
		getSymbolSkeletonToolName,
		expandSymbolContextToolName,
		readFileToolName,
		writeFileToolName,
		editFileToolName,
		editLinesToolName,
		deleteFileToolName,
		moveFileToolName,
	} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
