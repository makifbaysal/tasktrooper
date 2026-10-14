package findings

import (
	"regexp"
	"strings"
)

// Block rules read a handler's whole body rather than one line: an empty
// catch, and a catch that neither logs, rethrows nor reports — the error is
// gone and nothing says it happened.

const maxBlockLines = 80

var (
	catchOpen = re(`(^|[^.\w$])catch\b\s*(\(([^)]*)\))?([^{;]*)\{`)
	// handledRe is anything that gets the error in front of a person or a
	// caller: a log line, a rethrow, a report, a UI message, a callback.
	handledRe        = re(`(?i)\b(log|logs|logger|logging|console|print\w*|puts|throw|throws|rethrow|raise|reject|panic|fatal\w*|abort|exit|capture\w*|crashlytics|record(Error|Exception)|report\w*|sentry|bugsnag|notify\w*|toast\w*|alert\w*|snackbar|show\w*(Error|Message|Dialog)\w*|set\w*Error\w*|error|errors|onError|fail\w*|Timber|NSLog|os_log|emit|next|reply|status|warn\w*|assert\w*|failure|completion|callback|cb|done|resolve|interrupt|cancel\w*|Result)\b`)
	identRe          = re(`\$?[A-Za-z_]\w*`)
	swiftCatchBinder = re(`\blet\s+(\w+)`)
	intentionalName  = re(`(?i)^(_\w*|ignored?|expected|unused|ok)$`)
	pyExcept         = re(`^(\s*)except\b([^:]*):\s*(.*)$`)
	pyExceptAs       = re(`\bas\s+(\w+)`)
	pyHandledRe      = re(`(?i)\b(log|logs|logger|logging|print|raise|warn\w*|sentry|capture\w*|report\w*|traceback|exit|abort|error|errors|fail\w*|abort|messages)\b`)
	pyExpected       = re(`\b(ImportError|ModuleNotFoundError|KeyboardInterrupt|StopIteration|StopAsyncIteration|GeneratorExit|CancelledError)\b`)
	goErrOpen        = re(`\bif\s+([^{]*;\s*)?\w*[eE]rr\w*\s*!=\s*nil\s*\{\s*(\})?\s*$`)
	rubyRescue       = re(`^\s*rescue\b`)
	pyRequestsCall   = re(`\b(requests\.(get|post|put|patch|delete|head|options|request)|urllib\.request\.urlopen|urlopen)\s*\(`)
)

type blockFinding struct {
	rule string
	line int
	text string
}

func scanBlocks(l lang, lines []string, skip []bool) []blockFinding {
	switch {
	case l == langPython:
		return pythonBlocks(lines, skip)
	case l == langGo:
		return goBlocks(lines, skip)
	case l == langRuby:
		return rubyBlocks(lines, skip)
	case l.cLike():
		return catchBlocks(lines, skip)
	}
	return nil
}

func catchBlocks(lines []string, skip []bool) []blockFinding {
	var out []blockFinding
	for i, line := range lines {
		if skip[i] || !strings.Contains(line, "catch") {
			continue
		}
		loc := catchOpen.FindStringSubmatchIndex(line)
		if loc == nil {
			continue
		}
		params := ""
		if loc[6] >= 0 {
			params = line[loc[6]:loc[7]]
		}
		trailer := ""
		if loc[8] >= 0 {
			trailer = line[loc[8]:loc[9]]
		}
		body, ok := blockBody(lines, i, loc[1])
		if !ok {
			continue
		}
		// A Swift catch that names an error pattern (catch APIError.http(404, _),
		// catch let e where …) handles exactly that case on purpose.
		if t := strings.TrimSpace(trailer); t != "" && (!strings.HasPrefix(t, "let ") || strings.Contains(t, " where ")) {
			continue
		}
		names := catchNames(params, trailer)
		if intentional(names) {
			continue
		}
		code, commented := stripComments(body)
		switch {
		case strings.TrimSpace(code) == "":
			if !commented {
				out = append(out, blockFinding{rule: RuleEmptyCatchBlock, line: i, text: line})
			}
		case !handledRe.MatchString(code) && !mentionsAny(code, names):
			out = append(out, blockFinding{rule: RuleCatchWithoutLogging, line: i, text: line})
		}
	}
	return out
}

// catchNames are the names the caught error is bound to. A Swift catch with
// no pattern binds `error` implicitly.
func catchNames(params, trailer string) []string {
	var names []string
	for _, id := range identRe.FindAllString(params, -1) {
		if isTypeLike(id) || id == "final" || id == "var" || id == "val" || id == "let" {
			continue
		}
		names = append(names, id)
	}
	if m := swiftCatchBinder.FindStringSubmatch(trailer); m != nil {
		names = append(names, m[1])
	}
	if params == "" && strings.TrimSpace(trailer) == "" {
		names = append(names, "error")
	}
	return names
}

func isTypeLike(id string) bool {
	id = strings.TrimPrefix(id, "$")
	return id == "" || (id[0] >= 'A' && id[0] <= 'Z')
}

func intentional(names []string) bool {
	for _, n := range names {
		if intentionalName.MatchString(strings.TrimPrefix(n, "$")) {
			return true
		}
	}
	return false
}

func mentionsAny(code string, names []string) bool {
	for _, n := range names {
		if regexp.MustCompile(`(^|[^\w$])` + regexp.QuoteMeta(n) + `\b`).MatchString(code) {
			return true
		}
	}
	return false
}

