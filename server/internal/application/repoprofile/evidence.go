package repoprofile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const maxEvidencePerSection = 8

func validateEvidence(root string, evidence []domain.ProfileEvidence) (valid []domain.ProfileEvidence, invalid []string) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, e := range evidence {
		rel := strings.TrimSpace(e.Path)
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		if len(valid) >= maxEvidencePerSection {
			break
		}

		clean := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(rel, "./")))
		if filepath.IsAbs(clean) {

			if r, rerr := filepath.Rel(absRoot, clean); rerr == nil && !strings.HasPrefix(r, "..") {
				clean = r
			} else {
				invalid = append(invalid, rel)
				continue
			}
		}
		if strings.HasPrefix(clean, "..") {
			invalid = append(invalid, rel)
			continue
		}
		if _, serr := os.Stat(filepath.Join(absRoot, clean)); serr != nil {
			invalid = append(invalid, rel)
			continue
		}
		e.Path = filepath.ToSlash(clean)
		if e.Line < 0 {
			e.Line = 0
		}
		valid = append(valid, e)
	}
	return valid, invalid
}

func changedPathsSince(ctx context.Context, root, base string) []string {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(base) == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "diff", "--name-only", base+"..HEAD") //nolint:gosec
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if p := strings.TrimSpace(line); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

func headCommit(ctx context.Context, root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "rev-parse", "--short", "HEAD") //nolint:gosec
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
