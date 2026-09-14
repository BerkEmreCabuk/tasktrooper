package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appName = "local-llm"

type Layout struct {
	Root       string
	Workspaces string
	Files      string
	Postgres   string
	Config     string
}

func Resolve() (*Layout, error) {
	root, err := dataRoot()
	if err != nil {
		return nil, err
	}
	layout := &Layout{
		Root:       root,
		Workspaces: filepath.Join(root, "workspaces"),
		Files:      filepath.Join(root, "files"),
		Postgres:   filepath.Join(root, "postgres"),
		Config:     filepath.Join(root, "config.yml"),
	}
	for _, dir := range []string{layout.Root, layout.Workspaces, layout.Files, layout.Postgres} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir %s: %w", dir, err)
		}
	}
	return layout, nil
}

func dataRoot() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", appName), nil
	case "linux":
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, appName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", appName), nil
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
