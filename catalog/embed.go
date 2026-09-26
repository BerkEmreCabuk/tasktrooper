// Package catalog embeds the LLM-facing prose the server renders from:
// prompts, guard wording, tool descriptions, and the partials they share.
// The bundled agent definitions (catalog/agents/**, read by the server's
// catalogrepo adapter and synced into Postgres) are a separate concern and
// are not embedded here — that sync runs async after boot and prompts must
// not depend on it.
package catalog

import (
	"embed"
	"io/fs"
)

//go:embed all:system
var System embed.FS

// SystemFS roots the embedded filesystem at system/, so callers see
// prompts/, guards/, tools/, partials/, schemas/ and README.md directly
// instead of having to join "system" onto every path.
func SystemFS() fs.FS {
	sub, err := fs.Sub(System, "system")
	if err != nil {
		panic("catalog: system subtree missing from embed: " + err.Error())
	}
	return sub
}
