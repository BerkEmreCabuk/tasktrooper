package antigravity

import (
	"regexp"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// quotaPattern matches AGY's own wording for a spent quota — "Individual
// quota reached" in its own sentence, or the raw "RESOURCE_EXHAUSTED" the CLI
// relays from the backend when it gives up before writing one of its own.
// Reported verbatim against agy CLI 1.1.x:
//
//	⚠ Individual quota reached. Please upgrade your subscription to
//	increase your limits. Resets in 143h57m55s.
//	RESOURCE_EXHAUSTED (code 429): Individual quota reached. Contact your
//	administrator to enable overages. Resets in 167h39m40s.
var quotaPattern = regexp.MustCompile(`(?i)quota reached|resource_exhausted`)

// resetsInPattern pulls AGY's own countdown, "Resets in 143h57m55s" — already
// Go's duration syntax, so no unit translation is needed before
// time.ParseDuration.
var resetsInPattern = regexp.MustCompile(`(?i)resets in\s+([0-9]+h)?([0-9]+m)?([0-9]+s)?`)

const quotaDetailMax = 300

// quotaBlockFrom builds the park from a finished AGY session, or nil when
// nothing in its result text or stderr says the quota was hit.
func quotaBlockFrom(out outcome, stderrTail, sessionID string, now time.Time) *domain.QuotaBlock {
	text := strings.Join([]string{out.Text, stderrTail}, "\n")
	if !quotaPattern.MatchString(text) {
		return nil
	}
	resumeAt := now.Add(domain.DefaultQuotaParkWindow)
	if d, ok := parseResetsIn(text); ok {
		resumeAt = now.Add(d)
	}
	return &domain.QuotaBlock{
		ResumeAt:     resumeAt,
		CLISessionID: sessionID,
		Detail:       quotaDetail(text),
		Provider:     domain.LLMProviderAntigravity,
	}
}

// parseResetsIn reads the "Resets in <duration>" suffix. The bool is false
// when there is none, which is a normal outcome, not an error: the caller
// has a default window.
func parseResetsIn(text string) (time.Duration, bool) {
	m := resetsInPattern.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	raw := m[1] + m[2] + m[3]
	if raw == "" {
		return 0, false
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

func quotaDetail(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && quotaPattern.MatchString(line) {
			return domain.TruncateHead(line, quotaDetailMax)
		}
	}
	return "Antigravity quota reached"
}
