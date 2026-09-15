package indexer

import (
	"strings"
	"unicode/utf8"
)

func FormatEmbedInput(filePath, symbolName, signature, content string) string {
	var b strings.Builder
	b.WriteString(filePath)
	if symbolName != "" {
		b.WriteByte(' ')
		b.WriteString(symbolName)
	}
	if signature != "" {
		b.WriteByte(' ')
		b.WriteString(signature)
	}
	b.WriteByte('\n')
	b.WriteString(content)
	return b.String()
}

// maxEmbedContentChars caps the chunk content sent for one embedding. The
// path, symbol and signature prefix always goes in whole; only the body is
// cut. Four thousand characters is about 900 tokens, which the local embedder
// answers in about a second under load, where a 10,000-character chunk took
// ten. The head of a symbol carries what retrieval needs.
const maxEmbedContentChars = 4000

// capEmbedContent cuts content to maxEmbedContentChars without splitting a
// UTF-8 sequence.
func capEmbedContent(content string) string {
	if len(content) <= maxEmbedContentChars {
		return content
	}
	cut := maxEmbedContentChars
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut]
}
