// Package prompt also holds the prompt library: LLM-facing prose (system
// prompts, guard wording, tool descriptions) moved out of Go string
// literals and into catalog/system/**, rendered from Go text/template with
// data supplied by the caller. See catalog/system/README.md for the file
// format and server/.ai/architecture.md's "Prompt library" section for how
// it is wired at runtime.
package prompt

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"text/template"

	"gopkg.in/yaml.v3"
)

type entryKind string

const (
	kindPrompt  entryKind = "prompt"
	kindGuard   entryKind = "guard"
	kindTool    entryKind = "tool"
	kindPartial entryKind = "partial"
)

type entry struct {
	key     string
	kind    entryKind
	version string
	inputs  []string
	schema  string
	body    string
	tmpl    *template.Template
}

// Library is a parsed, ready-to-render set of catalog/system/** files.
// A Library is safe for concurrent Render calls; it is immutable after
// LoadFS returns.
type Library struct {
	entries map[string]*entry
}

// LoadFS parses every prompts/, guards/, tools/ and partials/ file under
// fsys. README.md, schemas/** and dotfiles (e.g. a partials/.gitkeep
// placeholder) are not templates and are skipped. Every error names the
// file that caused it.
func LoadFS(fsys fs.FS) (*Library, error) {
	lib := &Library{entries: map[string]*entry{}}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := path.Base(p)
		if strings.HasPrefix(base, ".") || p == "README.md" || strings.HasPrefix(p, "schemas/") {
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		raw, readErr := fs.ReadFile(fsys, p)
		if readErr != nil {
			return fmt.Errorf("prompt: read %s: %w", p, readErr)
		}
		if loadErr := lib.load(p, string(raw)); loadErr != nil {
			return fmt.Errorf("prompt: %s: %w", p, loadErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return lib, nil
}

type frontMatter struct {
	Key     string   `yaml:"key"`
	Version string   `yaml:"version"`
	Inputs  []string `yaml:"inputs"`
	Schema  string   `yaml:"schema"`
}

func (l *Library) load(relPath, raw string) error {
	fmText, body, err := splitFrontMatter(raw)
	if err != nil {
		return err
	}
	var meta frontMatter
	if err := yaml.Unmarshal([]byte(fmText), &meta); err != nil {
		return fmt.Errorf("parse front matter: %w", err)
	}
	wantKey, kind, err := deriveKey(relPath)
	if err != nil {
		return err
	}
	if meta.Key != wantKey {
		return fmt.Errorf("front matter key %q does not match path-derived key %q", meta.Key, wantKey)
	}

	body = stripSingleTrailingNewline(body)
	tmpl, err := template.New(wantKey).Option("missingkey=error").Funcs(l.funcMap()).Parse(body)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	l.entries[wantKey] = &entry{
		key:     wantKey,
		kind:    kind,
		version: meta.Version,
		inputs:  meta.Inputs,
		schema:  meta.Schema,
		body:    body,
		tmpl:    tmpl,
	}
	return nil
}

// deriveKey maps a file's path under system/ to its render key, per
// catalog/system/README.md's table.
func deriveKey(relPath string) (string, entryKind, error) {
	switch {
	case strings.HasPrefix(relPath, "prompts/"):
		rest := strings.TrimSuffix(strings.TrimPrefix(relPath, "prompts/"), ".md")
		return strings.ReplaceAll(rest, "/", "."), kindPrompt, nil
	case strings.HasPrefix(relPath, "guards/"):
		name := strings.TrimSuffix(strings.TrimPrefix(relPath, "guards/"), ".md")
		return "guard." + name, kindGuard, nil
	case strings.HasPrefix(relPath, "tools/"):
		name := strings.TrimSuffix(strings.TrimPrefix(relPath, "tools/"), ".md")
		return "tool." + name, kindTool, nil
	case strings.HasPrefix(relPath, "partials/"):
		name := strings.TrimSuffix(strings.TrimPrefix(relPath, "partials/"), ".md")
		return "partial." + name, kindPartial, nil
	default:
		return "", "", fmt.Errorf("path must be under prompts/, guards/, tools/ or partials/")
	}
}

// splitFrontMatter separates the leading "---" YAML block from the body,
// preserving every byte of the body exactly as written (including line
// endings) so stripSingleTrailingNewline is the only normalization applied.
func splitFrontMatter(raw string) (fmText, body string, err error) {
	lines := strings.SplitAfter(raw, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return "", "", fmt.Errorf("missing front matter opening ---")
	}
	var fmLines []string
	closed := false
	idx := 1
	for ; idx < len(lines); idx++ {
		if strings.TrimRight(lines[idx], "\r\n") == "---" {
			closed = true
			idx++
			break
		}
		fmLines = append(fmLines, lines[idx])
	}
	if !closed {
		return "", "", fmt.Errorf("missing front matter closing ---")
	}
	return strings.Join(fmLines, ""), strings.Join(lines[idx:], ""), nil
}

// stripSingleTrailingNewline is the ONLY normalization applied to a body:
// exactly one trailing "\n" is removed if present, nothing else. See
// catalog/system/README.md, "Byte-exact bodies".
func stripSingleTrailingNewline(body string) string {
	return strings.TrimSuffix(body, "\n")
}

// Render executes the template registered under key against data.
func (l *Library) Render(key string, data any) (string, error) {
	e, ok := l.entries[key]
	if !ok {
		return "", fmt.Errorf("prompt: unknown key %q", key)
	}
	var buf strings.Builder
	if err := e.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("prompt: render %q: %w", key, err)
	}
	return buf.String(), nil
}

// MustRender is Render, panicking on error. A missing or broken prompt is a
// build defect: it belongs in a test failure, not a runtime error path.
func (l *Library) MustRender(key string, data any) string {
	s, err := l.Render(key, data)
	if err != nil {
		panic(err.Error())
	}
	return s
}

// KeyInfo describes one entry a Library loaded: its render key and which
// system/ subdirectory it came from ("prompt", "guard", "tool", "partial").
type KeyInfo struct {
	Name string
	Kind string
}

// Keys lists every entry this Library loaded, sorted by name. Used by the
// completeness test in internal/platform/runtime to find prompt/guard/tool
// files with no corresponding Define — partials are intentionally not
// required to have one, so callers filter by Kind.
func (l *Library) Keys() []KeyInfo {
	out := make([]KeyInfo, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, KeyInfo{Name: e.key, Kind: string(e.kind)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (l *Library) renderPartial(name string, data any) (string, error) {
	e, ok := l.entries["partial."+name]
	if !ok {
		return "", fmt.Errorf("prompt: partial %q not found", name)
	}
	var buf strings.Builder
	if err := e.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("prompt: partial %q: %w", name, err)
	}
	return buf.String(), nil
}

func (l *Library) funcMap() template.FuncMap {
	return template.FuncMap{
		"join":    joinFn,
		"bullets": bulletsFn,
		"trunc":   truncFn,
		"indent":  indentFn,
		"plural":  pluralFn,
		"lower":   strings.ToLower,
		"upper":   strings.ToUpper,
		"partial": l.renderPartial,
	}
}

func joinFn(sep string, items []string) string {
	return strings.Join(items, sep)
}

func bulletsFn(items []string) string {
	lines := make([]string, len(items))
	for i, it := range items {
		lines[i] = "- " + it
	}
	return strings.Join(lines, "\n")
}

func truncFn(n int, s string) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func indentFn(n int, s string) string {
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func pluralFn(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}

var (
	defaultMu   sync.RWMutex
	defaultOnce sync.Once
	defaultLib  *Library
)

// defaultLoader is overridden by tests that cannot depend on the real
// catalog module without an import cycle risk; production always loads
// catalog.SystemFS() (wired in default_loader.go, which is the only file
// allowed to import the catalog module, keeping that dependency in one
// place).
var defaultLoader = loadEmbeddedCatalog

// Default lazily loads the embedded catalog.SystemFS() exactly once and
// returns it on every later call, until SetDefault installs an overlay. A
// broken embedded prompt is a build defect: this panics, naming the file,
// rather than returning an error every caller would have to check.
func Default() *Library {
	defaultOnce.Do(func() {
		lib, err := defaultLoader()
		if err != nil {
			panic("prompt: load embedded catalog: " + err.Error())
		}
		defaultMu.Lock()
		defaultLib = lib
		defaultMu.Unlock()
	})
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultLib
}

// SetDefault installs lib as the library Key[T].Render/MustRender use from
// now on — the runtime overlay described in server/.ai/architecture.md.
func SetDefault(lib *Library) {
	defaultMu.Lock()
	defaultLib = lib
	defaultMu.Unlock()
	// Default's sync.Once must not fire the embedded load after an overlay
	// is installed directly (e.g. by a test, before Default() was ever
	// called).
	defaultOnce.Do(func() {})
}

// MustRender renders key against the process default library, panicking on
// error.
func MustRender(key string, data any) string {
	return Default().MustRender(key, data)
}

// registryMu guards registry; Define is normally called only from package
// init functions, but the lock makes concurrent registration (e.g. from
// tests running in parallel) safe rather than merely usual.
var (
	registryMu sync.Mutex
	registry   = map[string]DefinedKey{}
)

// DefinedKey is one registered typed key, with its sample input bound so a
// completeness test can render it against a candidate Library without
// knowing T.
type DefinedKey struct {
	Name string
	// RenderWith renders this key's sample input against lib.
	RenderWith func(lib *Library) (string, error)
}

// Key is a typed handle to a prompt template: name fixes which catalog
// entry it renders, T fixes what data that entry expects.
type Key[T any] struct {
	name   string
	sample T
}

// Define registers name with a sample input (used by the completeness test
// in internal/platform/runtime, and by nothing else at runtime) and returns
// a typed Key. Define panics if name is already registered — two keys with
// the same name is a programming error, not a runtime condition.
func Define[T any](name string, sample T) Key[T] {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("prompt: key %q already defined", name))
	}
	registry[name] = DefinedKey{
		Name: name,
		RenderWith: func(lib *Library) (string, error) {
			return lib.Render(name, sample)
		},
	}
	return Key[T]{name: name, sample: sample}
}

// Name returns the catalog key this Key renders.
func (k Key[T]) Name() string { return k.name }

// Render renders k against the process default library (Default(), or
// whatever SetDefault last installed), panicking on error — a missing or
// broken prompt is a build defect, caught by the completeness test in
// internal/platform/runtime, not something calling code should have to
// handle.
func (k Key[T]) Render(in T) string {
	return MustRender(k.name, in)
}

// Text renders a zero-input key. Define[struct{}](name, struct{}{}) at
// package scope to declare one.
func Text(k Key[struct{}]) string {
	return k.Render(struct{}{})
}

// DefinedKeys returns every key registered with Define, across every
// package that has been imported into this binary, sorted by name.
func DefinedKeys() []DefinedKey {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := make([]DefinedKey, 0, len(registry))
	for _, k := range registry {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
