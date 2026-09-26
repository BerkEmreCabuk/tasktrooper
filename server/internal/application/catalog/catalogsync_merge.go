package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// mergeSkillSystemPrompt is the fixed instruction for reconciling two SKILL.md
// versions; see catalog/system/prompts/catalog/merge_skill_system.md.
func mergeSkillSystemPrompt() string {
	return "You merge two versions of the same agent skill into one SKILL.md document. " +
		"Reply with ONLY the merged document, frontmatter first (name:, description:, category:), " +
		"then ---, then the merged body. Keep every distinct instruction from both sides; " +
		"drop only what the two versions contradict themselves about."
}

// mergeSkillUserMessage carries the two document bodies the LLM must
// reconcile; see catalog/system/prompts/catalog/merge_skill_user.md.
func mergeSkillUserMessage(skillName, local, upstream string) string {
	var user strings.Builder
	fmt.Fprintf(&user, "Skill: %s\nThis machine's current copy (LOCAL):\n---\n%s\n---\n", skillName, local)
	fmt.Fprintf(&user, "New catalog revision (UPSTREAM):\n---\n%s\n---\n", upstream)
	return user.String()
}

// Asks the LLM to merge a skill changed both locally and upstream; the local copy's tags and tech stack win, and the model reconciles the prose.
func (s *Service) mergeSkill(ctx context.Context, agent domain.Agent, local domain.Skill, usk domain.UpstreamSkill) error {
	system := mergeSkillSystemPrompt()
	user := mergeSkillUserMessage(local.Name, local.Content, usk.Content)

	model := agent.Model
	if model == "" {
		model = agent.ModelHeavy
	}
	resp, err := s.llm.Chat(ctx, domain.AgentRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: system},
			{Role: domain.RoleUser, Content: user},
		},
		Model: model,
	})
	if err != nil {
		return fmt.Errorf("LLM merge call: %w", err)
	}
	meta, body, err := parseSeedDoc(resp.Message.Content)
	if err != nil {
		return fmt.Errorf("LLM merge output is not a SKILL.md: %w", err)
	}
	name := strings.TrimSpace(meta["name"])
	if name == "" {
		name = local.Name
	}
	_, err = s.UpdateSkillForAgent(ctx, agent.ID, local.ID, domain.UpdateSkillRequest{
		Name: name, Description: meta["description"], Category: meta["category"],
		Tags: local.Tags, Content: body, Enabled: usk.Enabled, TechStackID: local.TechStackID,
	})
	if err != nil {
		return err
	}
	updated, err := s.store.GetSkill(ctx, local.ID)
	if err != nil {
		return err
	}
	return s.stampSkillSHA(ctx, updated, usk.Sha)
}
