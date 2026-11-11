package telemetry

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range ToolNames {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
