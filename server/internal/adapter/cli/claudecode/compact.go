package claudecode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// compactBeforeResumeThreshold is the context size, in prompt tokens, above
// which a revision compacts the session it resumes. Every turn of a resumed
// session re-reads its whole history, so a session that reached 400K tokens
// pays ~400K per turn for the revision's small fix (T-114: 42 turns at ~420K,
// ~18M tokens). Below the threshold compaction costs more than it saves: an
// extra call plus latency, and a lossy summary of a context that was cheap.
const compactBeforeResumeThreshold = 100_000

// compactBeforeResume runs `/compact` on the session a revision is about to
// resume when that session's last context is at or above the threshold, so the
// revision starts from a focused summary instead of every screenshot and
// command output of the rounds before. It never fails the run: when the size
// cannot be read or the compaction does not succeed, the session is resumed as
// it is. The returned trace (nil when nothing ran) carries on into the resumed
// session so the run keeps one trace.
func (e *Executor) compactBeforeResume(ctx context.Context, inv invocation) *core.Trace {
	tokens, ok := sessionContextTokens(claudeConfigDir(), inv.resumeSessionID)
	logger := log.With().Str("task_key", inv.label).Str("cli_session_id", inv.resumeSessionID).Int("context_tokens", tokens).Logger()
	if !ok {
		logger.Debug().Msg("agent cli: session transcript not found; resuming without compaction")
		return nil
	}
	if tokens < compactBeforeResumeThreshold {
		return nil
	}

	c := inv
	c.prompt = "/compact " + prompt.Text(compactFocusKey)
	// /compact is a local command: no tool runs, so the MCP servers the init
	// guard waits for are not needed and must not fail the step.
	c.mcpPath = ""
	s, err := e.spawn(ctx, c)
	if err != nil {
		logger.Warn().Err(err).Msg("agent cli: compaction before revision resume could not start; resuming the full session")
		return nil
	}
	if s.timedOut || s.out.IsError || (s.out.Subtype != "" && s.out.Subtype != "success") {
		logger.Warn().Str("subtype", s.out.Subtype).Bool("timed_out", s.timedOut).Str("stderr", s.stderrTail).
			Msg("agent cli: compaction before revision resume failed; resuming the full session")
		return s.trace
	}
	logger.Info().Msg("agent cli: compacted the session before resuming it for a revision")
	return s.trace
}

// claudeConfigDir is where the CLI keeps its transcripts: CLAUDE_CONFIG_DIR
// (passed through to the child unchanged) or ~/.claude.
func claudeConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// sessionContextTokens is the size of the context the session's last main
// agent turn was sent (fresh + cache-written + cache-read input), read from
// the CLI's own transcript. A compaction boundary after that turn resets it.
// The transcript lives under projects/<encoded cwd>/<session>.jsonl; session
// ids are unique, so it is found by name rather than by re-deriving the CLI's
// path encoding.
func sessionContextTokens(configDir, sessionID string) (int, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if configDir == "" || sessionID == "" || strings.ContainsAny(sessionID, `/\*?[`) {
		return 0, false
	}
	matches, _ := filepath.Glob(filepath.Join(configDir, "projects", "*", sessionID+".jsonl"))
	if len(matches) == 0 {
		return 0, false
	}
	f, err := os.Open(matches[0])
	if err != nil {
		return 0, false
	}
	defer f.Close()
	return lastContextTokens(f)
}

type transcriptRecord struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Usage *cliUsage `json:"usage"`
	} `json:"message"`
}

var (
	usageMarker    = []byte(`"usage"`)
	boundaryMarker = []byte(`"compact_boundary"`)
)

func lastContextTokens(r io.Reader) (int, bool) {
	br := bufio.NewReaderSize(r, 64<<10)
	tokens, seen := 0, false
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && (bytes.Contains(line, usageMarker) || bytes.Contains(line, boundaryMarker)) {
			var rec transcriptRecord
			if json.Unmarshal(line, &rec) == nil && !rec.IsSidechain {
				switch {
				case rec.Type == "system" && rec.Subtype == "compact_boundary":
					tokens, seen = 0, true
				case rec.Type == "assistant" && rec.Message.Usage != nil:
					u := rec.Message.Usage
					tokens, seen = u.InputTokens+u.CacheCreationInputTokens+u.CacheReadInputTokens, true
				}
			}
		}
		if err != nil {
			// io.EOF ends a complete transcript; any other read error ends it
			// early, and what was read so far is still the best answer.
			return tokens, seen
		}
	}
}
