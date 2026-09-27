package orchestrator_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
)

// TestParseErrorsPromptGoldenBeforeMove pins the exact error text
// parseGoalIntake and parsePlannerJSON build today — text that reaches the
// model unchanged through pipelineCorrection's ErrorText — before it moves
// into catalog/system/guards/orchestrator_parse_*.md.
func TestParseErrorsPromptGoldenBeforeMove(t *testing.T) {
	t.Run("intake", func(t *testing.T) {
		cases := []struct {
			name, content, want string
		}{
			{
				"invalidJSON",
				`not json`,
				"invalid intake json: json: invalid character o as null",
			},
			{
				"missingConstraints",
				`{"ready":true,"purpose":"p","goal":"g","questions":[]}`,
				"intake missing constraints field",
			},
			{
				"missingQuestions",
				`{"ready":true,"purpose":"p","goal":"g","constraints":[]}`,
				"intake missing questions field",
			},
			{
				"missingPurpose",
				`{"ready":true,"purpose":"","goal":"g","constraints":[],"questions":[]}`,
				"intake missing purpose",
			},
			{
				"missingGoal",
				`{"ready":true,"purpose":"p","goal":"","constraints":[],"questions":[]}`,
				"intake missing goal",
			},
			{
				"readyRequiresEmptyQuestions",
				`{"ready":true,"purpose":"p","goal":"g","constraints":[],"questions":[{"id":"q1","prompt":"x","options":[{"id":"a","label":"A"},{"id":"other","label":"Other"}]}]}`,
				"intake ready=true requires empty questions",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := orchestrator.ParseGoalIntakeForTest(tc.content)
				if err == nil || err.Error() != tc.want {
					t.Errorf("got %v, want %q", err, tc.want)
				}
			})
		}
	})

	t.Run("planner", func(t *testing.T) {
		cases := []struct {
			name, content, want string
		}{
			{
				"invalidJSON",
				`not json`,
				"invalid planner json: json: invalid character o as null",
			},
			{
				"missingQuestions",
				`{"ready":false,"purpose":"","goal":""}`,
				"planner output missing questions field",
			},
			{
				"taskMissingToolNames",
				`{"ready":true,"questions":[],"tasks":[{"id":"t1","title":"T","description":"D","agent_id":"a1"}]}`,
				"task t1 missing tool_names field",
			},
			{
				"taskInvalidToolNames",
				`{"ready":true,"questions":[],"tasks":[{"id":"t1","title":"T","description":"D","agent_id":"a1","tool_names":123}]}`,
				"task t1 invalid tool_names: json: cannot unmarshal number into Go value of type []string",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := orchestrator.ParsePlannerOutputForTest(tc.content)
				if err == nil || err.Error() != tc.want {
					t.Errorf("got %v, want %q", err, tc.want)
				}
			})
		}
	})
}
