package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type FileChange struct {
	Path string
	Body string
	Mode uint32
}

func (f FileChange) gitMode() string {
	if f.Mode&0o100 != 0 {
		return "100755"
	}
	return "100644"
}

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

func refSegments(branch string) string {
	parts := strings.Split(branch, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
