package observability

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{
		listErrorGroupsToolName,
		proposeErrorDefinitionsToolName,
		listDataSourceMetricsToolName,
		proposeSignalBindingToolName,
		proposeAlertRuleToolName,
	} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}

type outsideJobInput struct {
	ToolName     string
	Case         string
	RepositoryID string
	Component    string
}

var outsideJobKey = prompt.Define("guard.observability_outside_job", outsideJobInput{
	ToolName: listErrorGroupsToolName, Case: outsideComponent, RepositoryID: "8f0e7c5a-1b2c-4d3e-9f40-5a6b7c8d9e0f", Component: "services/api",
})

type outsideRunRepositoryInput struct {
	ToolName     string
	RepositoryID string
}

var outsideRunRepositoryKey = prompt.Define("guard.observability_outside_run_repository", outsideRunRepositoryInput{
	ToolName: listErrorGroupsToolName, RepositoryID: "8f0e7c5a-1b2c-4d3e-9f40-5a6b7c8d9e0f",
})
