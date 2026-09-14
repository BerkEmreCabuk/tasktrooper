package chunker

import (
	"path/filepath"
	"strings"
)

type Chunk struct {
	FilePath   string
	Language   string
	Kind       string
	SymbolName string
	StartLine  int
	EndLine    int
	Content    string
	Signature  string
}

type Chunker interface {
	Chunk(filePath string, content []byte) ([]Chunk, error)
}

type Registry struct {
	byExt    map[string]Chunker
	fallback Chunker
}

func NewRegistry() *Registry {
	return &Registry{
		byExt:    make(map[string]Chunker),
		fallback: FallbackChunker{},
	}
}

// DefaultRegistry wires one symbol-aware chunker per supported language. Every
// extension missing here still indexes, through FallbackChunker's fixed-size
// slices — it just loses symbol-level retrieval.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(".go", GoChunker{})
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts"} {
		r.Register(ext, TSChunker{})
	}
	r.Register(".py", PythonChunker{})
	r.Register(".java", JavaChunker{})
	for _, ext := range []string{".kt", ".kts"} {
		r.Register(ext, KotlinChunker{})
	}
	r.Register(".swift", SwiftChunker{})
	return r
}

func (r *Registry) Register(ext string, c Chunker) {
	r.byExt[strings.ToLower(ext)] = c
}

func (r *Registry) SetFallback(c Chunker) {
	r.fallback = c
}

func (r *Registry) Chunk(filePath string, content []byte) ([]Chunk, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if c, ok := r.byExt[ext]; ok {
		return c.Chunk(filePath, content)
	}
	return r.fallback.Chunk(filePath, content)
}

func languageFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".swift":
		return "swift"
	default:
		return "unknown"
	}
}

func extractLines(content string, startLine, endLine int) string {
	lines := strings.Split(content, "\n")
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > endLine || startLine > len(lines) {
		return ""
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}
