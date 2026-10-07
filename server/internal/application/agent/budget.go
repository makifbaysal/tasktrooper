package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	budgetWarningTurns      = 3
	runTokenWarnRatio       = 0.85
	repeatNoteThreshold     = 2
	repeatAbortThreshold    = 4
	errStreakNoteThreshold  = 3
	errStreakAbortThreshold = 8
	toolErrorNoteThreshold  = 5
	sameCallNoteThreshold   = 3
	sameCallAbortThreshold  = 8
	lastCallsKept           = 5
	topFailingToolsKept     = 3
)

type RunStats struct {
	Iterations         int
	ToolCalls          int
	ByTool             map[string]int
	RepeatedNoProgress int
	SkippedRepeats     int
	ToolErrors         int
	ErrorsByTool       map[string]int
	MaxErrorStreak     int
	LastCalls          []string
}

func (s RunStats) Summary() string {
	if s.ToolCalls == 0 {
		return fmt.Sprintf("%d iterations, no tool calls", s.Iterations)
	}
	parts := make([]string, 0, len(s.ByTool))
	for _, p := range rankCounts(s.ByTool, 0) {
		parts = append(parts, fmt.Sprintf("%s×%d", p.name, p.count))
	}
	out := fmt.Sprintf("%d iterations, %d tool calls [%s]", s.Iterations, s.ToolCalls, strings.Join(parts, " "))
	if s.RepeatedNoProgress > 0 {
		out += fmt.Sprintf(", %d repeated no-progress calls", s.RepeatedNoProgress)
	}
	if s.SkippedRepeats > 0 {
		out += fmt.Sprintf(", %d repeats answered without re-running", s.SkippedRepeats)
	}
	if s.ToolErrors > 0 {
		out += fmt.Sprintf(", %d tool errors (%s), longest failing streak %d",
			s.ToolErrors, s.FailurePattern(), s.MaxErrorStreak)
	}
	if len(s.LastCalls) > 0 {
		out += ", last: " + strings.Join(s.LastCalls, " → ")
	}
	return out
}

func (s RunStats) FailurePattern() string {
	ranked := rankCounts(s.ErrorsByTool, topFailingToolsKept)
	if len(ranked) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ranked))
	for _, p := range ranked {
		parts = append(parts, fmt.Sprintf("%s failed %d×", p.name, p.count))
	}
	return strings.Join(parts, ", ")
}

func (s RunStats) ErrorRate() float64 {
	if s.ToolCalls == 0 {
		return 0
	}
	return float64(s.ToolErrors) / float64(s.ToolCalls) * 100
}

type countPair struct {
	name  string
	count int
}

func rankCounts(counts map[string]int, top int) []countPair {
	pairs := make([]countPair, 0, len(counts))
	for name, count := range counts {
		pairs = append(pairs, countPair{name, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].name < pairs[j].name
	})
	if top > 0 && len(pairs) > top {
		pairs = pairs[:top]
	}
	return pairs
}

type runStatsCarrier interface {
	RunStats() RunStats
}

func StatsFromError(err error) (RunStats, bool) {
	for err != nil {
		if carrier, ok := err.(runStatsCarrier); ok {
			return carrier.RunStats(), true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return RunStats{}, false
		}
		err = unwrapped.Unwrap()
	}
	return RunStats{}, false
}

type giveUpCause struct {
	Stuck          bool
	DeadEnd        bool
	TokenExhausted bool
	TokensUsed     int
}

type BudgetExhaustedError struct {
	Budget         int
	Stats          RunStats
	Partial        string
	Stuck          bool
	DeadEnd        bool
	TokenExhausted bool
	TokensUsed     int
}

func (e *BudgetExhaustedError) Error() string {
	switch {
	case e.DeadEnd:
		return fmt.Sprintf("agent loop stopped: %d tool calls in a row failed (%s)", errStreakAbortThreshold, e.Stats.Summary())
	case e.Stuck:
		return fmt.Sprintf("agent loop stopped: repeated the same call with no new result (%s)", e.Stats.Summary())
	case e.TokenExhausted:
		return fmt.Sprintf("run token budget exhausted after %d tokens (%s)", e.TokensUsed, e.Stats.Summary())
	default:
		return fmt.Sprintf("agent loop exceeded maximum iterations (%d): %s", e.Budget, e.Stats.Summary())
	}
}

func (e *BudgetExhaustedError) RunStats() RunStats { return e.Stats }

type ChatFailedError struct {
	Stats RunStats
	Err   error
}

func (e *ChatFailedError) Error() string {
	if rl, ok := domain.RateLimitOf(e.Err); ok {
		return rl.UserMessage()
	}
	return fmt.Sprintf("llm chat failed: %v (%s)", e.Err, e.Stats.Summary())
}

func (e *ChatFailedError) Unwrap() error      { return e.Err }
func (e *ChatFailedError) RunStats() RunStats { return e.Stats }

type callTracker struct {
	seen      map[string]*callRecord
	lastKey   string
	errStreak int
	stats     RunStats
}

type callRecord struct {
	resultHash string
	repeats    int
	execs      int
}

type callOutcome struct {
	Repeats    int
	Execs      int
	ErrStreak  int
	ToolErrors int
}

func newCallTracker() *callTracker {
	return &callTracker{
		seen:  map[string]*callRecord{},
		stats: RunStats{ByTool: map[string]int{}, ErrorsByTool: map[string]int{}},
	}
}

func callKey(name, args string) string {
	return name + "|" + strings.TrimSpace(args)
}

