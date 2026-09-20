package opencode

import (
	"regexp"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var rateLimitPattern = regexp.MustCompile(`(?i)rate.?limit.?exceeded|rate limit reached|rate_limit_exceeded|too many requests|\b429\b|quota exceeded`)

const quotaDetailMax = 300

func quotaBlockFrom(out core.Outcome, stderrTail, sessionID string, now time.Time) *domain.QuotaBlock {
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