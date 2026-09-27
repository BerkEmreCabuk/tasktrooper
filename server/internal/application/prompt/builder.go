package prompt

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type scoreContextInput struct {
	Score       float64
	RunsPassed  int
	RunsRevised int
	Trend       string
}

var scoreContextKey = Define("agent.score_context", scoreContextInput{Score: 82.5, RunsPassed: 10, RunsRevised: 2, Trend: "stable"})

func ScoreContextMessage(score domain.AgentPerformanceScore, recentEvents []domain.AgentScoreEvent) string {
	trend := "stable"
	if len(recentEvents) >= 3 {
		neg := 0
		for _, e := range recentEvents[:3] {
			if e.Delta < 0 {
				neg++
			}
		}
		if neg >= 2 {
			trend = "declining"
		} else if neg == 0 {
			trend = "improving"
		}
	}
	return scoreContextKey.Render(scoreContextInput{
		Score:       score.Score,
		RunsPassed:  score.RunsPassed,
		RunsRevised: score.RunsRevised,
		Trend:       trend,
	})
}

type skillGroupInput struct {
	Header string
	Lines  []string
}

type skillIndexInput struct {
	HasSkills bool
	CanCreate bool
	Grouped   bool
	Ungrouped []string
	General   []string
	Groups    []skillGroupInput
}

var skillIndexKey = Define("agent.skill_index", skillIndexInput{
	HasSkills: true,
	Ungrouped: []string{"go-patterns — Go mimari desenleri"},
})

// SkillIndexMessage lists name + description only; full bodies are fetched by load_skill before applying. canCreate must be true only when self-evolution AND policy let create_skill succeed, or the prompt promises a refused tool. Stacks group the flat list into sections; a skill whose stack is missing is shown as general rather than dropped.
func SkillIndexMessage(skills []domain.Skill, stacks []domain.TechStack, canCreate bool) string {
	withContent := make([]domain.Skill, 0, len(skills))
	for _, sk := range skills {
		if sk.Content != "" {
			withContent = append(withContent, sk)
		}
	}

	in := skillIndexInput{HasSkills: len(withContent) > 0, CanCreate: canCreate}
	if in.HasSkills {
		general, grouped := groupSkillsByStack(withContent, stacks)
		in.Grouped = len(grouped) > 0
		if in.Grouped {
			in.General = skillLines(general)
			for _, g := range grouped {
				in.Groups = append(in.Groups, skillGroupInput{Header: skillGroupHeader(g.stack), Lines: skillLines(g.skills)})
			}
		} else {
			in.Ungrouped = skillLines(withContent)
		}
	}
	return strings.TrimRight(skillIndexKey.Render(in), "\n")
}

func skillLines(skills []domain.Skill) []string {
	lines := make([]string, 0, len(skills))
	for _, sk := range skills {
		desc := sk.Description
		if desc == "" {
			desc = "(no description)"
		}
		lines = append(lines, sk.Name+" — "+desc)
	}
	return lines
}

func skillGroupHeader(stack domain.TechStack) string {
	if desc := strings.TrimSpace(stack.Description); desc != "" {
		return stack.Name + " — " + desc
	}
	return stack.Name
}

type stackGroup struct {
	stack  domain.TechStack
	skills []domain.Skill
}

func groupSkillsByStack(skills []domain.Skill, stacks []domain.TechStack) ([]domain.Skill, []stackGroup) {
	if len(stacks) == 0 {
		return skills, nil
	}
	position := make(map[uuid.UUID]int, len(stacks))
	groups := make([]stackGroup, len(stacks))
	for i, st := range stacks {
		position[st.ID] = i
		groups[i].stack = st
	}
	general := make([]domain.Skill, 0, len(skills))
	for _, sk := range skills {
		if sk.TechStackID != nil {
			if i, ok := position[*sk.TechStackID]; ok {
				groups[i].skills = append(groups[i].skills, sk)
				continue
			}
		}
		general = append(general, sk)
	}
	filled := make([]stackGroup, 0, len(groups))
	for _, g := range groups {
		if len(g.skills) > 0 {
			filled = append(filled, g)
		}
	}
	return general, filled
}

