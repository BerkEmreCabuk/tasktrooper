package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestRepoArea(t *testing.T) {
	backend := domain.RepoSubProject{Path: "backend", Kind: domain.RepoKindBackend}
	mobile := domain.RepoSubProject{Path: "mobile", Kind: domain.RepoKindMobile}
	web := domain.RepoSubProject{Path: "web", Kind: domain.RepoKindFrontend}
	admin := domain.RepoSubProject{Path: "admin", Kind: domain.RepoKindFrontend}

	cases := []struct {
		name string
		kind string
		subs []domain.RepoSubProject
		want string
	}{
		{"backend", domain.RepoKindBackend, nil, domain.RepoKindBackend},
		{"worker", domain.RepoKindWorker, nil, domain.RepoKindBackend},
		{"frontend", domain.RepoKindFrontend, nil, domain.RepoKindFrontend},
		{"mobile", domain.RepoKindMobile, nil, domain.RepoKindMobile},
		{"undetected kind", "", nil, domain.RepoKindBackend},
		{"monorepo without sub-projects", domain.RepoKindMonorepo, nil, domain.RepoKindBackend},
		{"monorepo tie goes to backend", domain.RepoKindMonorepo, []domain.RepoSubProject{mobile, backend}, domain.RepoKindBackend},
		{"monorepo mostly frontend", domain.RepoKindMonorepo, []domain.RepoSubProject{web, backend, admin}, domain.RepoKindFrontend},
		{"monorepo only mobile", domain.RepoKindMonorepo, []domain.RepoSubProject{mobile}, domain.RepoKindMobile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.RepoArea(tc.kind, tc.subs); got != tc.want {
				t.Fatalf("RepoArea(%q) = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}
