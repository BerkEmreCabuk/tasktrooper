package agent_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type FindingsDigestSuite struct {
	suite.Suite
}

func TestFindingsDigestSuite(t *testing.T) {
	suite.Run(t, new(FindingsDigestSuite))
}

func call(id, name, args string) domain.Message {
	return domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{
		ID: id, Type: "function", Function: domain.FunctionCall{Name: name, Arguments: args},
	}}}
}

func toolResult(id, name, content string) domain.Message {
	return domain.Message{Role: domain.RoleTool, ToolCallID: id, Name: name, Content: content}
}

func workedHistory() []domain.Message {
	return []domain.Message{
		{Role: domain.RoleUser, Content: "make the verify loop carry context"},
		call("1", "grep_code", `{"pattern":"verifyAndFix","path":"internal"}`),
		toolResult("1", "grep_code", "internal/application/board/verify.go:45"),
		call("2", "read_file", `{"path":"internal/application/board/verify.go"}`),
		toolResult("2", "read_file", "package board"),
		call("3", "codebase_search", `{"query":"where the fix round rebuilds history"}`),
		toolResult("3", "codebase_search", "verify.go"),
		call("4", "write_file", `{"path":"internal/application/agent/digest.go","content":"package agent"}`),
		toolResult("4", "write_file", "written"),
		call("5", "edit_file", `{"path":"internal/application/board/verify.go","old_string":"a","new_string":"b"}`),
		toolResult("5", "edit_file", "edited"),
		call("6", "run_terminal", `{"command":"go build ./..."}`),
		toolResult("6", "run_terminal", "exit error: exit status 1\noutput:\nverify.go:110: undefined: withFindingsDigest"),
		{Role: domain.RoleAssistant, Content: "Wrote the digest builder; the build still fails on one undefined symbol."},
	}
}

func (s *FindingsDigestSuite) TestNamesFilesSearchesAndCommands() {
	digest := agent.DigestFromMessages(workedHistory(), "", 0)

	s.Require().NotEmpty(digest)
	s.True(agent.IsFindingsDigest(digest), "the digest must be recognisable so a later round can replace it")

	s.Contains(digest, "Files changed:")
	s.Contains(digest, "internal/application/agent/digest.go (write_file)")
	s.Contains(digest, "internal/application/board/verify.go (edit_file)")
	s.Contains(digest, "Files read: internal/application/board/verify.go")
	s.Contains(digest, `"verifyAndFix" in internal (grep_code)`)
	s.Contains(digest, `"where the fix round rebuilds history" (codebase_search)`)
	s.Contains(digest, "`go build ./...` → exit status 1")
	s.Contains(digest, "Outcome: Wrote the digest builder")
}

func (s *FindingsDigestSuite) TestCommandKeepsNewestOutcome() {
	history := []domain.Message{
		call("1", "run_terminal", `{"command":"go test ./..."}`),
		toolResult("1", "run_terminal", "exit error: exit status 2\noutput:\nFAIL"),
		call("2", "run_terminal", `{"command":"go test ./..."}`),
		toolResult("2", "run_terminal", "ok  	github.com/x/y	0.4s"),
	}

	digest := agent.DigestFromMessages(history, "done", 0)

	s.Contains(digest, "`go test ./...` → ok (×2, 1 failed)")
}

func (s *FindingsDigestSuite) TestRepeatedEditsOnOneFileCollapse() {
	history := []domain.Message{
		call("1", "write_file", `{"path":"a.go","content":"x"}`),
		toolResult("1", "write_file", "written"),
		call("2", "edit_file", `{"path":"a.go","old_string":"x","new_string":"y"}`),
		toolResult("2", "edit_file", "edited"),
		call("3", "edit_file", `{"path":"a.go","old_string":"y","new_string":"z"}`),
		toolResult("3", "edit_file", "edited"),
	}

	digest := agent.DigestFromMessages(history, "", 0)

	s.Contains(digest, "Files changed: a.go (write_file, edit_file ×3)")
}

