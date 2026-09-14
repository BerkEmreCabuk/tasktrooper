package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// captureHTTPLogs points the global logger at a buffer for one test, so a
// failure path can be asserted on what it LOGGED rather than only on what it
// returned to the caller.
func captureHTTPLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = previous })
	return &buf
}

// A real GitHub API rejection must not vanish into a bare "bad request"
// response: the operator needs the status code and GitHub's response body in
// the backend logs to tell "GitHub says no" apart from every other failure
// mode this endpoint can hit.
func TestLogWebhookSetupFailureRecordsTheGitHubAPIDetail(t *testing.T) {
	buf := captureHTTPLogs(t)
	id := uuid.New()
	err := errors.New("github webhook setup: create repo hook: github api: 422 Validation Failed")

	logWebhookSetupFailure(id, err)

	got := buf.String()
	if !strings.Contains(got, "422") {
		t.Errorf("log line missing GitHub status code.\n%s", got)
	}
	if !strings.Contains(got, "Validation Failed") {
		t.Errorf("log line missing GitHub response detail.\n%s", got)
	}
	if !strings.Contains(got, id.String()) {
		t.Errorf("log line missing repository id.\n%s", got)
	}
}

// erroringRepositoryStore fails Get with a caller-chosen error, standing in
// for a real GitHub API rejection (or any other SetupWebhook failure): the
// service returns it unwrapped, so this exercises the same handler path a
// genuine "GitHub says no" would.
type erroringRepositoryStore struct {
	port.RepositoryStore
	err error
}

func (s *erroringRepositoryStore) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return domain.Repository{}, s.err
}

// The click that started this task: SetupRepositoryWebhook must not swallow a
// real backend failure into a bare 400 with no trace of what GitHub said. The
// handler has to log the detail server-side AND still answer the caller with
// the reason (not a generic message), which is what badRequest(c, err.Error())
// already gives it — this pins both halves through the actual route.
func TestSetupRepositoryWebhookLogsAndReportsARealFailure(t *testing.T) {
	buf := captureHTTPLogs(t)
	wantErr := errors.New("github webhook setup: create repo hook: github api: 422 Validation Failed")
	svc := repository.NewService(&erroringRepositoryStore{err: wantErr}, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	app := fiber.New()
	app.Post("/v1/repositories/:id/webhook", (&Handler{repositorySvc: svc}).SetupRepositoryWebhook)

	id := uuid.New()
	resp, err := app.Test(httptest.NewRequest("POST", "/v1/repositories/"+id.String()+"/webhook", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusBadRequest)
	}

	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(body.Error.Message, "422 Validation Failed") {
		t.Errorf("response message = %q, want it to name the reason, not a generic error", body.Error.Message)
	}

	logged := buf.String()
	if !strings.Contains(logged, id.String()) {
		t.Errorf("log line missing repository id.\n%s", logged)
	}
	if !strings.Contains(logged, "422 Validation Failed") {
		t.Errorf("log line missing the GitHub API detail.\n%s", logged)
	}
}

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// TestVerifyGitHubSignature pins the fail-closed contract of the webhook's
// only authentication step: the exact GitHub header format passes, and every
// degraded form — wrong secret, tampered body, missing header, missing
// secret, wrong prefix — is rejected.
func TestVerifyGitHubSignature(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	secret := "s3cret"

	if !verifyGitHubSignature(secret, body, signBody(secret, body)) {
		t.Fatal("valid signature must verify")
	}
	if verifyGitHubSignature(secret, body, signBody("wrong", body)) {
		t.Fatal("signature from another secret must fail")
	}
	if verifyGitHubSignature(secret, []byte(`{"ref":"refs/heads/evil"}`), signBody(secret, body)) {
		t.Fatal("tampered body must fail")
	}
	if verifyGitHubSignature(secret, body, "") {
		t.Fatal("missing header must fail")
	}
	if verifyGitHubSignature("", body, signBody("", body)) {
		t.Fatal("missing stored secret must fail even with a matching signature")
	}
	raw := signBody(secret, body)
	if verifyGitHubSignature(secret, body, raw[len("sha256="):]) {
		t.Fatal("hex without the sha256= prefix must fail")
	}
}

// --- reading a workflow event ----------------------------------------------

// workflow_run and check_suite carry the same three facts under DIFFERENT keys,
// and one struct reads both. That is the whole reason these two accessors exist,
// and getting them wrong is silent: a delivery whose head SHA reads as "" is
// discarded as "no head sha in payload", which looks exactly like the push-only
// hook the board was stuck behind in the first place.
func TestWorkflowPayloadReadsBothEventShapes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		payload    githubWorkflowPayload
		wantSHA    string
		wantStatus string
	}{
		{
			name: "workflow_run",
			payload: githubWorkflowPayload{
				Action: "completed",
				WorkflowRun: struct {
					HeadSHA    string `json:"head_sha"`
					Status     string `json:"status"`
					Conclusion string `json:"conclusion"`
				}{HeadSHA: "abc123", Status: "completed", Conclusion: "failure"},
			},
			wantSHA:    "abc123",
			wantStatus: "completed",
		},
		{
			name: "check_suite",
			payload: githubWorkflowPayload{
				Action: "completed",
				CheckSuite: struct {
					HeadSHA    string `json:"head_sha"`
					Status     string `json:"status"`
					Conclusion string `json:"conclusion"`
				}{HeadSHA: "def456", Status: "completed", Conclusion: "success"},
			},
			wantSHA:    "def456",
			wantStatus: "completed",
		},
		{
			name:       "neither",
			payload:    githubWorkflowPayload{Action: "requested"},
			wantSHA:    "",
			wantStatus: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.payload.headSHA(); got != tc.wantSHA {
				t.Errorf("headSHA() = %q, want %q", got, tc.wantSHA)
			}
			if got := tc.payload.status(); got != tc.wantStatus {
				t.Errorf("status() = %q, want %q", got, tc.wantStatus)
			}
		})
	}
}

// The real GitHub bodies, unmarshalled. Pins the JSON tags rather than the Go
// literals above: a renamed tag would leave every delivery looking empty.
func TestWorkflowPayloadUnmarshalsRealDeliveries(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "workflow_run",
			body: `{"action":"completed","repository":{"full_name":"acme/widgets"},
			        "workflow_run":{"head_sha":"cafebabe","status":"completed","conclusion":"failure"}}`,
		},
		{
			name: "check_suite",
			body: `{"action":"completed","repository":{"full_name":"acme/widgets"},
			        "check_suite":{"head_sha":"cafebabe","status":"completed","conclusion":"failure"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p githubWorkflowPayload
			if err := json.Unmarshal([]byte(tc.body), &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if p.Repository.FullName != "acme/widgets" {
				t.Errorf("full_name = %q", p.Repository.FullName)
			}
			if p.headSHA() != "cafebabe" {
				t.Errorf("head sha = %q, want cafebabe", p.headSHA())
			}
			if p.Action != "completed" || p.status() != "completed" {
				t.Errorf("action=%q status=%q, want completed/completed", p.Action, p.status())
			}
		})
	}
}
