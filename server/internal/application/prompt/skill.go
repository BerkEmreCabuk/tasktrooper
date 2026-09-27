package prompt

var skillSelfEvolutionDisabledKey = Define[struct{}]("guard.skill_self_evolution_disabled", struct{}{})

type skillMaxSkillsInput struct{ Count int }

var skillMaxSkillsKey = Define("guard.skill_max_skills", skillMaxSkillsInput{Count: 20})

// skillNameExistsInput.Name is the bare name — the template supplies its own
// quotes, matching the original manual `"` + name + `"` concatenation.
// skillNoTechStacksInput.Name and skillUnknownTechStackInput.Name are
// %q-quoted by the caller instead: those two originals used fmt's %q verb,
// and their templates do not re-add quotes.
type skillNameExistsInput struct{ Name string }

var skillNameExistsKey = Define("guard.skill_name_exists", skillNameExistsInput{Name: "deploy checklist"})

type skillNoTechStacksInput struct{ Name string }

var skillNoTechStacksKey = Define("guard.skill_no_tech_stacks", skillNoTechStacksInput{Name: `"django"`})

type skillUnknownTechStackInput struct {
	Name      string
	Available string
}

var skillUnknownTechStackKey = Define("guard.skill_unknown_tech_stack", skillUnknownTechStackInput{Name: `"django"`, Available: "React, Rails"})

type skillNotFoundInput struct {
	Query     string
	Available string
}

var skillNotFoundKey = Define("tool_results.skill_not_found", skillNotFoundInput{Query: "deploy checklist", Available: "release checklist, qa checklist"})

var skillCreatedKey = Define[struct{}]("tool_results.skill_created", struct{}{})

// SkillSelfEvolutionDisabledText is create_skill's refusal for an agent whose
// SelfEvolutionEnabled flag is off.
func SkillSelfEvolutionDisabledText() string { return Text(skillSelfEvolutionDisabledKey) }

// SkillMaxSkillsText is create_skill's refusal once an agent already holds
// its configured MaxSkills.
func SkillMaxSkillsText(count int) string {
	return skillMaxSkillsKey.Render(skillMaxSkillsInput{Count: count})
}

// SkillNameExistsText is create_skill's refusal for a name the agent already
// holds. name is bare; the template supplies its own quotes.
func SkillNameExistsText(name string) string {
	return skillNameExistsKey.Render(skillNameExistsInput{Name: name})
}

// SkillNoTechStacksText is create_skill's refusal for a tech_stack argument
// when the agent has none registered. name must already be %q-quoted.
func SkillNoTechStacksText(name string) string {
	return skillNoTechStacksKey.Render(skillNoTechStacksInput{Name: name})
}

// SkillUnknownTechStackText is create_skill's refusal for a tech_stack that
// does not match any of the agent's own stacks. name must already be
// %q-quoted.
func SkillUnknownTechStackText(name, available string) string {
	return skillUnknownTechStackKey.Render(skillUnknownTechStackInput{Name: name, Available: available})
}

// SkillNotFoundText is load_skill's refusal when the query matches none of
// the agent's enabled skills.
func SkillNotFoundText(query, available string) string {
	return skillNotFoundKey.Render(skillNotFoundInput{Query: query, Available: available})
}

// SkillCreatedMessage is the "message" field of a successful create_skill
// response.
func SkillCreatedMessage() string { return Text(skillCreatedKey) }
