package orchestrator_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/require"
)

// assertGolden pins fn's output against a testdata fixture. Set UPDATE_GOLDEN=1
// to (re)write the fixture from the current output — used once, before a
// prompt moves into catalog/system, to capture the byte-exact baseline the
// migration must reproduce.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden fixture %s (run with UPDATE_GOLDEN=1 to create it)", path)
	require.Equal(t, string(want), got, "golden mismatch for %s", name)
}

// --- system prompts ---

func TestGolden_IntakeSystemPrompt(t *testing.T) {
	t.Run("minimal", func(t *testing.T) {
		p := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{})
		assertGolden(t, "intake_system_minimal", p)
	})

	t.Run("rich_solo_agent", func(t *testing.T) {
		p := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{
			SoloAgentName:        "product-manager",
			SoloAgentDescription: "The PM agent plans and tracks board work.",
			Lang:                 "en",
			Workspace:            "## Workspace\n- Repository: tasktrooper (id=r1)\n",
			SoloAgentRules: []domain.OrchestratorRule{
				{Name: "board-not-chat-backlog", Content: "Backlog items live on the board, not in chat."},
			},
			SoloAgentSkills: []domain.Skill{
				{Name: "acceptance-criteria-gwt", Description: "Write AC in Given/When/Then.", Enabled: true},
				{Name: "disabled-skill", Description: "Should not render.", Enabled: false},
			},
		})
		assertGolden(t, "intake_system_rich_solo_agent", p)
	})

	t.Run("lang_tr", func(t *testing.T) {
		p := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{Lang: "tr"})
		assertGolden(t, "intake_system_lang_tr", p)
	})
}

func TestGolden_PlannerSystemPrompt(t *testing.T) {
	t.Run("minimal", func(t *testing.T) {
		p := orchestrator.BuildPlannerSystemPromptWithOptionsForTest(orchestrator.PlannerOptions{Lang: "en"})
		assertGolden(t, "planner_system_minimal", p)
	})

	t.Run("rich_catalogs_and_skills", func(t *testing.T) {
		soloID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
		p := orchestrator.BuildPlannerSystemPromptWithOptionsForTest(orchestrator.PlannerOptions{
			ConstrainedAgentID: &soloID,
			Lang:               "en",
			Workspace:          "## Workspace\n- Repository: tasktrooper (id=r1)\n",
		})
		assertGolden(t, "planner_system_rich_solo", p)
	})

	t.Run("lang_tr", func(t *testing.T) {
		p := orchestrator.BuildPlannerSystemPromptLegacyForTest(
			[]domain.Agent{{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Name: "backend-developer", SubagentType: "generalPurpose", Description: "Backend"}},
			[]domain.Skill{{Name: "go-hexagonal-architecture", Description: "Keep layers clean.", Enabled: true}},
			[]domain.OrchestratorRule{{Name: "tdd-first", Content: "Write the failing test first."}},
		)
		assertGolden(t, "planner_system_lang_tr", p)
	})
}

func TestGolden_ReplannerSystemPrompt(t *testing.T) {
	t.Run("minimal", func(t *testing.T) {
		p := orchestrator.BuildReplannerSystemPromptForTest()
		assertGolden(t, "replanner_system_minimal", p)
	})
}

func TestGolden_VerifierSystemPrompt(t *testing.T) {
	p := orchestrator.BuildVerifierSystemPromptForTest()
	assertGolden(t, "verifier_system", p)
}

func TestGolden_SynthesizePrompts(t *testing.T) {
	assertGolden(t, "synthesize_system", orchestrator.SynthesizeSystemPromptForTest())
	assertGolden(t, "synthesize_user_content", orchestrator.SynthesizeUserContentForTest("Ship the login page", "Build and QA the login page", "## t1\ndone\n\n"))
}

// --- conversation.go correction/rejection wrapper prose ---

