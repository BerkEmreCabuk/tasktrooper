package llmprovider_test

import (
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestResolveTimeoutDuration_ProviderOverride(t *testing.T) {
	got := llmprovider.ResolveTimeoutDuration(600, domain.LLMProviderLocal, 120*time.Second)
	assert.Equal(t, 600*time.Second, got)
}

func TestResolveTimeoutDuration_GlobalFallback(t *testing.T) {
	got := llmprovider.ResolveTimeoutDuration(0, domain.LLMProviderOpenAI, 90*time.Second)
	assert.Equal(t, 120*time.Second, got)
}

func TestResolveTimeoutDuration_LocalDefinitionBeatsGlobal(t *testing.T) {
	got := llmprovider.ResolveTimeoutDuration(0, domain.LLMProviderLocal, 120*time.Second)
	assert.Equal(t, 300*time.Second, got)
}

func TestResolveTimeoutDuration_DefinitionDefault(t *testing.T) {
	got := llmprovider.ResolveTimeoutDuration(0, domain.LLMProviderLocal, 0)
	assert.Equal(t, 300*time.Second, got)
}

func TestResolveTimeoutDuration_StoredBelowGlobalUsesGlobalFloor(t *testing.T) {
	got := llmprovider.ResolveTimeoutDuration(120, domain.LLMProviderLocal, 300*time.Second)
	assert.Equal(t, 300*time.Second, got)
}
