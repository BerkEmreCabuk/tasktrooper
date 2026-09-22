package session_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/stretchr/testify/assert"
)

func TestResolveChatModel(t *testing.T) {
	tests := []struct {
		name       string
		reqModel   string
		sessModel  string
		agentModel string
		want       string
	}{
		{
			name:       "agent model beats the model the session was opened with",
			sessModel:  "anthropic/claude-opus-5",
			agentModel: "mistral-small-latest",
			want:       "mistral-small-latest",
		},
		{
			name:       "explicit request model still wins",
			reqModel:   "gpt-4o",
			sessModel:  "anthropic/claude-opus-5",
			agentModel: "mistral-small-latest",
			want:       "gpt-4o",
		},
		{
			name:      "session model is kept when there is no agent",
			sessModel: "local-model",
			want:      "local-model",
		},
		{
			name:       "agent model is used when the session has none",
			agentModel: "mistral-small-latest",
			want:       "mistral-small-latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := session.ResolveChatModelForTest(tt.reqModel, tt.sessModel, tt.agentModel)
			assert.Equal(t, tt.want, got)
		})
	}
}
