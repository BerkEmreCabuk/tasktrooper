package mapper

import (
	"strings"
)

const defaultMaxDocChars = 200

func FormatSkeleton(symbols []Symbol, maxDocChars int) string {
	if maxDocChars <= 0 {
		maxDocChars = defaultMaxDocChars
	}
	var b strings.Builder
	for i, sym := range symbols {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(formatSymbolLine(sym, maxDocChars))
	}
	return b.String()
}

func FormatFileSkeleton(sk FileSkeleton, maxDocChars int) string {
	if maxDocChars <= 0 {
		maxDocChars = defaultMaxDocChars
	}
	var b strings.Builder
	b.WriteString(sk.Path)
	if sk.Package != "" {
		b.WriteString(" (package ")
		b.WriteString(sk.Package)
		b.WriteByte(')')
	}
	b.WriteByte('\n')
	if sk.Doc != "" {
		b.WriteString(truncateDoc(sk.Doc, maxDocChars))
		b.WriteByte('\n')
	}
	if len(sk.Imports) > 0 {
		b.WriteString("imports: ")
		b.WriteString(strings.Join(sk.Imports, ", "))
		b.WriteByte('\n')
	}
	body := FormatSkeleton(sk.Symbols, maxDocChars)
	if body != "" {
		b.WriteString(body)
	}
	return b.String()
}

func formatSymbolLine(sym Symbol, maxDocChars int) string {
	var b strings.Builder
	b.WriteString(sym.Kind)
	b.WriteByte(' ')
	if sym.Receiver != "" {
		b.WriteByte('(')
		b.WriteString(sym.Receiver)
		b.WriteString(") ")
	}
	if sym.Signature != "" {
		b.WriteString(sym.Signature)
	} else {
		b.WriteString(sym.Name)
	}
	if sym.Doc != "" {
		b.WriteString(" — ")
		b.WriteString(truncateDoc(sym.Doc, maxDocChars))
	}
	return b.String()
}

func truncateDoc(doc string, maxDocChars int) string {
	doc = strings.TrimSpace(doc)
	doc = strings.Join(strings.Fields(doc), " ")
	if len(doc) <= maxDocChars {
		return doc
	}
	if maxDocChars <= 3 {
		return doc[:maxDocChars]
	}
	return doc[:maxDocChars-3] + "..."
}
