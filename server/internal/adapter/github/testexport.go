package github

// NewAPIErrorForTest builds the same *apiError shape a real GitHub response
// produces, so packages outside this one can drive the classifiers that read it
// — chiefly IsCIUnavailable, which decides whether the board's code-review gate
// opens because Actions can never run.
//
// It is exported here rather than in a _test.go file because the callers are in
// OTHER packages (internal/application/board), and a test-only symbol is not
// visible across a package boundary. The alternative — spinning an httptest
// server per case just to make the API return 402 — would test the transport
// instead of the decision, and the decision is the part that wedged three cards.
//
// Same reasoning as internal/application/orchestrator/testexport.go, which
// exists for the same reason.
func NewAPIErrorForTest(status int, message string) error {
	return &apiError{Status: status, Message: message}
}
