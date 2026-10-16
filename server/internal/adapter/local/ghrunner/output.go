package ghrunner

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
)

var (
	listeningLine = regexp.MustCompile(`Listening for Jobs`)
	runningLine   = regexp.MustCompile(`Running job: (.+?)\s*$`)
	completedLine = regexp.MustCompile(`Job (.+?) completed with result: (\w+)`)
)

type event struct {
	kind   string // "listening" | "running" | "completed"
	job    string
	result string
}

func parseLine(line string) (event, bool) {
	if m := completedLine.FindStringSubmatch(line); m != nil {
		return event{kind: "completed", job: m[1], result: m[2]}, true
	}
	if m := runningLine.FindStringSubmatch(line); m != nil {
		return event{kind: "running", job: m[1]}, true
	}
	if listeningLine.MatchString(line) {
		return event{kind: "listening"}, true
	}
	return event{}, false
}

const tailLines = 12

// output is the runner's stdout and stderr: each whole line goes to onEvent
// when it is one the status follows, and the last few are kept for the
// sentence shown when the runner stops on its own.
type output struct {
	mu      sync.Mutex
	partial []byte
	tail    []string
	onEvent func(event)
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	o.partial = append(o.partial, p...)
	var lines []string
	for {
		i := bytes.IndexByte(o.partial, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(o.partial[:i]), "\r")
		o.partial = o.partial[i+1:]
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
		o.tail = append(o.tail, line)
		if len(o.tail) > tailLines {
			o.tail = o.tail[len(o.tail)-tailLines:]
		}
	}
	o.mu.Unlock()
	for _, line := range lines {
		if ev, ok := parseLine(line); ok && o.onEvent != nil {
			o.onEvent(ev)
		}
	}
	return len(p), nil
}

func (o *output) lastLines() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.Join(o.tail, "\n")
}
