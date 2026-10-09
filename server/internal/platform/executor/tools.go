package executor

import (
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/browser"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/code"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/search"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/shell"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/web"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// indexFreeCodeTools are the code tools that read the checkout directly. The
// rest of the code kit (codebase_search, expand_symbol_context) query a
// semantic index, which this process does not build yet.
var indexFreeCodeTools = map[string]bool{
	"grep_code":           true,
	"get_repo_tree":       true,
	"get_symbol_skeleton": true,
}

type localTools struct {
	workspace []port.ToolExecutor
	host      []port.ToolExecutor
	browser   *browser.Session
}

func (t localTools) close() {
	if t.browser != nil {
		t.browser.Close()
	}
}

// buildLocalTools are the tools a run executes on this computer. Workspace
// tools act inside the run's checkout, which the registry hands them through
// the context; the terminal is additionally confined to workspaceRoot.
func buildLocalTools(cfg *domain.Config, workspaceRoot string) localTools {
	var tools localTools
	tools.workspace = append(tools.workspace,
		code.NewReadFileTool(),
		code.NewWriteFileTool(),
		code.NewEditFileTool(),
		code.NewEditLinesTool(),
		code.NewDeleteFileTool(),
		code.NewMoveFileTool(),
	)
	kit := code.NewToolKit(nil, nil, mapper.NewService(cfg.Mapping), cfg.Indexer, cfg.Graph, "")
	for _, tool := range code.NewExecutors(kit) {
		if indexFreeCodeTools[tool.Name()] {
			tools.workspace = append(tools.workspace, tool)
		}
	}
	if cfg.Tools.Terminal.Enabled {
		tools.workspace = append(tools.workspace,
			shell.New(workspaceRoot, cfg.Tools.Terminal.Timeout, cfg.Tools.Terminal.MaxTimeout, cfg.Tools.Terminal.Sandbox))
	}
	if cfg.Tools.Web.Enabled {
		tools.workspace = append(tools.workspace, web.NewDownloadTool())
		tools.host = append(tools.host, web.New(cfg.Tools.Web.MaxResponseBytes))
	}
	tools.host = append(tools.host, web.NewHTTPRequestTool())
	if cfg.Tools.Search.Enabled {
		tools.host = append(tools.host, search.New(cfg.Tools.Search.MaxResults))
	}
	if cfg.Tools.Browser.Enabled {
		tools.browser = browser.NewSession()
		tools.host = append(tools.host, browser.NewExecutors(tools.browser)...)
	}
	return tools
}
