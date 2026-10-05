package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	maxChildSessions   = 20
	childExportTimeout = 15 * time.Second
)

type sessionExport struct {
	Messages []struct {
		Info struct {
			Role   string `json:"role"`
			Tokens struct {
				Input  int `json:"input"`
				Output int `json:"output"`
				Cache  struct {
					Read  int `json:"read"`
					Write int `json:"write"`
				} `json:"cache"`
			} `json:"tokens"`
		} `json:"info"`
	} `json:"messages"`
}

// parseSessionExport sums an `opencode export` document's assistant messages;
// each carries the totals of its steps.
func parseSessionExport(raw []byte) (domain.Usage, error) {
	var doc sessionExport
	if err := json.Unmarshal(raw, &doc); err != nil {
		return domain.Usage{}, err
	}
	var u domain.Usage
	for _, m := range doc.Messages {
		if m.Info.Role != "assistant" {
			continue
		}
		u.PromptTokens += m.Info.Tokens.Input
		u.CompletionTokens += m.Info.Tokens.Output
		u.CacheReadTokens += m.Info.Tokens.Cache.Read
		u.CacheWriteTokens += m.Info.Tokens.Cache.Write
	}
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
	return u, nil
}

// childUsage meters subagent sessions, which `run --format json` filters out
// of its stream. A session whose export fails is skipped: under-metering one
// subagent must not fail a run that otherwise succeeded.
func childUsage(ctx context.Context, bin string, sessionIDs []string) domain.Usage {
	var total domain.Usage
	if len(sessionIDs) > maxChildSessions {
		log.Warn().Int("sessions", len(sessionIDs)).Int("cap", maxChildSessions).Msg("opencode subagent usage: too many child sessions, metering the first ones only")
		sessionIDs = sessionIDs[:maxChildSessions]
	}
	for _, id := range sessionIDs {
		u, err := exportUsage(ctx, bin, id)
		if err != nil {
			log.Warn().Err(err).Str("child_session_id", id).Msg("opencode subagent usage: skipping child session")
			continue
		}
		total.PromptTokens += u.PromptTokens
		total.CompletionTokens += u.CompletionTokens
		total.TotalTokens += u.TotalTokens
		total.CacheReadTokens += u.CacheReadTokens
		total.CacheWriteTokens += u.CacheWriteTokens
	}
	return total
}

func exportUsage(ctx context.Context, bin, sessionID string) (domain.Usage, error) {
	ctx, cancel := context.WithTimeout(ctx, childExportTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "export", sessionID)
	cmd.Env = core.ChildEnv(ctx, false, nil)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return domain.Usage{}, fmt.Errorf("opencode export: %w", err)
	}
	return parseSessionExport(stdout.Bytes())
}
