package github

import (
	"context"
	"fmt"
	"net/url"
	"sort"
)

// repoHook mirrors the fields we need from GitHub's hook resource.
type repoHook struct {
	ID     int64    `json:"id"`
	Active bool     `json:"active"`
	Events []string `json:"events"`
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

// WebhookEvents is the event set every TaskTrooper repository hook subscribes
// to. Adding to it is cheap (GitHub fans out to the same endpoint); each entry
// has to earn its place, because every delivery costs a signature verification
// and a repository lookup on a public, unauthenticated path.
//
//	push         — the original and still the reason the hook exists: a push to
//	               the default branch reindexes the repository and refreshes the
//	               project profile.
//	workflow_run — the one the board actually needed and never had. It carries
//	               head_sha, status and conclusion for an Actions run, which is
//	               precisely what task_pipelines stores, and it fires for every
//	               run regardless of whether a pull request exists. Without it
//	               the code-review gate had NO event source at all: the hook was
//	               registered for push only, so a green CI run reached the board
//	               through nothing but the in-process poll, and a poll that dies
//	               with its pod takes the card with it.
//	check_suite  — a wake signal, not a result. Actions is not the only thing
//	               that can post checks (any GitHub App does), and a check_suite
//	               completion carries head_sha, so it tells the board "go resolve
//	               this commit now" for CI the Actions API would not have shown.
//	               It is handled identically to workflow_run and resolves through
//	               the same path, so a repository that emits both simply gets the
//	               answer twice, which is idempotent.
//
// pull_request is deliberately ABSENT. The board opens and merges its own pull
// requests (EnsurePullRequest, taskpr_merge), so almost every delivery would be
// an echo of something this system just did, and nothing in the pipeline gate
// reads PR state — the gate is about run conclusions on a head commit. It would
// multiply deliveries without adding a signal.
var WebhookEvents = []string{"push", "workflow_run", "check_suite"}

// EnsureRepoWebhook installs (or refreshes) the webhook that points the repo at
// targetURL, and returns the hook's GitHub id. If a hook for the same target
// URL already exists it is updated in place instead of duplicated, which also
// rotates its secret to the one supplied and converges its event list onto
// WebhookEvents — so calling this again is always safe and always leaves GitHub
// agreeing with both the secret we stored and the events we need.
func EnsureRepoWebhook(ctx context.Context, token, owner, repo, targetURL, secret string) (int64, error) {
	return ensureRepoWebhookAt(ctx, "", token, owner, repo, targetURL, secret)
}

// EnsureRepoWebhookAt is EnsureRepoWebhook against a caller-chosen API base
// instead of github.com. It exists so the caller (repository.Service) can
// point SetupWebhook's happy path at a fake GitHub server in tests — the real
// install/rotate logic has no other way to be exercised end to end without a
// live GitHub token.
func EnsureRepoWebhookAt(ctx context.Context, base, token, owner, repo, targetURL, secret string) (int64, error) {
	return ensureRepoWebhookAt(ctx, base, token, owner, repo, targetURL, secret)
}

func ensureRepoWebhookAt(ctx context.Context, base, token, owner, repo, targetURL, secret string) (int64, error) {
	hooksPath := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/hooks"

	var existing []repoHook
	if err := doJSONAt(ctx, base, token, "GET", hooksPath+"?per_page=100", nil, &existing); err != nil {
		return 0, fmt.Errorf("list repo hooks: %w", err)
	}

	config := map[string]any{
		"url":          targetURL,
		"content_type": "json",
		"secret":       secret,
		"insecure_ssl": "0",
	}
	for _, h := range existing {
		if h.Config.URL != targetURL {
			continue
		}
		var updated repoHook
		body := map[string]any{"active": true, "events": WebhookEvents, "config": config}
		if err := doJSONAt(ctx, base, token, "PATCH", fmt.Sprintf("%s/%d", hooksPath, h.ID), body, &updated); err != nil {
			return 0, fmt.Errorf("update repo hook: %w", err)
		}
		return updated.ID, nil
	}

	var created repoHook
	body := map[string]any{
		"name":   "web",
		"active": true,
		"events": WebhookEvents,
		"config": config,
	}
	if err := doJSONAt(ctx, base, token, "POST", hooksPath, body, &created); err != nil {
		return 0, fmt.Errorf("create repo hook: %w", err)
	}
	return created.ID, nil
}

// ReconcileRepoWebhookEvents brings an EXISTING hook's event list up to
// WebhookEvents without touching its secret, and reports whether it had to
// change anything.
//
// This is the repair path, and it is separate from EnsureRepoWebhook for one
// reason: every repository registered before this change has a hook subscribed
// to `push` and nothing else, and the only way to fix them through
// EnsureRepoWebhook is to rotate the secret on every one of them at every boot.
// A rotation is atomic on our side but not on GitHub's — there is a window in
// which in-flight deliveries are signed with the old secret and rejected — and
// paying that cost repeatedly, forever, to fix a list of strings is the wrong
// trade. So this PATCHes `events` alone, and sends nothing else.
//
// It converges: a hook already carrying the right events is left completely
// untouched (no request at all), so this is safe to run on every boot. A repo
// with no hook for our URL returns (0, false, nil) — installing one is
// EnsureRepoWebhook's job, and it needs a secret this function does not have.
func ReconcileRepoWebhookEvents(ctx context.Context, token, owner, repo, targetURL string) (int64, bool, error) {
	return reconcileRepoWebhookEventsAt(ctx, "", token, owner, repo, targetURL)
}

func reconcileRepoWebhookEventsAt(ctx context.Context, base, token, owner, repo, targetURL string) (int64, bool, error) {
	hooksPath := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/hooks"

	var existing []repoHook
	if err := doJSONAt(ctx, base, token, "GET", hooksPath+"?per_page=100", nil, &existing); err != nil {
		return 0, false, fmt.Errorf("list repo hooks: %w", err)
	}
	for _, h := range existing {
		if h.Config.URL != targetURL {
			continue
		}
		if h.Active && hasAllEvents(h.Events, WebhookEvents) {
			return h.ID, false, nil
		}
		var updated repoHook
		// The union, not the replacement: an operator who added an event of
		// their own to this hook did so on purpose, and a reconciler that
		// silently deletes it is worse than one that never ran.
		body := map[string]any{"active": true, "events": unionEvents(h.Events, WebhookEvents)}
		if err := doJSONAt(ctx, base, token, "PATCH", fmt.Sprintf("%s/%d", hooksPath, h.ID), body, &updated); err != nil {
			return h.ID, false, fmt.Errorf("update repo hook events: %w", err)
		}
		return updated.ID, true, nil
	}
	return 0, false, nil
}

func hasAllEvents(have, want []string) bool {
	set := make(map[string]bool, len(have))
	for _, e := range have {
		set[e] = true
	}
	// GitHub's wildcard: a hook subscribed to everything already carries
	// everything, and rewriting it to an explicit list would NARROW it.
	if set["*"] {
		return true
	}
	for _, e := range want {
		if !set[e] {
			return false
		}
	}
	return true
}

// unionEvents merges want into have, sorted so the request body is stable and
// two reconcilers cannot flip a hook back and forth.
func unionEvents(have, want []string) []string {
	set := make(map[string]bool, len(have)+len(want))
	for _, e := range have {
		set[e] = true
	}
	for _, e := range want {
		set[e] = true
	}
	out := make([]string, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}
