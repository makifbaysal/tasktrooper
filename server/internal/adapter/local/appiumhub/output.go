package appiumhub

import (
	"bytes"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
)

const (
	tailLines = 12
	maxLine   = 300
	// maxPartial bounds a line that never ends: at Appium's default level a
	// page source is logged whole, and that is not worth holding.
	maxPartial = 64 << 10
)

// output forwards the hub's lines to the debug log — its default level logs
// every request, far too much for this process's own log — and keeps the
// last few, which are what explain a start that failed.
type output struct {
	mu      sync.Mutex
	partial []byte
	lines   []string
}

func (o *output) Write(b []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.partial = append(o.partial, b...)
	rest := o.partial
	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			break
		}
		o.line(string(rest[:i]))
		rest = rest[i+1:]
	}
	if len(rest) > maxPartial {
		o.line(string(rest))
		rest = nil
	}
	o.partial = append(o.partial[:0], rest...)
	return len(b), nil
}

func (o *output) line(s string) {
	s = strings.TrimRight(s, "\r")
	if strings.TrimSpace(s) == "" {
		return
	}
	log.Debug().Str("component", "appium").Msg(s)
	o.lines = append(o.lines, clip(s, maxLine))
	if len(o.lines) > tailLines {
		o.lines = o.lines[len(o.lines)-tailLines:]
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (o *output) tail() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	lines := append([]string(nil), o.lines...)
	if p := strings.TrimSpace(string(o.partial)); p != "" {
		lines = append(lines, clip(p, maxLine))
	}
	return strings.Join(lines, " | ")
}

func (o *output) suffix() string {
	if t := o.tail(); t != "" {
		return ": " + t
	}
	return ""
}
