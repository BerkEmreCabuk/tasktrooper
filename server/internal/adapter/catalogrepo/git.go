package catalogrepo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// git commands run through the system git. The token a private repo needs lives
// in the URL the operator sets in AGENT_CATALOG_REPO (or a credential helper) —
// never on argv, matching the no-secrets-on-argv rule. GIT_TERMINAL_PROMPT=0
// keeps an auth failure a clean error instead of a hung prompt.
func gitEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
}

func gitRun(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", redactURLs(strings.Join(args, " ")), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// redactURLs hides the password part of a credential-bearing clone URL before
// it reaches a log line. AGENT_CATALOG_REPO may embed a token for a private
// repo; that token never belongs on stderr.
func redactURLs(s string) string {
	re := regexp.MustCompile(`//([^/:@]+):([^@]+)@`)
	return re.ReplaceAllString(s, "//${1}:***@")
}

func gitClone(ctx context.Context, url, dest string) error {
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	_ = os.RemoveAll(tmp)
	if _, err := gitRun(ctx, "clone", "--depth", "1", url, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func gitPull(ctx context.Context, dir string) error {
	if _, err := gitRun(ctx, "-C", dir, "fetch", "origin"); err != nil {
		return err
	}
	if _, err := gitRun(ctx, "-C", dir, "reset", "--hard", "origin/HEAD"); err != nil {
		return err
	}
	return nil
}

func gitHead(ctx context.Context, dir string) (string, error) {
	return gitRun(ctx, "-C", dir, "rev-parse", "HEAD")
}
