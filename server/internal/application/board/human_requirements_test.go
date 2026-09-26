package board

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestHumanRequirementsMessageCarriesOnlyTheHumansComments(t *testing.T) {
	at := time.Date(2026, 9, 27, 1, 10, 0, 0, time.UTC)
	comments := []domain.TaskComment{
		{AuthorType: "agent", Content: "Scope violation: the Runtimes marquee is out of scope."},
		{AuthorType: "user", Content: "Show the logos in the runtimes marquee too.", CreatedAt: at},
		{AuthorType: "system", Content: prompt.ClarificationAnswerComment("Which screens?", "Both")},
	}

	msg := humanRequirementsMessage(comments)

	assert.Contains(t, msg, "- 2026-09-27 01:10Z Show the logos in the runtimes marquee too.")
	assert.Contains(t, msg, "the comment wins")
	assert.Contains(t, msg, "never flag it as scope creep")
	assert.NotContains(t, msg, "Scope violation")
	assert.NotContains(t, msg, "Which screens?")
}

func TestHumanRequirementsMessageCollapsesADoubleSubmit(t *testing.T) {
	comments := []domain.TaskComment{
		{AuthorType: "user", Content: "Logos in the marquee too.\n"},
		{AuthorType: "user", Content: "Logos in the marquee too."},
	}

	assert.Equal(t, 1, strings.Count(humanRequirementsMessage(comments), "Logos in the marquee too."))
}

func TestHumanRequirementsMessageKeepsTheNewest(t *testing.T) {
	comments := make([]domain.TaskComment, 0, humanRequirementsLimit+2)
	for i := range humanRequirementsLimit + 2 {
		comments = append(comments, domain.TaskComment{AuthorType: "user", Content: fmt.Sprintf("requirement #%02d", i)})
	}

	msg := humanRequirementsMessage(comments)

	assert.NotContains(t, msg, "requirement #00")
	assert.NotContains(t, msg, "requirement #01")
	assert.Contains(t, msg, fmt.Sprintf("requirement #%02d", humanRequirementsLimit+1))
}

func TestHumanRequirementsMessageEmptyWithoutHumanComments(t *testing.T) {
	assert.Empty(t, humanRequirementsMessage(nil))
	assert.Empty(t, humanRequirementsMessage([]domain.TaskComment{{AuthorType: "agent", Content: "done"}, {AuthorType: "user", Content: "  "}}))
}
