package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var taskKeySanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func ResolveRoot(configured string) (string, error) {
	root := strings.TrimSpace(configured)
	if root == "" {
		root = "./data/workspaces"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	return abs, nil
}

func SessionDir(root string, sessionID uuid.UUID) (string, error) {
	absRoot, err := ResolveRoot(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(absRoot, sessionID.String()), nil
}

// AgentDir, ajanın kalıcı çalışma klasörü: {root}/agents/{agent-id}. Aynı
// ajanın tüm oturumları bu klasörü paylaşır.
func AgentDir(root string, agentID uuid.UUID) (string, error) {
	absRoot, err := ResolveRoot(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(absRoot, "agents", agentID.String()), nil
}

// CleanDirName accepts only a plain single directory name. Anything with a
// separator, or any form of "..", is rejected rather than sanitised: the value
// is joined onto a workspace root, and a name that can climb out of it is a bug
// wherever it came from.
func CleanDirName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return ""
	}
	if strings.ContainsRune(trimmed, os.PathSeparator) || strings.ContainsRune(trimmed, '/') {
		return ""
	}
	return trimmed
}

// IsRepoCheckout reports whether dir is already a git checkout — the shape a
// board run prepares before any agent starts: the repository cloned into the
// run's own workspace with the task branch checked out.
//
// A worktree checkout stores .git as a file rather than a directory, so both
// count.
func IsRepoCheckout(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func SubtaskDir(sessionDir, taskKey string) (string, error) {
	if sessionDir == "" {
		return "", fmt.Errorf("session workspace is empty")
	}
	safe := SanitizeTaskKey(taskKey)
	if safe == "" {
		safe = "task"
	}
	return filepath.Join(sessionDir, safe), nil
}

func SanitizeTaskKey(taskKey string) string {
	safe := strings.TrimSpace(taskKey)
	safe = taskKeySanitizer.ReplaceAllString(safe, "_")
	safe = strings.Trim(safe, "_")
	return safe
}

func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func RemoveDir(path string) error {
	if path == "" {
		return nil
	}
	return os.RemoveAll(path)
}

// RemoveDirWithin is RemoveDir for a path that was computed rather than read
// back from a row: it refuses to delete anything that is not under root, and
// refuses to delete root itself.
//
// The distinction matters because the callers are new. RemoveDir has only ever
// been handed a stored session directory; a reaper hands it the result of
// joining a root with an ID, and a root that arrives empty turns that join into
// something rooted at the process's working directory. An os.RemoveAll one
// component too high erases every workspace on the volume, and nothing
// downstream would report it as anything but a missing checkout.
func RemoveDirWithin(root, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	absRoot, err := ResolveRoot(root)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	if abs == absRoot {
		return fmt.Errorf("refusing to remove the workspace root %q", absRoot)
	}
	ok, err := IsWithinRoot(abs, absRoot)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("refusing to remove %q: outside the workspace root", abs)
	}
	return os.RemoveAll(abs)
}
