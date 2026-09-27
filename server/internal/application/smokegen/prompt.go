package smokegen

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var systemPromptKey = prompt.Define[struct{}]("smokegen.system", struct{}{})

// systemPrompt is the fixed smoke-check drafting instruction; see
// catalog/system/prompts/smokegen/system.md.
var systemPrompt = prompt.Text(systemPromptKey)

type userPromptInput struct {
	RepoName, ComponentDir string
	HasBrief               bool
	Brief                  string
	ExistingLines          []string
}

var userPromptKey = prompt.Define("smokegen.user", userPromptInput{
	RepoName: "sample-repo", ComponentDir: "server", HasBrief: true, Brief: "Go + Postgres.",
	ExistingLines: []string{"- GET /health"},
})

func userPrompt(comp domain.Component, repo domain.Repository, brief string, existing []domain.SmokeCheck) string {
	dir := comp.Path
	if dir == "." {
		dir = "the repository root"
	}
	var existingLines []string
	for _, c := range existing {
		existingLines = append(existingLines, fmt.Sprintf("- %s %s", c.Method, c.Path))
	}
	return userPromptKey.Render(userPromptInput{
		RepoName: repo.Name, ComponentDir: dir,
		HasBrief: strings.TrimSpace(brief) != "", Brief: brief,
		ExistingLines: existingLines,
	})
}

// parseChecks accepts the object the prompt asks for, and tolerates the two
// ways models drift from it: prose or code fences around the object, and a
// bare array instead of {"checks": [...]}.
func parseChecks(raw string) ([]domain.SmokeCheck, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, errors.New("empty answer")
	}
	start := strings.IndexAny(text, "{[")
	if start < 0 {
		return nil, errors.New("no JSON in the answer")
	}
	if text[start] == '[' {
		end := strings.LastIndex(text, "]")
		if end < start {
			return nil, errors.New("unterminated JSON array")
		}
		var checks []domain.SmokeCheck
		if err := json.Unmarshal([]byte(text[start:end+1]), &checks); err != nil {
			return nil, err
		}
		return checks, nil
	}
	end := strings.LastIndex(text, "}")
	if end < start {
		return nil, errors.New("unterminated JSON object")
	}
	var out struct {
		Checks *[]domain.SmokeCheck `json:"checks"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return nil, err
	}
	if out.Checks == nil {
		return nil, errors.New(`the object has no "checks" field`)
	}
	return *out.Checks, nil
}

// filterChecks keeps the proposals that would pass the delivery profile's own
// validation, are not already present, and fit under limit; everything else
// is counted as dropped so the UI can say the agent over-delivered.
func filterChecks(proposed, existing []domain.SmokeCheck, limit int) ([]domain.SmokeCheck, int) {
	seen := make(map[string]bool, len(existing)+len(proposed))
	for _, c := range existing {
		seen[checkKey(c.Normalized())] = true
	}
	kept := make([]domain.SmokeCheck, 0, len(proposed))
	dropped := 0
	for _, c := range proposed {
		c = c.Normalized()
		if c.Method == "HEAD" {
			c.Contains = ""
		}
		if domain.ValidateSmokeChecks([]domain.SmokeCheck{c}) != nil {
			dropped++
			continue
		}
		key := checkKey(c)
		if seen[key] || len(kept) >= limit {
			dropped++
			continue
		}
		seen[key] = true
		kept = append(kept, c)
	}
	return kept, dropped
}

func checkKey(c domain.SmokeCheck) string {
	return c.Method + " " + strings.TrimRight(c.Path, "/")
}
