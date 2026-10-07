package mcpserver

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers the refusal text (*Server).unavailable returns to an
// agent cli session — see catalog/system/prompts/mcp/**.

type toolNameInput struct{ Name string }

var (
	askUserUnavailableKey   = prompt.Define("mcp.ask_user_unavailable", toolNameInput{Name: "ask_user"})
	skillLoadUnavailableKey = prompt.Define("mcp.skill_load_unavailable", toolNameInput{Name: skillLoadTool})
	toolNotExposedKey       = prompt.Define("mcp.tool_not_exposed", toolNameInput{Name: "read_file"})
	toolNotRegisteredKey    = prompt.Define("mcp.tool_not_registered", toolNameInput{Name: "set_criterion_completed"})
	toolPolicyDeniedKey     = prompt.Define("mcp.tool_policy_denied", toolNameInput{Name: "mcp_github_create_pr"})
)

var clarificationWaitKey = prompt.Define("mcp.clarification_wait", toolNameInput{Name: "ask_user"})

var clarificationRecordedKey = prompt.Define("mcp.clarification_recorded", toolNameInput{Name: "ask_user"})

type resourceBlockInput struct{ Name, Resource, Detail string }

var resourceBlockKey = prompt.Define("mcp.resource_block", resourceBlockInput{
	Name: "reserve_device", Resource: "mobile_device", Detail: "another run has it checked out",
})

var emptyResultKey = prompt.Define("mcp.empty_result", toolNameInput{Name: "grep_code"})
