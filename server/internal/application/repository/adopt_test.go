package repository

import (
	"strings"
	"testing"
)

func TestAssertCheckoutIsRepo(t *testing.T) {
	const want = "https://github.com/acme/api.git"

	same := []string{
		"https://github.com/acme/api.git",
		"https://github.com/acme/api",
		"https://github.com/acme/api/",
		"https://x-access-token:ghp_secret@github.com/acme/api.git",
		"git@github.com:acme/api.git",
		"ssh://git@github.com/acme/api",
		"https://GitHub.com/Acme/API.git",
	}
	for _, found := range same {
		if err := assertCheckoutIsRepo(found, "/w/repos/api", want); err != nil {
			t.Errorf("origin %q was refused as a different repository: %v", found, err)
		}
	}

	other := []string{
		"https://github.com/rival/api.git",
		"git@github.com:rival/api.git",
		"https://gitlab.com/acme/api.git",
		"https://github.com/acme/api-v2.git",
	}
	for _, found := range other {
		err := assertCheckoutIsRepo(found, "/w/repos/api", want)
		if err == nil {
			t.Errorf("origin %q was adopted as %q", found, want)
			continue
		}

		if got := err.Error(); strings.Contains(got, "rival") || strings.Contains(got, "gitlab.com") || strings.Contains(got, "api-v2") {
			t.Errorf("the refusal disclosed the other repository: %s", got)
		}
	}
}

func TestAssertCheckoutIsRepo_MissingRemoteIsNotAMismatch(t *testing.T) {
	for _, tc := range []struct{ found, want string }{
		{"", "https://github.com/acme/api.git"},
		{"https://github.com/acme/api.git", ""},
		{"", ""},
		{"   ", "  "},
	} {
		if err := assertCheckoutIsRepo(tc.found, "/w/repos/api", tc.want); err != nil {
			t.Errorf("found=%q want=%q was refused: %v", tc.found, tc.want, err)
		}
	}
}
