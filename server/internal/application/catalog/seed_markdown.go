package catalog

import (
	"embed"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

//go:embed seeddata
var seedData embed.FS

// skillSeed pairs a skill's create request with the tech stack name (if any)
// its `tech_stack:` front-matter named. A name travels here instead of an id
// because no id exists yet at parse time: EnsureRoleAgents resolves it against
// the stack it creates on the agent being seeded, and EnsureRoleTemplates never
// has an agent to resolve one against at all — it stores the name on the
// template the same way domain.TemplateSkill does. Empty means general, the
// same convention TemplateSkill.TechStack uses.
type skillSeed struct {
	req       domain.CreateSkillRequest
	techStack string
}

func mdSkill(scope, name string) skillSeed {
	// One directory per skill, holding SKILL.md: the shape Claude Code and
	// Cursor discover natively, and the shape application/agentfs writes the
	// catalog into a task workspace as. A seed file and the file an
	// agent reads at run time are then the same kind of file.
	path := fmt.Sprintf("seeddata/skills/%s/%s/SKILL.md", scope, name)
	meta, body := mustSeedDoc(path)
	if meta["name"] != name {
		panic(fmt.Sprintf("catalog seeddata: %s: frontmatter name %q does not match filename", path, meta["name"]))
	}
	if meta["category"] == "" || meta["description"] == "" {
		panic(fmt.Sprintf("catalog seeddata: %s: frontmatter requires category and description", path))
	}
	return skillSeed{
		req: domain.CreateSkillRequest{
			Name:        name,
			Category:    meta["category"],
			Description: meta["description"],
			Content:     body,
			Tags:        []string{},
			Enabled:     true,
		},
		techStack: strings.TrimSpace(meta["tech_stack"]),
	}
}

// mdSkillDisabled seeds a skill the role still OWNS but must not use yet: the
// row exists, the prompt builder skips it (it injects enabled skills only), and
// turning the capability back on is a one-word edit rather than an archaeology
// exercise over deleted files.
//
// This is how a deferred capability is parked. Deleting the skill from the role
// (deprecatedRoleSkills) is for one that is wrong, not for one whose turn has
// not come — that path also drops any edit an operator made to it.
func mdSkillDisabled(scope, name string) skillSeed {
	seed := mdSkill(scope, name)
	seed.req.Enabled = false
	return seed
}

func mdPrompt(agentName string) (description, systemPrompt string) {
	path := fmt.Sprintf("seeddata/agents/%s.md", agentName)
	meta, body := mustSeedDoc(path)
	if meta["description"] == "" {
		panic(fmt.Sprintf("catalog seeddata: %s: frontmatter requires description", path))
	}
	return meta["description"], body
}

func mustSeedDoc(path string) (map[string]string, string) {
	raw, err := seedData.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("catalog seeddata: %s: %v", path, err))
	}
	meta, body, err := parseSeedDoc(string(raw))
	if err != nil {
		panic(fmt.Sprintf("catalog seeddata: %s: %v", path, err))
	}
	return meta, body
}

func parseSeedDoc(raw string) (map[string]string, string, error) {
	content := strings.TrimPrefix(raw, "\ufeff")
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return nil, "", fmt.Errorf("missing frontmatter opening ---")
	}
	meta := map[string]string{}
	bodyStart := -1
	for i := 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if line == "---" {
			bodyStart = i + 1
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return nil, "", fmt.Errorf("invalid frontmatter line %d: %q", i+1, line)
		}
		meta[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if bodyStart == -1 {
		return nil, "", fmt.Errorf("missing frontmatter closing ---")
	}
	body := strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
	if body == "" {
		return nil, "", fmt.Errorf("empty document body")
	}
	return meta, body, nil
}