func TestGolden_PipelineCorrection(t *testing.T) {
	err := errors.New("unexpected end of JSON input")
	msgs := orchestrator.PipelineCorrectionForTest(`{"ready": true,}`, err)
	require.Len(t, msgs, 2)
	assertGolden(t, "pipeline_correction_echo", msgs[0].Content)
	assertGolden(t, "pipeline_correction_instruction", msgs[1].Content)
}

func TestGolden_PipelineRejection(t *testing.T) {
	err := errors.New(`task t1 references unknown agent_id a1`)
	msgs := orchestrator.PipelineRejectionForTest(`{"tasks": []}`, err)
	require.Len(t, msgs, 2)
	assertGolden(t, "pipeline_rejection_echo", msgs[0].Content)
	assertGolden(t, "pipeline_rejection_instruction", msgs[1].Content)
}

// --- planner validation guard messages (validatePlannerOutput) ---

func plannerAgents() []domain.Agent {
	return []domain.Agent{{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Name: "backend-developer", Enabled: true}}
}

func TestGolden_PlannerValidationGuards(t *testing.T) {
	agentID := plannerAgents()[0].ID

	cases := []struct {
		name   string
		output domain.PlannerOutput
		prior  []domain.PlannerTask
	}{
		{
			name:   "missing_summary",
			output: domain.PlannerOutput{Ready: true, Tasks: []domain.PlannerTask{{ID: "t1", Title: "T", Description: "D", AgentID: agentID.String()}}},
		},
		{
			name:   "no_tasks",
			output: domain.PlannerOutput{Ready: true, Summary: "s"},
		},
		{
			name: "too_many_tasks",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "T1", Description: "D1", AgentID: agentID.String()},
				{ID: "t2", Title: "T2", Description: "D2", AgentID: agentID.String()},
			}},
		},
		{
			name: "task_missing_id",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{Title: "T", Description: "D", AgentID: agentID.String()},
			}},
		},
		{
			name: "task_missing_title",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Description: "D", AgentID: agentID.String()},
			}},
		},
		{
			name: "task_missing_description",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "T", AgentID: agentID.String()},
			}},
		},
		{
			name: "task_missing_agent",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "T", Description: "D"},
			}},
		},
		{
			name: "unknown_agent",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "T", Description: "D", AgentID: "99999999-9999-9999-9999-999999999999"},
			}},
		},
		{
			name: "duplicate_task_id",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "T1", Description: "D1", AgentID: agentID.String()},
				{ID: "t1", Title: "T2", Description: "D2", AgentID: agentID.String()},
			}},
		},
		{
			name:  "reused_prior_id",
			prior: []domain.PlannerTask{{ID: "t1", Title: "Old", Description: "Old work", AgentID: agentID.String()}},
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "New", Description: "New work", AgentID: agentID.String()},
			}},
		},
		{
			name:  "duplicate_title_prior",
			prior: []domain.PlannerTask{{ID: "t1", Title: "Ship it", Description: "Old work", AgentID: agentID.String()}},
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "r1", Title: "Ship it", Description: "New work", AgentID: agentID.String()},
			}},
		},
		{
			name: "duplicate_description_prior",
			prior: []domain.PlannerTask{{ID: "t1", Title: "Old title", AgentID: agentID.String(),
				Description: "This is a long enough description to trip the eighty character duplicate guard for sure."}},
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "r1", Title: "New title", AgentID: agentID.String(),
					Description: "This is a long enough description to trip the eighty character duplicate guard for sure."},
			}},
		},
		{
			name: "duplicate_description_turn",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "Title one", AgentID: agentID.String(),
					Description: "This is a long enough description to trip the eighty character duplicate guard for sure."},
				{ID: "t2", Title: "Title two", AgentID: agentID.String(),
					Description: "This is a long enough description to trip the eighty character duplicate guard for sure."},
			}},
		},
		{
			name: "invalid_skill_id",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "T", Description: "D", AgentID: agentID.String(), SkillIDs: []string{"not-a-uuid"}},
			}},
		},
		{
			name: "unknown_dependency",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "T", Description: "D", AgentID: agentID.String(), DependsOn: []string{"t9"}},
			}},
		},
		{
			name: "disjoint_writes",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "T1", Description: "D1", AgentID: agentID.String(), ToolNames: []string{"create_board_task"}, ParallelGroup: 0},
				{ID: "t2", Title: "T2", Description: "D2", AgentID: agentID.String(), ToolNames: []string{"move_board_task"}, ParallelGroup: 0},
			}},
		},
		{
			name: "bookkeeping_only",
			output: domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
				{ID: "t1", Title: "Do the work", Description: "D1", AgentID: agentID.String(), ToolNames: []string{"run_terminal"}},
				{ID: "t2", Title: "Move it to code review", Description: "D2", AgentID: agentID.String(), ToolNames: []string{"move_board_task"}},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.prior != nil {
				err = orchestrator.ValidateRepairPlanForTest(tc.output, plannerAgents(), nil, 1, tc.prior)
			} else {
				err = orchestrator.ValidatePlannerOutputForTest(tc.output, plannerAgents(), nil, 1)
			}
			require.Error(t, err)
			assertGolden(t, "planner_guard_"+tc.name, err.Error())
		})
	}

	t.Run("skill_not_owned", func(t *testing.T) {
		skillID := "33333333-3333-3333-3333-333333333333"
		output := domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
			{ID: "t1", Title: "T", Description: "D", AgentID: agentID.String(), SkillIDs: []string{skillID}},
		}}
		ownership := map[string]map[string]bool{agentID.String(): {}}
		err := orchestrator.ValidatePlannerOutputForTest(output, plannerAgents(), ownership, 1)
		require.Error(t, err)
		assertGolden(t, "planner_guard_skill_not_owned", err.Error())
	})

	t.Run("multiple_creators", func(t *testing.T) {
		output := domain.PlannerOutput{Ready: true, Summary: "s", Tasks: []domain.PlannerTask{
			{ID: "t1", Title: "T1", Description: "D1", AgentID: agentID.String(), ToolNames: []string{"create_board_task"}, ParallelGroup: 0},
			{ID: "t2", Title: "T2", Description: "D2", AgentID: agentID.String(), ToolNames: []string{"create_board_task"}, ParallelGroup: 1},
		}}
		err := orchestrator.ValidatePlannerOutputForTest(output, plannerAgents(), nil, 2)
		require.Error(t, err)
		assertGolden(t, "planner_guard_multiple_creators", err.Error())
	})
}

