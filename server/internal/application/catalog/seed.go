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
// seedSkill (used both by the old boot seed and by CreateAgentFromTemplate
// today) stores every skill without one, so this is what fills them in.
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

type roleAgentDef struct {
	agent domain.CreateAgentRequest
	// techStacks are the tech stacks this role's own skills are filed under,
	// created on the agent template (EnsureRoleTemplates) before its skills
	// are, so every skill's front-matter tech_stack name has something to
	// resolve against when CreateAgentFromTemplate recreates them
	// (recreateTechStacks in templates.go). A role with no stack-specific
	// skills (product-manager, qa-agent, system-architect today) leaves this
	// nil.
	techStacks []domain.CreateTechStackRequest
	skills     []skillSeed
	rules      []domain.CreateOrchestratorRuleRequest
	kpis       []domain.CreateKPIRequest
}

func roleAgentDefinitions() []roleAgentDef {
	defs := []roleAgentDef{
		systemArchitectAgent(),
		backendDeveloperAgent(),
		frontendDeveloperAgent(),
		mobileDeveloperAgent(),
		productManagerAgent(),
		qaAgent(),
	}
	for i := range defs {
		defs[i].kpis = defaultRoleKPIs(defs[i].agent.Name)
	}
	return defs
}

// defaultRoleKPIs is mirrored by migrations/142_role_kpis_v2.up.sql, which
// brings installs seeded before a change here onto the new set by matching
// existing rows on (agent name, KPI name). Bump that migration's own VALUES
// table alongside any change made here.
func defaultRoleKPIs(agentName string) []domain.CreateKPIRequest {
	devTasksCompleted := domain.CreateKPIRequest{
		MetricKey: "tasks_completed", Name: "Weekly tasks completed", Period: domain.KPIPeriodWeekly,
		TargetFull: 10, TargetHalf: 4, Weight: 1, Enabled: true,
	}
	revisions := domain.CreateKPIRequest{
		MetricKey: "revisions_received", Name: "Weekly revisions", Period: domain.KPIPeriodWeekly,
		TargetFull: 1, TargetHalf: 3, Weight: 1, Enabled: true,
	}
	// Speed targets carry weight 1 while the quality KPIs beside them carry
	// 1.5 and 2. The composite therefore cannot be raised by trading quality
	// away, which is the same rule the clean-only measurement enforces from the
	// other side.
	firstPassRate := domain.CreateKPIRequest{
		MetricKey: "first_pass_rate", Name: "First-pass rate", Period: domain.KPIPeriodWeekly,
		TargetFull: 90, TargetHalf: 75, Weight: 2, Enabled: true,
	}
	uatRejections := domain.CreateKPIRequest{
		MetricKey: "uat_failures", Name: "Weekly UAT rejections", Period: domain.KPIPeriodWeekly,
		TargetFull: 0, TargetHalf: 2, Weight: 1.5, Enabled: true,
	}
	bugs := domain.CreateKPIRequest{
		MetricKey: "bugs_assigned", Name: "Weekly bugs", Period: domain.KPIPeriodWeekly,
		TargetFull: 2, TargetHalf: 5, Weight: 1, Enabled: true,
	}
	devSpeed := domain.CreateKPIRequest{
		MetricKey: "clean_time_in_progress", Name: "Clean cycle time (in progress)", Period: domain.KPIPeriodWeekly,
		TargetFull: 1.5, TargetHalf: 4, Weight: 1, Enabled: true,
	}
	toolErrorRate := domain.CreateKPIRequest{
		MetricKey: "tool_error_rate", Name: "Tool error rate", Period: domain.KPIPeriodWeekly,
		TargetFull: 3, TargetHalf: 6, Weight: 0.5, Enabled: true,
	}
	gateRejectedRuns := domain.CreateKPIRequest{
		MetricKey: "gate_rejected_runs", Name: "Gate-rejected runs", Period: domain.KPIPeriodWeekly,
		TargetFull: 0, TargetHalf: 2, Weight: 1.5, Enabled: true,
	}
	architectSpeed := domain.CreateKPIRequest{
		MetricKey: "clean_time_code_review", Name: "Clean review time", Period: domain.KPIPeriodWeekly,
		TargetFull: 0.5, TargetHalf: 2, Weight: 1, Enabled: true,
	}
	// The architect also implements analiz tasks, which sit in in_progress.
	architectAnalysisSpeed := domain.CreateKPIRequest{
		MetricKey: "clean_time_in_progress", Name: "Clean analysis time", Period: domain.KPIPeriodWeekly,
		TargetFull: 2, TargetHalf: 6, Weight: 1, Enabled: true,
	}
	reviewEscapes := domain.CreateKPIRequest{
		MetricKey: "review_escapes", Name: "Weekly review escapes", Period: domain.KPIPeriodWeekly,
		TargetFull: 0, TargetHalf: 1, Weight: 2, Enabled: true,
	}
	qaTasksCompleted := domain.CreateKPIRequest{
		MetricKey: "tasks_completed", Name: "Weekly tasks completed", Period: domain.KPIPeriodWeekly,
		TargetFull: 5, TargetHalf: 2, Weight: 1, Enabled: true,
	}
	qaSpeed := domain.CreateKPIRequest{
		MetricKey: "clean_time_in_qa", Name: "Clean QA time", Period: domain.KPIPeriodWeekly,
		TargetFull: 0.5, TargetHalf: 2, Weight: 1, Enabled: true,
	}
	pmSpeed := domain.CreateKPIRequest{
		MetricKey: "clean_time_pm_uat", Name: "Clean UAT time", Period: domain.KPIPeriodWeekly,
		TargetFull: 0.25, TargetHalf: 1, Weight: 1, Enabled: true,
	}
	switch agentName {
	case "system-architect":
		return []domain.CreateKPIRequest{
			devTasksCompleted, revisions, firstPassRate, architectSpeed, architectAnalysisSpeed,
			reviewEscapes, gateRejectedRuns, toolErrorRate,
		}
	case "backend-developer", "frontend-developer", "mobile-developer":
		return []domain.CreateKPIRequest{
			devTasksCompleted, revisions, firstPassRate, uatRejections, bugs, devSpeed, toolErrorRate,
		}
	case "qa-agent":
		return []domain.CreateKPIRequest{
			qaTasksCompleted,
			{MetricKey: "uat_failures", Name: "Weekly UAT escapes", Period: domain.KPIPeriodWeekly,
				TargetFull: 0, TargetHalf: 2, Weight: 1.5, Enabled: true},
			qaSpeed, gateRejectedRuns, toolErrorRate,
		}
	case "product-manager":
		return []domain.CreateKPIRequest{qaTasksCompleted, pmSpeed, gateRejectedRuns, toolErrorRate}
	default:
		return nil
	}
}