// blockBody returns what sits between the { that ends at line[i][col-1] and
// its matching }, string- and line-comment-aware, giving up past
// maxBlockLines.
func blockBody(lines []string, i, col int) (string, bool) {
	var b strings.Builder
	depth := 1
	for li := i; li < len(lines) && li < i+maxBlockLines; li++ {
		line := lines[li]
		start := 0
		if li == i {
			start = col
		}
		var quote byte
		for ci := start; ci < len(line); ci++ {
			c := line[ci]
			if quote != 0 {
				if c == '\\' {
					ci++
					continue
				}
				if c == quote {
					quote = 0
				}
				b.WriteByte(c)
				continue
			}
			switch c {
			case '"', '\'', '`':
				quote = c
			case '/':
				if ci+1 < len(line) && line[ci+1] == '/' {
					b.WriteString(line[ci:])
					ci = len(line)
					continue
				}
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return b.String(), true
				}
			}
			b.WriteByte(c)
		}
		b.WriteByte('\n')
	}
	return "", false
}

var blockComment = re(`(?s)/\*.*?\*/`)

// stripComments drops // and /* */ comments, reporting whether there were
// any: an empty catch that says why it is empty is a decision, not a bug.
func stripComments(body string) (string, bool) {
	commented := false
	if blockComment.MatchString(body) {
		commented = true
		body = blockComment.ReplaceAllString(body, "")
	}
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			commented = true
			line = line[:idx]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), commented
}

func pythonBlocks(lines []string, skip []bool) []blockFinding {
	var out []blockFinding
	for i, line := range lines {
		if skip[i] || !strings.Contains(line, "except") {
			continue
		}
		m := pyExcept.FindStringSubmatch(line)
		if m == nil || pyExpected.MatchString(m[2]) {
			continue
		}
		indent := len(m[1])
		var body []string
		commented := false
		if inline := strings.TrimSpace(m[3]); inline != "" && !strings.HasPrefix(inline, "#") {
			body = append(body, inline)
		} else {
			for j := i + 1; j < len(lines) && j < i+maxBlockLines; j++ {
				t := strings.TrimSpace(lines[j])
				if t == "" {
					continue
				}
				if len(lines[j])-len(strings.TrimLeft(lines[j], " \t")) <= indent {
					break
				}
				if strings.HasPrefix(t, "#") {
					commented = true
					continue
				}
				body = append(body, t)
			}
		}
		if strings.Contains(m[3], "#") {
			commented = true
		}
		var name []string
		if as := pyExceptAs.FindStringSubmatch(m[2]); as != nil {
			name = append(name, as[1])
		}
		code := strings.Join(body, "\n")
		switch {
		case code == "pass" || code == "..." || code == "":
			if !commented {
				out = append(out, blockFinding{rule: RuleEmptyCatchBlock, line: i, text: line})
			}
		case !pyHandledRe.MatchString(code) && !mentionsAny(code, name):
			out = append(out, blockFinding{rule: RuleCatchWithoutLogging, line: i, text: line})
		}
	}
	return out
}

func goBlocks(lines []string, skip []bool) []blockFinding {
	var out []blockFinding
	for i, line := range lines {
		if skip[i] || !strings.Contains(line, "nil") {
			continue
		}
		m := goErrOpen.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		empty := m[2] != ""
		if !empty {
			for j := i + 1; j < len(lines) && j < i+maxBlockLines; j++ {
				t := strings.TrimSpace(lines[j])
				if t == "" {
					continue
				}
				empty = strings.HasPrefix(t, "}")
				break
			}
		}
		if empty {
			out = append(out, blockFinding{rule: RuleEmptyCatchBlock, line: i, text: line})
		}
	}
	return out
}

func rubyBlocks(lines []string, skip []bool) []blockFinding {
	var out []blockFinding
	for i, line := range lines {
		if skip[i] || !rubyRescue.MatchString(line) {
			continue
		}
		for j := i + 1; j < len(lines) && j < i+maxBlockLines; j++ {
			t := strings.TrimSpace(lines[j])
			if t == "" {
				continue
			}
			if t == "end" {
				out = append(out, blockFinding{rule: RuleEmptyCatchBlock, line: i, text: line})
			}
			break
		}
	}
	return out
}

// pythonRequestsWithoutTimeout reads each requests/urlopen call to its
// closing parenthesis, since keyword arguments usually wrap onto later lines.
func pythonRequestsWithoutTimeout(lines []string, skip []bool) []blockFinding {
	var out []blockFinding
	for i, line := range lines {
		if skip[i] || (!strings.Contains(line, "requests.") && !strings.Contains(line, "urlopen")) {
			continue
		}
		loc := pyRequestsCall.FindStringIndex(line)
		if loc == nil {
			continue
		}
		call := callText(lines, i, loc[1])
		if !strings.Contains(call, "timeout") {
			out = append(out, blockFinding{rule: RuleHTTPClientWithoutTimeout, line: i, text: line})
		}
	}
	return out
}

func callText(lines []string, i, col int) string {
	var b strings.Builder
	depth := 1
	for li := i; li < len(lines) && li < i+12; li++ {
		line := lines[li]
		start := 0
		if li == i {
			start = col
		}
		for ci := start; ci < len(line); ci++ {
			switch line[ci] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return b.String()
				}
			}
			b.WriteByte(line[ci])
		}
		b.WriteByte('\n')
	}
	return b.String()
}
