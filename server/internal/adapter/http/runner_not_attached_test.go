package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// The board parks on a missing Mac and a person does not. These tests pin the
// second half: a request somebody is watching must fail at once with the code
// the web and iOS clients switch on, because the only thing that resolves it is
// that person opening a laptop — and a spinner, a 500, or a 400 reading "bad
// query" all hide the one instruction that ends the wait.

// runnerNotAttachedApp mounts one route that returns err through report.
func runnerNotAttachedApp(t *testing.T, actor string, err error, report func(*fiber.Ctx, error) error) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Get("/probe", func(c *fiber.Ctx) error {
		c.SetUserContext(tenant.With(c.UserContext(), tenant.Identity{
			TenantID: uuid.New(), Role: tenant.RoleMember, UserID: actor,
		}))
		return report(c, err)
	})
	return app
}

func probe(t *testing.T, app *fiber.App, lang string) (int, runnerNotAttachedResponse, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	res, err := app.Test(req, -1)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	var body runnerNotAttachedResponse
	_ = json.Unmarshal(raw, &body)
	return res.StatusCode, body, string(raw)
}

// TestAMissingMacIsA409NotA500 is the whole point: this is a state with an
// instruction attached, not a server fault, and the clients have a screen for
// it that only a 409 with this code opens.
func TestAMissingMacIsA409NotA500(t *testing.T) {
	block := &domain.RunnerBlock{MemberUID: "member-1", Detail: "no runner session for this member"}
	app := runnerNotAttachedApp(t, "member-1", block, internalError)

	status, body, raw := probe(t, app, "")
	require.Equal(t, fiber.StatusConflict, status, "body was %s", raw)
	require.Equal(t, "runner_not_attached", body.Code, "the clients switch on the top-level code")
	require.Equal(t, "runner_not_attached", body.Error.Type, "and on error.type, so both must carry it")
	require.NotEmpty(t, body.Error.Message)
}

// TestNoRetryAfterOnAMissingMac: the control plane deliberately omits it, and
// so must this. Nothing about a closed laptop becomes true by waiting, so a
// client that backed off and retried would show a spinner exactly where the
// "connect your Mac" screen belongs.
func TestNoRetryAfterOnAMissingMac(t *testing.T) {
	app := runnerNotAttachedApp(t, "member-1", &domain.RunnerBlock{MemberUID: "member-1"}, internalError)
	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/probe", nil), -1)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Empty(t, res.Header.Get("Retry-After"))
}

// TestSelfDistinguishesYourMacFromAColleaguesc is the team wording. "Open your
// laptop" and "this card belongs to somebody whose machine is offline" are
// different sentences and only one of them is an instruction to the reader.
func TestSelfDistinguishesYourMacFromAColleagues(t *testing.T) {
	own := &domain.RunnerBlock{MemberUID: "member-1", Detail: "no session"}
	other := &domain.RunnerBlock{MemberUID: "member-2", Detail: "no session"}

	_, mine, _ := probe(t, runnerNotAttachedApp(t, "member-1", own, internalError), "")
	require.True(t, mine.Self)
	require.Equal(t, "member-1", mine.MemberUID)

	_, theirs, _ := probe(t, runnerNotAttachedApp(t, "member-1", other, internalError), "")
	require.False(t, theirs.Self, "a colleague's offline Mac must not tell this person to open theirs")
	require.Equal(t, "member-2", theirs.MemberUID)
}

// TestTheEmbeddingSentinelIsAlwaysSelf: that call is made AS the acting member
// — the tunnel session belongs to one person — so there is nobody else it could
// have been, and the body says so rather than leaving the client to guess.
func TestTheEmbeddingSentinelIsAlwaysSelf(t *testing.T) {
	app := runnerNotAttachedApp(t, "member-1", domain.ErrEmbeddingRunnerNotAttached(), internalError)
	status, body, raw := probe(t, app, "")
	require.Equal(t, fiber.StatusConflict, status, "body was %s", raw)
	require.Equal(t, "runner_not_attached", body.Code)
	require.True(t, body.Self)
	require.Empty(t, body.MemberUID, "the embedding path names no member because it needs none")
}

// TestAWrappedBlockIsStillRecognised: the condition is raised in the transport
// and reported several layers up, so it arrives wrapped. A check that only
// matched the bare value would work in a test and fail in production.
func TestAWrappedBlockIsStillRecognised(t *testing.T) {
	wrapped := fmt.Errorf("preparing the task workspace: %w",
		fmt.Errorf("reaching the assignee's Mac: %w", &domain.RunnerBlock{MemberUID: "member-9"}))
	status, body, _ := probe(t, runnerNotAttachedApp(t, "member-1", wrapped, internalError), "")
	require.Equal(t, fiber.StatusConflict, status)
	require.Equal(t, "member-9", body.MemberUID)
	require.False(t, body.Self)
}

