package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeChatExecutor stands in for the local Claude Code CLI. It records the
// request it was given — which is the whole point of these tests: the bug was
// never that the executor misbehaved, it was that it was never called.
type fakeChatExecutor struct {
	supports bool
	got      domain.ChatExecution
	calls    int
	result   domain.ChatResult
	err      error
	// stream, when set, is played into the caller's ChatStream before returning,
	// so the streaming contract can be asserted without a subprocess.
	stream func(port.ChatStream)
}

func (f *fakeChatExecutor) Supports(p domain.LLMProviderType) bool {
	return f.supports && p == domain.LLMProviderClaudeCode
}

func (f *fakeChatExecutor) ExecuteChat(_ context.Context, req domain.ChatExecution, out port.ChatStream) (domain.ChatResult, error) {
	f.calls++
	f.got = req
	if f.stream != nil {
		f.stream(out)
	}
	return f.result, f.err
}

// recordingSessionStore is the chat row. Only the two methods this path uses do
// anything; the rest satisfy the interface.
type recordingSessionStore struct {
	port.SessionStore
	cliSessionID string
	writes       int
	err          error
}

func (s *recordingSessionStore) UpdateCLISessionID(_ context.Context, _ uuid.UUID, id string) error {
	s.writes++
	if s.err != nil {
		return s.err
	}
	s.cliSessionID = id
	return nil
}

func chatSession() domain.Session {
	return domain.Session{ID: uuid.New(), WorkspaceDir: "/tmp/repo"}
}

func runTurn(
	t *testing.T,
	svc *session.Service,
	sess domain.Session,
	out port.ChatStream,
) (domain.AgentResponse, error) {
	t.Helper()
	return svc.RunHostExecutedTurnForTest(
		context.Background(), sess,
		domain.LLMProviderClaudeCode, "opus", domain.ToolPolicy{AllowTools: []string{"read_file"}},
		"/tmp/repo",
		[]domain.Message{
			{Role: domain.RoleSystem, Content: "You are the backend developer."},
			{Role: domain.RoleUser, Content: "Where are the routes?"},
		},
		"Where are the routes?", "en", out,
	)
}

// The bug, stated as a test: a chat turn for an agent on a host-executed
// provider must reach the executor, carrying the workspace, the policy and the
// model — never the HTTP LLM client, which has no base URL for this provider and
// built the empty string as its endpoint.
func TestChatTurnGoesToTheExecutorNotTheHTTPClient(t *testing.T) {
	executor := &fakeChatExecutor{
		supports: true,
		result: domain.ChatResult{
			Response:     domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "In handler.go."}},
			CLISessionID: "cli-1",
		},
	}
	store := &recordingSessionStore{}
	svc := session.NewHostExecutedServiceForTest(store, executor)

	resp, err := runTurn(t, svc, chatSession(), port.ChatStream{})
	require.NoError(t, err)

	assert.Equal(t, 1, executor.calls)
	assert.Equal(t, "In handler.go.", resp.Message.Content)
	assert.Equal(t, "/tmp/repo", executor.got.WorkDir)
	assert.Equal(t, "opus", executor.got.Model)
	assert.Equal(t, domain.LLMProviderClaudeCode, executor.got.Provider)
	assert.Equal(t, []string{"read_file"}, executor.got.Policy.AllowTools,
		"the CLI session is served the same tools the loop would have enforced")
	assert.Empty(t, executor.got.ResumeSessionID, "a fresh chat has nothing to resume")
}

// Continuity across turns is what makes this a conversation rather than a series
// of unrelated questions: the id is recorded on the chat row, and the next turn
// hands it back.
func TestChatRecordsTheCLISessionAndResumesItOnTheNextTurn(t *testing.T) {
	executor := &fakeChatExecutor{
		supports: true,
		result: domain.ChatResult{
			Response:     domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "In handler.go."}},
			CLISessionID: "cli-77",
		},
	}
	store := &recordingSessionStore{}
	svc := session.NewHostExecutedServiceForTest(store, executor)

	sess := chatSession()
	_, err := runTurn(t, svc, sess, port.ChatStream{})
	require.NoError(t, err)
	assert.Equal(t, "cli-77", store.cliSessionID, "the id is the conversation's continuity, and only the caller can persist it")

	// The second turn: the row now carries the id, so it must be resumed and the
	// user's new message must be sent on its own.
	sess.CLISessionID = store.cliSessionID
	_, err = runTurn(t, svc, sess, port.ChatStream{})
	require.NoError(t, err)

	assert.Equal(t, 2, executor.calls)
	assert.Equal(t, "cli-77", executor.got.ResumeSessionID)
	assert.Equal(t, "Where are the routes?", executor.got.Prompt,
		"a resumed session is sent only what is new; it already holds the rest")
	assert.Equal(t, 1, store.writes, "an unchanged id is not rewritten on every turn")
}

// A turn that failed still leaves a live CLI session behind, and that session
// holds everything said so far. Losing its id would silently restart the
// conversation — and re-pay for it — on the next message.
func TestChatRecordsTheCLISessionEvenWhenTheTurnFailed(t *testing.T) {
	executor := &fakeChatExecutor{
		supports: true,
		result:   domain.ChatResult{CLISessionID: "cli-parked"},
		err:      errors.New("claude code ended without a result"),
	}
	store := &recordingSessionStore{}
	svc := session.NewHostExecutedServiceForTest(store, executor)

	_, err := runTurn(t, svc, chatSession(), port.ChatStream{})
	require.Error(t, err)
	assert.Equal(t, "cli-parked", store.cliSessionID)
}

