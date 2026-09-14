package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// FileChange is one generated file to land in a repository: its path relative
// to the repository root, its whole body, and the git file mode it must end up
// with.
type FileChange struct {
	Path string
	Body string
	// Mode is the unix mode the file needs. Only the executable bit is
	// meaningful to git, which stores exactly two blob modes; anything with an
	// owner-execute bit becomes 100755 and everything else 100644.
	Mode uint32
}

// gitMode maps a unix mode onto the two blob modes a git tree can hold.
func (f FileChange) gitMode() string {
	if f.Mode&0o100 != 0 {
		return "100755"
	}
	return "100644"
}

// CommitFiles writes files onto branch as ONE commit and returns the commit
// SHA, or ("", false, nil) when every file already has exactly that content
// and mode.
//
// It goes through the git data API (blob → tree → commit → ref) rather than
// the contents API, for a reason the contents API cannot be talked out of: it
// creates every file 100644, and a release script that is not executable is a
// release that fails on the machine that runs it directly. The tree entry is
// the only place the mode can be stated.
//
// The ref update is a plain fast-forward — never forced. A branch that moved
// between the read and the write comes back as GitHub's own 422, which is the
// correct answer: something else pushed, and this commit was computed against
// a parent that is no longer the tip.
//
// The no-change case returns before any write. A release started twice against
// an unchanged binding must not leave two empty commits on the user's default
// branch, and comparing the trees is what makes the write idempotent.
func CommitFiles(ctx context.Context, token, owner, repo, branch, message string, files []FileChange) (string, bool, error) {
	return commitFilesAt(ctx, "", token, owner, repo, branch, message, files)
}

func commitFilesAt(ctx context.Context, base, token, owner, repo, branch, message string, files []FileChange) (string, bool, error) {
	if len(files) == 0 {
		return "", false, nil
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", false, fmt.Errorf("github: committing generated files needs a branch")
	}

	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	refPath := repoPath(owner, repo) + "/git/ref/heads/" + refSegments(branch)
	if err := doJSONAt(ctx, base, token, http.MethodGet, refPath, nil, &ref); err != nil {
		return "", false, fmt.Errorf("github: reading %s: %w", branch, err)
	}
	if ref.Object.SHA == "" {
		return "", false, fmt.Errorf("github: %s has no commit to build on", branch)
	}

	var head struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := doJSONAt(ctx, base, token, http.MethodGet,
		repoPath(owner, repo)+"/git/commits/"+url.PathEscape(ref.Object.SHA), nil, &head); err != nil {
		return "", false, fmt.Errorf("github: reading the tip commit of %s: %w", branch, err)
	}

	entries := make([]map[string]string, 0, len(files))
	for _, file := range files {
		var blob struct {
			SHA string `json:"sha"`
		}
		// Sent base64 rather than as a utf-8 string: the generated script is
		// shell, and a body GitHub had to interpret as text is a body it could
		// re-encode.
		if err := doJSONAt(ctx, base, token, http.MethodPost, repoPath(owner, repo)+"/git/blobs", map[string]string{
			"content":  base64.StdEncoding.EncodeToString([]byte(file.Body)),
			"encoding": "base64",
		}, &blob); err != nil {
			return "", false, fmt.Errorf("github: writing %s: %w", file.Path, err)
		}
		entries = append(entries, map[string]string{
			"path": file.Path,
			"mode": file.gitMode(),
			"type": "blob",
			"sha":  blob.SHA,
		})
	}

	var tree struct {
		SHA string `json:"sha"`
	}
	if err := doJSONAt(ctx, base, token, http.MethodPost, repoPath(owner, repo)+"/git/trees", map[string]any{
		"base_tree": head.Tree.SHA,
		"tree":      entries,
	}, &tree); err != nil {
		return "", false, fmt.Errorf("github: building the release tree: %w", err)
	}
	if tree.SHA == head.Tree.SHA {
		return "", false, nil
	}

	var commit struct {
		SHA string `json:"sha"`
	}
	if err := doJSONAt(ctx, base, token, http.MethodPost, repoPath(owner, repo)+"/git/commits", map[string]any{
		"message": message,
		"tree":    tree.SHA,
		"parents": []string{ref.Object.SHA},
	}, &commit); err != nil {
		return "", false, fmt.Errorf("github: committing the release files: %w", err)
	}
	if err := doJSONAt(ctx, base, token, http.MethodPatch,
		repoPath(owner, repo)+"/git/refs/heads/"+refSegments(branch),
		map[string]any{"sha": commit.SHA, "force": false}, nil); err != nil {
		return "", false, fmt.Errorf("github: pointing %s at the release commit: %w", branch, err)
	}
	return commit.SHA, true, nil
}

// refSegments escapes a branch name for a URL PATH one segment at a time: a
// ref is a path (`feature/tt-1`), so escaping the whole name would turn its
// slashes into %2F and address a branch that does not exist, while leaving it
// raw would let a crafted name steer the request out of the ref namespace.
func refSegments(branch string) string {
	parts := strings.Split(branch, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
