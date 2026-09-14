package indexer

import (
	"path/filepath"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
)

// indexableExtensions is the source-file allowlist. Every entry here has a
// symbol-aware chunker in chunker.DefaultRegistry; adding an extension without
// one still works but degrades that language to fixed-size slices.
var indexableExtensions = map[string]struct{}{
	".go":    {},
	".ts":    {},
	".tsx":   {},
	".mts":   {},
	".cts":   {},
	".js":    {},
	".jsx":   {},
	".mjs":   {},
	".cjs":   {},
	".py":    {},
	".java":  {},
	".kt":    {},
	".kts":   {},
	".swift": {},
}

func WalkIndexableFiles(root string) ([]string, error) {
	paths, err := mapper.Walk(root, mapper.WalkOptions{UseGitignore: true})
	if err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(paths))
	for _, rel := range paths {
		ext := strings.ToLower(filepath.Ext(rel))
		if _, ok := indexableExtensions[ext]; ok {
			filtered = append(filtered, rel)
		}
	}
	return filtered, nil
}

func IsIndexablePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := indexableExtensions[ext]
	return ok
}
