package ci

import "strings"

// tokenize is a small shell-words splitter: single/double quotes group
// tokens, backslash escapes the next rune. It reports false for anything it
// cannot make sense of (an unterminated quote or a trailing backslash), and
// the caller skips the segment rather than guess.
func tokenize(s string) ([]string, bool) {
	var tokens []string
	var cur strings.Builder
	inSingle, inDouble, started := false, false, false
	runes := []rune(s)

	flush := func() {
		if started {
			tokens = append(tokens, cur.String())
			cur.Reset()
			started = false
		}
	}

	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			} else {
				cur.WriteRune(c)
			}
		case inDouble:
			if c == '"' {
				inDouble = false
			} else if c == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\') {
				i++
				cur.WriteRune(runes[i])
			} else {
				cur.WriteRune(c)
			}
		case c == '\'':
			inSingle, started = true, true
		case c == '"':
			inDouble, started = true, true
		case c == '\\':
			if i+1 >= len(runes) {
				return nil, false
			}
			i++
			cur.WriteRune(runes[i])
			started = true
		case c == ' ' || c == '\t':
			flush()
		default:
			cur.WriteRune(c)
			started = true
		}
	}
	if inSingle || inDouble {
		return nil, false
	}
	flush()
	return tokens, true
}

// splitTopLevel splits s on sep, ignoring occurrences inside single or double
// quotes.
func splitTopLevel(s, sep string) []string {
	var out []string
	var cur strings.Builder
	inSingle, inDouble := false, false
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case inSingle:
			cur.WriteByte(c)
			if c == '\'' {
				inSingle = false
			}
			i++
		case inDouble:
			cur.WriteByte(c)
			if c == '"' {
				inDouble = false
			}
			i++
		case c == '\'':
			inSingle = true
			cur.WriteByte(c)
			i++
		case c == '"':
			inDouble = true
			cur.WriteByte(c)
			i++
		case strings.HasPrefix(s[i:], sep):
			out = append(out, cur.String())
			cur.Reset()
			i += len(sep)
		default:
			cur.WriteByte(c)
			i++
		}
	}
	out = append(out, cur.String())
	return out
}

// unquote strips one layer of matching quotes a cd target may be wrapped in.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
