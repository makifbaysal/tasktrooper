package prompt

import (
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var clarificationGuidanceKey = Define[struct{}]("clarification.guidance", struct{}{})

func ClarificationGuidance() string {
	return Text(clarificationGuidanceKey)
}

// Over MCP ask_user is served only to a run that waits for the answer (a board run or a chat turn, see domain.ClarificationSink), and even then the CLI has to end its own turn; so this names ask_user conditionally and keeps the closing-message fallback for every session without it.
var cliClarificationGuidanceKey = Define[struct{}]("clarification.cli_guidance", struct{}{})

func CLIClarificationGuidance() string {
	return Text(cliClarificationGuidanceKey)
}

type clarificationQuestionBlock struct {
	Number  int
	Prompt  string
	Options []string
}

type formatClarificationMessageInput struct {
	Context   string
	Questions []clarificationQuestionBlock
}

var formatClarificationMessageKey = Define("clarification.format_message", formatClarificationMessageInput{
	Context:   "Sample context.",
	Questions: []clarificationQuestionBlock{{Number: 1, Prompt: "Sample question?", Options: []string{"A", "B"}}},
})

func FormatClarificationMessage(req domain.ClarificationRequest) string {
	blocks := make([]clarificationQuestionBlock, 0, len(req.Questions))
	for i, q := range req.Questions {
		blocks = append(blocks, clarificationQuestionBlock{Number: i + 1, Prompt: q.Prompt, Options: optionLabels(q.Options)})
	}
	return strings.TrimSpace(formatClarificationMessageKey.Render(formatClarificationMessageInput{
		Context:   req.Context,
		Questions: blocks,
	}))
}

func optionLabels(options []domain.ClarificationOption) []string {
	labels := make([]string, 0, len(options))
	for _, opt := range options {
		labels = append(labels, opt.Label)
	}
	return labels
}

// The stored assistant message only held the context line; the questions lived in a JSONB column the model never saw, so the next turn read the answer without knowing what it answered.
type historyNoteInput struct {
	Lines []string
}

var historyNoteKey = Define("clarification.history_note", historyNoteInput{Lines: []string{"1. Sample question?"}})

func ClarificationHistoryNote(req domain.ClarificationRequest) string {
	if len(req.Questions) == 0 {
		return ""
	}
	lines := make([]string, len(req.Questions))
	for i, q := range req.Questions {
		lines[i] = numberedQuestion(i, q)
	}
	return historyNoteKey.Render(historyNoteInput{Lines: lines})
}

type formatQuestionsInput struct {
	Parts []string
}

var formatQuestionsKey = Define("clarification.format_questions", formatQuestionsInput{
	Parts: []string{"Sample context.", "1. Sample question?"},
})

// A parked task once remembered only the context line, which is not a question; the question text is the part that must survive.
func FormatClarificationQuestions(req domain.ClarificationRequest) string {
	var parts []string
	if ctx := strings.TrimSpace(req.Context); ctx != "" {
		parts = append(parts, ctx)
	}
	for i, q := range req.Questions {
		parts = append(parts, numberedQuestion(i, q))
	}
	return formatQuestionsKey.Render(formatQuestionsInput{Parts: parts})
}

func numberedQuestion(index int, q domain.ClarificationQuestion) string {
	line := fmt.Sprintf("%d. %s", index+1, q.Prompt)
	if len(q.Options) == 0 {
		return line
	}
	labels := make([]string, 0, len(q.Options))
	for _, opt := range q.Options {
		labels = append(labels, opt.Label)
	}
	return line + " [options: " + strings.Join(labels, " | ") + "]"
}

// Answers used to live only in the throwaway chat session, so the task remembered nothing and the next run asked again; the comment is that memory and the prefix is how a later run finds it.
const ClarificationCommentPrefix = "[clarification]"

func ClarificationAnswerComment(question, answer string) string {
	var sb strings.Builder
	sb.WriteString(ClarificationCommentPrefix)
	if q := strings.TrimSpace(question); q != "" {
		sb.WriteString("\nAsked:\n")
		sb.WriteString(q)
	}
	sb.WriteString("\nAnswered by the human:\n")
	sb.WriteString(strings.TrimSpace(answer))
	return sb.String()
}

func IsClarificationComment(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), ClarificationCommentPrefix)
}

// Everything behind this is uncapped (ListByTask carries no LIMIT) and goes into every run, so a long-lived card would replay its whole clarification history on each dispatch.
const (
	maxAnsweredClarifications     = 10
	maxAnsweredClarificationChars = 2000
)

type answeredInput struct {
	ShownCount   int
	OmittedCount int
	Answers      []string
}

var answeredKey = Define("clarification.answered", answeredInput{
	ShownCount: 1,
	Answers:    []string{"Asked:\nSample question?\nAnswered by the human:\nSample answer."},
})

func AnsweredClarificationsMessage(comments []domain.TaskComment) string {
	answered := make([]string, 0, len(comments))
	for _, c := range comments {
		if !IsClarificationComment(c.Content) {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.Content), ClarificationCommentPrefix))
		if body != "" {
			answered = append(answered, body)
		}
	}
	if len(answered) == 0 {
		return ""
	}
	omitted := 0
	if len(answered) > maxAnsweredClarifications {
		omitted = len(answered) - maxAnsweredClarifications
		answered = answered[omitted:]
	}
	shown := make([]string, len(answered))
	for i, a := range answered {
		body := domain.TruncateHead(a, maxAnsweredClarificationChars)
		if len(body) < len(a) {
			body += "…"
		}
		shown[i] = body
	}
	return strings.TrimSpace(answeredKey.Render(answeredInput{
		ShownCount:   len(shown),
		OmittedCount: omitted,
		Answers:      shown,
	}))
}

type ackInput struct {
	Lang string
}

var ackKey = Define("clarification.ack", ackInput{Lang: "en"})

func ClarificationAckMessage(lang string) string {
	return ackKey.Render(ackInput{Lang: lang})
}

var fallbackMessageKey = Define[struct{}]("clarification.fallback_message", struct{}{})

func BuildClarificationResponse(req domain.ClarificationRequest) domain.AgentResponse {
	content := req.Context
	if content == "" {
		content = Text(fallbackMessageKey)
	}
	reqCopy := req
	return domain.AgentResponse{
		Message:       domain.Message{Role: domain.RoleAssistant, Content: content},
		Clarification: &reqCopy,
	}
}

var askUserTaskGuidanceKey = Define[struct{}]("clarification.ask_user_task_guidance", struct{}{})

func AskUserTaskGuidance() string {
	return Text(askUserTaskGuidanceKey)
}

type userFacingLanguageRuleInput struct {
	Locale string
}

var userFacingLanguageRuleKey = Define("clarification.user_facing_language_rule", userFacingLanguageRuleInput{Locale: "English"})

func UserFacingLanguageRule(lang string) string {
	return userFacingLanguageRuleKey.Render(userFacingLanguageRuleInput{Locale: LocaleDisplayName(lang)})
}
