package code

import (
	"os/exec"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// NewGrepCodeToolWithoutRipgrep is grep_code as it runs on a machine with no rg on PATH.
func NewGrepCodeToolWithoutRipgrep(kit *ToolKit) port.ToolExecutor {
	return &grepCodeTool{kit: kit, lookPath: func(string) (string, error) { return "", exec.ErrNotFound }}
}
