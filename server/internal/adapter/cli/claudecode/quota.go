package claudecode

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var usageLimitPattern = regexp.MustCompile(`(?i)usage limit reached|usage limit exceeded|exceeded your (?:usage|rate) limit|usage credit limit reached|reached your weekly usage limit|hit your (?:usage )?limit|usage limit resets`)

var resetEpochPattern = regexp.MustCompile(`usage limit reached\s*\|\s*(\d{9,13})`)

var resumeMissingPattern = regexp.MustCompile(`(?i)no conversation found|no session found|session .{0,40}not found`)

func quotaBlockFrom(out outcome, stderrTail, sessionID string, now time.Time) *domain.QuotaBlock {
	if out.RateLimit != nil && out.RateLimit.Status == "rejected" {
		resumeAt := out.RateLimit.resetTime(now)
		if resumeAt.IsZero() {
			resumeAt = now.Add(domain.DefaultQuotaParkWindow)
		}
		return &domain.QuotaBlock{
			ResumeAt:     resumeAt,
			CLISessionID: sessionID,
			Detail:       fmt.Sprintf("agent cli usage limit (%s window) reached", out.RateLimit.RateLimitType),
			Provider:     domain.LLMProviderClaudeCode,
		}
	}
	if out.APIErrorStatus == 429 {
		resumeAt := out.RateLimit.resetTime(now)
		if resumeAt.IsZero() {
			resumeAt = now.Add(domain.DefaultQuotaParkWindow)
		}
		detail := "agent cli usage limit reached"
		if out.RateLimit != nil {
			detail = fmt.Sprintf("agent cli usage limit (%s window) reached", out.RateLimit.RateLimitType)
		}
		return &domain.QuotaBlock{
			ResumeAt:     resumeAt,
			CLISessionID: sessionID,
			Detail:       detail,
			Provider:     domain.LLMProviderClaudeCode,
		}
	}

	text := strings.Join([]string{out.Text, stderrTail}, "\n")
	if !usageLimitPattern.MatchString(text) {
		return nil
	}
	resumeAt, ok := parseResetEpoch(text)
	if !ok || !resumeAt.After(now) {
		if structured := out.RateLimit.resetTime(now); !structured.IsZero() {
			resumeAt = structured
		} else {
			resumeAt = now.Add(domain.DefaultQuotaParkWindow)
		}
	}
	return &domain.QuotaBlock{
		ResumeAt:     resumeAt,
		CLISessionID: sessionID,
		Detail:       quotaDetail(text),
		Provider:     domain.LLMProviderClaudeCode,
	}
}

func parseResetEpoch(text string) (time.Time, bool) {
	m := resetEpochPattern.FindStringSubmatch(text)
	if len(m) != 2 {
		return time.Time{}, false
	}
	raw, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || raw <= 0 {
		return time.Time{}, false
	}
	if len(m[1]) >= 13 {
		return time.UnixMilli(raw), true
	}
	return time.Unix(raw, 0), true
}

const quotaDetailMax = 300

func quotaDetail(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && usageLimitPattern.MatchString(line) {
			return domain.TruncateHead(line, quotaDetailMax)
		}
	}
	return "agent cli usage limit reached"
}
