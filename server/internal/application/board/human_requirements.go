package board

import (
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// humanRequirementsLimit keeps the newest of the human's comments: a later
// one supersedes an earlier one, so the oldest are the ones worth dropping.
const humanRequirementsLimit = 10

// humanRequirementsMessage hands every run on a task the comments the human
// wrote on it, as requirements. Before it, only a revision run saw comments at
// all (and only the last few of anyone's), so a scope change the human wrote
// on the card was built by the developer and then flagged as scope creep by
// the reviewer, who judged the diff against the description alone.
func humanRequirementsMessage(comments []domain.TaskComment) string {
	human := make([]domain.TaskComment, 0, len(comments))
	for _, c := range comments {
		content := strings.TrimSpace(c.Content)
		if c.AuthorType != "user" || content == "" || prompt.IsClarificationComment(content) || domain.IsChangeRequestComment(content) {
			continue
		}
		// A double-submitted comment is one requirement, not two.
		if n := len(human); n > 0 && strings.TrimSpace(human[n-1].Content) == content {
			continue
		}
		human = append(human, c)
	}
	if len(human) == 0 {
		return ""
	}
	if len(human) > humanRequirementsLimit {
		human = human[len(human)-humanRequirementsLimit:]
	}

	var sb strings.Builder
	sb.WriteString(humanRequirementsHeader())
	for _, c := range human {
		content := strings.TrimSpace(c.Content)
		if len(content) > 2000 {
			content = truncateHead(content, 2000) + "…"
		}
		stamp := ""
		if !c.CreatedAt.IsZero() {
			stamp = c.CreatedAt.UTC().Format("2006-01-02 15:04Z") + " "
		}
		sb.WriteString(fmt.Sprintf("- %s%s\n", stamp, content))
	}
	return sb.String()
}

// humanRequirementsHeader states why these comments are here and how they
// rank against the description, the out-of-scope list and the acceptance
// criteria — every run needs this framing, not just the ones with comments
// to show, so it stays fixed while the comment list below it varies.
func humanRequirementsHeader() string {
	return prompt.Text(humanRequirementsHeaderKey) + "\n"
}
