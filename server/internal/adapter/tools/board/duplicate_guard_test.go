package board

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestTitleSimilarity_CatchesTheObservedCollision(t *testing.T) {
	// The two tasks a single plan actually produced for one request.
	score := titleSimilarity(
		"Implement Google Play Store Link on Acme Website",
		"Add Google Play Store Link to Acme Website",
	)

	assert.GreaterOrEqual(t, score, duplicateTitleThreshold,
		"differently-phrased titles for the same work must be caught")
}

func TestTitleSimilarity_KeepsDistinctWorkApart(t *testing.T) {
	cases := []struct{ a, b string }{
		{"Add Google Play link to footer", "Add App Store link to footer"},
		{"Add task export endpoint", "Fix checkout price rounding"},
		{"Implement login page", "Implement signup page"},
	}
	for _, c := range cases {
		assert.Less(t, titleSimilarity(c.a, c.b), duplicateTitleThreshold,
			"%q and %q are different work", c.a, c.b)
	}
}

func TestTitleSimilarity_IdenticalAndEmpty(t *testing.T) {
	assert.Equal(t, 1.0, titleSimilarity("Add Google Play link", "add GOOGLE play LINK"))
	assert.Equal(t, 0.0, titleSimilarity("", "Add Google Play link"))
	// A title made only of stop words has no meaningful tokens.
	assert.Equal(t, 0.0, titleSimilarity("to the a", "Add Google Play link"))
}

func TestIsOpenColumn_TerminalColumnsDoNotBlock(t *testing.T) {
	assert.False(t, isOpenColumn(domain.TaskColumnDone))
	assert.False(t, isOpenColumn(domain.TaskColumnReleased))
	assert.True(t, isOpenColumn(domain.TaskColumnBacklog))
	assert.True(t, isOpenColumn(domain.TaskColumnTodo))
	assert.True(t, isOpenColumn(domain.TaskColumnInProgress))
}

func TestTitleTokens_DropsStopWordsAndPunctuation(t *testing.T) {
	got := titleTokens("Add the Google-Play link, to Acme!")

	assert.Equal(t, map[string]bool{
		"add": true, "google": true, "play": true, "link": true, "acme": true,
	}, got)
}
