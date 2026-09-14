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

// maxEvidencePerSection caps how many paths one section may cite. Evidence is
// there to be checked; a section citing forty files is citing none.
const maxEvidencePerSection = 8

// validateEvidence keeps the citations that resolve to a real file inside the
// working copy and reports the ones that do not.
//
// Path traversal is refused rather than cleaned: an evidence path escaping the
// repository is either a confused model or a probe, and neither should end up
// stored as a fact about the codebase.
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
			// An absolute path can still be inside the working copy — accept
			// it only after rewriting it to a repo-relative one.
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

// changedPathsSince lists the repo-relative files that moved between base and
// the current HEAD. An unknown base commit (history rewritten, shallow clone)
// yields nothing, which the caller reads as "no scoped work to do" and falls
// back to the age gate — better than refreshing everything on a bad diff.
func changedPathsSince(ctx context.Context, root, base string) []string {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(base) == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "diff", "--name-only", base+"..HEAD") //nolint:gosec // base is a stored commit sha
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

// headCommit stamps a section with the commit it was written against, so a
// later reader can tell how far the tree has moved since.
func headCommit(ctx context.Context, root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "rev-parse", "--short", "HEAD") //nolint:gosec // fixed binary and args
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
