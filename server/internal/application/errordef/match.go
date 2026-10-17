package errordef

import (
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	maxRegexLen = 512
	// maxRegexProg bounds the compiled program: RE2 runs in linear time, but
	// a pattern like (a{1000}){1000} still compiles to a program too large
	// to run on every group of every inbox read, times every definition.
	maxRegexProg = 1000
	// maxMatchText is how much of a message or a sample a pattern reads.
	maxMatchText   = 4 << 10
	regexCacheSize = 1024
)

// facts is what a definition is matched against, read off one error group.
// Providers report a message, a sample and a source; the exception type,
// endpoint, status and key=value attributes are read out of those the way
// runtimes and loggers print them.
type facts struct {
	fingerprint   string
	message       string
	sample        string
	exceptionType string
	method        string
	endpoint      string
	status        int
	attributes    map[string]string
}

var (
	exceptionRe = regexp.MustCompile(`^\s*([A-Za-z_$][\w$]*(?:(?:\.|::|\\)[A-Za-z_$][\w$]*)*)\s*(?::|\()`)
	endpointRe  = regexp.MustCompile(`\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(/[^\s?"'#]*)`)
	statusRe    = regexp.MustCompile(`(?i)\b(?:status(?:[ _]?code)?|http(?:[ _]status)?)\s*[:=]?\s*([1-5]\d\d)\b`)
	reqStatusRe = regexp.MustCompile(`\b(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+/[^\s]*\s+([1-5]\d\d)\b`)
	attributeRe = regexp.MustCompile(`\b([A-Za-z_][\w.\-]*)=("[^"]*"|[^\s,;]+)`)
)

func factsOf(g domain.OpsErrorGroup) facts {
	f := facts{
		fingerprint: g.Fingerprint,
		message:     clip(g.Message),
		sample:      clip(g.Sample),
		attributes:  map[string]string{},
	}
	texts := []string{f.message, f.sample}
	f.exceptionType = exceptionType(f.message, f.sample)
	for _, t := range texts {
		if m := endpointRe.FindStringSubmatch(t); m != nil {
			f.method, f.endpoint = m[1], m[2]
			break
		}
	}
	for _, t := range texts {
		if f.status = statusIn(t); f.status != 0 {
			break
		}
	}
	for _, m := range attributeRe.FindAllStringSubmatch(f.message, 50) {
		f.attributes[m[1]] = strings.Trim(m[2], `"`)
	}
	if g.Source != "" {
		f.attributes["source"] = g.Source
	}
	if g.Provider != "" {
		f.attributes["provider"] = string(g.Provider)
	}
	return f
}

func clip(s string) string {
	if len(s) > maxMatchText {
		return s[:maxMatchText]
	}
	return s
}

// exceptionType is the identifier before the first ":" or "(" of the
// message's first line, else of the sample's last line (a Python traceback
// ends with it, and starts with "Traceback (") or its first.
func exceptionType(message, sample string) string {
	lines := []string{firstLine(message), lastLine(sample), firstLine(sample)}
	for _, l := range lines {
		if m := exceptionRe.FindStringSubmatch(l); m != nil {
			return m[1]
		}
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n\r\t ")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func statusIn(text string) int {
	for _, re := range []*regexp.Regexp{statusRe, reqStatusRe} {
		if m := re.FindStringSubmatch(text); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				return n
			}
		}
	}
	return 0
}

// matches holds when every field the match sets holds for f.
func (c *regexCache) matches(m domain.ErrorMatch, f facts) bool {
	if m.Empty() {
		return false
	}
	if m.Fingerprint != "" && m.Fingerprint != f.fingerprint {
		return false
	}
	if m.MessageRegex != "" {
		re := c.get(m.MessageRegex)
		if re == nil || !(re.MatchString(f.message) || re.MatchString(f.sample)) {
			return false
		}
	}
	if m.ExceptionType != "" && !sameException(m.ExceptionType, f.exceptionType) {
		return false
	}
	if m.Endpoint != "" && !endpointMatches(m.Endpoint, f.method, f.endpoint) {
		return false
	}
	if m.Status != 0 && m.Status != f.status {
		return false
	}
	for k, v := range m.Attributes {
		if got, ok := f.attributes[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// sameException: "NullPointerException" names "java.lang.NullPointerException"
// too, and "Errno::ENOENT" or "App\\NotFound" the same way.
func sameException(want, got string) bool {
	if got == "" {
		return false
	}
	if want == got {
		return true
	}
	for _, sep := range []string{".", "::", `\`} {
		if strings.HasSuffix(got, sep+want) {
			return true
		}
	}
	return false
}

// endpointMatches: "POST /api/users" also names the method; a trailing "*"
// matches any rest of the path; elsewhere a "*", ":id" or "{id}" segment
// matches any one segment.
func endpointMatches(pattern, method, path string) bool {
	if path == "" {
		return false
	}
	pattern = strings.TrimSpace(pattern)
	if sp := strings.IndexByte(pattern, ' '); sp > 0 {
		if !strings.EqualFold(pattern[:sp], method) {
			return false
		}
		pattern = strings.TrimSpace(pattern[sp+1:])
	}
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(path, prefix)
	}
	want := strings.Split(strings.Trim(pattern, "/"), "/")
	got := strings.Split(strings.Trim(path, "/"), "/")
	if len(want) != len(got) {
		return false
	}
	for i, seg := range want {
		if seg == "*" || strings.HasPrefix(seg, ":") || (strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")) {
			continue
		}
		if seg != got[i] {
			return false
		}
	}
	return true
}

// regexCache compiles each pattern once; a pattern that does not compile is
// cached as nil, which matches nothing.
type regexCache struct {
	mu    sync.RWMutex
	cache map[string]*regexp.Regexp
}

func newRegexCache() *regexCache {
	return &regexCache{cache: map[string]*regexp.Regexp{}}
}

func (c *regexCache) get(pattern string) *regexp.Regexp {
	c.mu.RLock()
	re, ok := c.cache[pattern]
	c.mu.RUnlock()
	if ok {
		return re
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if re, ok := c.cache[pattern]; ok {
		return re
	}
	if len(c.cache) >= regexCacheSize {
		c.cache = map[string]*regexp.Regexp{}
	}
	re, err := compileBounded(pattern)
	if err != nil {
		re = nil
	}
	c.cache[pattern] = re
	return re
}

// compileBounded refuses a pattern longer than maxRegexLen or one whose
// program is larger than maxRegexProg instructions.
func compileBounded(pattern string) (*regexp.Regexp, error) {
	if len(pattern) > maxRegexLen {
		return nil, errRegexTooLong
	}
	parsed, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, err
	}
	prog, err := syntax.Compile(parsed.Simplify())
	if err != nil {
		return nil, err
	}
	if len(prog.Inst) > maxRegexProg {
		return nil, errRegexTooLarge
	}
	return regexp.Compile(pattern)
}
