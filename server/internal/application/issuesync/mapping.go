package issuesync

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const maxDescriptionLen = 20000

// buildDescription keeps the issue body as-is: it is untrusted, never
// interpreted, only ever task text. The conversion prompt relies on the
// Source line coming first.
func buildDescription(key, url, body string) string {
	head := fmt.Sprintf("Source: [%s](%s)", key, url)
	out := head
	if strings.TrimSpace(body) != "" {
		out += "\n\n" + body
	}
	if len(out) > maxDescriptionLen {
		out = out[:maxDescriptionLen]
	}
	return out
}

func jiraPriorityToTaskPriority(name string) (domain.TaskPriority, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "highest":
		return domain.TaskPriorityCritical, true
	case "high":
		return domain.TaskPriorityHigh, true
	case "medium":
		return domain.TaskPriorityMedium, true
	case "low", "lowest":
		return domain.TaskPriorityLow, true
	default:
		return "", false
	}
}

func hasBugLabel(labels []string) bool {
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), "bug") {
			return true
		}
	}
	return false
}

func parseGitHubIssueKey(key string) (owner, repo string, number int, ok bool) {
	hash := strings.LastIndexByte(key, '#')
	if hash < 0 || hash == len(key)-1 {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(key[hash+1:])
	if err != nil || n <= 0 {
		return "", "", 0, false
	}
	slash := strings.IndexByte(key[:hash], '/')
	if slash <= 0 || slash == hash-1 {
		return "", "", 0, false
	}
	owner = key[:slash]
	repo = key[slash+1 : hash]
	if owner == "" || repo == "" {
		return "", "", 0, false
	}
	return owner, repo, n, true
}
