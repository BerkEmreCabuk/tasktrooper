package code

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// maxOverlayParseFiles bounds per-query live parsing. Files beyond the cap
// still count as stale (their index rows are filtered out) but are not
// parsed, so a huge unindexed delta degrades to grep-visibility only.
const maxOverlayParseFiles = 64

// workspaceOverlay makes unpushed/unindexed edits visible to index-backed
// tools at query time, without embedding anything: stale index rows are
// filtered out and the touched files are re-parsed live instead.
type workspaceOverlay struct {
	root  string
	stale map[string]struct{}
	live  []string

	reg    *chunker.Registry
	parsed bool
	chunks []chunker.Chunk
}

// buildOverlay diffs the live workspace against the commit the index was
// built from (falling back to uncommitted-only via git status when the index
// carries no commit stamp). Returns nil when there is nothing to overlay or
// the workspace state cannot be determined — callers then behave exactly as
// before the overlay existed.
//
// It is OFF where the workspaces are remote, and that is not an optimisation.
// The overlay shells `git` in the session's workspace directory and reads the
// changed files' bytes back as tool results, which is a filesystem read on a
// path this process does not own: in the cloud that path is either the
// assignee's Mac (meaningless here) or another customer's clone on a shared
// volume. It is also
// what made the claim that codebase_search and expand_symbol_context are "pure
// index reads" — the stated reason those two survive the cloud tool gate —
// false. With the overlay off, that claim is true.
func buildOverlay(ctx context.Context, kit *ToolKit, idx domain.WorkspaceIndex) *workspaceOverlay {
	if kit == nil {
		return nil
	}
	reg := kit.Chunkers
	root := registry.EffectiveWorkspaceDir(ctx)
	if root == "" || reg == nil {
		return nil
	}
	paths, err := gitChangedPaths(ctx, root, idx.CommitSHA)
	if err != nil || len(paths) == 0 {
		return nil
	}

	ov := &workspaceOverlay{root: root, stale: make(map[string]struct{}, len(paths)), reg: reg}
	for _, rel := range paths {
		ov.stale[rel] = struct{}{}
		if !indexer.IsIndexablePath(rel) {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(root, rel)); statErr != nil {
			continue // deleted: stale-filter only, nothing to parse
		}
		if len(ov.live) < maxOverlayParseFiles {
			ov.live = append(ov.live, rel)
		}
	}
	return ov
}

// gitChangedPaths lists workspace-relative paths that differ from baseSHA,
// or from HEAD (uncommitted + untracked) when baseSHA is empty.
func gitChangedPaths(ctx context.Context, root, baseSHA string) ([]string, error) {
	var paths []string
	if baseSHA != "" {
		out, err := exec.CommandContext(ctx, "git", "-C", root, "diff", "--name-status", "--no-renames", "-z", baseSHA).Output()
		if err != nil {
			return nil, err
		}
		fields := splitNul(out)
		for i := 0; i+1 < len(fields); i += 2 {
			paths = append(paths, fields[i+1])
		}
		untracked, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--others", "--exclude-standard", "-z").Output()
		if err != nil {
			return nil, err
		}
		paths = append(paths, splitNul(untracked)...)
		return paths, nil
	}

	out, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain", "--no-renames", "-z").Output()
	if err != nil {
		return nil, err
	}
	for _, entry := range splitNul(out) {
		if len(entry) > 3 {
			paths = append(paths, entry[3:])
		}
	}
	return paths, nil
}

func splitNul(out []byte) []string {
	var fields []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			fields = append(fields, f)
		}
	}
	return fields
}

func (o *workspaceOverlay) isStale(relPath string) bool {
	if o == nil {
		return false
	}
	_, ok := o.stale[relPath]
	return ok
}

func (o *workspaceOverlay) liveChunks() []chunker.Chunk {
	if o.parsed {
		return o.chunks
	}
	o.parsed = true
	for _, rel := range o.live {
		content, err := os.ReadFile(filepath.Join(o.root, rel))
		if err != nil {
			continue
		}
		chunks, err := o.reg.Chunk(rel, content)
		if err != nil {
			continue // mid-edit syntax errors: file stays grep-visible only
		}
		o.chunks = append(o.chunks, chunks...)
	}
	return o.chunks
}

