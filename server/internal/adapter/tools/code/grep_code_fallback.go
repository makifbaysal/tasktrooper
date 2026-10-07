package code

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
)

// binarySniffLen matches ripgrep's heuristic: a NUL in the first block means binary.
const binarySniffLen = 8 << 10

const fallbackMaxFileSize = 8 << 20

// searchWithoutRipgrep is grep_code on a machine with no rg on PATH — a stock
// Windows install has none. Failing there instead sent agents into read_file
// loops until the loop guard stopped the run. Go's regexp is RE2, which accepts
// the same syntax as ripgrep's default engine for everything but look-around
// and backreferences.
func searchWithoutRipgrep(ctx context.Context, root, searchPath string, args grepCodeArgs, maxResults int) ([]grepMatch, error) {
	expr := args.Pattern
	if !args.CaseSensitive {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	var glob *regexp.Regexp
	if args.Glob != "" {
		if glob, err = globRegexp(args.Glob, !args.CaseSensitive); err != nil {
			return nil, fmt.Errorf("invalid glob %q: %w", args.Glob, err)
		}
	}

	scope, err := filepath.Rel(root, searchPath)
	if err != nil {
		return nil, err
	}
	scope = filepath.ToSlash(scope)

	files, err := mapper.Walk(root, mapper.WalkOptions{UseGitignore: true})
	if err != nil {
		return nil, err
	}

	matches := []grepMatch{}
	for _, rel := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if scope != "." && rel != scope && !strings.HasPrefix(rel, scope+"/") {
			continue
		}
		if glob != nil && !globMatches(glob, args.Glob, rel) {
			continue
		}
		matches = append(matches, grepFile(filepath.Join(root, filepath.FromSlash(rel)), rel, re, maxResults-len(matches))...)
		if len(matches) >= maxResults {
			break
		}
	}
	return matches, nil
}

func grepFile(fullPath, rel string, re *regexp.Regexp, limit int) []grepMatch {
	info, err := os.Stat(fullPath)
	if err != nil || info.Size() > fallbackMaxFileSize {
		return nil
	}
	data, err := os.ReadFile(fullPath)
	if err != nil || bytes.IndexByte(data[:min(len(data), binarySniffLen)], 0) >= 0 {
		return nil
	}
	var out []grepMatch
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if !re.MatchString(line) {
			continue
		}
		out = append(out, grepMatch{FilePath: rel, Line: i + 1, Content: line})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// globMatches follows ripgrep: a glob without a slash matches the file name at
// any depth, one with a slash matches the whole relative path.
func globMatches(re *regexp.Regexp, glob, rel string) bool {
	if strings.Contains(glob, "/") {
		return re.MatchString(rel)
	}
	return re.MatchString(rel[strings.LastIndex(rel, "/")+1:])
}

func globRegexp(glob string, ignoreCase bool) (*regexp.Regexp, error) {
	var b strings.Builder
	if ignoreCase {
		b.WriteString("(?i)")
	}
	b.WriteString("^")
	glob = strings.TrimPrefix(glob, "/")
	depth := 0
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '{':
			b.WriteString("(?:")
			depth++
		case c == '}' && depth > 0:
			b.WriteString(")")
			depth--
		case c == ',' && depth > 0:
			b.WriteString("|")
		case c == '[':
			end := strings.IndexByte(glob[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := glob[i+1 : i+1+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + class + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
