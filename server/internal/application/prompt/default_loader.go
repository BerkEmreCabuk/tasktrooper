package prompt

import "github.com/makifbaysal/tasktrooper/catalog"

// loadEmbeddedCatalog is the one place this package reaches into the
// catalog module — kept separate so library.go's parsing logic can be unit
// tested against an in-memory fs.FS without needing the embed.
func loadEmbeddedCatalog() (*Library, error) {
	return LoadFS(catalog.SystemFS())
}
