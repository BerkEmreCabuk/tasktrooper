package repository

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestWithGitWarningCarriesThePresenceReason(t *testing.T) {
	cases := []struct {
		name     string
		presence domain.GitPresence
		want     string
	}{
		{
			name:     "working copy warns about nothing",
			presence: domain.GitPresence{State: domain.GitPresenceRepository},
			want:     "",
		},
		{
			name:     "folder with no repository",
			presence: domain.GitPresence{State: domain.GitPresenceNoRepository},
			want:     "This project is not a git repository yet",
		},
		{
			name:     "folder that is not on this host",
			presence: domain.GitPresence{State: domain.GitPresencePathMissing},
			want:     domain.GitPresence{State: domain.GitPresencePathMissing}.Warning(),
		},
		{
			name:     "folder that cannot be read",
			presence: domain.GitPresence{State: domain.GitPresenceUnreadable, Reason: "permission denied"},
			want:     "The project folder could not be read: permission denied",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			presence := tc.presence
			svc := &Service{git: &fakeReleaseGit{presence: &presence}}

			got := svc.withGitWarning(domain.Repository{ID: uuid.New(), RootPath: "/srv/work/repos/app"})

			if got.GitWarning != tc.want {
				t.Fatalf("GitWarning = %q, want %q", got.GitWarning, tc.want)
			}
			if strings.Contains(got.GitWarning, "/srv/work/repos/app") {
				t.Fatalf("GitWarning = %q repeats root_path, which the card prints right underneath", got.GitWarning)
			}
		})
	}
}

func TestWithGitWarningPrefersTheRecordedSetupFailure(t *testing.T) {
	id := uuid.New()
	svc := &Service{
		git:         &fakeReleaseGit{presence: &domain.GitPresence{State: domain.GitPresenceNoRepository}},
		gitWarnings: map[uuid.UUID]string{id: "Git/GitHub setup failed: boom"},
	}

	got := svc.withGitWarning(domain.Repository{ID: id, RootPath: "/srv/work/repos/app"})

	if want := "Git/GitHub setup failed: boom"; got.GitWarning != want {
		t.Fatalf("GitWarning = %q, want %q", got.GitWarning, want)
	}
}
