package session_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/stretchr/testify/assert"
)

// The provider a chat turn is sent to always comes from the agent record, while
// the model used to come from the session row — a snapshot taken when the chat
// was opened. Changing the agent's model left that snapshot behind, so the
// request carried the old provider's model name to the new provider and came
// back "Invalid model".
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
