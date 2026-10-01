package prompt

type runtimeComponentsAmbiguousInput struct {
	Count int
	Paths string
}

var runtimeComponentsAmbiguousKey = Define("guard.runtime_components_ambiguous", runtimeComponentsAmbiguousInput{Count: 2, Paths: "api, web"})

type runtimeEnvironmentNotBoundInput struct {
	Env       string
	Component string
}

var runtimeEnvironmentNotBoundKey = Define("guard.runtime_environment_not_bound", runtimeEnvironmentNotBoundInput{Env: "production", Component: "api"})

type runtimeInvalidSinceInput struct{ Raw string }

var runtimeInvalidSinceKey = Define("guard.runtime_invalid_since", runtimeInvalidSinceInput{Raw: `"5x"`})

type runtimeSinceInFutureInput struct{ Raw string }

var runtimeSinceInFutureKey = Define("guard.runtime_since_in_future", runtimeSinceInFutureInput{Raw: `"2099-01-01T00:00:00Z"`})

// RuntimeComponentsAmbiguousText is what a runtime tool says when a
// repository has more than one active component and none is its root, so
// the call must name which one it means.
func RuntimeComponentsAmbiguousText(count int, paths string) string {
	return runtimeComponentsAmbiguousKey.Render(runtimeComponentsAmbiguousInput{Count: count, Paths: paths})
}

// RuntimeEnvironmentNotBoundText is what a runtime tool says when the
// resolved component has no cloud account connected for the requested
// environment.
func RuntimeEnvironmentNotBoundText(env, component string) string {
	return runtimeEnvironmentNotBoundKey.Render(runtimeEnvironmentNotBoundInput{Env: env, Component: component})
}

// RuntimeInvalidSinceText is a runtime tool's refusal for a since value it
// could not parse as a duration or an RFC3339 timestamp. raw must already be
// %q-quoted by the caller.
func RuntimeInvalidSinceText(raw string) string {
	return runtimeInvalidSinceKey.Render(runtimeInvalidSinceInput{Raw: raw})
}

// RuntimeSinceInFutureText is a runtime tool's refusal for an RFC3339 since
// timestamp that is later than now. raw must already be %q-quoted by the
// caller.
func RuntimeSinceInFutureText(raw string) string {
	return runtimeSinceInFutureKey.Render(runtimeSinceInFutureInput{Raw: raw})
}

var runtimeLogsTruncatedKey = Define[struct{}]("tool_results.runtime_logs_truncated", struct{}{})

// RuntimeLogsTruncatedText is query_runtime_logs' "note" field when the
// provider capped the returned window.
func RuntimeLogsTruncatedText() string { return Text(runtimeLogsTruncatedKey) }
