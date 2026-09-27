package orchestrator

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// Strict JSON Schemas for the toolless stages' structured output — required,
// all-properties, additionalProperties:false — so providers can constrain decoding
// instead of relying on the prose rule, at no cost to the prose-only fallback.
// Content lives in catalog/system/schemas/*.json; these keys are the Go-side
// handle onto it (see catalog/system/README.md, "Schemas").
var (
	intakeOutputSchemaKey    = prompt.DefineSchema("goal_intake")
	plannerOutputSchemaKey   = prompt.DefineSchema("planner_output")
	replannerOutputSchemaKey = prompt.DefineSchema("replan_output")
	verifierOutputSchemaKey  = prompt.DefineSchema("verification_result")
)
