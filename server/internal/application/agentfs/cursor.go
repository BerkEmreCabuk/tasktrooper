package agentfs

import (
	"fmt"
	"strings"
)

const cursorRulesDir = ".cursor/rules"

const cursorAgentOrder = "000-"

func renderCursor(b Bundle) []file {
	skills := usableSkills(b.Skills)
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	slugs := uniqueNames(names)

	files := make([]file, 0, len(skills)+1)
	files = append(files, file{
		rel:  fmt.Sprintf("%s/%s%s.mdc", cursorRulesDir, ttPrefix+cursorAgentOrder, strings.TrimPrefix(slug(b.Agent.Name), ttPrefix)),
		body: cursorAgentRule(b),
	})
	for i, s := range skills {
		files = append(files, file{
			rel:  fmt.Sprintf("%s/%s.mdc", cursorRulesDir, slugs[i]),
			body: cursorSkillRule(describe(s.Description, s.Name), s.Name, s.Content, b.stackName(s)),
		})
	}
	return files
}

func cursorAgentRule(b Bundle) string {
	sections := make([]string, 0, 2)
	if prompt := strings.TrimSpace(b.Agent.SystemPrompt); prompt != "" {
		sections = append(sections, prompt)
	}
	if rules := ruleBody(b.Rules); rules != "" {
		sections = append(sections, "## Rules\n\n"+rules)
	}
	if len(sections) == 0 {
		sections = append(sections, fmt.Sprintf("You are the %s agent.", strings.TrimSpace(b.Agent.Name)))
	}
	return mdc(describe(b.Agent.Description, b.Agent.Name), true, "", strings.Join(sections, "\n\n"))
}

func cursorSkillRule(description, name, content, techStack string) string {
	body := fmt.Sprintf("# %s\n\n%s", strings.TrimSpace(name), strings.TrimSpace(content))
	return mdc(description, false, techStack, body)
}

func mdc(description string, alwaysApply bool, techStack, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "description: %s\n", yamlString(description))
	b.WriteString("globs: \"\"\n")
	fmt.Fprintf(&b, "alwaysApply: %t\n", alwaysApply)
	if techStack != "" {
		fmt.Fprintf(&b, "tech_stack: %s\n", yamlString(techStack))
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}
