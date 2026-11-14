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

var voiceModeKey = prompt.Define("session.voice_mode", struct{ Language string }{Language: "Turkish"})

var voiceLanguages = map[string]string{"tr": "Turkish", "en": "English"}

// appendVoiceModePrompt tells the agent its reply is read aloud. It goes last
// among the system messages so it outranks the general formatting guidance
// that asks for markdown.
func appendVoiceModePrompt(history []domain.Message, voice string) []domain.Message {
	language, ok := voiceLanguages[voice]
	if !ok {
		return history
	}
	content := voiceModeKey.Render(struct{ Language string }{Language: language})
	last := 0
	for i, m := range history {
		if m.Role == domain.RoleSystem {
			last = i + 1
		}
	}
	out := make([]domain.Message, 0, len(history)+1)
	out = append(out, history[:last]...)
	out = append(out, domain.Message{Role: domain.RoleSystem, Content: content})
	return append(out, history[last:]...)
}
