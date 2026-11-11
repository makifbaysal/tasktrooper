package domain

import (
	"regexp"
	"strings"
)

// Memory is what an agent still needs to know weeks from now, on a task nobody
// has written yet. Runs kept filing the state of the card they were on into it,
// and because recall pulls memories into every future run they crowded out the
// facts worth keeping. The test applied here is anchoring, because it is the
// one an agent can act on without judgement: a note that names a task key, a PR
// number, a commit SHA or a column transition is about one card. The durable
// version of the same lesson has the card taken out, and that is what the agent
// is told to write instead.

// A board key: T-28, B-3, DE-14. Standards and protocol names take the same
// shape, so the ones that turn up in real engineering notes are excluded rather
// than left to produce false rejections.
var (
	taskKeyPattern = regexp.MustCompile(`\b([A-Z]{1,4})-(\d{1,6})\b`)
	// Only multi-letter standards names are excluded; "T-28" and "S-4" are
	// plausible task keys and neither is a standard.
	notTaskKeys = map[string]bool{
		"UTF": true, "HTTP": true, "SHA": true, "ISO": true, "RFC": true, "MD": true,
		"IPV": true, "TLS": true, "AES": true, "RSA": true, "SPF": true, "IEEE": true,
		"PEP": true, "GPT": true, "CVE": true, "SQL": true, "OAUTH": true, "ES": true,
	}
)

// memoryDurabilityRule.code is a stable, short identifier — not prose: the
// adapter renders the actual reason an agent (or a human reviewing a
// reflection outcome) sees from catalog/system/guards/memory_run_log_reason.md,
// keyed by this code, so the phrasing lives in one place instead of being
// duplicated across every caller.
type memoryDurabilityRule struct {
	pattern *regexp.Regexp
	code    string
}

var memoryDurabilityRules = []memoryDurabilityRule{
	{
		// "PR #8", "pull request #12"
		pattern: regexp.MustCompile(`(?i)\b(?:pr|pull request)\s*#\s*\d+`),
		code:    "pinned_pr",
	},
	{
		// A commit SHA: hex, long enough not to be a word, mixed enough not to
		// be a plain number or a version.
		pattern: regexp.MustCompile(`\b(?:[0-9a-f]{7,40})\b`),
		code:    "commit_sha",
	},
	{
		// "moved code_review→ready_for_qa", "bounced in_qa -> need_revision"
		pattern: regexp.MustCompile(`(?i)\b(?:backlog|todo|in_progress|code_review|ready_for_qa|in_qa|analiz_review|need_revision|blocked|done|released)\b\s*(?:→|->|to)\s*\b(?:backlog|todo|in_progress|code_review|ready_for_qa|in_qa|analiz_review|need_revision|blocked|done|released)\b`),
		code:    "column_move",
	},
	{
		// "this task", "this run", "the task I am on"
		pattern: regexp.MustCompile(`(?i)\bthis (?:task|run|card|ticket|pr|review round)\b`),
		code:    "current_run",
	},
	{
		// "feature/t-28", "tt-123"
		pattern: regexp.MustCompile(`(?i)\b(?:feature/|branch\s+)?tt?-\d{1,6}\b`),
		code:    "task_branch",
	},
}

// memoryLooksLikeSHA keeps the SHA rule from firing on a plain number or a word
// made of hex letters ("deadbeef" is a SHA-shaped joke; "1234567" is a number).
func memoryLooksLikeSHA(text string) bool {
	for _, match := range regexp.MustCompile(`\b[0-9a-f]{7,40}\b`).FindAllString(text, -1) {
		hasDigit, hasLetter := false, false
		for _, r := range match {
			if r >= '0' && r <= '9' {
				hasDigit = true
			} else {
				hasLetter = true
			}
		}
		if hasDigit && hasLetter {
			return true
		}
	}
	return false
}

func memoryMentionsTaskKey(text string) bool {
	for _, m := range taskKeyPattern.FindAllStringSubmatch(text, -1) {
		if !notTaskKeys[strings.ToUpper(m[1])] {
			return true
		}
	}
	return false
}

// MemoryRunLogCode reports the stable code for why this content is a run
// log rather than a memory, or "" when it is worth keeping. The code is not
// prose — see memoryDurabilityRule's doc comment — the adapter renders it
// for whichever audience needs it (an agent's tool result, a human's
// reflection-outcome detail) from
// catalog/system/guards/memory_run_log_reason.md.
func MemoryRunLogCode(content string) string {
	text := strings.TrimSpace(content)
	if text == "" {
		return ""
	}
	if memoryMentionsTaskKey(text) {
		return "task_key"
	}
	for _, rule := range memoryDurabilityRules {
		if rule.pattern == nil {
			continue
		}
		if rule.code == "commit_sha" {
			if memoryLooksLikeSHA(text) {
				return rule.code
			}
			continue
		}
		if rule.pattern.MatchString(text) {
			return rule.code
		}
	}
	return ""
}

// memoryTokenSet reduces a memory to the words that carry its meaning, so two
// notes that say the same thing in a slightly different order are recognisably
// the same note.
func memoryTokenSet(text string) map[string]struct{} {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	set := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if len(f) < 3 {
			continue // "the", "a", "is" — noise in a similarity test
		}
		set[f] = struct{}{}
	}
	return set
}

// memorySimilarity is the Jaccard overlap of two memories' word sets: 1 means
// the same words, 0 means nothing in common.
func memorySimilarity(a, b string) float64 {
	setA, setB := memoryTokenSet(a), memoryTokenSet(b)
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	shared := 0
	for word := range setA {
		if _, ok := setB[word]; ok {
			shared++
		}
	}
	union := len(setA) + len(setB) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// memoryDuplicateThreshold is deliberately high: refusing a save is only
// correct when the memory really is already there; a related-but-different
// lesson must go in.
const memoryDuplicateThreshold = 0.6

// MemoryDuplicateOf returns the existing memory this content merely repeats, if
// any.
func MemoryDuplicateOf(existing []AgentMemory, content string) (AgentMemory, bool) {
	best := AgentMemory{}
	bestScore := 0.0
	for _, mem := range existing {
		if score := memorySimilarity(mem.Content, content); score > bestScore {
			best, bestScore = mem, score
		}
	}
	if bestScore >= memoryDuplicateThreshold {
		return best, true
	}
	return AgentMemory{}, false
}
