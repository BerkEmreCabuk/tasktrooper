package evolution

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// Go-side handles onto catalog/system/schemas/*.json, in place of the
// schema-builder functions in schema.go, golden.go and promote.go (see
// catalog/system/README.md, "Schemas").
var (
	agentReflectionSchemaKey   = prompt.DefineSchema("agent_reflection")
	goldenGateVerdictSchemaKey = prompt.DefineSchema("golden_gate_verdict")
	memoryPromotionSchemaKey   = prompt.DefineSchema("memory_promotion")
	memoryOrSkillSchemaKey     = prompt.DefineSchema("memory_or_skill")
)