// symbolsNamed returns live symbols with the exact name, shaped like index
// rows so callers can merge them with store results transparently.
func (o *workspaceOverlay) symbolsNamed(name string) []domain.WorkspaceSymbol {
	if o == nil {
		return nil
	}
	var out []domain.WorkspaceSymbol
	seen := make(map[string]struct{})
	for _, ch := range o.liveChunks() {
		if ch.SymbolName != name || ch.Kind == "block" {
			continue
		}
		key := ch.FilePath + ":" + ch.SymbolName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, domain.WorkspaceSymbol{
			ID:        uuid.New(),
			FilePath:  ch.FilePath,
			Kind:      ch.Kind,
			Name:      ch.SymbolName,
			Signature: ch.Signature,
			StartLine: ch.StartLine,
			EndLine:   ch.EndLine,
		})
	}
	return out
}

// chunkFor serves a live chunk for a stale file's symbol, replacing the
// outdated GetChunkBySymbol row.
func (o *workspaceOverlay) chunkFor(relPath, symbolName string) (chunker.Chunk, bool) {
	if o == nil {
		return chunker.Chunk{}, false
	}
	for _, ch := range o.liveChunks() {
		if ch.FilePath == relPath && ch.SymbolName == symbolName {
			return ch, true
		}
	}
	return chunker.Chunk{}, false
}

func (o *workspaceOverlay) liveEdges() []domain.WorkspaceEdge {
	if o == nil {
		return nil
	}
	var edges []domain.WorkspaceEdge
	byFile := make(map[string][]chunker.Chunk)
	for _, ch := range o.liveChunks() {
		byFile[ch.FilePath] = append(byFile[ch.FilePath], ch)
	}
	for _, rel := range o.live {
		content, err := os.ReadFile(filepath.Join(o.root, rel))
		if err != nil {
			continue
		}
		edges = append(edges, indexer.BuildFileEdges(rel, content, byFile[rel])...)
	}
	return edges
}

// searchLive keyword-scores live chunks against the query. No embeddings:
// unpushed code is matched lexically until the post-push pass embeds it.
func (o *workspaceOverlay) searchLive(query string, limit int) []chunker.Chunk {
	if o == nil || limit <= 0 {
		return nil
	}
	tokens := queryTokens(query)
	if len(tokens) == 0 {
		return nil
	}
	type scored struct {
		chunk chunker.Chunk
		score int
	}
	var hits []scored
	for _, ch := range o.liveChunks() {
		name := strings.ToLower(ch.SymbolName)
		sig := strings.ToLower(ch.Signature)
		content := strings.ToLower(ch.Content)
		score := 0
		for _, tok := range tokens {
			switch {
			case strings.Contains(name, tok):
				score += 3
			case strings.Contains(sig, tok):
				score += 2
			case strings.Contains(content, tok):
				score++
			}
		}
		if score > 0 {
			hits = append(hits, scored{chunk: ch, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if hits[i].chunk.FilePath != hits[j].chunk.FilePath {
			return hits[i].chunk.FilePath < hits[j].chunk.FilePath
		}
		return hits[i].chunk.StartLine < hits[j].chunk.StartLine
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]chunker.Chunk, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.chunk)
	}
	return out
}

var overlayStopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "func": {}, "function": {},
	"code": {}, "file": {}, "with": {}, "that": {},
}

func queryTokens(query string) []string {
	raw := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '_')
	})
	seen := make(map[string]struct{}, len(raw))
	var tokens []string
	for _, tok := range raw {
		if len(tok) < 3 {
			continue
		}
		if _, stop := overlayStopwords[tok]; stop {
			continue
		}
		if _, dup := seen[tok]; dup {
			continue
		}
		seen[tok] = struct{}{}
		tokens = append(tokens, tok)
	}
	return tokens
}
