package workspace

import (
	"fmt"
	"path/filepath"

	"github.com/google/uuid"
)

// reposDirName holds one mirror clone per repository, keyed by directory name.
// It is the only non-uuid child of the root the server creates on its own,
// which is what lets anything walking the root tell a clone from a checkout.
const reposDirName = "repos"

// TaskDir is a board task's own checkout: <root>/task-<task-uuid>.
//
// Creating a checkout and reaping one live in different packages, so both
// derive the path here: a reaper that derived it even slightly differently
// would either miss directories forever or remove the wrong one.
func TaskDir(configuredRoot string, taskID uuid.UUID) (string, error) {
	root, err := ResolveRoot(configuredRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "task-"+taskID.String()), nil
}

// ReposDir is where the mirror clones live: <root>/repos.
func ReposDir(configuredRoot string) (string, error) {
	root, err := ResolveRoot(configuredRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, reposDirName), nil
}

// RepoDir is one mirror clone: <root>/repos/<name>.
//
// name must be a single path component. It is joined onto the root, and a
// value that can climb out of repos/ would land on another repository's clone
// or a task's checkout.
func RepoDir(configuredRoot, name string) (string, error) {
	clean := CleanDirName(name)
	if clean == "" {
		return "", fmt.Errorf("repository directory name %q is not a single path component", name)
	}
	repos, err := ReposDir(configuredRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(repos, clean), nil
}

// WithinRoot reports whether path lies inside the workspace root.
//
// The error is not folded into the bool: a caller deciding whether to delete
// or adopt has to treat "could not tell" as a refusal, not as a false that
// reads like an answer.
func WithinRoot(configuredRoot, path string) (bool, error) {
	root, err := ResolveRoot(configuredRoot)
	if err != nil {
		return false, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("resolve path: %w", err)
	}
	return IsWithinRoot(abs, root)
}