// TestNotReadyIsTheSame409: to a person watching a spinner, "connected but
// still starting" and "not connected" are one instruction — look at that
// machine — and the sentence already says which it is.
func TestNotReadyIsTheSame409(t *testing.T) {
	block := &domain.RunnerBlock{MemberUID: "member-1", Detail: "the supervisor has not reported yet", NotReady: true}
	status, body, _ := probe(t, runnerNotAttachedApp(t, "member-1", block, internalError), "")
	require.Equal(t, fiber.StatusConflict, status)
	require.Equal(t, "runner_not_attached", body.Code)
	require.Contains(t, body.Error.Message, "not ready")
}

// TestCodeSearchAnswers409NotBadRequest: answering a query means embedding it,
// and the only embedder is on the acting member's laptop. A 400 there reads as
// "your query was wrong", which sends the person to rewrite a search that was
// fine.
func TestCodeSearchAnswers409NotBadRequest(t *testing.T) {
	app := runnerNotAttachedApp(t, "member-1", domain.ErrEmbeddingRunnerNotAttached(), badRequestErr)
	status, body, raw := probe(t, app, "")
	require.Equal(t, fiber.StatusConflict, status, "body was %s", raw)
	require.Equal(t, "runner_not_attached", body.Code)
}

// TestOrdinaryFailuresAreUntouched. The check runs in front of every handler's
// error path, so it must recognise nothing else: a genuine 500 reported as a
// 409 would send a person to their laptop for a database outage.
func TestOrdinaryFailuresAreUntouched(t *testing.T) {
	app := runnerNotAttachedApp(t, "member-1", errors.New("connection refused"), internalError)
	status, body, _ := probe(t, app, "")
	require.Equal(t, fiber.StatusInternalServerError, status)
	require.Empty(t, body.Code)

	bad := runnerNotAttachedApp(t, "member-1", errors.New("topK must be positive"), badRequestErr)
	status, body, _ = probe(t, bad, "")
	require.Equal(t, fiber.StatusBadRequest, status)
	require.Empty(t, body.Code)
}

// TestTheSentenceFollowsAcceptLanguage: the same reader sees the quota park's
// Turkish, so this one is Turkish too or the two states read as two products.
func TestTheSentenceFollowsAcceptLanguage(t *testing.T) {
	block := &domain.RunnerBlock{MemberUID: "member-1"}
	_, tr, _ := probe(t, runnerNotAttachedApp(t, "member-1", block, internalError), "tr-TR")
	require.Contains(t, tr.Error.Message, "Mac")
	require.Equal(t, block.UserMessage("tr"), tr.Error.Message)

	_, en, _ := probe(t, runnerNotAttachedApp(t, "member-1", block, internalError), "en-GB")
	require.Equal(t, block.UserMessage(""), en.Error.Message)
}

// Connecting the CLI is a third route onto this path, and the newest.
//
// On a cloud deployment the connect probe asks the member's Mac what its
// environment report found, so "there is no Mac" now reaches agentCLIError —
// which must not turn it into a 500, and must not turn it into one of the two
// CLI sentinels either. Those send a person to an installer or a login; this
// one sends them to a laptop, and only a 409 with this code opens that screen.
func TestAgentCLIConnectReportsAMissingMacAsSuch(t *testing.T) {
	block := &domain.RunnerBlock{MemberUID: "member-1", Detail: "no runner session for this member"}
	app := runnerNotAttachedApp(t, "member-1", block, agentCLIError)

	status, body, raw := probe(t, app, "")
	require.Equal(t, fiber.StatusConflict, status, "body was %s", raw)
	require.Equal(t, "runner_not_attached", body.Code)
	require.True(t, body.Self, "the caller's own Mac: connect is always about the machine in front of them")
}

// The same for a Mac that is attached and still starting. It resolves itself in
// seconds, and telling that user their CLI is missing would send them to
// reinstall software that is already there.
func TestAgentCLIConnectReportsANotReadyMacAsSuch(t *testing.T) {
	block := &domain.RunnerBlock{Detail: "the supervisor has not pushed a preflight yet", NotReady: true}
	app := runnerNotAttachedApp(t, "member-1", block, agentCLIError)

	status, body, raw := probe(t, app, "")
	require.Equal(t, fiber.StatusConflict, status, "body was %s", raw)
	require.Equal(t, "runner_not_attached", body.Code)
	require.Contains(t, body.Error.Message, "not ready yet")
}

// The two CLI sentinels still get their own 400 and their own type, unchanged:
// that is what lets a client show "install it" and "sign in" as the different
// instructions they are.
func TestAgentCLIErrorKeepsTheTwoSentinelsApart(t *testing.T) {
	for kind, sentinel := range map[string]error{
		"agent_cli_binary_missing":  domain.ErrAgentCLIBinaryMissing,
		"agent_cli_unauthenticated": domain.ErrAgentCLIUnauthenticated,
	} {
		app := runnerNotAttachedApp(t, "member-1", fmt.Errorf("the Mac says so: %w", sentinel), agentCLIError)
		status, body, raw := probe(t, app, "")
		require.Equal(t, fiber.StatusBadRequest, status, "body was %s", raw)
		require.Equal(t, kind, body.Error.Type)
		require.Contains(t, body.Error.Message, "the Mac says so")
	}
}
