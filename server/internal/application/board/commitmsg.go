package board

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const commitSubjectMaxChars = 72

const commitMessageMaxTokens = 512

type commitDetails struct {
	TaskKey   string
	Title     string
	Summary   string
	AgentName string
	Prefix    string
	Writer    modelRef
	// Coauthor ("Name <email>") replaces the built-in Co-authored-by trailers;
	// CoauthorDisabled drops them. Both come from the agent's settings.
	Coauthor         string
	CoauthorDisabled bool
}

func (d commitDetails) withAgent(a domain.Agent) commitDetails {
	d.AgentName = a.Name
	d.Writer = agentWriterModel(a)
	d.Coauthor = a.CommitCoauthor
	d.CoauthorDisabled = a.CommitCoauthorDisabled
	return d
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
	return body + commitTrailers(d)
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
	input := commitMessageUserInput(strings.TrimSpace(d.Title), strings.TrimSpace(d.Summary))
	resp, err := llm.Chat(ctx, domain.AgentRequest{
		ProviderType: d.Writer.Provider,
		Model:        d.Writer.Model,
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: prompt.Text(commitMessageSystemKey)},
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

// commitMessageUserInput is the user turn handed to the commit-writer LLM:
// the task title, and — when the run left one — its own summary, already
// trimmed and truncated by the caller.
func commitMessageUserInput(title, summary string) string {
	if summary != "" {
		summary = truncateHead(summary, 2000)
	}
	return commitMessageUserInputKey.Render(commitMessageUserInputData{Title: title, Summary: summary})
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

const commitCoAuthorDomain = "agents.tasktrooper.ai"

// commitProductName and commitProductEmail are TaskTrooper's own co-author
// identity, stamped on every agent commit next to the role that made the
// change. GitHub only links and credits a co-author when the trailer's
// address belongs to a GitHub account, so commitProductEmail has to be
// registered on one (the account's own noreply address is the simplest
// choice) or GitHub leaves it unattributed.
const (
	commitProductName  = "Task Trooper"
	commitProductEmail = "agents@tasktrooper.ai"
)

func commitTrailers(d commitDetails) string {
	var lines []string
	if key := strings.TrimSpace(d.TaskKey); key != "" {
		lines = append(lines, "Task: "+key)
	}
	switch custom, err := domain.NormalizeCommitCoauthor(d.Coauthor); {
	case d.CoauthorDisabled:
	case err == nil && custom != "":
		lines = append(lines, "Co-authored-by: "+custom)
	default:
		if name := strings.TrimSpace(d.AgentName); name != "" {
			lines = append(lines, fmt.Sprintf("Co-authored-by: %s <%s@%s>", name, commitCoAuthorSlug(name), commitCoAuthorDomain))
		}
		lines = append(lines, fmt.Sprintf("Co-authored-by: %s <%s>", commitProductName, commitProductEmail))
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(lines, "\n") + "\n"
}

func commitCoAuthorSlug(name string) string {
	var b strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
