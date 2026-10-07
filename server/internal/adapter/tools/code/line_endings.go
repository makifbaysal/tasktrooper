package code

import (
	"io"
	"os"
	"strings"
)

// Line endings are the file's, not the model's. A Windows checkout with
// core.autocrlf=true hands every text file over with CRLF, while read_file
// shows the model bare lines and everything it sends back uses LF: a
// multi-line old_string then never matches, and a rewrite flips every line of
// the file in the diff.

const (
	eolLF   = "\n"
	eolCRLF = "\r\n"
)

// eolSniffBytes bounds how much of an existing file write_file reads to learn
// its convention; the first few thousand lines settle it.
const eolSniffBytes = 64 << 10

// dominantEOL is CRLF when most of content's line breaks are CRLF, and LF
// otherwise, including for content with no line break at all.
func dominantEOL(content string) string {
	crlf := strings.Count(content, eolCRLF)
	if crlf > 0 && 2*crlf > strings.Count(content, eolLF) {
		return eolCRLF
	}
	return eolLF
}

func withEOL(s, eol string) string {
	lf := strings.ReplaceAll(s, eolCRLF, eolLF)
	if eol == eolLF {
		return lf
	}
	return strings.ReplaceAll(lf, eolLF, eol)
}

// fileEOL is the convention of the file at path, or "" when it has no line
// break to judge by (or cannot be read).
func fileEOL(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head, err := io.ReadAll(io.LimitReader(f, eolSniffBytes))
	if err != nil || !strings.Contains(string(head), eolLF) {
		return ""
	}
	return dominantEOL(string(head))
}
