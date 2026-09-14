package context

import (
	gocontext "context"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type BudgetSuite struct {
	suite.Suite
}

func TestBudgetSuite(t *testing.T) {
	suite.Run(t, new(BudgetSuite))
}

func (s *BudgetSuite) TestApplyUnderBudgetUnchanged() {
	b := Budget{MaxTokens: 100, ReserveOutput: 10}
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: "sys"},
		{Role: domain.RoleUser, Content: "hello"},
	}
	result := b.Apply(messages)
	s.Equal(messages, result)
}

func (s *BudgetSuite) TestApplyRemovesOldToolResultsBeforeOtherMessages() {
	b := Budget{
		MaxTokens:          30,
		ReserveOutput:      0,
		KeepRecentMessages: 2,
	}
	oldTool := domain.Message{Role: domain.RoleTool, Content: repeat("t", 160), ToolCallID: "1", Name: "run_terminal"}
	oldUser := domain.Message{Role: domain.RoleUser, Content: repeat("u", 160)}
	recentAssistant := domain.Message{Role: domain.RoleAssistant, Content: "ok"}
	lastUser := domain.Message{Role: domain.RoleUser, Content: "final"}

	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: "sys"},
		oldTool,
		oldUser,
		recentAssistant,
		lastUser,
	}

	result := b.Apply(messages)
	s.LessOrEqual(CountTokens(result), b.TokenLimit())
	s.NotContains(result, oldTool)
	s.NotContains(result, oldUser)
	s.Contains(result, recentAssistant)
	s.Contains(result, lastUser)
	s.Equal(domain.RoleSystem, result[0].Role)
}

func (s *BudgetSuite) TestApplyPreservesSystemMessages() {
	b := Budget{MaxTokens: 10, ReserveOutput: 0, KeepRecentMessages: 1}
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: repeat("s", 400)},
		{Role: domain.RoleUser, Content: repeat("a", 400)},
		{Role: domain.RoleUser, Content: "keep"},
	}
	result := b.Apply(messages)
	s.Len(result, 2)
	s.Equal(domain.RoleSystem, result[0].Role)
	s.Equal("keep", result[1].Content)
}

func (s *BudgetSuite) TestApplyPreservesLastUserMessage() {
	b := Budget{MaxTokens: 15, ReserveOutput: 0, KeepRecentMessages: 1}
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: repeat("old", 200)},
		{Role: domain.RoleAssistant, Content: repeat("mid", 200)},
		{Role: domain.RoleUser, Content: "last-user"},
	}
	result := b.Apply(messages)
	s.Contains(result, domain.Message{Role: domain.RoleUser, Content: "last-user"})
}

func (s *BudgetSuite) TestApplyKeepsRecentMessages() {
	b := Budget{MaxTokens: 20, ReserveOutput: 0, KeepRecentMessages: 3}
	recent1 := domain.Message{Role: domain.RoleAssistant, Content: repeat("r1", 40)}
	recent2 := domain.Message{Role: domain.RoleTool, Content: repeat("r2", 40), ToolCallID: "2"}
	recent3 := domain.Message{Role: domain.RoleUser, Content: "recent-last"}
	old := domain.Message{Role: domain.RoleUser, Content: repeat("old", 200)}

	messages := []domain.Message{old, recent1, recent2, recent3}
	result := b.Apply(messages)
	s.NotContains(result, old)
	s.Contains(result, recent1)
	s.Contains(result, recent2)
	s.Contains(result, recent3)
}

