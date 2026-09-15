package postgres

import (
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// These cover the read-time translation without a database: localizeRootPath is
// what every scan of a repositories row runs through, and its whole job is to
// decide whether a stored absolute path belongs to this host.

func TestLocalizeRootPath_ForeignPathReanchored(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "x", "workspaces")
	store := (&RepositoryStore{}).SetHostRoots(wsRoot, nil)

	repo := domain.Repository{Name: "acme-web", RootPath: "/data/workspaces/acme-web"}
	store.localizeRootPath(&repo)

	if want := filepath.Join(wsRoot, "repos", "acme-web"); repo.RootPath != want {
		t.Fatalf("root path = %q, want %q", repo.RootPath, want)
	}
}

func TestLocalizeRootPath_LocalPathUnchanged(t *testing.T) {
	dir := t.TempDir()
	wsRoot := filepath.Join(dir, "workspaces")
	local := filepath.Join(wsRoot, "repos", "acme-web")
	store := (&RepositoryStore{}).SetHostRoots(wsRoot, nil)

	repo := domain.Repository{Name: "acme-web", RootPath: local}
	store.localizeRootPath(&repo)

	if repo.RootPath != local {
		t.Fatalf("root path = %q, want %q", repo.RootPath, local)
	}
}

// A store that was never told about a host (tests, any caller that does not
// touch working copies) must behave exactly as it did before this existed.
func TestLocalizeRootPath_NoHostRootsIsIdentity(t *testing.T) {
	store := &RepositoryStore{}
	repo := domain.Repository{Name: "acme-web", RootPath: "/data/workspaces/acme-web"}
	store.localizeRootPath(&repo)
	if repo.RootPath != "/data/workspaces/acme-web" {
		t.Fatalf("root path = %q, want it unchanged", repo.RootPath)
	}
}

func TestRepoDirName(t *testing.T) {
	cases := map[string]string{
		"/data/workspaces/repos/acme-web": "acme-web",
		"/data/workspaces/repos/":         "repos",
		"acme-web":                        "acme-web",
		"  /data/x  ":                     "x",
		"/":                               "",
		"":                                "",
		".":                               "",
	}
	for in, want := range cases {
		if got := repoDirName(in); got != want {
			t.Fatalf("repoDirName(%q) = %q, want %q", in, got, want)
		}
	}
}