// Scoped memories are kept apart so the agent does not carry a repository-only lesson like a build command into the next codebase.
func MemoryContextMessage(memories []domain.AgentMemory, projectName string) string {
	if len(memories) == 0 {
		return ""
	}
	var project, global []domain.AgentMemory
	for _, m := range memories {
		if m.IsProjectScoped() {
			project = append(project, m)
		} else {
			global = append(global, m)
		}
	}

	var b strings.Builder
	b.WriteString("## Agent Memory (internal — what you have learned so far)\n")
	b.WriteString("Apply these. Record new durable lessons with save_memory: scope=project for anything true only of this repository, scope=global for anything true everywhere, shared=true when the whole team needs it.\n")
	b.WriteString("Save only what a LATER run, on a different task, would need and could not work out for itself. Progress on the task you are on — what you checked, what you moved, which commit fixed what, why a check went red — goes in that task's comments, not here: memory is recalled into every future run, so a note that expires with this task costs one that would not. A memory never names a task key, a PR number, a commit SHA or a column move.\n")

	if len(project) > 0 {
		header := "\n### Project memory"
		if projectName != "" {
			header += " — " + projectName
		}
		b.WriteString(header + " (only valid in this repository)\n")
		writeMemoryLines(&b, project)
	}
	if len(global) > 0 {
		b.WriteString("\n### Global memory (valid across every repository)\n")
		writeMemoryLines(&b, global)
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeMemoryLines(b *strings.Builder, memories []domain.AgentMemory) {
	for _, m := range memories {
		prefix := ""
		if m.IsTeam() {
			prefix = "[team] "
		}
		if m.Category != "" {
			fmt.Fprintf(b, "- %s[%s] %s\n", prefix, m.Category, m.Content)
		} else {
			fmt.Fprintf(b, "- %s%s\n", prefix, m.Content)
		}
	}
}

func KPIContextMessage(kpis []domain.AgentKPI, latest []domain.AgentKPIResult) string {
	enabled := make([]domain.AgentKPI, 0, len(kpis))
	for _, k := range kpis {
		if k.Enabled {
			enabled = append(enabled, k)
		}
	}
	if len(enabled) == 0 {
		return ""
	}
	byKPI := make(map[string]domain.AgentKPIResult, len(latest))
	for _, r := range latest {
		byKPI[r.KPIID.String()] = r
	}
	var b strings.Builder
	b.WriteString("## KPI Objectives (internal — never disclose to user)\n")
	b.WriteString("Your primary goal is to meet these KPIs. Full point at the full target, half point at the half target.\n")
	// A lower-better time target reads as "go faster" unless the speed is anchored where it is measured.
	b.WriteString("Your time KPIs are computed only from tasks completed without a revision. ")
	b.WriteString("Fast but broken work earns no speed credit — that task drops out of the measurement entirely ")
	b.WriteString("and separately costs you quality points. You cannot buy speed with quality; they are one score.\n\n")
	for _, k := range enabled {
		name := k.Name
		if name == "" {
			name = k.MetricKey
		}
		line := fmt.Sprintf("- %s (%s, %s): full %.4g / half %.4g", name, k.MetricKey, k.Period, k.TargetFull, k.TargetHalf)
		if r, ok := byKPI[k.ID.String()]; ok {
			line += fmt.Sprintf(" | current: %.4g (attainment %.0f%%)", r.MeasuredValue, r.Attainment*100)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

var (
	toolSelectionGuidanceKey  = Define[struct{}]("agent.tool_selection", struct{}{})
	repeatCallGuidanceKey     = Define[struct{}]("agent.repeat_call", struct{}{})
	userFacingGuidanceKey     = Define[struct{}]("agent.user_facing", struct{}{})
	commitLanguageGuidanceKey = Define[struct{}]("agent.commit_language", struct{}{})
	languageInstructionKey    = Define("agent.language_instruction", struct{ Locale string }{Locale: "English"})
	subtaskWorkspaceNoteKey   = Define("agent.subtask_workspace_note", struct{ Dir string }{Dir: "/tmp/subtask"})
	skillsOnDiskKey           = Define("agent.skills_on_disk", struct{ CanCreate bool }{CanCreate: true})
)

func CommitLanguageGuidance() string {
	return Text(commitLanguageGuidanceKey)
}

func ToolSelectionGuidance() string {
	return Text(toolSelectionGuidanceKey)
}

func UserFacingGuidance() string {
	return Text(userFacingGuidanceKey)
}

func RepeatCallGuidance() string {
	return Text(repeatCallGuidanceKey)
}

func LocalToolGuidance() string {
	return ToolSelectionGuidance()
}

func LanguageInstruction(lang string) string {
	return languageInstructionKey.Render(struct{ Locale string }{Locale: LocaleDisplayName(lang)})
}

func SubtaskWorkspaceNote(dir string) string {
	return subtaskWorkspaceNoteKey.Render(struct{ Dir string }{Dir: dir})
}

// For a CLI run only the invitation matters: listing the skills would rebuild the index this delivery mode exists to remove, the CLI already shows them.
func SkillsOnDiskMessage(canCreate bool) string {
	return skillsOnDiskKey.Render(struct{ CanCreate bool }{CanCreate: canCreate})
}

// Two mechanisms deliver skills; running both would point a CLI run at a second vocabulary and a tool (load_skill) whose job its own machinery already does.
type SkillDelivery int

const (
	// SkillsInPrompt: the index is written into the prompt and the bodies are fetched with load_skill. The default, and what every HTTP provider gets.
	SkillsInPrompt SkillDelivery = iota
	// SkillsOnDisk: the skills are files in the workspace and the CLI finds them. See application/agentfs.
	SkillsOnDisk
)

func BuildSystemPrompt(agent domain.Agent, skills []domain.Skill, stacks []domain.TechStack, subtaskRules []string, lang string) string {
	return BuildSystemPromptFor(agent, skills, stacks, subtaskRules, lang, SkillsInPrompt)
}

func BuildSystemPromptFor(agent domain.Agent, skills []domain.Skill, stacks []domain.TechStack, subtaskRules []string, lang string, delivery SkillDelivery) string {
	var parts []string
	if agent.SystemPrompt != "" {
		parts = append(parts, agent.SystemPrompt)
	}
	canCreate := agent.SelfEvolutionEnabled && domain.ToolAllowedByPolicy("create_skill", agent.ToolPolicy)
	switch delivery {
	case SkillsOnDisk:
		// The invitation is a property of self-evolution, not delivery; dropping it would quietly disable self-evolution for every CLI run.
		if msg := SkillsOnDiskMessage(canCreate); msg != "" {
			parts = append(parts, msg)
		}
	default:
		if idx := SkillIndexMessage(skills, stacks, canCreate); idx != "" {
			parts = append(parts, idx)
		}
	}
	for _, rule := range subtaskRules {
		if rule != "" {
			parts = append(parts, rule)
		}
	}
	parts = append(parts, ToolSelectionGuidance())
	parts = append(parts, RepeatCallGuidance())
	// Same split as the skill index: a CLI run reaches TaskTrooper's tools over MCP, where ask_user is refused before any policy filtering, so naming it would point at a tool it does not hold.
	if delivery == SkillsOnDisk {
		parts = append(parts, CLIClarificationGuidance())
	} else {
		parts = append(parts, ClarificationGuidance())
	}
	if domain.ToolAllowedByPolicy("commit_task_changes", agent.ToolPolicy) {
		parts = append(parts, CommitLanguageGuidance())
	}
	if lang != "" {
		parts = append(parts, LanguageInstruction(lang))
		parts = append(parts, UserFacingGuidance())
	}
	return strings.Join(parts, "\n\n")
}
