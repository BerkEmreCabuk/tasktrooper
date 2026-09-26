package port

import (
	"context"
	"io/fs"
)

// PromptSource resolves an overlay for the embedded prompt library: a
// system/ tree (see catalog/system/README.md) that lets an operator change
// prompt wording without a rebuild. label identifies the source for
// logging (e.g. "dir:/path/to/catalog").
type PromptSource interface {
	Open(ctx context.Context) (fsys fs.FS, label string, err error)
}
