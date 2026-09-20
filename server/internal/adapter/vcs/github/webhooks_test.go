package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEnsureRepoPushWebhookCreates: no hook for the target URL yet → POST a
// new push hook carrying the secret, return GitHub's hook id.
func TestEnsureRepoPushWebhookCreates(t *testing.T) {
	var created map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks"):
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 7, "config": map[string]any{"url": "https://elsewhere.example/hook"}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/hooks"):
			json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 42})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	id, err := ensureRepoWebhookAt(context.Background(), srv.URL, "tok", "o", "r", "https://bridge.example/v1/github/webhook", "sec")
	if err != nil {
		t.Fatalf("ensureRepoWebhookAt: %v", err)
	}
	if id != 42 {
		t.Fatalf("hook id = %d, want 42", id)
	}
	cfg, _ := created["config"].(map[string]any)
	if cfg["url"] != "https://bridge.example/v1/github/webhook" || cfg["secret"] != "sec" || cfg["content_type"] != "json" {
		t.Fatalf("created config = %v", cfg)
	}
	if !hasWebhookEvents(created["events"]) {
		t.Fatalf("created events = %v, want all of %v", created["events"], WebhookEvents)
	}
}

// hasWebhookEvents reports whether a decoded JSON events list carries every
// event in WebhookEvents.
func hasWebhookEvents(raw any) bool {
	list, _ := raw.([]any)
	got := map[string]bool{}
	for _, e := range list {
		if s, ok := e.(string); ok {
			got[s] = true
		}
	}
	for _, want := range WebhookEvents {
		if !got[want] {
			return false
		}
	}
	return true
}

// TestEnsureRepoPushWebhookUpdatesExisting: a hook already points at the
// target URL → PATCH it in place (rotating the secret), no duplicate POST.
func TestEnsureRepoPushWebhookUpdatesExisting(t *testing.T) {
	var patched map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks"):
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 9, "config": map[string]any{"url": "https://bridge.example/v1/github/webhook"}},
			})
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/hooks/9"):
			json.NewDecoder(r.Body).Decode(&patched)
			json.NewEncoder(w).Encode(map[string]any{"id": 9})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	id, err := ensureRepoWebhookAt(context.Background(), srv.URL, "tok", "o", "r", "https://bridge.example/v1/github/webhook", "rotated")
	if err != nil {
		t.Fatalf("ensureRepoWebhookAt: %v", err)
	}
	if id != 9 {
		t.Fatalf("hook id = %d, want 9", id)
	}
	cfg, _ := patched["config"].(map[string]any)
	if cfg["secret"] != "rotated" {
		t.Fatalf("patched config must rotate the secret, got %v", cfg)
	}
}

// --- repairing the hooks that already exist --------------------------------

// The repair this whole change hinges on. Every repository registered before the
// Actions events were added carries a hook subscribed to `push` and NOTHING
// else, which is why a green CI run had no way of reaching the board and three
// cards sat wedged in code_review. Reconciliation widens that hook in place.
//
// The two things asserted beyond "it PATCHed" are the two that make it safe to
// run on every boot: the secret is NOT touched (no `config` key in the body, so
// no rotation window where in-flight deliveries fail their signature), and the
// existing events are kept rather than replaced.
func TestReconcileWidensAPushOnlyHook(t *testing.T) {
	var patched map[string]any
	patches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks"):
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"id": 9, "active": true, "events": []string{"push"},
					"config": map[string]any{"url": "https://bridge.example/v1/github/webhook"},
				},
			})
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/hooks/9"):
			patches++
			json.NewDecoder(r.Body).Decode(&patched)
			json.NewEncoder(w).Encode(map[string]any{"id": 9})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	id, changed, err := reconcileRepoWebhookEventsAt(context.Background(), srv.URL, "tok", "o", "r",
		"https://bridge.example/v1/github/webhook")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if id != 9 || !changed {
		t.Fatalf("id=%d changed=%v, want 9/true — a push-only hook must be repaired", id, changed)
	}
	if patches != 1 {
		t.Fatalf("%d PATCHes, want exactly 1", patches)
	}
	if !hasWebhookEvents(patched["events"]) {
		t.Fatalf("patched events = %v, want all of %v — workflow_run is the event the board needs",
			patched["events"], WebhookEvents)
	}
	if _, rotated := patched["config"]; rotated {
		t.Fatal("the repair sent a config block: it would rotate the secret on every boot, " +
			"and each rotation drops the deliveries signed with the old one")
	}
	if patched["active"] != true {
		t.Fatalf("the repair must also re-activate a disabled hook, got active=%v", patched["active"])
	}
}

// Converged means converged: a hook that already carries every event we need
// costs one GET and no write at all. This is what makes the boot-time pass safe
// to run forever.
func TestReconcileIsANoOpOnAnAlreadyCorrectHook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks"):
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"id": 9, "active": true, "events": WebhookEvents,
					"config": map[string]any{"url": "https://bridge.example/v1/github/webhook"},
				},
			})
		default:
			t.Errorf("a converged hook must not be written to: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	id, changed, err := reconcileRepoWebhookEventsAt(context.Background(), srv.URL, "tok", "o", "r",
		"https://bridge.example/v1/github/webhook")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if id != 9 || changed {
		t.Fatalf("id=%d changed=%v, want 9/false", id, changed)
	}
}

