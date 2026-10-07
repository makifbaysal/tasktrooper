package prompt

// Both renderings live here because board and session each need one and neither may import the other; one file keeps the chat's first line and the per-turn context agreeing about the same task.

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The description and technical notes have no length limit on write, but the context message pays for them on every turn.
const maxTaskChatFieldChars = 2000

type taskChatCriterionLine struct {
	Mark string
	Text string
}

func taskChatCriterionLines(criteria []domain.AcceptanceCriterion) []taskChatCriterionLine {
	lines := make([]taskChatCriterionLine, 0, len(criteria))
	for _, c := range criteria {
		mark := " "
		if c.Completed {
			mark = "x"
		}
		lines = append(lines, taskChatCriterionLine{Mark: mark, Text: strings.TrimSpace(c.Text)})
	}
	return lines
}

type taskChatOpeningInput struct {
	Key         string
	Title       string
	Column      string
	Unassigned  bool
	Description string
	Criteria    []taskChatCriterionLine
	HasPR       bool
	PRHasNumber bool
	PRNumber    int
	PRURL       string
}

var taskChatOpeningKey = Define("agent.task_chat_opening", taskChatOpeningInput{
	Key: "TT-1", Title: "Sample task", Column: "todo", Unassigned: true,
})

// A chat message, not a system prompt: the human reads it, so it names the task the way the board does and offers the two things they most often want next.
func TaskChatOpeningMessage(task domain.BoardTask, criteria []domain.AcceptanceCriterion) string {
	in := taskChatOpeningInput{
		Key:        strings.TrimSpace(task.Key),
		Title:      strings.TrimSpace(task.Title),
		Column:     string(task.Column),
		Unassigned: task.AssigneeAgentID == nil,
		Criteria:   taskChatCriterionLines(criteria),
	}
	if desc := strings.TrimSpace(task.Description); desc != "" {
		in.Description = domain.TruncateHead(desc, maxTaskChatFieldChars)
	}
	if url := strings.TrimSpace(task.PRURL); url != "" {
		in.HasPR = true
		in.PRURL = url
		if task.PRNumber > 0 {
			in.PRHasNumber = true
			in.PRNumber = task.PRNumber
		}
	}
	return strings.TrimSpace(taskChatOpeningKey.Render(in))
}

type taskChatContextInput struct {
	Key            string
	Title          string
	TaskID         string
	Column         string
	TaskType       string
	Priority       string
	Branch         string
	WorkspaceDir   string
	HasPR          bool
	PRHasNumber    bool
	PRNumber       int
	PRURL          string
	Description    string
	TechnicalNotes string
	Criteria       []taskChatCriterionLine
}

var taskChatContextKey = Define("agent.task_chat_context", taskChatContextInput{
	Key: "TT-1", Title: "Sample task", TaskID: "00000000-0000-0000-0000-000000000000",
	Column: "todo", TaskType: "feature", Priority: "medium",
})

// Deliberately cheap: fields, branch and PR identity only — a diff pasted into every turn is paid for every turn and stale by the second one.
func TaskChatContextMessage(task domain.BoardTask, criteria []domain.AcceptanceCriterion, branch, workspaceDir string) string {
	in := taskChatContextInput{
		Key:          strings.TrimSpace(task.Key),
		Title:        strings.TrimSpace(task.Title),
		TaskID:       task.ID.String(),
		Column:       string(task.Column),
		TaskType:     string(task.TaskType),
		Priority:     string(task.Priority),
		Branch:       branch,
		WorkspaceDir: ShellPath(workspaceDir),
		Criteria:     taskChatCriterionLines(criteria),
	}
	if url := strings.TrimSpace(task.PRURL); url != "" {
		in.HasPR = true
		in.PRURL = url
		if task.PRNumber > 0 {
			in.PRHasNumber = true
			in.PRNumber = task.PRNumber
		}
	}
	if desc := strings.TrimSpace(task.Description); desc != "" {
		in.Description = domain.TruncateHead(desc, maxTaskChatFieldChars)
	}
	if tech := strings.TrimSpace(task.TechnicalDescription); tech != "" {
		in.TechnicalNotes = domain.TruncateHead(tech, maxTaskChatFieldChars)
	}
	return strings.TrimSpace(taskChatContextKey.Render(in))
}
