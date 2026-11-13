package claudecode

import (
	"context"
	"errors"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)


func (e *Executor) ExecuteChat(ctx context.Context, req domain.ChatExecution, out port.ChatStream) (domain.ChatResult, error) {
	if e == nil {
		return domain.ChatResult{}, errors.New("agent cli executor is not configured")
	}

	if strings.TrimSpace(req.WorkDir) == "" {
		return domain.ChatResult{}, errors.New("agent cli executor: no chat workspace to run in")
	}

	mcpCfg, releaseMCP, err := core.ResolveMCP(ctx, e.mcpProvider, e.mcp, MCPRun{Policy: req.Policy, Label: req.SessionID})
	defer releaseMCP()
	if err != nil {
		return domain.ChatResult{}, err
	}

	mcpPath, cleanupMCP, err := writeMCPConfigFile(mcpCfg)
	if err != nil {
		return domain.ChatResult{}, err
	}
	defer cleanupMCP()

	images := core.StageImages(req.History)
	defer images.Cleanup()

	fresh := func() invocation {
		systemPrompt, prompt := flattenHistory(images.History)
		return invocation{
			workDir: req.WorkDir,
			systemPrompt: withToolManifest(systemPrompt, mcpCfg.Tools),
			prompt:       prompt,
			model:        req.Model,
			label:        req.SessionID,
			mcpPath:      mcpPath,
			stream:       out,
		}
	}

	inv := fresh()
	resumed := strings.TrimSpace(req.ResumeSessionID) != ""
	if resumed {
		inv.systemPrompt = ""
		inv.prompt = strings.TrimSpace(req.Prompt)
		inv.resumeSessionID = req.ResumeSessionID
		if inv.prompt == "" {
			_, inv.prompt = flattenHistory(images.History)
		} else {
			inv.prompt = core.WithNote(inv.prompt, images.LastUserNote())
		}
	}

	s, err := e.spawn(ctx, inv)
	if err != nil {
		return domain.ChatResult{}, err
	}

	if resumed && resumeRefused(s) {
		log.Info().
			Str("session_id", req.SessionID).
			Str("cli_session_id", req.ResumeSessionID).
			Msg("agent cli could not resume this chat's cli session; starting a fresh one from the stored history")
		out.SegmentBreak()
		retry := fresh()
		retry.trace = s.trace
		if s, err = e.spawn(ctx, retry); err != nil {
			return domain.ChatResult{}, err
		}
		resumed = false
	}

	resp, err := e.finish(ctx, req.SessionID, s)
	result := domain.ChatResult{
		Response: resp,
		CLISessionID: s.sessionID(),
		Resumed:      resumed,
	}
	return result, err
}

func resumeRefused(s session) bool {
	if !s.failed() {
		return false
	}
	return resumeMissingPattern.MatchString(strings.Join([]string{s.out.Text, s.stderrTail}, "\n"))
}
