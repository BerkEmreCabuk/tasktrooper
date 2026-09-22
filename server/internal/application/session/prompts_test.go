package session_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type PromptsSuite struct {
	suite.Suite
}

func (s *PromptsSuite) TestPrependWorkspacePrompt_TurkishLocale() {
	history := []domain.Message{{Role: domain.RoleUser, Content: "merhaba"}}
	result := session.PrependWorkspacePromptForTest(history, "/data/ws/abc", "tr")
	s.Require().Len(result, 6)
	s.Equal(domain.RoleSystem, result[0].Role)
	s.Contains(result[0].Content, "INTERNAL")
	s.Contains(result[0].Content, "/data/ws/abc")
	s.Equal(domain.RoleSystem, result[1].Role)
	s.Equal(prompt.UserFacingGuidance(), result[1].Content)
	s.Equal(domain.RoleSystem, result[2].Role)
	s.Equal(prompt.LanguageInstruction("tr"), result[2].Content)
	s.Equal(domain.RoleSystem, result[3].Role)
	s.Equal(prompt.ToolSelectionGuidance(), result[3].Content)

	s.Equal(domain.RoleSystem, result[4].Role)
	s.Equal(prompt.RepeatCallGuidance(), result[4].Content)
	s.Equal("merhaba", result[5].Content)
}

func (s *PromptsSuite) TestPrependWorkspacePrompt_EnglishLocale() {
	result := session.PrependWorkspacePromptForTest(nil, "/data/ws/abc", "en")
	s.Require().Len(result, 5)
	s.Contains(result[0].Content, "INTERNAL")
	s.Equal(prompt.UserFacingGuidance(), result[1].Content)
	s.Equal(prompt.LanguageInstruction("en"), result[2].Content)
	s.Equal(prompt.ToolSelectionGuidance(), result[3].Content)
	s.Equal(prompt.RepeatCallGuidance(), result[4].Content)
}

func TestPromptsSuite(t *testing.T) {
	suite.Run(t, new(PromptsSuite))
}
