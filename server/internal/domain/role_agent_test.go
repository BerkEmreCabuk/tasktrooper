package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestDeveloperAgentForKind(t *testing.T) {
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
		{"backend", domain.RepoKindBackend, nil, domain.AgentBackendDeveloper},
		{"worker", domain.RepoKindWorker, nil, domain.AgentBackendDeveloper},
		{"frontend", domain.RepoKindFrontend, nil, domain.AgentFrontendDeveloper},
		{"mobile", domain.RepoKindMobile, nil, domain.AgentMobileDeveloper},
		{"undetected kind", "", nil, domain.AgentBackendDeveloper},
		{"monorepo without sub-projects", domain.RepoKindMonorepo, nil, domain.AgentBackendDeveloper},
		{"monorepo tie goes to backend", domain.RepoKindMonorepo, []domain.RepoSubProject{mobile, backend}, domain.AgentBackendDeveloper},
		{"monorepo mostly frontend", domain.RepoKindMonorepo, []domain.RepoSubProject{web, backend, admin}, domain.AgentFrontendDeveloper},
		{"monorepo only mobile", domain.RepoKindMonorepo, []domain.RepoSubProject{mobile}, domain.AgentMobileDeveloper},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.DeveloperAgentForKind(tc.kind, tc.subs); got != tc.want {
				t.Fatalf("DeveloperAgentForKind(%q) = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}
