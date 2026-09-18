package domain

// RepoArea names the backend/frontend/mobile area that owns a repository —
// what a role assignment's Areas is scoped by, and what RoleResolver.AgentForRole
// matches against. A monorepo has no kind of its own to key on, so it resolves
// to the area that owns most of its sub-projects; ties go to backend, then
// frontend, then mobile, and a monorepo with no recorded sub-projects falls
// back to backend.
func RepoArea(kind string, subProjects []RepoSubProject) string {
	if kind != RepoKindMonorepo {
		return areaForSingleKind(kind)
	}
	counts := make(map[string]int, 3)
	for _, sp := range subProjects {
		if sp.Kind == RepoKindMonorepo {
			continue
		}
		counts[areaForSingleKind(sp.Kind)]++
	}
	best, bestCount := RepoKindBackend, 0
	for _, area := range []string{RepoKindBackend, RepoKindFrontend, RepoKindMobile} {
		if counts[area] > bestCount {
			best, bestCount = area, counts[area]
		}
	}
	return best
}

func areaForSingleKind(kind string) string {
	switch kind {
	case RepoKindFrontend:
		return RepoKindFrontend
	case RepoKindMobile:
		return RepoKindMobile
	default: // backend, worker, or not detected yet
		return RepoKindBackend
	}
}
