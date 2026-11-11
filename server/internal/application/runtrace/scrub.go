package runtrace

import (
	"regexp"
	"strings"
)

const redacted = "[redacted]"

var (
	pemBlock    = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`)
	urlUserinfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.\-]*://)[^/\s:@]+(?::[^/\s@]*)?@`)
	knownToken  = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_\-]{16,}|AKIA[0-9A-Z]{16}|xox[abprs]-[A-Za-z0-9\-]{10,}|AIza[0-9A-Za-z_\-]{30,}|glpat-[A-Za-z0-9_\-]{16,}|npm_[A-Za-z0-9]{30,}|eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,})`)
	bearer      = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._\-~+/]{8,}=*`)
	secretPair  = regexp.MustCompile(`(?i)((?:authorization|x-api-key|api[-_]?key|apikey|token|secret|password|passwd|pwd|access[-_]?key|client[-_]?secret|private[-_]?key)["']?\s*[:=]\s*["']?)([^\s"'&;,)]+)`)
	secretFlag  = regexp.MustCompile(`(?i)(\s--?(?:token|password|passwd|secret|api-?key|access-?key|auth|bearer)[a-z-]*\s+)(?:"[^"]*"|'[^']*'|\S+)`)
)

// Scrub masks credentials in a command line or its output. It works on the
// spelling of the secret, not its value, because the trace never learns the
// values: a pattern that misses one is a leak, so the set is deliberately
// broad and errs towards masking.
func Scrub(s string) string {
	if s == "" {
		return s
	}
	s = pemBlock.ReplaceAllString(s, redacted)
	s = urlUserinfo.ReplaceAllString(s, "${1}"+redacted+"@")
	s = knownToken.ReplaceAllString(s, redacted)
	s = bearer.ReplaceAllString(s, "${1} "+redacted)
	s = secretFlag.ReplaceAllString(s, "${1}"+redacted)
	s = secretPair.ReplaceAllStringFunc(s, func(m string) string {
		sub := secretPair.FindStringSubmatch(m)
		if strings.EqualFold(sub[2], redacted) || sub[2] == redacted {
			return m
		}
		return sub[1] + redacted
	})
	return s
}
