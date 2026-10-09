package issuesync

import "regexp"

// jiraKeyPattern, jiraProjectKeyPattern and quoteJQL duplicate the jira
// adapter's: application must not import adapter, and this package builds its
// own JQL.
var jiraKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,19}-[1-9][0-9]{0,9}$`)
var jiraProjectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,19}$`)

func quoteJQL(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '"')
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		if r == '\\' || r == '"' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	out = append(out, '"')
	return string(out)
}
