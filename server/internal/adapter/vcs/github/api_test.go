package github

import "testing"

func TestParseOwnerRepo(t *testing.T) {
	cases := []struct {
		in    string
		owner string
		repo  string
		ok    bool
	}{
		{"https://github.com/acme/widget.git", "acme", "widget", true},
		{"https://github.com/acme/widget", "acme", "widget", true},
		{"git@github.com:acme/widget.git", "acme", "widget", true},
		{"git@github.com:acme/widget", "acme", "widget", true},
		{"https://x-access-token:tok@github.com/acme/widget.git", "acme", "widget", true},
		{"https://gitlab.com/acme/widget.git", "", "", false},
		{"", "", "", false},
		{"https://github.com/acme", "", "", false},
	}
	for _, tc := range cases {
		owner, repo, ok := ParseOwnerRepo(tc.in)
		if owner != tc.owner || repo != tc.repo || ok != tc.ok {
			t.Errorf("ParseOwnerRepo(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, owner, repo, ok, tc.owner, tc.repo, tc.ok)
		}
	}
}
