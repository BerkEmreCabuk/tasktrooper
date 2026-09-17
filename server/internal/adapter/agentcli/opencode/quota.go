package opencode

import (
	"regexp"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// rateLimitPattern matches OpenCode relaying an upstream provider's rate
// limit: "Error from provider (<Name>): Rate limit exceeded." on the
// structured `error` event's message, "rate_limit_exceeded" as the event's own
// error name, or the same words in the "ERROR service=llm ..." line OpenCode
// prints to stderr when the structured event never arrives — a known upstream
// gap (see config-reference.md's note on the missing step_finish event).
//
// OpenCode proxies whichever provider the agent's model belongs to, so this is
// THAT provider's own limit relayed through OpenCode's error shape, not one
// "OpenCode subscription" the way Claude Code's usage limit is — which is why
// there is no reset-time parsing here: no upstream provider's limit format is
// documented to survive the relay, so every park uses the default window.
var rateLimitPattern = regexp.MustCompile(`(?i)rate.?limit.?exceeded|rate limit reached|rate_limit_exceeded|too many requests|\b429\b|quota exceeded`)

const quotaDetailMax = 300

// quotaBlockFrom builds the park from a finished OpenCode session, or nil when
// nothing in its error status, error text or stderr says a provider rate
// limit was hit.
func quotaBlockFrom(out outcome, stderrTail, sessionID string, now time.Time) *domain.QuotaBlock {
	text := strings.Join([]string{out.Status, out.Text, stderrTail}, "\n")
	if !rateLimitPattern.MatchString(text) {
		return nil
	}
	return &domain.QuotaBlock{
		ResumeAt:     now.Add(domain.DefaultQuotaParkWindow),
		CLISessionID: sessionID,
		Detail:       quotaDetail(text),
		Provider:     domain.LLMProviderOpencode,
	}
}

func quotaDetail(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && rateLimitPattern.MatchString(line) {
			return domain.TruncateHead(line, quotaDetailMax)
		}
	}
	return "OpenCode provider rate limit reached"
}