// A spent subscription is not a park here — there is no card and no sweeper —
// and it is not a generic failure either. It has to become a sentence the person
// who just pressed enter can act on, and the fact that makes it actionable is
// when the quota comes back.
func TestQuotaBlockBecomesAnActionableChatMessage(t *testing.T) {
	resumeAt := time.Now().Add(90 * time.Minute)
	executor := &fakeChatExecutor{
		supports: true,
		result:   domain.ChatResult{CLISessionID: "cli-limited"},
		err:      &domain.QuotaBlock{ResumeAt: resumeAt, CLISessionID: "cli-limited", Detail: "Claude AI usage limit reached"},
	}
	svc := session.NewHostExecutedServiceForTest(&recordingSessionStore{}, executor)

	_, err := runTurn(t, svc, chatSession(), port.ChatStream{})
	require.Error(t, err)

	// Still recognisable as the typed condition, so the transport can render it
	// as a calm warning rather than a red failure.
	block, ok := domain.QuotaBlockOf(err)
	require.True(t, ok)
	assert.Equal(t, "cli-limited", block.CLISessionID)

	// And already a sentence, in the reader's own local time — the transport
	// runs after the request context is gone and must not have to look up a
	// language on an error path.
	assert.Contains(t, err.Error(), "usage limit")
	assert.Contains(t, err.Error(), resumeAt.Local().Format("15:04"),
		"a limit with no time attached is not actionable")
	assert.NotContains(t, err.Error(), "unsupported protocol scheme")
}

// The Turkish rendering exists because this is a user-facing sentence and the
// tenant's language is already loaded where it is built.
func TestQuotaMessageFollowsTheTenantLanguage(t *testing.T) {
	block := &domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour)}

	tr := domain.NewQuotaNotice(block, "tr").Error()
	assert.Contains(t, tr, "Claude Code kullanım limiti doldu")
	assert.Contains(t, tr, "civarında yenilenecek")

	en := domain.NewQuotaNotice(block, "en").Error()
	assert.Contains(t, en, "Claude Code usage limit is spent")
}

// A transcript is read back by the user AND replayed to the model, so a quota
// notice must not be stored as "**Error:** ..." — it is an account condition
// with a time on it, not a fault in the run.
func TestQuotaNoticeIsWrittenToTheTranscriptAsAWarning(t *testing.T) {
	store := &capturingStore{}
	notice := domain.NewQuotaNotice(&domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour)}, "en")

	session.AppendAssistantErrorForTest(context.Background(), store, uuid.New(), notice)

	require.Len(t, store.appended, 1)
	assert.True(t, strings.HasPrefix(store.appended[0], domain.RateLimitNoticePrefix),
		"the client styles the bubble off this prefix; without it the user sees a red failure")
	assert.NotContains(t, store.appended[0], "**Error:**")
}

// The guard, from the chat side: on a host with no CLI — every cloud pod — the
// turn must fail with the sentence that names the actual problem, not fall
// through to a client that will build an empty URL.
func TestChatWithoutAnExecutorFailsWithOneClearSentence(t *testing.T) {
	for name, executor := range map[string]port.ChatExecutor{
		"no executor registered at all":  nil,
		"an executor for another engine": &fakeChatExecutor{supports: false},
	} {
		t.Run(name, func(t *testing.T) {
			svc := session.NewHostExecutedServiceForTest(&recordingSessionStore{}, executor)

			_, err := runTurn(t, svc, chatSession(), port.ChatStream{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "local runner host")
			assert.NotContains(t, err.Error(), "unsupported protocol scheme",
				"the empty-URL failure is exactly what this branch exists to prevent")
		})
	}
}

// A chat with nowhere to run says so. Falling back to the server's own working
// directory would point the CLI's file and shell tools at the server's source
// tree.
func TestChatRequiresAWorkspace(t *testing.T) {
	executor := &fakeChatExecutor{supports: true}
	svc := session.NewHostExecutedServiceForTest(&recordingSessionStore{}, executor)

	_, err := svc.RunHostExecutedTurnForTest(
		context.Background(), chatSession(),
		domain.LLMProviderClaudeCode, "opus", domain.ToolPolicy{},
		"   ", nil, "hi", "en", port.ChatStream{},
	)
	require.Error(t, err)
	assert.Equal(t, 0, executor.calls, "nothing is spawned without a directory to spawn it in")
	assert.Contains(t, err.Error(), "workspace")
}

// The streamed text reaches the caller's callback exactly as the agent loop's
// does — same callback, same ordering, same segment boundary — so the SSE
// transcript needs no knowledge of which engine answered.
func TestChatStreamsThroughTheCallersCallback(t *testing.T) {
	var got []string
	executor := &fakeChatExecutor{
		supports: true,
		result:   domain.ChatResult{Response: domain.AgentResponse{Message: domain.Message{Content: "final"}}},
		stream: func(out port.ChatStream) {
			out.Text("thinking out loud")
			out.SegmentBreak()
			out.Text("final")
		},
	}
	svc := session.NewHostExecutedServiceForTest(&recordingSessionStore{}, executor)

	_, err := runTurn(t, svc, chatSession(), port.ChatStream{
		OnText:         func(s string) { got = append(got, "t:"+s) },
		OnSegmentBreak: func() { got = append(got, "break") },
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"t:thinking out loud", "break", "t:final"}, got)
}

// capturingStore records what was written into the transcript.
type capturingStore struct {
	port.SessionStore
	appended []string
}

func (s *capturingStore) AppendMessage(_ context.Context, _ uuid.UUID, _ domain.Role, content string, _, _ []byte) (domain.SessionMessage, error) {
	s.appended = append(s.appended, content)
	return domain.SessionMessage{}, nil
}
