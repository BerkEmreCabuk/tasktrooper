package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateTagPostsGitRef(t *testing.T) {
	var gotPath string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ref":"refs/tags/rollback/prod/1"}`))
	}))
	defer srv.Close()

	api := NewActionsAPI("tok")
	api.SetBaseURL(srv.URL)
	if err := api.CreateTag(context.Background(), "o", "r", "rollback/prod/1", "abc123"); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if gotPath != "/repos/o/r/git/refs" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["ref"] != "refs/tags/rollback/prod/1" || gotBody["sha"] != "abc123" {
		t.Fatalf("body = %v", gotBody)
	}
}

// An empty branch must list runs across every branch — the monitor has no
// single branch to filter by.
func TestListWorkflowRunsOmitsBranchWhenEmpty(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"workflow_runs":[{"id":7,"run_number":3,"head_sha":"s","head_branch":"main","status":"completed","conclusion":"success","html_url":"u","event":"workflow_dispatch"}]}`))
	}))
	defer srv.Close()

	api := NewActionsAPI("tok")
	api.SetBaseURL(srv.URL)
	runs, err := api.ListWorkflowRuns(context.Background(), "o", "r", "prod.yml", "")
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if strings.Contains(gotQuery, "branch=") {
		t.Fatalf("query carried a branch filter: %q", gotQuery)
	}
	if len(runs) != 1 || runs[0].ID != 7 || runs[0].Event != "workflow_dispatch" {
		t.Fatalf("runs = %+v", runs)
	}
}
