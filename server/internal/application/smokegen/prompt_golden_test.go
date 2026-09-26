package smokegen

import (
	"errors"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Golden fixtures pin smokegen's LLM-facing prose exactly as it read before
// the move to catalog/system/prompts/smokegen/*.md (see the prompt library
// program). The move must keep these byte-identical.

func TestSystemPromptGolden(t *testing.T) {
	const want = `You draft post-deploy smoke checks for one component of a software project.

A smoke check is a single read-only HTTP request sent to PRODUCTION right after every deploy. If it fails, the release is rolled back. So every check must be safe to send to production at any time, and must pass whenever the deploy is healthy.

Rules:
- Only GET or HEAD.
- Only public, unauthenticated, side-effect-free endpoints: the home page, health / readiness / liveness / version / status endpoints, key public pages, public read-only API GETs, a static asset. Never anything that needs a login, a cookie, an API key or a token, and never anything that writes, sends mail, charges, enqueues work or triggers a job.
- "path" starts with "/" and is relative to the production URL. Use an absolute https URL only for a different public host this component itself serves.
- Set "expect_status" only when the code makes the status certain (for example a health handler that returns 200). Leave it out otherwise; any 2xx/3xx then passes.
- Set "contains" only for short text the code guarantees in the response body (for example a health JSON field such as "\"status\":\"ok\""). Never for HEAD. Never for content that changes (dates, counts, user data).
- Optionally set "max_latency_ms" (1-10000), e.g. 3000 for a health endpoint.
- Give each check a short human "name".
- Propose 3 to 8 checks, most valuable first. Fewer is fine when the code offers fewer safe endpoints.

Read the code before answering: the router / route definitions, the framework's conventions (file-based routes, controllers, handlers), health handlers, middleware that requires auth. Do not guess endpoints that do not exist in the code.

Reply with ONLY a JSON object, no prose, no code fences:
{"checks":[{"name":"...","method":"GET","path":"/...","expect_status":200,"contains":"...","max_latency_ms":3000}]}`
	if systemPrompt != want {
		t.Fatalf("systemPrompt =\n%q\nwant\n%q", systemPrompt, want)
	}
}

func TestUserPromptGolden(t *testing.T) {
	repo := domain.Repository{Name: "tasktrooper"}
	cases := []struct {
		name     string
		comp     domain.Component
		brief    string
		existing []domain.SmokeCheck
		want     string
	}{
		{
			name: "no brief, no existing checks, root component",
			comp: domain.Component{Path: "."},
			want: "Repository: tasktrooper. The component lives in the repository root — read the code there.\n" +
				"\nNo project brief is available; work it out from the code.\n" +
				"\nReply with only the JSON object.",
		},
		{
			name:  "with brief, non-root component, no existing checks",
			comp:  domain.Component{Path: "server"},
			brief: "Go + Postgres. Runs on: https://app.example.com",
			want: "Repository: tasktrooper. The component lives in server — read the code there.\n" +
				"\nProject brief (stack, commands, where it runs — the production URL is listed under \"Runs on\" when one is bound):\n" +
				"Go + Postgres. Runs on: https://app.example.com\n" +
				"\nReply with only the JSON object.",
		},
		{
			name:  "with brief and existing checks",
			comp:  domain.Component{Path: "desktop"},
			brief: "Electron shell.",
			existing: []domain.SmokeCheck{
				{Method: "GET", Path: "/health"},
				{Method: "HEAD", Path: "/"},
			},
			want: "Repository: tasktrooper. The component lives in desktop — read the code there.\n" +
				"\nProject brief (stack, commands, where it runs — the production URL is listed under \"Runs on\" when one is bound):\n" +
				"Electron shell.\n" +
				"\nThese checks already exist — do not propose them again:\n" +
				"- GET /health\n" +
				"- HEAD /\n" +
				"\nReply with only the JSON object.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := userPrompt(tc.comp, repo, tc.brief, tc.existing)
			if got != tc.want {
				t.Fatalf("userPrompt(...) =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestSmokeRetryMessageGolden(t *testing.T) {
	got := smokeRetryMessage(errors.New("unexpected end of JSON input"))
	want := "That was not a valid JSON object (unexpected end of JSON input). Reply again with ONLY the JSON object {\"checks\":[...]}, no prose, no code fences."
	if got != want {
		t.Fatalf("smokeRetryMessage(...) =\n%q\nwant\n%q", got, want)
	}
}
