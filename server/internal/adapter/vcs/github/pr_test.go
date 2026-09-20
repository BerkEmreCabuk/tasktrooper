package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// prTestServer stands in for the GitHub API. Handlers are keyed by
// "METHOD /path"; anything unexpected fails the test loudly rather than being
// silently absorbed, which is how a wrong path (or a missing escape) shows up.
func prTestServer(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) *PRAPI {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := routes[r.Method+" "+r.URL.Path]; ok {
			if got := r.Header.Get("Authorization"); got != "Bearer tok" {
				t.Errorf("Authorization = %q, want the bearer token", got)
			}
			h(w, r)
			return
		}
		t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	api := NewPRAPI()
	api.SetBaseURL(srv.URL)
	return api
}

func TestGetPullRequestMapsTheFieldsCallersDecideWith(t *testing.T) {
	api := prTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /repos/acme/widget/pulls/42": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 42, "title": "Add the store link", "state": "open",
				"draft": true, "merged": false, "mergeable_state": "clean",
				"additions": 12, "deletions": 3, "changed_files": 2,
				"html_url": "https://github.com/acme/widget/pull/42",
				"head":     map[string]any{"ref": "feature/task-1234abcd-add-the-store-link", "sha": "bbb2222"},
				"base":     map[string]any{"ref": "main"},
			})
		},
	})

	pr, err := api.GetPullRequest(context.Background(), "tok", "acme", "widget", 42)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.Number != 42 || pr.Title != "Add the store link" || pr.State != "open" {
		t.Errorf("identity fields wrong: %+v", pr)
	}
	if !pr.Draft || pr.Merged || pr.MergeableState != "clean" {
		t.Errorf("state fields wrong: %+v", pr)
	}
	if pr.HeadRef != "feature/task-1234abcd-add-the-store-link" || pr.HeadSHA != "bbb2222" || pr.BaseRef != "main" {
		t.Errorf("refs wrong: %+v", pr)
	}
	if pr.Additions != 12 || pr.Deletions != 3 || pr.ChangedFiles != 2 {
		t.Errorf("counts wrong: %+v", pr)
	}
}

// Owner and repo reach the URL escaped. The older calls in this package
// interpolate them raw; everything added here must not.
func TestPullRequestPathsEscapeTheirSegments(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 1})
	}))
	defer srv.Close()
	api := NewPRAPI()
	api.SetBaseURL(srv.URL)

	if _, err := api.GetPullRequest(context.Background(), "tok", "acme/../evil", "wid get", 1); err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	// The separators inside the owner are encoded, so the owner stays ONE segment
	// and cannot walk out of /repos/<owner>/<repo>/ — that is what escaping buys.
	if want := "/repos/acme%2F..%2Fevil/wid%20get/pulls/1"; gotPath != want {
		t.Errorf("escaped path = %s, want %s", gotPath, want)
	}
	if strings.Count(gotPath, "/") != 5 {
		t.Errorf("the owner leaked extra path segments: %s", gotPath)
	}
}

// The file list pages: a real branch routinely passes 100 changed files, and the
// existing single-page list calls in this package would silently lose the rest.
func TestListPullRequestFilesFollowsPages(t *testing.T) {
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("per_page = %q", r.URL.Query().Get("per_page"))
		}
		var files []map[string]any
		switch r.URL.Query().Get("page") {
		case "1":
			for i := 0; i < 100; i++ {
				files = append(files, map[string]any{"filename": fmt.Sprintf("f%d.go", i), "status": "modified", "additions": 1})
			}
		default:
			files = append(files, map[string]any{
				"filename": "renamed.go", "previous_filename": "old.go", "status": "renamed",
			})
		}
		_ = json.NewEncoder(w).Encode(files)
	}))
	defer srv.Close()
	api := NewPRAPI()
	api.SetBaseURL(srv.URL)

	files, err := api.ListPullRequestFiles(context.Background(), "tok", "acme", "widget", 42)
	if err != nil {
		t.Fatalf("ListPullRequestFiles: %v", err)
	}
	if pages != 2 {
		t.Errorf("pages fetched = %d, want 2 (a short page ends the walk)", pages)
	}
	if len(files) != 101 {
		t.Fatalf("files = %d, want 101", len(files))
	}
	last := files[len(files)-1]
	if last.Path != "renamed.go" || last.PreviousPath != "old.go" || last.Status != "renamed" {
		t.Errorf("rename not mapped: %+v", last)
	}
}

