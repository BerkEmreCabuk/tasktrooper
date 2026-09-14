package agent_test

import (
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateToolOutputKeepsWithinBudget(t *testing.T) {
	content := strings.Repeat("a", 200)

	out := agent.TruncateToolOutputForTest(content, 100)

	assert.Len(t, out, 100+len(agent.ToolOutputTruncateMarkerForTest())+1)
	assert.Contains(t, out, "omitted")
}

func TestTruncateToolOutputLeavesShortOutputAlone(t *testing.T) {
	content := "build succeeded"

	assert.Equal(t, content, agent.TruncateToolOutputForTest(content, 100))
	assert.Equal(t, content, agent.TruncateToolOutputForTest(content, 0))
}

func TestTruncateToolOutputKeepsTheErrorAtTheEnd(t *testing.T) {
	noise := strings.Repeat("ok  \tgithub.com/example/pkg\t0.01s\n", 2000)
	content := noise + "FAIL\tgithub.com/example/broken [build failed]\nmain.go:12: undefined: doThing\n"

	out := agent.TruncateToolOutputForTest(content, 4000)

	require.Less(t, len(out), len(content), "output must actually have been truncated")
	assert.Contains(t, out, "undefined: doThing", "the error at the end must survive truncation")
}

func TestTruncateToolOutputKeepsSomeOfTheHead(t *testing.T) {
	content := "$ go build ./...\n" + strings.Repeat("x", 20000) + "\nfinal line\n"

	out := agent.TruncateToolOutputForTest(content, 4000)

	assert.Contains(t, out, "$ go build ./...")
	assert.Contains(t, out, "final line")
}

func TestTruncateToolOutputNeverSplitsARune(t *testing.T) {
	content := strings.Repeat("ö", 500) // two bytes per rune

	for budget := 90; budget < 110; budget++ {
		out := agent.TruncateToolOutputForTest(content, budget)
		assert.True(t, utf8ValidString(out), "budget %d produced invalid UTF-8", budget)
	}
}

func utf8ValidString(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
