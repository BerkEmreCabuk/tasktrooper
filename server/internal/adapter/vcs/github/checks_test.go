package github

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestFailingChecksCombinesCheckRunsAndCommitStatuses(t *testing.T) {
	api := prTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /repos/acme/widget/commits/deadbeef/check-runs": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") != "1" {
				t.Errorf("page = %q, want 1", r.URL.Query().Get("page"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"check_runs": []map[string]any{
					{"name": "lint", "conclusion": "success"},
					{"name": "server tests", "conclusion": "failure"},
					{"name": "slow job", "conclusion": ""},
				},
			})
		},
		"GET /repos/acme/widget/commits/deadbeef/status": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":       "failure",
				"total_count": 2,
				"statuses": []map[string]any{
					{"state": "success", "context": "vercel"},
					{"state": "error", "context": "codecov/patch"},
				},
			})
		},
	})

	failing, err := api.FailingChecks(context.Background(), "tok", "acme", "widget", "deadbeef")
	if err != nil {
		t.Fatalf("FailingChecks: %v", err)
	}
	want := map[string]bool{"server tests": true, "codecov/patch": true}
	if len(failing) != len(want) {
		t.Fatalf("failing = %v, want exactly %v", failing, want)
	}
	for _, name := range failing {
		if !want[name] {
			t.Errorf("unexpected failing check %q", name)
		}
	}
}

func TestFailingChecksPaginatesCheckRuns(t *testing.T) {
	calls := 0
	api := prTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /repos/acme/widget/commits/deadbeef/check-runs": func(w http.ResponseWriter, r *http.Request) {
			calls++
			runs := make([]map[string]any, 0, prPageSize)
			if r.URL.Query().Get("page") == "1" {
				for i := 0; i < prPageSize; i++ {
					runs = append(runs, map[string]any{"name": "job-page1", "conclusion": "success"})
				}
			} else {
				runs = append(runs, map[string]any{"name": "job-page2", "conclusion": "cancelled"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"check_runs": runs})
		},
		"GET /repos/acme/widget/commits/deadbeef/status": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "success", "total_count": 0})
		},
	})

	failing, err := api.FailingChecks(context.Background(), "tok", "acme", "widget", "deadbeef")
	if err != nil {
		t.Fatalf("FailingChecks: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (paginated past the first full page)", calls)
	}
	if len(failing) != 1 || failing[0] != "job-page2" {
		t.Fatalf("failing = %v, want [job-page2]", failing)
	}
}

func TestBranchHeadSHAReadsTheRefObject(t *testing.T) {
	api := prTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /repos/acme/widget/git/ref/heads/main": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": map[string]any{"sha": "abc123"},
			})
		},
	})

	sha, err := api.BranchHeadSHA(context.Background(), "tok", "acme", "widget", "main")
	if err != nil {
		t.Fatalf("BranchHeadSHA: %v", err)
	}
	if sha != "abc123" {
		t.Errorf("sha = %q, want abc123", sha)
	}
}

func TestBranchHeadSHAEscapesSlashesInTheBranchName(t *testing.T) {
	api := prTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /repos/acme/widget/git/ref/heads/release/2026-09": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": map[string]any{"sha": "def456"},
			})
		},
	})

	sha, err := api.BranchHeadSHA(context.Background(), "tok", "acme", "widget", "release/2026-09")
	if err != nil {
		t.Fatalf("BranchHeadSHA: %v", err)
	}
	if sha != "def456" {
		t.Errorf("sha = %q, want def456", sha)
	}
}
