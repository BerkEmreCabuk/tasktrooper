package orchestrator

// Strict JSON Schemas for the toolless stages' structured output — required,
// all-properties, additionalProperties:false — so providers can constrain decoding
// instead of relying on the prose rule, at no cost to the prose-only fallback.

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

func stringArraySchema() map[string]interface{} {
	return map[string]interface{}{
		"type":  "array",
		"items": map[string]interface{}{"type": "string"},
	}
}

// Planner's task shape; carries "difficulty", which the replanner's does not.
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
