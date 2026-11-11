package domain

import (
	"errors"
	"regexp"
	"strings"
)

const CommitCoauthorMaxLen = 200

const commitCoauthorTrailerPrefix = "Co-authored-by:"

var commitCoauthorRe = regexp.MustCompile(`^([^<>\r\n]+?)\s*<([^<>\s@]+@[^<>\s@]+\.[^<>\s@]+)>$`)

var ErrInvalidCommitCoauthor = errors.New(`commit co-author must be a single line like "Name <email@example.com>"`)

// NormalizeCommitCoauthor accepts "Name <email>" or a full "Co-authored-by:"
// trailer and returns the bare "Name <email>" form; empty stays empty. Any
// control character, or an address that is not name+email, is rejected so the
// value can never inject a second trailer or header line into a commit.
func NormalizeCommitCoauthor(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	if len(v) > CommitCoauthorMaxLen {
		return "", ErrInvalidCommitCoauthor
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f || r == 0x2028 || r == 0x2029 || r == 0x85 {
			return "", ErrInvalidCommitCoauthor
		}
	}
	if len(v) >= len(commitCoauthorTrailerPrefix) && strings.EqualFold(v[:len(commitCoauthorTrailerPrefix)], commitCoauthorTrailerPrefix) {
		v = strings.TrimSpace(v[len(commitCoauthorTrailerPrefix):])
	}
	m := commitCoauthorRe.FindStringSubmatch(v)
	if m == nil {
		return "", ErrInvalidCommitCoauthor
	}
	return strings.TrimSpace(m[1]) + " <" + m[2] + ">", nil
}
