package prompt_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestGoldenScoreContextMessage(t *testing.T) {
	cases := []struct {
		name   string
		score  domain.AgentPerformanceScore
		events []domain.AgentScoreEvent
	}{
		{
			name:  "stable_no_events",
			score: domain.AgentPerformanceScore{Score: 82.5, RunsPassed: 10, RunsRevised: 2},
		},
		{
			name:  "declining",
			score: domain.AgentPerformanceScore{Score: 41.25, RunsPassed: 4, RunsRevised: 6},
			events: []domain.AgentScoreEvent{
				{Delta: -1, Reason: "revised"},
				{Delta: -1, Reason: "revised"},
				{Delta: 1, Reason: "clean"},
			},
		},
		{
			name:  "improving",
			score: domain.AgentPerformanceScore{Score: 95, RunsPassed: 20, RunsRevised: 1},
			events: []domain.AgentScoreEvent{
				{Delta: 1, Reason: "clean"},
				{Delta: 1, Reason: "clean"},
				{Delta: 1, Reason: "clean"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := prompt.ScoreContextMessage(tc.score, tc.events)
			assertGolden(t, "score_context_"+tc.name, got)
		})
	}
}

func TestGoldenSkillIndexMessage(t *testing.T) {
	django := domain.TechStack{ID: uuid.New(), Name: "Django", Description: "DRF backend"}
	cases := []struct {
		name      string
		skills    []domain.Skill
		stacks    []domain.TechStack
		canCreate bool
	}{
		{name: "empty_no_create"},
		{name: "empty_can_create", canCreate: true},
		{
			name:   "flat_list",
			skills: []domain.Skill{{Name: "a", Description: "desc a", Content: "body"}, {Name: "b", Description: "desc b", Content: "body"}},
		},
		{
			name: "grouped_with_general_and_create",
			skills: []domain.Skill{
				{Name: "general-skill", Description: "everywhere", Content: "body"},
				{Name: "drf-viewsets", Description: "viewsets", Content: "body", TechStackID: &django.ID},
			},
			stacks:    []domain.TechStack{django},
			canCreate: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := prompt.SkillIndexMessage(tc.skills, tc.stacks, tc.canCreate)
			assertGolden(t, "skill_index_"+tc.name, got)
		})
	}
}

func TestGoldenSkillsOnDiskMessage(t *testing.T) {
	assertGolden(t, "skills_on_disk_false", prompt.SkillsOnDiskMessage(false))
	assertGolden(t, "skills_on_disk_true", prompt.SkillsOnDiskMessage(true))
}

func TestGoldenStaticGuidance(t *testing.T) {
	assertGolden(t, "tool_selection_guidance", prompt.ToolSelectionGuidance())
	assertGolden(t, "repeat_call_guidance", prompt.RepeatCallGuidance())
	assertGolden(t, "user_facing_guidance", prompt.UserFacingGuidance())
	assertGolden(t, "commit_language_guidance", prompt.CommitLanguageGuidance())
}

func TestGoldenLanguageInstruction(t *testing.T) {
	assertGolden(t, "language_instruction_en", prompt.LanguageInstruction("en"))
	assertGolden(t, "language_instruction_tr", prompt.LanguageInstruction("tr"))
	assertGolden(t, "language_instruction_empty", prompt.LanguageInstruction(""))
}

func TestGoldenSubtaskWorkspaceNote(t *testing.T) {
	assertGolden(t, "subtask_workspace_note_basic", prompt.SubtaskWorkspaceNote("/tmp/ws"))
	assertGolden(t, "subtask_workspace_note_nested", prompt.SubtaskWorkspaceNote("/data/ws/abc/subtask-1"))
	assertGolden(t, "subtask_workspace_note_empty", prompt.SubtaskWorkspaceNote(""))
}
