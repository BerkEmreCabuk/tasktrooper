package antigravity

import (
	"regexp"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var quotaPattern = regexp.MustCompile(`(?i)quota reached|resource_exhausted`)

var resetsInPattern = regexp.MustCompile(`(?i)resets in\s+([0-9]+h)?([0-9]+m)?([0-9]+s)?`)

const quotaDetailMax = 300

func quotaBlockFrom(out core.Outcome, stderrTail, sessionID string, now time.Time) *domain.QuotaBlock {
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