// --- clarification question validation prose ---

func TestGolden_ClarificationValidationGuards(t *testing.T) {
	cases := []struct {
		name      string
		questions []domain.ClarificationQuestion
	}{
		{name: "no_questions", questions: nil},
		{name: "missing_id", questions: []domain.ClarificationQuestion{{Prompt: "Which repo?"}}},
		{name: "missing_prompt", questions: []domain.ClarificationQuestion{{ID: "q1"}}},
		{name: "too_few_options", questions: []domain.ClarificationQuestion{{ID: "q1", Prompt: "Which repo?", Options: []domain.ClarificationOption{{ID: "a", Label: "A"}}}}},
		{name: "option_missing_label", questions: []domain.ClarificationQuestion{{ID: "q1", Prompt: "Which repo?", Options: []domain.ClarificationOption{{ID: "a", Label: "A"}, {ID: "b"}}}}},
		{
			name: "choice_missing_other",
			questions: []domain.ClarificationQuestion{{ID: "q1", Prompt: "Which repo?", Options: []domain.ClarificationOption{
				{ID: "a", Label: "A"}, {ID: "b", Label: "B"},
			}}},
		},
		{
			name: "choice_reserved_before_other",
			questions: []domain.ClarificationQuestion{{ID: "q1", Prompt: "Which repo?", Options: []domain.ClarificationOption{
				{ID: "a", Label: "A"}, {ID: "skip", Label: "Skip"}, {ID: "other", Label: "Other"},
			}}},
		},
		{
			name: "choice_needs_concrete_option",
			questions: []domain.ClarificationQuestion{{ID: "q1", Prompt: "Which repo?", Options: []domain.ClarificationOption{
				{ID: "other", Label: "Other"},
			}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := orchestrator.ValidateClarificationQuestionsForTest(tc.questions)
			require.Error(t, err)
			assertGolden(t, "clarification_guard_"+tc.name, err.Error())
		})
	}
}

func TestGolden_DomainClarificationValidationGuards(t *testing.T) {
	cases := []struct {
		name string
		q    domain.ClarificationQuestion
	}{
		{name: "domain_too_few_options", q: domain.ClarificationQuestion{ID: "q1", Options: []domain.ClarificationOption{{ID: "a", Label: "A"}}}},
		{name: "domain_text_mode_extra_option", q: domain.ClarificationQuestion{ID: "q1", Options: []domain.ClarificationOption{{ID: "free_text", Label: "Free text"}, {ID: "extra", Label: "Extra"}}}},
		{name: "domain_choice_missing_other", q: domain.ClarificationQuestion{ID: "q1", Options: []domain.ClarificationOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}},
		{name: "domain_choice_reserved_before_other", q: domain.ClarificationQuestion{ID: "q1", Options: []domain.ClarificationOption{{ID: "a", Label: "A"}, {ID: "skip", Label: "Skip"}, {ID: "other", Label: "Other"}}}},
		{name: "domain_choice_needs_concrete", q: domain.ClarificationQuestion{ID: "q1", Options: []domain.ClarificationOption{{ID: "other", Label: "Other"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateClarificationQuestion(tc.q)
			require.Error(t, err)
			assertGolden(t, tc.name, err.Error())
		})
	}
}

// --- executor.go prose helpers ---

func TestGolden_ClarificationBlockedReason(t *testing.T) {
	t.Run("with_question_prompt", func(t *testing.T) {
		req := domain.ClarificationRequest{Context: "need more info", Questions: []domain.ClarificationQuestion{{Prompt: "Which repository?"}}}
		assertGolden(t, "clarification_blocked_with_prompt", orchestrator.ClarificationBlockedReasonForTest(req))
	})
	t.Run("empty", func(t *testing.T) {
		assertGolden(t, "clarification_blocked_empty", orchestrator.ClarificationBlockedReasonForTest(domain.ClarificationRequest{}))
	})
}

func TestGolden_StartedNotFinishedReason(t *testing.T) {
	got := orchestrator.StartedNotFinishedReasonForTest(map[string]int{"move_board_task": 1}, []string{"move_board_task", "claim_board_task"})
	assertGolden(t, "started_not_finished_reason", got)
}

func TestGolden_BoardWriteNotLandedReason(t *testing.T) {
	got := orchestrator.BoardWriteNotLandedReasonForTest(map[string]int{}, []string{"create_board_task"})
	assertGolden(t, "board_write_not_landed_reason", got)
}

func TestGolden_PlannedSkillFocus(t *testing.T) {
	agentID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	skillID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	got := orchestrator.PlannedSkillFocusForTest([]string{skillID.String()}, []domain.Skill{{ID: skillID, AgentID: agentID, Name: "acceptance-criteria-gwt"}})
	assertGolden(t, "planned_skill_focus", got)
}

func TestGolden_PriorAttemptNote(t *testing.T) {
	t.Run("zero_value", func(t *testing.T) {
		assertGolden(t, "prior_attempt_note_zero", orchestrator.PriorAttemptNoteForTest(0, nil, agent.RunStats{}, nil, ""))
	})
	t.Run("rich", func(t *testing.T) {
		err := fmt.Errorf("tool run_terminal failed: exit status 1")
		stats := agent.RunStats{
			ByTool:    map[string]int{"run_terminal": 3, "read_file": 1},
			LastCalls: []string{"read_file", "run_terminal"},
		}
		got := orchestrator.PriorAttemptNoteForTest(1, err, stats, nil, "- created board task DE-1")
		assertGolden(t, "prior_attempt_note_rich", got)
	})
}
