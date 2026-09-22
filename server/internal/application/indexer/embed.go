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

const maxEmbedContentChars = 4000

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
