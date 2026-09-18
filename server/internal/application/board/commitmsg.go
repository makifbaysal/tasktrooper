package board

import (
	"context"
	"errors"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const commitSubjectMaxChars = 72

const commitMessageMaxTokens = 512

const commitMessagePrompt = "You write git commit messages for an automated engineering agent.\n" +
	"Reply with the commit message and nothing else: no code fences, no quotes, no commentary.\n" +
	"ALWAYS write in English, even when the task and the summary are in another language — translate them.\n" +
	"First line: Conventional Commits (`type(scope): summary`), imperative mood, lowercase after the colon, at most 72 characters.\n" +
	"Then, only if it adds something the subject does not, a blank line and at most three short body lines saying what changed and why.\n" +
	"Describe only what the input says was done. Never invent changes, files or reasons."

type commitDetails struct {
	TaskKey   string
	Title     string
	Summary   string
	AgentName string
	Prefix    string
	Writer    modelRef
}

func agentWriterModel(agentRec domain.Agent) modelRef {
	return modelRef{Provider: agentRec.ProviderType, Model: agentRec.Model}
}

type modelRef struct {
	Provider domain.LLMProviderType
	Model    string
}

func writeCommitMessage(ctx context.Context, llm port.LLMClient, d commitDetails) string {
	body := englishCommitBody(ctx, llm, d)
	if d.Prefix != "" && !strings.HasPrefix(strings.ToLower(body), strings.ToLower(d.Prefix)) {
		body = d.Prefix + body
	}
	return body + commitTrailers(d.TaskKey, d.AgentName)
}

func (r *Runner) writeCommitMessage(ctx context.Context, d commitDetails) string {
	return writeCommitMessage(ctx, r.llm, d)
}

func englishCommitBody(ctx context.Context, llm port.LLMClient, d commitDetails) string {
	fallback := strings.TrimSpace(d.Title)
	if summary := strings.TrimSpace(d.Summary); summary != "" {
		fallback = strings.TrimSpace(fallback + "\n\n" + summary)
	}
	if llm == nil || fallback == "" {
		return fallback
	}
	input := "Task title: " + strings.TrimSpace(d.Title)
	if summary := strings.TrimSpace(d.Summary); summary != "" {
		input += "\n\nWhat the agent reports it did:\n" + truncateHead(summary, 2000)
	}
	resp, err := llm.Chat(ctx, domain.AgentRequest{
		ProviderType: d.Writer.Provider,
		Model:        d.Writer.Model,
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: commitMessagePrompt},
			{Role: domain.RoleUser, Content: input},
		},
		MaxTokens: commitMessageMaxTokens,
	})
	if err != nil {
		log.Warn().Err(err).
			Str("writer_provider", string(d.Writer.Provider)).
			Bool("permanent", errors.Is(err, domain.ErrHostExecutedUnservable)).
			Msg("english commit message rewrite skipped; committing the task's own title and summary instead")
		return fallback
	}
	written := sanitizeCommitMessage(resp.Message.Content)
	if written == "" {
		return fallback
	}
	return written
}

func sanitizeCommitMessage(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text, "\n"); idx >= 0 {
			text = text[idx+1:]
		}
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
		text = strings.TrimSpace(text)
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	subject := strings.TrimSpace(lines[0])
	if subject == "" {
		return ""
	}
	if len(subject) > commitSubjectMaxChars {
		subject = strings.TrimSpace(subject[:commitSubjectMaxChars])
	}
	lines[0] = subject
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// commitTrailers puts the task key and the agent name in trailers instead of
// the subject line: a subject prefixed with "T-1 " is not a valid Conventional
// Commits header, which fails commitlint and action-semantic-pull-request on
// any repository that enforces one, and the PR title is this same subject.
func commitTrailers(taskKey, agentName string) string {
	var lines []string
	if key := strings.TrimSpace(taskKey); key != "" {
		lines = append(lines, "Task: "+key)
	}
	if name := strings.TrimSpace(agentName); name != "" {
		lines = append(lines, "Agent: "+name)
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(lines, "\n") + "\n"
}
