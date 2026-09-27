package prompt

// toolRepositoryRequiredKey backs the one refusal adapter/tools/runtime and
// adapter/tools/projectmodel both need — their resolveRepositoryID helpers
// are otherwise byte-identical, so they share this key instead of drifting.

type toolRepositoryRequiredInput struct{ ToolName string }

var toolRepositoryRequiredKey = Define("guard.tool_repository_required", toolRepositoryRequiredInput{ToolName: "get_environment"})

// ToolRepositoryRequiredText is what a tool call gets back when it named no
// repository_id and this run is not bound to one either.
func ToolRepositoryRequiredText(toolName string) string {
	return toolRepositoryRequiredKey.Render(toolRepositoryRequiredInput{ToolName: toolName})
}
