package session

import (
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var projectPurposeKey = prompt.Define("session.project_purpose", struct{ Description string }{Description: "A todo app for personal task tracking."})

func prependProjectPrompt(history []domain.Message, description string) []domain.Message {
	content := projectPurposeKey.Render(struct{ Description string }{Description: description})
	return append([]domain.Message{{Role: domain.RoleSystem, Content: content}}, history...)
}

func prependProjectToolsNotePrompt(history []domain.Message, note string) []domain.Message {
	return append([]domain.Message{{Role: domain.RoleSystem, Content: note}}, history...)
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

var workspaceNoteKey = prompt.Define("session.workspace_note", struct{ Dir string }{Dir: "/tmp/ws"})

func workspaceSystemMessage(workspaceDir string) domain.Message {
	content := workspaceNoteKey.Render(struct{ Dir string }{Dir: prompt.ShellPath(workspaceDir)})
	return domain.Message{Role: domain.RoleSystem, Content: content}
}

func languageSystemMessage(lang string) domain.Message {
	return domain.Message{Role: domain.RoleSystem, Content: prompt.LanguageInstruction(lang)}
}
