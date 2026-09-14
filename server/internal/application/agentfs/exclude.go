package agentfs

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const excludeHeader = "# TaskTrooper agent catalog (materialised per run, not source)"

var excludePatterns = []string{
	claudeSkillsDir + "/" + ttPrefix + "*/",
	claudeAgentsDir + "/" + ttPrefix + "*.md",
	cursorRulesDir + "/" + ttPrefix + "*.mdc",
}

func Exclude(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("agentfs: root is empty")
	}

	if _, err := os.Stat(filepath.Join(root, ".git")); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("agentfs: stat .git in %s: %w", root, err)
	}

	infoDir := filepath.Join(root, ".git", "info")
	path := filepath.Join(infoDir, "exclude")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("agentfs: read %s: %w", path, err)
	}

	have := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(string(existing)))
	for scanner.Scan() {
		have[strings.TrimSpace(scanner.Text())] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("agentfs: scan %s: %w", path, err)
	}

	missing := make([]string, 0, len(excludePatterns))
	for _, p := range excludePatterns {
		if _, ok := have[p]; !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	if _, ok := have[excludeHeader]; !ok {
		b.WriteString(excludeHeader + "\n")
	}
	for _, p := range missing {
		b.WriteString(p + "\n")
	}

	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("agentfs: mkdir %s: %w", infoDir, err)
	}
	if _, err := writeIfChanged(path, b.String()); err != nil {
		return err
	}
	return nil
}
