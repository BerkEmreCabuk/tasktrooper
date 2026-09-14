package mcpserver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestMintIssuesADistinctTokenPerRun(t *testing.T) {
	tokens := NewRunTokenRegistry()

	seen := make(map[string]bool)
	for range 64 {
		token, err := tokens.Mint(Run{Ctx: context.Background()})
		require.NoError(t, err)
		assert.False(t, seen[token], "a reused token would let one run act as another")
		assert.GreaterOrEqual(t, len(token), 40, "32 random bytes, base64url-encoded")
		seen[token] = true
	}
	assert.Equal(t, 64, tokens.Live())
}

func TestLookupResolvesTheRunAndRevokeEndsIt(t *testing.T) {
	tokens := NewRunTokenRegistry()
	policy := domain.ToolPolicy{AllowTools: []string{"move_board_task"}}
	ctx := context.WithValue(context.Background(), testKey{}, "run-ctx")

	token, err := tokens.Mint(Run{Ctx: ctx, Policy: policy, TaskKey: "tt-42"})
	require.NoError(t, err)

	run, ok := tokens.Lookup(token)
	require.True(t, ok)
	assert.Equal(t, policy, run.Policy)
	assert.Equal(t, "tt-42", run.TaskKey)
	assert.Equal(t, "run-ctx", run.Ctx.Value(testKey{}))

	tokens.Revoke(token)
	_, ok = tokens.Lookup(token)
	assert.False(t, ok, "a token has to die with the run that minted it")
	assert.Equal(t, 0, tokens.Live())

	// Revoking twice is what a defer on a path that already released does.
	tokens.Revoke(token)
	tokens.Revoke("")
}

func TestLookupRejectsTheEmptyToken(t *testing.T) {
	tokens := NewRunTokenRegistry()
	_, ok := tokens.Lookup("")
	assert.False(t, ok, "an empty Authorization header must not resolve to anything")
}

// Without the run's context every tool call would land on a background context
// with no workspace, no task and no ledger — served, but unattributable.
func TestMintRefusesARunWithoutAContext(t *testing.T) {
	_, err := NewRunTokenRegistry().Mint(Run{TaskKey: "tt-42"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

type testKey struct{}
