package prompt

var opsRepositoryIDInvalidKey = Define[struct{}]("guard.ops_repository_id_invalid", struct{}{})

// OpsRepositoryIDInvalidText is what a runtime-ops tool call gets back when
// it named no repository this run can resolve.
func OpsRepositoryIDInvalidText() string { return Text(opsRepositoryIDInvalidKey) }