func (t *callTracker) canSkip(name, args string) bool {
	key := callKey(name, args)
	if key == "" || key != t.lastKey {
		return false
	}
	rec, ok := t.seen[key]
	return ok && rec.repeats >= 1
}

func (t *callTracker) observe(name, args, result string, isError bool) callOutcome {
	t.count(name, args, isError)

	key := callKey(name, args)
	hash := hashString(normalizeResult(result))
	rec, ok := t.seen[key]
	switch {
	case !ok:
		t.seen[key] = &callRecord{resultHash: hash, execs: 1}
	case rec.resultHash != hash:
		rec.resultHash = hash
		rec.repeats = 0
		rec.execs++
	default:
		rec.repeats++
		rec.execs++
		t.stats.RepeatedNoProgress++
	}
	t.lastKey = key

	return t.outcome(name, key)
}

func (t *callTracker) observeSkipped(name, args string) callOutcome {
	t.count(name, args, false)
	t.stats.SkippedRepeats++

	key := callKey(name, args)
	if rec, ok := t.seen[key]; ok {
		rec.repeats++
		rec.execs++
		t.stats.RepeatedNoProgress++
	}
	t.lastKey = key

	return t.outcome(name, key)
}

func (t *callTracker) count(name, args string, isError bool) {
	t.stats.ToolCalls++
	t.stats.ByTool[name]++
	t.stats.LastCalls = append(t.stats.LastCalls, name+"("+previewArgs(args)+")")
	if len(t.stats.LastCalls) > lastCallsKept {
		t.stats.LastCalls = t.stats.LastCalls[len(t.stats.LastCalls)-lastCallsKept:]
	}

	if !isError {
		t.errStreak = 0
		return
	}
	t.stats.ToolErrors++
	t.stats.ErrorsByTool[name]++
	t.errStreak++
	if t.errStreak > t.stats.MaxErrorStreak {
		t.stats.MaxErrorStreak = t.errStreak
	}
}

func (t *callTracker) outcome(name, key string) callOutcome {
	out := callOutcome{ErrStreak: t.errStreak, ToolErrors: t.stats.ErrorsByTool[name]}
	if rec, ok := t.seen[key]; ok {
		out.Repeats = rec.repeats
		out.Execs = rec.execs
	}
	return out
}

func (t *callTracker) snapshot(iterations int) RunStats {
	stats := t.stats
	stats.Iterations = iterations
	return stats
}

var volatile = []struct {
	pattern *regexp.Regexp
	with    string
}{
	{regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]"), ""},
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|\s?[+-]\d{2}:?\d{2})?`), "<ts>"},
	{regexp.MustCompile(`\b\d{1,2}:\d{2}:\d{2}(\.\d+)?\b`), "<ts>"},
	{regexp.MustCompile(`\b\d+m\s?\d+(\.\d+)?s\b`), "<dur>"},
	{regexp.MustCompile(`(?i)\b\d+(\.\d+)?\s?(ns|µs|us|ms|s|sec|secs|seconds|min|mins|h)\b`), "<dur>"},
	{regexp.MustCompile(`(?i)\b\d+(\.\d+)?\s?(b|kb|mb|gb|kib|mib|gib)\b`), "<size>"},
	{regexp.MustCompile(`\b\d+(\.\d+)?%`), "<pct>"},
}

func normalizeResult(s string) string {
	for _, v := range volatile {
		s = v.pattern.ReplaceAllString(s, v.with)
	}
	return strings.TrimSpace(s)
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func previewArgs(args string) string {
	args = strings.Join(strings.Fields(args), " ")
	if len(args) > 80 {
		return args[:80] + "…"
	}
	return args
}

func budgetWarningMessage(remaining int) string {
	return budgetWarningKey.Render(budgetWarningInput{Remaining: remaining})
}

func runTokenWarnThreshold(cap int) int {
	return int(float64(cap) * runTokenWarnRatio)
}

func tokenBudgetWarningMessage(used, cap int) string {
	return tokenBudgetWarningKey.Render(tokenBudgetWarningInput{Used: used, Cap: cap})
}

func repeatNudgeMessage(name string, repeats int) string {
	left := max(repeatAbortThreshold-repeats, 1)
	return repeatNudgeKey.Render(repeatNudgeInput{Name: name, Count: repeats + 1, Left: left})
}

func sameCallMessage(name string, execs int) string {
	left := max(sameCallAbortThreshold-execs, 1)
	return sameCallNudgeKey.Render(sameCallNudgeInput{Name: name, Execs: execs, Left: left})
}

func emptyResultNote(name string) string {
	return emptyResultNoteKey.Render(emptyResultNoteInput{Name: name})
}

// attachmentIDsNote is appended to a tool result that carried images: the
// archived ids are otherwise visible only in the run's activity log, never to
// the model that just took the screenshot.
func attachmentIDsNote(ids []string) string {
	return attachmentIDsKey.Render(attachmentIDsInput{IDs: strings.Join(ids, ", ")})
}

func errorStreakMessage(streak int) string {
	left := max(errStreakAbortThreshold-streak, 1)
	return errorStreakNudgeKey.Render(errorStreakNudgeInput{Streak: streak, Left: left})
}

func toolErrorMessage(name string, count int) string {
	return toolErrorNoteKey.Render(toolErrorNoteInput{Name: name, Count: count})
}

var wrapUpPrompt = prompt.Text(wrapUpKey)

var emptyTurnPrompt = prompt.Text(emptyTurnPromptKey)

var emptyTurnFallback = prompt.Text(emptyTurnFallbackKey)

func outputTruncatedMessage(limit int) string {
	return outputTruncatedKey.Render(outputTruncatedInput{Limit: limit})
}
