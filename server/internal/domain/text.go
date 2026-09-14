package domain

import "unicode/utf8"

// Truncation here is byte-based on purpose — the limits it serves are Postgres
// column sizes and provider payload budgets, both of which count bytes — but it
// must never cut through a rune.
//
// The failure it prevents, seen repeatedly: a byte slice that lands mid-rune
// produces invalid UTF-8. Postgres rejects the write with "invalid byte
// sequence for encoding UTF8", so the activity step or run row never lands, and
// providers reject the message outright. Every string this codebase truncates is
// LLM output, a git diff or user text — routinely non-ASCII, and always so in
// Turkish, which is the default language here.

// TruncateHead keeps the first maxBytes bytes without splitting a rune.
func TruncateHead(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// TruncateTail keeps the last maxBytes bytes without splitting a rune. Prefer it
// for command output: compilers, test runners and package managers print
// progress first and the diagnosis last.
func TruncateTail(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := len(s) - maxBytes
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return s[cut:]
}
