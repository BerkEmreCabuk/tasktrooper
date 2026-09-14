package indexer

import "strings"

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
