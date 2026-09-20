package catalogrepo

import (
	"fmt"
	"strings"
)

// parseDoc is the SKILL.md frontmatter reader; the same format the seed
// catalog and self-evo writes use (application/catalog/seed_markdown.go).
func parseDoc(raw string) (map[string]string, string, error) {
	content := strings.TrimPrefix(raw, "\ufeff")
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return nil, "", fmt.Errorf("missing frontmatter opening ---")
	}
	meta := map[string]string{}
	bodyStart := -1
	for i := 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if line == "---" {
			bodyStart = i + 1
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return nil, "", fmt.Errorf("invalid frontmatter line %d: %q", i+1, line)
		}
		meta[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if bodyStart == -1 {
		return nil, "", fmt.Errorf("missing frontmatter closing ---")
	}
	body := strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
	if body == "" {
		return nil, "", fmt.Errorf("empty document body")
	}
	return meta, body, nil
}
