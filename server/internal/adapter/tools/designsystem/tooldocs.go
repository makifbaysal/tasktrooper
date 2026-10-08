package designsystem

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{getToolName, proposeToolName, requestToolName} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}

type needsDesignTaskInput struct{ ToolName string }

var needsDesignTaskKey = prompt.Define("guard.design_system_needs_design_task", needsDesignTaskInput{ToolName: proposeToolName})
