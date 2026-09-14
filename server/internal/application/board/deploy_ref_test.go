package board

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func swapDeployRefDeps(t *testing.T, tag func(ctx context.Context, token, owner, repo, tag, sha string) error, branch string) {
	t.Helper()
	origTag, origBranch := createReleaseTag, repoDefaultBranch
	createReleaseTag = tag
	repoDefaultBranch = func(context.Context, string, string, string) string { return branch }
	t.Cleanup(func() {
		createReleaseTag = origTag
		repoDefaultBranch = origBranch
	})
}

const deployRefSHA = "abc123def456789012345678901234567890abcd"

func TestDeployRefDispatchesTheMergeCommitAsATag(t *testing.T) {
	var taggedSHA, taggedName string
	swapDeployRefDeps(t, func(_ context.Context, _, _, _, tag, sha string) error {
		taggedName, taggedSHA = tag, sha
		return nil
	}, "main")

	ref := deployRef(context.Background(), "tok",
		domain.TaskGitInfo{Owner: "acme-org", Repo: "acme"},
		domain.BoardTask{ID: uuid.New(), MergeCommitSHA: deployRefSHA})

	if ref == "main" {
		t.Fatal("the release still dispatched the default branch — this is the drift bug")
	}
	want := domain.ReleaseTagForCommit(deployRefSHA)
	if ref != want {
		t.Fatalf("ref = %q, want %q", ref, want)
	}
	if taggedSHA != deployRefSHA {
		t.Fatalf("tagged %q, want the task's merge commit %q", taggedSHA, deployRefSHA)
	}
	if taggedName != want {
		t.Fatalf("tag name = %q, want %q", taggedName, want)
	}
}

func TestDeployRefReusesAnExistingReleaseTag(t *testing.T) {
	swapDeployRefDeps(t, func(context.Context, string, string, string, string, string) error {
		return githubAlreadyExists()
	}, "main")

	ref := deployRef(context.Background(), "tok",
		domain.TaskGitInfo{Owner: "acme-org", Repo: "acme"},
		domain.BoardTask{ID: uuid.New(), MergeCommitSHA: deployRefSHA})

	if ref != domain.ReleaseTagForCommit(deployRefSHA) {
		t.Fatalf("ref = %q, want the existing release tag reused", ref)
	}
}

func TestDeployRefFallsBackToTheDefaultBranchWithoutAMergeCommit(t *testing.T) {
	tagged := false
	swapDeployRefDeps(t, func(context.Context, string, string, string, string, string) error {
		tagged = true
		return nil
	}, "trunk")

	ref := deployRef(context.Background(), "tok",
		domain.TaskGitInfo{Owner: "acme-org", Repo: "acme"},
		domain.BoardTask{ID: uuid.New()})

	if ref != "trunk" {
		t.Fatalf("ref = %q, want the repository's default branch", ref)
	}
	if tagged {
		t.Fatal("nothing should be tagged for a task with no merge commit")
	}
}
func TestDeployRefFallsBackWhenTaggingFails(t *testing.T) {
	swapDeployRefDeps(t, func(context.Context, string, string, string, string, string) error {
		return errors.New("403 Resource not accessible by integration")
	}, "main")

	ref := deployRef(context.Background(), "tok",
		domain.TaskGitInfo{Owner: "acme-org", Repo: "acme"},
		domain.BoardTask{ID: uuid.New(), MergeCommitSHA: deployRefSHA})

	if ref != "main" {
		t.Fatalf("ref = %q, want the default-branch fallback", ref)
	}
}

func TestDeployRefLastResortIsMain(t *testing.T) {
	swapDeployRefDeps(t, func(context.Context, string, string, string, string, string) error { return nil }, "")

	if ref := deployRef(context.Background(), "tok", domain.TaskGitInfo{}, domain.BoardTask{}); ref != "main" {
		t.Fatalf("ref = %q, want main", ref)
	}
}

func createTagAgainst(status int, body string) error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	api := githubapi.NewActionsAPI("tok")
	api.SetBaseURL(srv.URL)
	return api.CreateTag(context.Background(), "acme-org", "acme", "release/abc123def456", deployRefSHA)
}

func githubAlreadyExists() error {
	return createTagAgainst(http.StatusUnprocessableEntity, `{"message":"Reference already exists"}`)
}

func TestIsRefAlreadyExistsRecognisesGitHubs422(t *testing.T) {
	if err := githubAlreadyExists(); !githubapi.IsRefAlreadyExists(err) {
		t.Fatalf("err = %v, want it recognised as an already-existing ref", err)
	}

	bad := createTagAgainst(http.StatusUnprocessableEntity, `{"message":"Object does not exist"}`)
	if githubapi.IsRefAlreadyExists(bad) {
		t.Fatalf("a 422 for a bad SHA must NOT read as an existing ref: %v", bad)
	}
	if githubapi.IsRefAlreadyExists(errors.New("connection reset")) {
		t.Fatal("a transport error must not read as an existing ref")
	}
}