// The diff is the biggest thing a tool result can carry, so it is capped — and the
// caller must be told, or a model reads a prefix as the whole change.
func TestPullRequestDiffCapsAndReportsTruncation(t *testing.T) {
	body := strings.Repeat("a", 500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github.v3.diff" {
			t.Errorf("Accept = %q, want the diff media type", got)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	api := NewPRAPI()
	api.SetBaseURL(srv.URL)

	diff, truncated, err := api.PullRequestDiff(context.Background(), "tok", "acme", "widget", 42, 100)
	if err != nil {
		t.Fatalf("PullRequestDiff: %v", err)
	}
	if !truncated {
		t.Errorf("truncated = false for a 500-byte diff capped at 100")
	}
	if len(diff) != 100 {
		t.Errorf("len(diff) = %d, want 100", len(diff))
	}

	short := strings.Repeat("b", 40)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(short))
	}))
	defer srv2.Close()
	api.SetBaseURL(srv2.URL)
	diff, truncated, err = api.PullRequestDiff(context.Background(), "tok", "acme", "widget", 42, 100)
	if err != nil {
		t.Fatalf("PullRequestDiff: %v", err)
	}
	if truncated || diff != short {
		t.Errorf("a diff under the cap must come back whole and untruncated (truncated=%v)", truncated)
	}
}

func TestListPullRequestReviewCommentsMapsAnchorsAndReplies(t *testing.T) {
	api := prTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /repos/acme/widget/pulls/42/comments": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "body": "this can be nil", "path": "main.go", "line": 12,
					"user": map[string]any{"login": "reviewer"}, "html_url": "https://github.com/c/1"},
				// A multi-line range reports start_line and no line.
				{"id": 2, "body": "and here", "path": "main.go", "start_line": 30,
					"in_reply_to_id": 1, "user": map[string]any{"login": "dev"}},
			})
		},
	})

	comments, err := api.ListPullRequestReviewComments(context.Background(), "tok", "acme", "widget", 42)
	if err != nil {
		t.Fatalf("ListPullRequestReviewComments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(comments))
	}
	if comments[0].Author != "reviewer" || comments[0].Line != 12 || comments[0].Path != "main.go" {
		t.Errorf("first comment: %+v", comments[0])
	}
	if comments[1].Line != 30 {
		t.Errorf("a range comment must fall back to start_line, got %d", comments[1].Line)
	}
	if comments[1].InReplyTo != 1 {
		t.Errorf("in_reply_to = %d, want 1", comments[1].InReplyTo)
	}
}

// Replying goes to the review comment's own replies endpoint — a top-level comment
// would leave the reviewer's thread unanswered.
func TestReplyToReviewCommentPostsIntoTheThread(t *testing.T) {
	var gotBody map[string]any
	api := prTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /repos/acme/widget/pulls/42/comments/555/replies": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 777, "body": "fixed", "in_reply_to_id": 555})
		},
	})

	comment, err := api.ReplyToReviewComment(context.Background(), "tok", "acme", "widget", 42, 555, "fixed")
	if err != nil {
		t.Fatalf("ReplyToReviewComment: %v", err)
	}
	if gotBody["body"] != "fixed" {
		t.Errorf("posted body = %v", gotBody["body"])
	}
	if comment.ID != 777 || comment.InReplyTo != 555 {
		t.Errorf("reply not mapped: %+v", comment)
	}
}

// A PR's conversation comments live under the ISSUE path — GitHub models a PR as an
// issue for these, and using the pulls path returns review comments instead.
func TestIssueCommentsUseTheIssuePath(t *testing.T) {
	api := prTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /repos/acme/widget/issues/42/comments": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 9, "body": "ship it", "user": map[string]any{"login": "pm"}},
			})
		},
		"POST /repos/acme/widget/issues/42/comments": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 10, "body": "done"})
		},
	})

	comments, err := api.ListIssueComments(context.Background(), "tok", "acme", "widget", 42)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if len(comments) != 1 || comments[0].Author != "pm" || comments[0].Path != "" {
		t.Errorf("conversation comment: %+v", comments)
	}

	created, err := api.CreateIssueComment(context.Background(), "tok", "acme", "widget", 42, "done")
	if err != nil {
		t.Fatalf("CreateIssueComment: %v", err)
	}
	if created.ID != 10 {
		t.Errorf("created comment id = %d, want 10", created.ID)
	}
}

// An API failure has to surface as an error, not as an empty PR that reads like a
// PR with no files and no comments.
func TestPullRequestErrorsSurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()
	api := NewPRAPI()
	api.SetBaseURL(srv.URL)

	if _, err := api.GetPullRequest(context.Background(), "tok", "acme", "widget", 42); err == nil {
		t.Fatal("expected an error for a 404")
	} else if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should carry the status: %v", err)
	}
	if _, _, err := api.PullRequestDiff(context.Background(), "tok", "acme", "widget", 42, 100); err == nil {
		t.Fatal("expected an error for a 404 diff")
	}
}
