package chunker

import (
	"path/filepath"
	"strings"
)

const (
	fallbackMaxLines = 200
	fallbackOverlap  = 20
)

type FallbackChunker struct{}

func (FallbackChunker) Chunk(filePath string, content []byte) ([]Chunk, error) {
	text := string(content)
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	lines := strings.Split(text, "\n")
	total := len(lines)
	ext := filepath.Ext(filePath)
	lang := languageFromExt(ext)

	var chunks []Chunk
	for start := 0; start < total; {
		end := start + fallbackMaxLines
		if end > total {
			end = total
		}
		chunks = append(chunks, Chunk{
			FilePath:   filePath,
			Language:   lang,
			Kind:       "file",
			SymbolName: filepath.Base(filePath),
			StartLine:  start + 1,
			EndLine:    end,
			Content:    strings.Join(lines[start:end], "\n"),
		})
		if end >= total {
			break
		}
		next := end - fallbackOverlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks, nil
}
