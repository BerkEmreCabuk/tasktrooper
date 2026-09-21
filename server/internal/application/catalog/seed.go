package catalog

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// seedSkill stores the skill without a vector. Embedding here made the seed as
// slow as the embedding provider: the requests-per-minute pacing alone put the
// role catalog past the boot step's deadline, and a failed catalog deletes its
// half-built agent. BackfillSkillEmbeddings embeds the skills afterwards.
func (s *Service) seedSkill(ctx context.Context, agentID uuid.UUID, req domain.CreateSkillRequest) error {
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	_, err := s.store.CreateSkill(ctx, domain.Skill{
		AgentID: agentID, Name: req.Name, Description: req.Description, Category: req.Category,
		Tags: tags, Content: req.Content, Enabled: req.Enabled,
		TechStackID: req.TechStackID,
	})
	return err
}

// BackfillSkillEmbeddings embeds every skill stored without a vector, with the
// client's normal pacing and retries, and returns how many it updated.
// seedSkill (used by CreateAgentFromTemplate) stores skills without one, so
// this is what fills them in.
func (s *Service) BackfillSkillEmbeddings(ctx context.Context) (int, error) {
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("list agents: %w", err)
	}
	updated := 0
	for _, agent := range agents {
		skills, err := s.store.ListSkillsByAgent(ctx, agent.ID)
		if err != nil {
			return updated, fmt.Errorf("list skills of %s: %w", agent.Name, err)
		}
		for _, sk := range skills {
			if len(sk.Embedding) > 0 {
				continue
			}
			emb, err := s.llm.Embed(ctx, sk.Name+"\n"+sk.Description+"\n"+sk.Content, s.embeddingModel)
			if err != nil {
				return updated, fmt.Errorf("embed skill %s: %w", sk.Name, err)
			}
			if len(emb) == 0 {
				continue
			}
			sk.Embedding = emb
			if _, err := s.store.UpdateSkill(ctx, sk); err != nil {
				return updated, fmt.Errorf("update skill %s: %w", sk.Name, err)
			}
			updated++
		}
	}
	return updated, nil
}
