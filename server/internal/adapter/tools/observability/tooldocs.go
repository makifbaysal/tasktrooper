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
