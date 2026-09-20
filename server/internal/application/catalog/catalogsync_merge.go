package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// mergeSkill asks the LLM to merge a skill that changed both on this agent and
// in the catalog. The provenance question it answers is skewed deliberately
// toward the LOCAL copy's intent: the local one is where self-evolution and
// the user's own edits land, so the merge keeps its tags and tech stack and
// lets the model reconcile the prose.
func (s *Service) mergeSkill(ctx context.Context, agent domain.Agent, local domain.Skill, usk domain.UpstreamSkill) error {
	system := "You merge two versions of the same agent skill into one SKILL.md document. " +
		"Reply with ONLY the merged document, frontmatter first (name:, description:, category:), " +
		"then ---, then the merged body. Keep every distinct instruction from both sides; " +
		"drop only what the two versions contradict themselves about."
	var user strings.Builder
	fmt.Fprintf(&user, "Skill: %s\nThis machine's current copy (LOCAL):\n---\n%s\n---\n", local.Name, local.Content)
	fmt.Fprintf(&user, "New catalog revision (UPSTREAM):\n---\n%s\n---\n", usk.Content)

	model := agent.Model
	if model == "" {
		model = agent.ModelHeavy
	}
	resp, err := s.llm.Chat(ctx, domain.AgentRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: system},
			{Role: domain.RoleUser, Content: user.String()},
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
		Tags: local.Tags, Content: body, Enabled: true, TechStackID: local.TechStackID,
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
