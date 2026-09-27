package prompt

type codeIndexUnavailableInput struct {
	Cause string
	Name  string
}

var codeIndexUnavailableKey = Define("guard.code_index_unavailable", codeIndexUnavailableInput{Cause: "not found", Name: "codebase_search"})

type codeEmbeddingUnreachableInput struct {
	Cause string
	Name  string
}

var codeEmbeddingUnreachableKey = Define("guard.code_embedding_unreachable", codeEmbeddingUnreachableInput{Cause: "dial tcp: timeout", Name: "codebase_search"})

type codeEmbeddingRejectedInput struct {
	StatusCode int
	Name       string
}

var codeEmbeddingRejectedKey = Define("guard.code_embedding_rejected", codeEmbeddingRejectedInput{StatusCode: 402, Name: "codebase_search"})

type codeEmbeddingRateLimitedInput struct {
	StatusCode       int
	RetryAfterClause string
}

var codeEmbeddingRateLimitedKey = Define("guard.code_embedding_rate_limited", codeEmbeddingRateLimitedInput{StatusCode: 429, RetryAfterClause: ", retry after 7s"})

// CodeIndexUnavailableText explains a missing semantic index in terms of
// what to do next — see adapter/tools/code/tools.go's indexUnavailableError.
func CodeIndexUnavailableText(cause, name string) string {
	return codeIndexUnavailableKey.Render(codeIndexUnavailableInput{Cause: cause, Name: name})
}

// CodeEmbeddingUnreachableText is codebase_search's answer when the
// embedding provider could not be reached at all.
func CodeEmbeddingUnreachableText(cause, name string) string {
	return codeEmbeddingUnreachableKey.Render(codeEmbeddingUnreachableInput{Cause: cause, Name: name})
}

// CodeEmbeddingRejectedText is codebase_search's answer when the embedding
// provider rejected the request outright (401/402/403) — this run's semantic
// search is done for good, so it says not to retry.
func CodeEmbeddingRejectedText(statusCode int, name string) string {
	return codeEmbeddingRejectedKey.Render(codeEmbeddingRejectedInput{StatusCode: statusCode, Name: name})
}

// CodeEmbeddingRateLimitedText is codebase_search's answer to a 429; unlike
// CodeEmbeddingRejectedText it allows one bounded retry.
func CodeEmbeddingRateLimitedText(statusCode int, retryAfterClause string) string {
	return codeEmbeddingRateLimitedKey.Render(codeEmbeddingRateLimitedInput{StatusCode: statusCode, RetryAfterClause: retryAfterClause})
}
