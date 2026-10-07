package mobilebuild

import (
	"bytes"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
)

const (
	// tailLines is how much of the transcript a failure's error carries.
	tailLines   = 200
	maxTailLine = 2000
	// maxPartial bounds a line that never ends. Cut there rather than held:
	// a reader that stops draining blocks the child on a full pipe.
	maxPartial = 1 << 20

	// redactMinLen is the length below which a value is not searched for: a
	// three-character secret matches half the words in a build log, and a
	// transcript of redaction markers hides the diagnosis, not the key.
	redactMinLen = 8
)

// alwaysRedact are name shapes scrubbed at ANY length. They are values a
// person chose — keytool takes a six-character password, an alias is `key0`
// out of Android Studio — and every one of them would be under the floor.
var alwaysRedact = []string{"_PASSWORD", "_SECRET", "_ALIAS"}

// newRedactor scrubs every secret value out of a line. The script masks
// nothing outside GitHub Actions, so on this machine the scrubbing is entirely
// this package's job.
func newRedactor(secrets map[string]string) func(string) string {
	type pair struct{ from, to string }
	var pairs []pair
	for name, value := range secrets {
		marker := "[redacted " + name + "]"
		minLen := redactMinLen
		if hasAnySuffix(name, alwaysRedact) {
			minLen = 1
		}
		kept, skipped := 0, 0
		seen := make(map[string]bool)
		for _, part := range secretParts(value) {
			if seen[part] {
				continue
			}
			seen[part] = true
			if len(part) < minLen {
				skipped++
				continue
			}
			kept++
			pairs = append(pairs, pair{from: part, to: marker})
		}
		// Said out loud: a secret nothing could be registered for goes through
		// the transcript in the clear, and that should be learnable from the
		// log rather than from wherever the transcript was pasted.
		switch {
		case kept == 0:
			log.Warn().Str("secret", name).Int("length", len(value)).
				Msg("mobile build: this secret is too short to scrub from the build log and will appear in it")
		case skipped > 0:
			log.Warn().Str("secret", name).Int("lines", skipped).
				Msg("mobile build: some lines of this secret are too short to scrub from the build log")
		}
	}
	// Longest first, so a value that contains another is replaced whole.
	sort.Slice(pairs, func(i, j int) bool {
		if len(pairs[i].from) != len(pairs[j].from) {
			return len(pairs[i].from) > len(pairs[j].from)
		}
		return pairs[i].from < pairs[j].from
	})
	return func(line string) string {
		for _, p := range pairs {
			line = strings.ReplaceAll(line, p.from, p.to)
		}
		return line
	}
}

// secretParts is a value and every physical line of it. The lines are the
// point: output is redacted line by line, so a wrapped base64 key printed
// whole arrives as several lines, none of which holds the whole value. A
// trailing \r goes, so a value stored with CRLF endings still matches.
func secretParts(value string) []string {
	parts := []string{value}
	if strings.Contains(value, "\n") {
		for _, line := range strings.Split(value, "\n") {
			if line = strings.TrimRight(line, "\r"); line != "" {
				parts = append(parts, line)
			}
		}
	}
	return parts
}

func hasAnySuffix(s string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

// transcript is the run's output after redaction: each line handed to the
// caller's Log as it arrives, and the last tailLines kept for the error.
// Nothing unredacted is stored or forwarded.
type transcript struct {
	redact func(string) string
	log    func(string)

	mu   sync.Mutex
	tail []string
}

func newTranscript(redact func(string) string, logf func(string)) *transcript {
	return &transcript{redact: redact, log: logf}
}

func (t *transcript) emit(raw string) {
	line := t.redact(strings.TrimRight(raw, "\r"))
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.log != nil {
		t.log(line)
	}
	t.tail = append(t.tail, clip(line, maxTailLine))
	if len(t.tail) > tailLines {
		t.tail = t.tail[len(t.tail)-tailLines:]
	}
}

func (t *transcript) tailSuffix() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.tail) == 0 {
		return ""
	}
	return "\n" + strings.Join(t.tail, "\n")
}

// writer is one stream's line splitter. stdout and stderr each get their own,
// so a partial line on one is never joined to the other's.
func (t *transcript) writer() *lineWriter {
	return &lineWriter{t: t}
}

type lineWriter struct {
	t       *transcript
	partial []byte
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.partial = append(w.partial, b...)
	rest := w.partial
	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			break
		}
		w.t.emit(string(rest[:i]))
		rest = rest[i+1:]
	}
	if len(rest) > maxPartial {
		w.t.emit(string(rest))
		rest = nil
	}
	w.partial = append(w.partial[:0], rest...)
	return len(b), nil
}

// flush emits a last line that never got its newline. Called after Wait, when
// os/exec's copying goroutine is done with the writer.
func (w *lineWriter) flush() {
	if len(w.partial) > 0 {
		w.t.emit(string(w.partial))
		w.partial = nil
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
