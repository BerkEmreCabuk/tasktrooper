package evolution

// The strict shape exists for OpenAI's strict json_schema mode, which rejects a schema with an optional property or unlisted keys — parseReflectionOutput itself has no presence checks. Every object is all-required with additionalProperties:false; a provider always filling in "skills": [] costs nothing.
func reflectionOutputSchema() map[string]interface{} {
	skillChange := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"action":      map[string]interface{}{"type": "string", "enum": []string{"create", "update", "delete"}},
			"skill_id":    map[string]interface{}{"type": "string"},
			"name":        map[string]interface{}{"type": "string"},
			"description": map[string]interface{}{"type": "string"},
			"category":    map[string]interface{}{"type": "string"},
			"content":     map[string]interface{}{"type": "string"},
			"source_urls": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"reason":      map[string]interface{}{"type": "string"},
		},
		"required": []string{"action", "skill_id", "name", "description", "category", "content", "source_urls", "reason"},
	}
	ruleChange := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"action":   map[string]interface{}{"type": "string", "enum": []string{"create", "update", "delete"}},
			"rule_id":  map[string]interface{}{"type": "string"},
			"name":     map[string]interface{}{"type": "string"},
			"content":  map[string]interface{}{"type": "string"},
			"priority": map[string]interface{}{"type": "integer"},
			"reason":   map[string]interface{}{"type": "string"},
		},
		"required": []string{"action", "rule_id", "name", "content", "priority", "reason"},
	}
	memoryChange := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"action":    map[string]interface{}{"type": "string", "enum": []string{"create", "delete"}},
			"memory_id": map[string]interface{}{"type": "string"},
			"content":   map[string]interface{}{"type": "string"},
			"category":  map[string]interface{}{"type": "string"},
			"reason":    map[string]interface{}{"type": "string"},
		},
		"required": []string{"action", "memory_id", "content", "category", "reason"},
	}
	revert := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"evolution_event_id": map[string]interface{}{"type": "string"},
			"reason":             map[string]interface{}{"type": "string"},
		},
		"required": []string{"evolution_event_id", "reason"},
	}
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"self_assessment": map[string]interface{}{"type": "string"},
			"skills":          map[string]interface{}{"type": "array", "items": skillChange},
			"rules":           map[string]interface{}{"type": "array", "items": ruleChange},
			"memories":        map[string]interface{}{"type": "array", "items": memoryChange},
			"reverts":         map[string]interface{}{"type": "array", "items": revert},
		},
		"required": []string{"self_assessment", "skills", "rules", "memories", "reverts"},
	}
}
