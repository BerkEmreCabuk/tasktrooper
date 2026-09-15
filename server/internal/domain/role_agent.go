package domain

// Role agents that author files in a repository. The system architect is
// deliberately not one of them: it designs and reviews, and it refuses a task
// that asks it to write code, docs or workflows into a branch.
const (
	AgentBackendDeveloper  = "backend-developer"
	AgentFrontendDeveloper = "frontend-developer"
	AgentMobileDeveloper   = "mobile-developer"
)

// DeveloperAgentForKind names the role agent that owns hands-on work (docs,
// CI/CD workflows, deploy setup, incident fixes) for a repository kind.
//
// A monorepo has no developer of its own, so it resolves to the developer that
// owns most of its sub-projects. Ties go to backend, then frontend, then
// mobile, and a monorepo with no recorded sub-projects falls back to backend.
func DeveloperAgentForKind(kind string, subProjects []RepoSubProject) string {
	if kind != RepoKindMonorepo {
		return developerAgentForSingleKind(kind)
	}
	counts := make(map[string]int, 3)
	for _, sp := range subProjects {
		if sp.Kind == RepoKindMonorepo {
			continue
		}
		counts[developerAgentForSingleKind(sp.Kind)]++
	}
	best, bestCount := AgentBackendDeveloper, 0
	for _, role := range []string{AgentBackendDeveloper, AgentFrontendDeveloper, AgentMobileDeveloper} {
		if counts[role] > bestCount {
			best, bestCount = role, counts[role]
		}
	}
	return best
}

func developerAgentForSingleKind(kind string) string {
	switch kind {
	case RepoKindFrontend:
		return AgentFrontendDeveloper
	case RepoKindMobile:
		return AgentMobileDeveloper
	default: // backend, worker, or not detected yet
		return AgentBackendDeveloper
	}
}
