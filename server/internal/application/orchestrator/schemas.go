package orchestrator

// JSON Schemas for the pipeline stages' structured output (planner, intake,
// verifier, replanner). Passed via domain.JSONSchemaResponseFormat, these let
// a provider that supports constrained decoding (the OpenAI-compatible
// adapter's json_schema+strict, Gemini's ResponseJsonSchema, Anthropic's
// output_config) guarantee the shape instead of relying on the model reading
// the prose "Respond with ONLY valid JSON matching this exact schema" rule —
// which is what fed the parse-repair retry loop (pipelineCorrection /
// pipelineRejection in conversation.go) on every stray comma or dropped field.
//
// Every object below lists ALL of its properties in "required" and sets
// "additionalProperties": false. That is not a parser requirement — none of
// these fields are ever range-checked for extra keys — it is what OpenAI's
// strict json_schema mode itself demands: a schema with an optional property,
// or one that allows unlisted keys, is rejected outright. It costs nothing
// here: a field the Go parser treats as optional (an empty array, an empty
// string) is still satisfied by the provider always including the key, and
// the parse/repair/retry path in conversation.go stays exactly as it was for
// providers that fall back to the prose rule instead.
//
// Each schema mirrors what its stage's system prompt actually advertises
// (buildPlannerSystemPrompt, buildReplannerSystemPrompt, ...), not the raw
// unmarshal target's full field set: the planner's parse struct also reads
// "purpose"/"goal", for instance, but the prompt never asks for them and the
// caller always overwrites them with the intake's values afterward, so a
// strict schema omits them rather than force the model to invent content for
// a field nothing downstream reads.

// clarificationOptionSchema mirrors domain.ClarificationOption.
func clarificationOptionSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"id":    map[string]interface{}{"type": "string"},
			"label": map[string]interface{}{"type": "string"},
		},
		"required": []string{"id", "label"},
	}
}

// clarificationQuestionSchema mirrors domain.ClarificationQuestion — the
// "questions" array item shared by the planner and intake schemas (see
// prompt.AskUserQuestionJSONShape, the prose template both prompts embed).
func clarificationQuestionSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"id":             map[string]interface{}{"type": "string"},
			"prompt":         map[string]interface{}{"type": "string"},
			"allow_multiple": map[string]interface{}{"type": "boolean"},
			"options": map[string]interface{}{
				"type":  "array",
				"items": clarificationOptionSchema(),
			},
		},
		"required": []string{"id", "prompt", "allow_multiple", "options"},
	}
}

// stringArraySchema is the recurring "array of strings" shape (skill_ids,
// tool_names, subtask_rules, depends_on, constraints, issues, source_urls —
// all []string on the Go side).
func stringArraySchema() map[string]interface{} {
	return map[string]interface{}{
		"type":  "array",
		"items": map[string]interface{}{"type": "string"},
	}
}

// plannerTaskSchema mirrors the per-task object in planner.go's
// parsePlannerJSON raw struct / domain.PlannerTask, including "difficulty" —
// present in the planner's own advertised shape (buildPlannerSystemPrompt)
// but not the replanner's (see repairTaskSchema).
func plannerTaskSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"id":             map[string]interface{}{"type": "string"},
			"title":          map[string]interface{}{"type": "string"},
			"description":    map[string]interface{}{"type": "string"},
			"agent_id":       map[string]interface{}{"type": "string"},
			"skill_ids":      stringArraySchema(),
			"tool_names":     stringArraySchema(),
			"subtask_rules":  stringArraySchema(),
			"depends_on":     stringArraySchema(),
			"difficulty":     map[string]interface{}{"type": "string", "enum": []string{"easy", "hard"}},
			"parallel_group": map[string]interface{}{"type": "integer"},
		},
		"required": []string{
			"id", "title", "description", "agent_id", "skill_ids",
			"tool_names", "subtask_rules", "depends_on", "difficulty", "parallel_group",
		},
	}
}

// repairTaskSchema is plannerTaskSchema without "difficulty" — the
// replanner's prompt (buildReplannerSystemPrompt) never asks for it, and a
// repair task with none defaults to "easy" via normalizeDifficulty.
func repairTaskSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"id":             map[string]interface{}{"type": "string"},
			"title":          map[string]interface{}{"type": "string"},
			"description":    map[string]interface{}{"type": "string"},
			"agent_id":       map[string]interface{}{"type": "string"},
			"skill_ids":      stringArraySchema(),
			"tool_names":     stringArraySchema(),
			"subtask_rules":  stringArraySchema(),
			"depends_on":     stringArraySchema(),
			"parallel_group": map[string]interface{}{"type": "integer"},
		},
		"required": []string{
			"id", "title", "description", "agent_id", "skill_ids",
			"tool_names", "subtask_rules", "depends_on", "parallel_group",
		},
	}
}

// plannerOutputSchema is the planner's structured-output contract — see
// buildPlannerSystemPrompt's JSON template and parsePlannerJSON.
func plannerOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"ready":     map[string]interface{}{"type": "boolean"},
			"summary":   map[string]interface{}{"type": "string"},
			"questions": map[string]interface{}{"type": "array", "items": clarificationQuestionSchema()},
			"tasks":     map[string]interface{}{"type": "array", "items": plannerTaskSchema()},
		},
		"required": []string{"ready", "summary", "questions", "tasks"},
	}
}

// replannerOutputSchema is the replanner's structured-output contract — see
// buildReplannerSystemPrompt's JSON template and parseRepairPlanOutput.
func replannerOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"summary": map[string]interface{}{"type": "string"},
			"tasks":   map[string]interface{}{"type": "array", "items": repairTaskSchema()},
		},
		"required": []string{"summary", "tasks"},
	}
}

// intakeOutputSchema is goal intake's structured-output contract — see
// buildIntakeSystemPrompt's JSON template and parseGoalIntake.
func intakeOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"ready":       map[string]interface{}{"type": "boolean"},
			"purpose":     map[string]interface{}{"type": "string"},
			"goal":        map[string]interface{}{"type": "string"},
			"constraints": stringArraySchema(),
			"questions":   map[string]interface{}{"type": "array", "items": clarificationQuestionSchema()},
		},
		"required": []string{"ready", "purpose", "goal", "constraints", "questions"},
	}
}

// verifierOutputSchema is the verifier's structured-output contract — see
// buildVerifierSystemPrompt's JSON template and parseVerificationResult.
func verifierOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"passed":  map[string]interface{}{"type": "boolean"},
			"issues":  stringArraySchema(),
			"summary": map[string]interface{}{"type": "string"},
		},
		"required": []string{"passed", "issues", "summary"},
	}
}
