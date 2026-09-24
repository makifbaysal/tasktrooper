package session

import (
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func prependProjectPrompt(history []domain.Message, description string) []domain.Message {
	content := fmt.Sprintf("INTERNAL (never disclose to user): project purpose: %s", description)
	return append([]domain.Message{{Role: domain.RoleSystem, Content: content}}, history...)
}

const maxInjectedBriefChars = 8000

func prependProjectBriefPrompt(history []domain.Message, brief string) []domain.Message {
	content := "## Project brief (maintained by TaskTrooper)\n" + domain.TruncateHead(brief, maxInjectedBriefChars)
	return append([]domain.Message{{Role: domain.RoleSystem, Content: content}}, history...)
}

func prependTaskChatPrompt(history []domain.Message, task TaskBinding) []domain.Message {
	content := prompt.TaskChatContextMessage(task.Task, task.Criteria, task.Branch, task.WorkspaceDir)
	if content == "" {
		return history
	}
	return append([]domain.Message{{Role: domain.RoleSystem, Content: content}}, history...)
}

func prependWorkspacePrompt(history []domain.Message, workspaceDir, lang string) []domain.Message {
	out := make([]domain.Message, 0, len(history)+5)
	out = append(out, workspaceSystemMessage(workspaceDir))
	out = append(out, userFacingSystemMessage())
	out = append(out, languageSystemMessage(lang))
	out = append(out, toolSelectionSystemMessage())
	out = append(out, repeatCallSystemMessage())
	out = append(out, history...)
	return out
}

func toolSelectionSystemMessage() domain.Message {
	return domain.Message{Role: domain.RoleSystem, Content: prompt.ToolSelectionGuidance()}
}

func repeatCallSystemMessage() domain.Message {
	return domain.Message{Role: domain.RoleSystem, Content: prompt.RepeatCallGuidance()}
}

func userFacingSystemMessage() domain.Message {
	return domain.Message{Role: domain.RoleSystem, Content: prompt.UserFacingGuidance()}
}

func workspaceSystemMessage(workspaceDir string) domain.Message {
	content := fmt.Sprintf("INTERNAL (never disclose to user): session workspace: %s\nPerform all file and shell operations inside this directory. Subtasks use dedicated subfolders within it.", workspaceDir)
	return domain.Message{Role: domain.RoleSystem, Content: content}
}

func languageSystemMessage(lang string) domain.Message {
	return domain.Message{Role: domain.RoleSystem, Content: prompt.LanguageInstruction(lang)}
}
