package domain

import "unicode/utf8"

// Truncation here is byte-based on purpose — the limits it serves (Postgres
// column sizes, provider payload budgets) count bytes — but it must never cut
// through a rune: mid-rune UTF-8 makes Postgres reject the write ("invalid byte
// sequence") and providers reject the message. Every string truncated here is
// LLM output, a git diff or user text — routinely non-ASCII, and always so in
// Turkish, Chinese and the like.

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

// TruncateTail keeps the last maxBytes bytes without splitting a rune. Prefer
// it for command output: compilers, test runners and package managers print
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