// An operator who added an event of their own did so on purpose. Reconciling to
// the exact WebhookEvents list would silently delete it, which is worse than
// never running.
func TestReconcileKeepsEventsSomebodyElseAdded(t *testing.T) {
	var patched map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks"):
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"id": 9, "active": true, "events": []string{"push", "issues"},
					"config": map[string]any{"url": "https://bridge.example/v1/github/webhook"},
				},
			})
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/hooks/9"):
			json.NewDecoder(r.Body).Decode(&patched)
			json.NewEncoder(w).Encode(map[string]any{"id": 9})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	if _, changed, err := reconcileRepoWebhookEventsAt(context.Background(), srv.URL, "tok", "o", "r",
		"https://bridge.example/v1/github/webhook"); err != nil || !changed {
		t.Fatalf("reconcile: changed=%v err=%v", changed, err)
	}
	if !hasWebhookEvents(patched["events"]) {
		t.Fatalf("events = %v, want ours included", patched["events"])
	}
	list, _ := patched["events"].([]any)
	found := false
	for _, e := range list {
		if e == "issues" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reconciliation deleted an event the operator added: %v", patched["events"])
	}
}

// A wildcard hook already receives everything. Rewriting it to an explicit list
// would NARROW it, which is the one way this repair could break a working setup.
func TestReconcileLeavesAWildcardHookAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks"):
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"id": 9, "active": true, "events": []string{"*"},
					"config": map[string]any{"url": "https://bridge.example/v1/github/webhook"},
				},
			})
		default:
			t.Errorf("a wildcard hook must not be narrowed: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	if _, changed, err := reconcileRepoWebhookEventsAt(context.Background(), srv.URL, "tok", "o", "r",
		"https://bridge.example/v1/github/webhook"); err != nil || changed {
		t.Fatalf("changed=%v err=%v, want false/nil", changed, err)
	}
}

// No hook for our URL: installing one needs a secret this function does not
// have, so it reports "nothing here" and leaves it to EnsureRepoWebhook.
func TestReconcileReportsNoHookRatherThanInventingOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks") {
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 7, "active": true, "events": []string{"push"},
					"config": map[string]any{"url": "https://elsewhere.example/hook"}},
			})
			return
		}
		t.Errorf("must not write anything: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	id, changed, err := reconcileRepoWebhookEventsAt(context.Background(), srv.URL, "tok", "o", "r",
		"https://bridge.example/v1/github/webhook")
	if err != nil || id != 0 || changed {
		t.Fatalf("id=%d changed=%v err=%v, want 0/false/nil", id, changed, err)
	}
}

// --- what "CI cannot run" means -------------------------------------------

// The classifier the gate hangs on. A 402 is the user's actual situation
// (Actions minutes exhausted); a 403 counts only when it names a permanent
// cause, because reading a rate limit as an exhausted account would open the
// review gate on a build that was about to succeed.
func TestIsCIUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		msg    string
		want   bool
	}{
		{"payment required", 402, "You have exceeded your included quota", true},
		{"actions disabled", 403, "Actions is disabled for this repository", true},
		{"spending limit", 403, "The job was not started because the spending limit was reached", true},
		{"upgrade required", 403, "Please upgrade your plan to run Actions", true},
		{"secondary rate limit", 403, "You have exceeded a secondary rate limit", false},
		{"plain forbidden", 403, "Resource not accessible by integration", false},
		{"not found", 404, "Not Found", false},
		{"server error", 500, "Internal Server Error", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCIUnavailable(NewAPIErrorForTest(tc.status, tc.msg)); got != tc.want {
				t.Fatalf("IsCIUnavailable(%d %q) = %v, want %v", tc.status, tc.msg, got, tc.want)
			}
		})
	}
	if IsCIUnavailable(context.Canceled) {
		t.Error("a non-API error must never be read as an exhausted CI account")
	}
}

// The same question asked of TEXT, which is all a deploy failure leaves behind:
// the dispatch error is written onto the pipeline job as a string, and the
// board reads that string when it decides whether a failed deploy is the
// developer's problem or the account's.
func TestIsCIUnavailableText(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{"dispatched 402", "dispatch failed: github api: 402 You have exceeded your included quota", true},
		{"actions disabled", "dispatch failed: github api: 403 Actions is disabled for this repository", true},
		{"spending limit", "dispatch failed: github api: 403 recent account payments have failed, the spending limit was reached", true},
		{"secondary rate limit", "dispatch failed: github api: 403 You have exceeded a secondary rate limit", false},
		{"a genuinely failed deploy", "deploy: Error: connection refused while applying the manifest", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCIUnavailableText(tc.text); got != tc.want {
				t.Fatalf("IsCIUnavailableText(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