func (s *FindingsDigestSuite) TestFromActivitySteps() {
	steps := []domain.SessionStep{
		step(s.T(), "tool_call_start", map[string]string{
			"tool": "read_file", "call_id": "a", "arguments": `{"path":"cmd/main.go"}`,
		}),
		step(s.T(), "tool_call_result", map[string]any{
			"tool": "read_file", "call_id": "a", "content": "package main", "is_error": false,
		}),
		step(s.T(), "tool_call_start", map[string]string{
			"tool": "run_terminal", "call_id": "b", "arguments": `{"command":"go vet ./..."}`,
		}),
		step(s.T(), "tool_call_result", map[string]any{
			"tool": "run_terminal", "call_id": "b", "content": "exit error: exit status 2\noutput:\nvet: bad", "is_error": true,
		}),
		step(s.T(), "assistant_message", map[string]string{"content": "vet is still unhappy"}),
	}

	digest := agent.DigestFromSteps(steps, "", 0)

	s.Contains(digest, "Files read: cmd/main.go")
	s.Contains(digest, "`go vet ./...` → exit status 2")
	s.Contains(digest, "Outcome: vet is still unhappy")
}

func (s *FindingsDigestSuite) TestOrphanResultStepSurvives() {
	steps := []domain.SessionStep{
		step(s.T(), "tool_call_result", map[string]any{
			"tool": "run_terminal", "call_id": "gone", "content": "exit status 0, no output.", "is_error": false,
		}),
	}

	digest := agent.DigestFromSteps(steps, "", 0)

	s.Contains(digest, "Commands:")
	s.Contains(digest, "run_terminal")
}

func (s *FindingsDigestSuite) TestRespectsCharCapAndDropsOldestFirst() {
	var history []domain.Message
	for i := range 40 {
		id := fmt.Sprintf("r%d", i)
		history = append(history,
			call(id, "read_file", fmt.Sprintf(`{"path":"internal/pkg/file%02d.go"}`, i)),
			toolResult(id, "read_file", "contents"),
		)
	}

	const cap = 600
	digest := agent.DigestFromMessages(history, "read a lot of files", cap)

	s.LessOrEqual(len(digest), cap, "the digest rides on every retry and must stay bounded")
	s.Contains(digest, "file39.go")
	s.NotContains(digest, "file00.go")
	s.Contains(digest, "(+", "truncation must admit that it dropped something")
	s.Contains(digest, "Outcome: read a lot of files")
}

func (s *FindingsDigestSuite) TestTruncationKeepsChangedFilesLongest() {
	history := []domain.Message{
		call("w", "write_file", `{"path":"kept.go","content":"x"}`),
		toolResult("w", "write_file", "written"),
	}
	for i := range 30 {
		id := fmt.Sprintf("s%d", i)
		history = append(history,
			call(id, "codebase_search", fmt.Sprintf(`{"query":"some fairly long question number %02d about the code"}`, i)),
			toolResult(id, "codebase_search", "hits"),
		)
	}

	digest := agent.DigestFromMessages(history, "", 400)

	s.LessOrEqual(len(digest), 400)
	s.Contains(digest, "kept.go (write_file)")
}

func (s *FindingsDigestSuite) TestOversizedSummaryIsCut() {
	digest := agent.DigestFromMessages(nil, strings.Repeat("uzun özet ", 500), 300)

	s.LessOrEqual(len(digest), 300)
	s.True(agent.IsFindingsDigest(digest))
}

func (s *FindingsDigestSuite) TestEmptyRunRendersNothing() {
	s.Empty(agent.DigestFromMessages(nil, "", 0))
	s.Empty(agent.DigestFromSteps(nil, "   ", 0))
	s.Empty(agent.DigestFromMessages([]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, "", 0))
	s.False(agent.IsFindingsDigest("an ordinary system message"))
}

func (s *FindingsDigestSuite) TestUnparseableArgumentsStillCount() {
	history := []domain.Message{
		call("1", "write_file", "not json at all"),
		toolResult("1", "write_file", "written"),
	}

	digest := agent.DigestFromMessages(history, "", 0)

	s.Contains(digest, "Files changed:")
	s.Contains(digest, "write_file")
}

func step(t *testing.T, kind string, payload any) domain.SessionStep {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal step payload: %v", err)
	}
	return domain.SessionStep{StepType: kind, Payload: data}
}
