package evolution

// reflectionOutputSchema is the reflection stage's structured-output contract
// — see reflectionSystemPrompt's JSON template and parseReflectionOutput /
// domain.ReflectionOutput. Passed via domain.JSONSchemaResponseFormat, it lets
// a provider that supports constrained decoding guarantee the shape instead
// of relying on the model reading the prose rule, which is what the retry in
// runLLM (one extra turn telling the model its JSON did not parse) exists to
// recover from on providers that don't.
//
// Every object requires all of its properties and sets
// "additionalProperties": false — not because parseReflectionOutput checks
// for them (it does a single json.Unmarshal into domain.ReflectionOutput with
// no presence checks at all), but because OpenAI's strict json_schema mode
// rejects a schema with an optional property or unlisted keys. A provider
// always filling in "skills": [] when nothing changed costs nothing: it is
// exactly what an agent with self-evolution disabled already returns today.
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
		},
		"required": []string{"action", "skill_id", "name", "description", "category", "content", "source_urls"},
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
		},
		"required": []string{"action", "rule_id", "name", "content", "priority"},
	}
	memoryChange := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"action":    map[string]interface{}{"type": "string", "enum": []string{"create", "delete"}},
			"memory_id": map[string]interface{}{"type": "string"},
			"content":   map[string]interface{}{"type": "string"},
			"category":  map[string]interface{}{"type": "string"},
		},
		"required": []string{"action", "memory_id", "content", "category"},
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
