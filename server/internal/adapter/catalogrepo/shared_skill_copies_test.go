package catalogrepo

import "testing"

// skillVariantAllowList names skills that are deliberately different across
// the agents that hold them (a real per-role variant, not drift). Keep this
// list explicit and short: anything not listed here is expected to be
// byte-identical in every agent that holds it.
var skillVariantAllowList = map[string]bool{
	// backend ships a container; frontend ships a container OR a static
	// export — the two decks of steps are not interchangeable.
	"cloud-deploy-gcp-aws": true,
}

// TestSharedSkillsStayIdentical catches the drift 987625f introduced in
// local-project-context (the frontend copy silently lost the "web/" path
// while the other four agents kept it): a skill name held by two or more
// agents is either a deliberate variant (skillVariantAllowList) or it must
// read exactly the same everywhere, content and description both. A skill
// that legitimately needs to differ belongs on the allow-list, not a silent
// edit to one copy.
func TestSharedSkillsStayIdentical(t *testing.T) {
	agents := repoCatalogAgents(t)

	type copyOf struct {
		agentSlug   string
		content     string
		description string
	}
	bySkill := make(map[string][]copyOf)
	for slug, agent := range agents {
		for _, sk := range agent.Skills {
			bySkill[sk.Name] = append(bySkill[sk.Name], copyOf{
				agentSlug:   slug,
				content:     sk.Content,
				description: sk.Description,
			})
		}
	}

	checked := 0
	for name, copies := range bySkill {
		if len(copies) < 2 {
			continue
		}
		if skillVariantAllowList[name] {
			continue
		}
		checked++
		want := copies[0]
		for _, got := range copies[1:] {
			if got.content != want.content {
				t.Errorf("skill %q: %s and %s have diverged (content) — shared copies must stay byte-identical, or be added to skillVariantAllowList", name, want.agentSlug, got.agentSlug)
			}
			if got.description != want.description {
				t.Errorf("skill %q: %s and %s have diverged (description)", name, want.agentSlug, got.agentSlug)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no shared skill (held by 2+ agents) was found to compare — repoCatalogAgents likely returned an empty/partial catalog")
	}
}
