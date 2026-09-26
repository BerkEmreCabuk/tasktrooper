package catalogrepo

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// PromptDir implements port.PromptSource for a local AGENT_CATALOG_REPO
// directory that carries its own system/ tree alongside agents/. It reads
// straight off disk on every call rather than caching — a local directory
// is meant to be edited live, unlike the git-cloned agents/ path.
type PromptDir struct {
	Dir string
}

func (p *PromptDir) Open(_ context.Context) (fs.FS, string, error) {
	root := filepath.Join(p.Dir, "system")
	st, err := os.Stat(root)
	if err != nil {
		return nil, "", err
	}
	if !st.IsDir() {
		return nil, "", fmt.Errorf("catalog prompt overlay: %s is not a directory", root)
	}
	return os.DirFS(root), LocalDirMarker + p.Dir, nil
}

var _ port.PromptSource = (*PromptDir)(nil)