func (s *BudgetSuite) TestApplyTrimPriorityTable() {
	cases := []struct {
		name       string
		maxTokens  int
		keepRecent int
		messages   []domain.Message
		mustKeep   []domain.Message
		mustRemove []domain.Message
	}{
		{
			name:       "tool before assistant",
			maxTokens:  25,
			keepRecent: 2,
			messages: []domain.Message{
				{Role: domain.RoleTool, Content: repeat("tool", 120), ToolCallID: "a"},
				{Role: domain.RoleAssistant, Content: repeat("asst", 120)},
				{Role: domain.RoleUser, Content: "u1"},
				{Role: domain.RoleUser, Content: "u2"},
			},
			mustKeep:   []domain.Message{{Role: domain.RoleUser, Content: "u2"}},
			mustRemove: []domain.Message{{Role: domain.RoleTool, Content: repeat("tool", 120), ToolCallID: "a"}},
		},
		{
			name:       "system always kept",
			maxTokens:  12,
			keepRecent: 1,
			messages: []domain.Message{
				{Role: domain.RoleSystem, Content: "rules"},
				{Role: domain.RoleUser, Content: repeat("x", 200)},
				{Role: domain.RoleUser, Content: "last"},
			},
			mustKeep: []domain.Message{
				{Role: domain.RoleSystem, Content: "rules"},
				{Role: domain.RoleUser, Content: "last"},
			},
			mustRemove: []domain.Message{{Role: domain.RoleUser, Content: repeat("x", 200)}},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			b := Budget{MaxTokens: tc.maxTokens, KeepRecentMessages: tc.keepRecent}
			result := b.Apply(tc.messages)
			s.LessOrEqual(CountTokens(result), b.TokenLimit())
			for _, keep := range tc.mustKeep {
				s.Contains(result, keep)
			}
			for _, remove := range tc.mustRemove {
				s.NotContains(result, remove)
			}
		})
	}
}

func (s *BudgetSuite) TestCountTokensApproximation() {
	s.Equal(0, CountTokens(nil))
	s.Equal(1, CountTokens([]domain.Message{{Role: domain.RoleUser, Content: "abcd"}}))
	s.Equal(2, CountTokens([]domain.Message{{Role: domain.RoleUser, Content: repeat("x", 5)}}))
}

func (s *BudgetSuite) TestCountToolTokensEmpty() {
	s.Equal(0, CountToolTokens(nil))
	s.Equal(0, CountToolTokens([]domain.ToolDefinition{}))
}

func (s *BudgetSuite) TestCountToolTokensCountsARealDefinitionSet() {
	tools := []domain.ToolDefinition{
		{
			Type: "function",
			Function: domain.FunctionDefinition{
				Name:        "read_file",
				Description: "Reads a file from the workspace and returns its contents as text.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{"type": "string", "description": "workspace-relative path"},
					},
					"required": []interface{}{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: domain.FunctionDefinition{
				Name:        "grep_code",
				Description: "Searches the codebase for a pattern and returns matching lines with file and line number.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"pattern": map[string]interface{}{"type": "string"},
						"path":    map[string]interface{}{"type": "string"},
					},
					"required": []interface{}{"pattern"},
				},
			},
		},
	}
	s.Greater(CountToolTokens(tools), 0)
}

// A bigger definition must cost more tokens: the count has to track the
// actual JSON size, not just the number of definitions.
func (s *BudgetSuite) TestCountToolTokensGrowsWithDefinitionSize() {
	small := []domain.ToolDefinition{{Type: "function", Function: domain.FunctionDefinition{Name: "a", Description: "x"}}}
	big := []domain.ToolDefinition{{Type: "function", Function: domain.FunctionDefinition{Name: "a", Description: repeat("x", 4000)}}}
	s.Greater(CountToolTokens(big), CountToolTokens(small))
}

func (s *BudgetSuite) TestSummarizeRollingNoOpBelowThreshold() {
	b := Budget{SummarizeThreshold: 1000, KeepRecentMessages: 2}
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "hello"},
		{Role: domain.RoleAssistant, Content: "hi"},
	}
	result, err := SummarizeRolling(gocontext.Background(), b, NoOpSummarizer{}, messages, "model")
	s.NoError(err)
	s.Equal(messages, result)
}

func (s *BudgetSuite) TestNoOpSummarizerReturnsEmpty() {
	summary, err := NoOpSummarizer{}.Summarize(gocontext.Background(), []domain.Message{
		{Role: domain.RoleUser, Content: "test"},
	}, "model")
	s.NoError(err)
	s.Empty(summary)
}

func repeat(char string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(char, n)
}
