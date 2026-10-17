package datasource

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var promMetricPattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// promQLDialect: a param is escaped for a double-quoted string, which is
// right only for an equality matcher's value — inside a regex matcher the
// value would be a pattern, and anywhere outside a string it would be raw
// PromQL. $__rate and $__interval are durations, so they go inside [...];
// $__quantile is a number, so it goes where neither a string, a selector
// nor a range is.
type promQLDialect struct{}

func (promQLDialect) validMetricName(name string) bool { return promMetricPattern.MatchString(name) }

func (promQLDialect) check(subject, query string, declared map[string]bool) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	seen := map[string]bool{}
	for _, use := range placeholderUses(query) {
		if seen[use.name] || use.harmless(declared) {
			continue
		}
		switch {
		case use.name == builtinQuantile:
			if use.quote != 0 || use.inBraces || use.inBrackets {
				seen[use.name] = true
				add("%s: $%s may appear only where a number goes, outside strings, {...} and [...]", subject, use.name)
			}
		case builtinPlaceholders[use.name]:
			if use.quote != 0 || !use.inBrackets {
				seen[use.name] = true
				add("%s: $%s may appear only inside [...]", subject, use.name)
			}
		case !declared[use.name]:
			seen[use.name] = true
			add("%s uses $%s, which is neither a declared param nor a built-in", subject, use.name)
		case !use.inEqualityMatcher():
			seen[use.name] = true
			add("%s: $%s must sit inside a double-quoted label value matched with = or !=", subject, use.name)
		}
	}
	return problems
}

func (promQLDialect) render(query string, params map[string]string, step time.Duration, quantile float64) (string, error) {
	return expand(query, params, func(name string) (string, bool) {
		switch name {
		case builtinRate:
			return promDuration(RateWindow(step)), true
		case builtinInterval:
			return promDuration(step), true
		case builtinQuantile:
			return strconv.FormatFloat(quantile, 'f', -1, 64), true
		}
		return "", false
	}, escapeQuoted)
}

func promDuration(d time.Duration) string {
	return fmt.Sprintf("%ds", max(int64(d/time.Second), 1))
}

var quotedEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)

func escapeQuoted(s string) string { return quotedEscaper.Replace(s) }

type placeholderUse struct {
	name       string
	quote      byte
	op         string
	inBraces   bool
	inBrackets bool
	inComment  bool
}

// harmless: a use in a comment is read by nothing but the expansion, which
// still needs the name to be declared or built in.
func (u placeholderUse) harmless(declared map[string]bool) bool {
	return u.inComment && (declared[u.name] || builtinPlaceholders[u.name] || u.name == builtinQuantile)
}

func (u placeholderUse) inEqualityMatcher() bool {
	return u.quote == '"' && u.inBraces && (u.op == "=" || u.op == "!=")
}

// placeholderUses reads a # comment to the end of its line as text: a quote
// there opens no string, and a $name there is a use inComment.
func placeholderUses(q string) []placeholderUse {
	var uses []placeholderUse
	braces, brackets := 0, 0
	for i := 0; i < len(q); i++ {
		switch c := q[i]; c {
		case '#':
			for i < len(q) && q[i] != '\n' {
				if q[i] == '$' {
					if name := identAt(q, i+1); name != "" {
						uses = append(uses, placeholderUse{name: name, inComment: true})
						i += len(name)
					}
				}
				i++
			}
		case '"', '\'', '`':
			op := matcherOpBefore(q, i)
			j := i + 1
			for ; j < len(q) && q[j] != c; j++ {
				if q[j] == '\\' && c != '`' {
					j++
					continue
				}
				if q[j] == '$' {
					if name := identAt(q, j+1); name != "" {
						uses = append(uses, placeholderUse{name: name, quote: c, op: op, inBraces: braces > 0, inBrackets: brackets > 0})
						j += len(name)
					}
				}
			}
			i = j
		case '{':
			braces++
		case '}':
			braces = max(braces-1, 0)
		case '[':
			brackets++
		case ']':
			brackets = max(brackets-1, 0)
		case '$':
			if name := identAt(q, i+1); name != "" {
				uses = append(uses, placeholderUse{name: name, inBraces: braces > 0, inBrackets: brackets > 0})
				i += len(name)
			}
		}
	}
	return uses
}

// matcherOpBefore is the operator right before the string starting at i:
// "=", "!=", "=~", "!~", or "" (a comparison like == or >=, or none).
func matcherOpBefore(q string, i int) string {
	k := i - 1
	for k >= 0 && (q[k] == ' ' || q[k] == '\t' || q[k] == '\n') {
		k--
	}
	if k < 0 {
		return ""
	}
	prev := byte(0)
	if k > 0 {
		prev = q[k-1]
	}
	switch {
	case q[k] == '~' && (prev == '=' || prev == '!'):
		return string([]byte{prev, '~'})
	case q[k] == '=' && prev == '!':
		return "!="
	case q[k] == '=' && prev != '=' && prev != '<' && prev != '>':
		return "="
	}
	return ""
}

func identAt(q string, i int) string {
	j := i
	for j < len(q) {
		c := q[j]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (j > i && c >= '0' && c <= '9') {
			j++
			continue
		}
		break
	}
	return q[i:j]
}
