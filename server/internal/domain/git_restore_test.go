package domain_test

import (
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The restore offer is a promise that something can be fetched here, so the
// decision behind it is the whole feature: offered where a clone would work,
// silent everywhere else, and never a reason to touch what is already on disk.
func TestCanRestoreWorkingCopy(t *testing.T) {
	const remote = "https://github.com/acme/app.git"

	cases := []struct {
		name      string
		presence  domain.GitPresence
		remoteURL string
		want      bool
		// reasonHas is a fragment the refusal must contain, so a refusal can
		// never degrade into an unexplained "no".
		reasonHas string
	}{
		{
			name:      "folder missing and remote known — the case the button exists for",
			presence:  domain.GitPresence{State: domain.GitPresencePathMissing},
			remoteURL: remote,
			want:      true,
		},
		{
			name:      "folder present — never clobber a working copy",
			presence:  domain.GitPresence{State: domain.GitPresenceRepository},
			remoteURL: remote,
			want:      false,
			reasonHas: "already a working copy",
		},
		{
			name:      "folder missing but no remote recorded — nowhere to fetch from",
			presence:  domain.GitPresence{State: domain.GitPresencePathMissing},
			remoteURL: "",
			want:      false,
			reasonHas: "No git remote is recorded",
		},
		{
			name:      "path exists but is not a repository — refused, and nothing is ours to delete",
			presence:  domain.GitPresence{State: domain.GitPresenceNoRepository},
			remoteURL: remote,
			want:      false,
			reasonHas: "Nothing was changed or deleted",
		},
		{
			name:      "path unreadable — an unknown is not a licence to re-clone",
			presence:  domain.GitPresence{State: domain.GitPresenceUnreadable, Reason: "permission denied"},
			remoteURL: remote,
			want:      false,
			reasonHas: "permission denied",
		},
		{
			name:      "path unreadable with no reason still refuses",
			presence:  domain.GitPresence{State: domain.GitPresenceUnreadable},
			remoteURL: remote,
			want:      false,
			reasonHas: "could not be read",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := domain.CanRestoreWorkingCopy(tc.presence, tc.remoteURL)
			if got != tc.want {
				t.Fatalf("CanRestoreWorkingCopy = %v, want %v (reason %q)", got, tc.want, reason)
			}
			if tc.want {
				if reason != "" {
					t.Fatalf("an offered restore carried a refusal reason: %q", reason)
				}
				return
			}
			if reason == "" {
				t.Fatal("restore was refused without saying why")
			}
			if !strings.Contains(reason, tc.reasonHas) {
				t.Fatalf("reason = %q, want it to mention %q", reason, tc.reasonHas)
			}
		})
	}
}

// A missing folder with no remote and a missing folder with one differ only in
// the remote, which is the half a user cannot see on the card. Asserting the
// pair together keeps the two halves of the rule from drifting.
func TestCanRestoreWorkingCopyNeedsBothHalves(t *testing.T) {
	missing := domain.GitPresence{State: domain.GitPresencePathMissing}
	if ok, _ := domain.CanRestoreWorkingCopy(missing, "   "); ok {
		t.Fatal("a blank remote was treated as a usable one")
	}
	present := domain.GitPresence{State: domain.GitPresenceRepository}
	if ok, _ := domain.CanRestoreWorkingCopy(present, "https://github.com/acme/app.git"); ok {
		t.Fatal("a repository that is already on disk was offered a restore")
	}
}
