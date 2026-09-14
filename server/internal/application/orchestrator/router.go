package orchestrator

func ShouldOrchestrate(force bool, fastPathEnabled bool) bool {
	if force {
		return true
	}
	if !fastPathEnabled {
		return true
	}
	return false
}
