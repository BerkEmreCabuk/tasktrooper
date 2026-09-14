package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// commitFilesServer answers the four git-data calls CommitFiles makes and
// records what it was sent. newTreeSHA is what POST /git/trees reports back —
// setting it to the base tree is how a test says "nothing changed".
type commitFilesServer struct {
	baseTreeSHA string
	newTreeSHA  string

	treeEntries []map[string]any
	blobBodies  []string
	commit      map[string]any
	refPatch    map[string]any
	refPatched  bool
}

func (s *commitFilesServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decode := func(out any) {
			if err := json.NewDecoder(r.Body).Decode(out); err != nil {
				t.Fatalf("decode %s: %v", r.URL.Path, err)
			}
		}
		switch {
		case r.URL.Path == "/repos/acme/widget/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"tip-sha"}}`))
		case r.URL.Path == "/repos/acme/widget/git/commits/tip-sha" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"tree":{"sha":"` + s.baseTreeSHA + `"}}`))
		case r.URL.Path == "/repos/acme/widget/git/blobs":
			var body struct {
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
			}
			decode(&body)
			if body.Encoding != "base64" {
				t.Fatalf("blob encoding = %q, want base64", body.Encoding)
			}
			raw, err := base64.StdEncoding.DecodeString(body.Content)
			if err != nil {
				t.Fatalf("blob content is not base64: %v", err)
			}
			s.blobBodies = append(s.blobBodies, string(raw))
			_, _ = w.Write([]byte(`{"sha":"blob-` + string(rune('a'+len(s.blobBodies)-1)) + `"}`))
		case r.URL.Path == "/repos/acme/widget/git/trees":
			var body struct {
				BaseTree string           `json:"base_tree"`
				Tree     []map[string]any `json:"tree"`
			}
			decode(&body)
			if body.BaseTree != s.baseTreeSHA {
				t.Fatalf("base_tree = %q, want %q", body.BaseTree, s.baseTreeSHA)
			}
			s.treeEntries = body.Tree
			_, _ = w.Write([]byte(`{"sha":"` + s.newTreeSHA + `"}`))
		case r.URL.Path == "/repos/acme/widget/git/commits" && r.Method == http.MethodPost:
			decode(&s.commit)
			_, _ = w.Write([]byte(`{"sha":"new-commit"}`))
		case r.URL.Path == "/repos/acme/widget/git/refs/heads/main" && r.Method == http.MethodPatch:
			decode(&s.refPatch)
			s.refPatched = true
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The mode is the reason this goes through the git data API at all: the
// contents API creates every file 100644, and a release script that lands
// without its executable bit fails on the machine that runs it directly.
func TestCommitFilesKeepsTheExecutableBitAndCommitsOnce(t *testing.T) {
	server := &commitFilesServer{baseTreeSHA: "base-tree", newTreeSHA: "new-tree"}
	srv := server.start(t)

	sha, changed, err := commitFilesAt(context.Background(), srv.URL, "tok", "acme", "widget", "main", "generated release pipeline", []FileChange{
		{Path: "scripts/mobile-release.sh", Body: "#!/usr/bin/env bash\n", Mode: 0o755},
		{Path: ".github/workflows/mobile-release.yml", Body: "name: mobile-release\n", Mode: 0o644},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sha != "new-commit" || !changed {
		t.Fatalf("sha = %q, changed = %v; want the new commit reported", sha, changed)
	}
	if len(server.treeEntries) != 2 {
		t.Fatalf("tree entries = %v, want both files in ONE tree", server.treeEntries)
	}
	if server.treeEntries[0]["mode"] != "100755" {
		t.Fatalf("script mode = %v, want 100755", server.treeEntries[0]["mode"])
	}
	if server.treeEntries[1]["mode"] != "100644" {
		t.Fatalf("workflow mode = %v, want 100644", server.treeEntries[1]["mode"])
	}
	if server.blobBodies[0] != "#!/usr/bin/env bash\n" {
		t.Fatalf("script body = %q, want it sent verbatim", server.blobBodies[0])
	}
	if parents, _ := server.commit["parents"].([]any); len(parents) != 1 || parents[0] != "tip-sha" {
		t.Fatalf("parents = %v, want the branch tip", server.commit["parents"])
	}
	if !server.refPatched || server.refPatch["force"] != false {
		t.Fatalf("ref patch = %v (patched=%v), want a non-forced fast-forward", server.refPatch, server.refPatched)
	}
}

// Starting the same release twice must not leave empty commits on the user's
// default branch, so an unchanged tree stops before the commit.
func TestCommitFilesWritesNothingWhenTheTreeIsUnchanged(t *testing.T) {
	server := &commitFilesServer{baseTreeSHA: "same-tree", newTreeSHA: "same-tree"}
	srv := server.start(t)

	sha, changed, err := commitFilesAt(context.Background(), srv.URL, "tok", "acme", "widget", "main", "generated release pipeline", []FileChange{
		{Path: "scripts/mobile-release.sh", Body: "#!/usr/bin/env bash\n", Mode: 0o755},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sha != "" || changed {
		t.Fatalf("sha = %q, changed = %v; want no commit", sha, changed)
	}
	if server.refPatched {
		t.Fatal("the branch was moved for a tree that did not change")
	}
}
