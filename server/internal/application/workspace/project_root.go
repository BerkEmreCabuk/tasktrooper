package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateProjectRoot resolves a caller-supplied project root and refuses
// anything outside the roots this tenant may register.
//
// The allowlist is [this tenant's workspace subtree] + the operator's
// configured allowed_roots, and the first term is why an EMPTY allowed_roots is
// not a wildcard. It used to be: `len(allowedRoots) == 0` returned the path
// unconditionally, which is the shipped cloud configuration, so
// POST /v1/repositories/open would index any readable directory on the pod —
// including another customer's clone — for whoever asked. An empty allowlist
// now means what an empty allowlist means everywhere else here: nothing extra.
//
// allowed_roots therefore only ever WIDENS the set, and exists for the
// self-hosted install that keeps its checkouts outside the managed workspace.
// A deployment that wants that back has to say so.
func ValidateProjectRoot(ctx context.Context, path, workspaceRoot string, allowedRoots []string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("project root is empty")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project root not accessible: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project root is not a directory")
	}

	// The tenant's own subtree comes first and is not optional. Resolving it
	// can fail only when the context carries no identity, and that is a
	// refusal: a request nobody can be identified for must not be able to
	// register a path.
	tenantRoot, err := TenantRoot(ctx, workspaceRoot)
	if err != nil {
		return "", err
	}
	roots := append([]string{tenantRoot}, allowedRoots...)
	for _, allowed := range roots {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if abs == allowedAbs || strings.HasPrefix(abs, allowedAbs+string(os.PathSeparator)) {
			return abs, nil
		}
	}
	// The path is not echoed back. On a shared volume the distinction between
	// "that directory is not yours" and "that directory does not exist" is an
	// oracle for enumerating other customers' repository names, and the caller
	// already knows what they asked for.
	return "", fmt.Errorf("project root is outside this workspace's allowed roots")
}

func EffectiveProjectRoot(sessionWorkspace, projectRoot string) string {
	if strings.TrimSpace(projectRoot) != "" {
		return projectRoot
	}
	return sessionWorkspace
}
