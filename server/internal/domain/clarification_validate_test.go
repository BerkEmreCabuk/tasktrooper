package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateClarificationQuestion_ChoiceWithOther(t *testing.T) {
	q := domain.ClarificationQuestion{
		ID:     "sections",
		Prompt: "Which sections?",
		Options: []domain.ClarificationOption{
			{ID: "home", Label: "Home"},
			{ID: "other", Label: "Other"},
		},
	}
	require.NoError(t, domain.ValidateClarificationQuestion(q))
}

func TestValidateClarificationQuestion_ChoiceWithoutOther(t *testing.T) {
	q := domain.ClarificationQuestion{
		ID:     "sections",
		Prompt: "Which sections?",
		Options: []domain.ClarificationOption{
			{ID: "home", Label: "Home"},
			{ID: "about", Label: "About"},
		},
	}
	assert.Error(t, domain.ValidateClarificationQuestion(q))
}

func TestNormalizeClarificationQuestion_AppendsOther(t *testing.T) {
	q := domain.ClarificationQuestion{
		ID:     "sections",
		Prompt: "Which sections?",
		Options: []domain.ClarificationOption{
			{ID: "home", Label: "Home"},
			{ID: "about", Label: "About"},
		},
	}
	normalized := domain.NormalizeClarificationQuestion(q)
	require.NoError(t, domain.ValidateClarificationQuestion(normalized))
	assert.Equal(t, "other", normalized.Options[len(normalized.Options)-1].ID)
}

func TestValidateClarificationQuestion_TextMode(t *testing.T) {
	q := domain.ClarificationQuestion{
		ID:     "url",
		Prompt: "URL?",
		Options: []domain.ClarificationOption{
			{ID: "free_text", Label: "Answer"},
			{ID: "skip", Label: "Skip"},
		},
	}
	require.NoError(t, domain.ValidateClarificationQuestion(q))
}

func TestValidateClarificationQuestion_AllowMultipleField(t *testing.T) {
	q := domain.ClarificationQuestion{
		ID:            "sections",
		Prompt:        "Which sections?",
		AllowMultiple: true,
		Options: []domain.ClarificationOption{
			{ID: "home", Label: "Home"},
			{ID: "other", Label: "Other"},
		},
	}
	require.NoError(t, domain.ValidateClarificationQuestion(q))
	assert.True(t, q.AllowMultiple)
}
