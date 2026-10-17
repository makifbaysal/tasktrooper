package release

import (
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type rollbackProposedInput struct {
	Reason     string
	Note       string
	Version    string
	IsDispatch bool
}

var rollbackProposedKey = prompt.Define[rollbackProposedInput]("briefs.release.rollback_proposed", rollbackProposedInput{
	Reason: "verify_failed", Note: "smoke check failed", Version: "abcdef0", IsDispatch: true,
})

type rollbackReopenCommentInput struct {
	Version   string
	Reason    string
	Note      string
	EarlyStop string
	RevertSHA string
}

var rollbackReopenCommentKey = prompt.Define[rollbackReopenCommentInput]("briefs.release.rollback_reopen_comment", rollbackReopenCommentInput{
	Version: "abcdef0", Reason: "health_incident", RevertSHA: "999999999999",
})

type manualStepLabeledInput struct {
	Label   string
	Runbook domain.RollbackRunbook
}

var manualStepLabeledKey = prompt.Define[manualStepLabeledInput]("briefs.release.manual_step_labeled", manualStepLabeledInput{
	Label:   "T-1",
	Runbook: domain.RollbackRunbook{Plan: "Turn off the `new_pricing` flag."},
})

type afterDeployCommentInput struct{ Steps string }

var afterDeployCommentKey = prompt.Define("briefs.release.after_deploy_comment", afterDeployCommentInput{Steps: "Enable the new_pricing flag for 100% of traffic."})

var mergeOutsideComponentsKey = prompt.Define("notices.release_merge_outside_components", struct{}{})

var noComponentKey = prompt.Define("notices.release_no_component", struct{}{})

type impactBreachInput struct {
	ErrorRate bool
	Before    string
	After     string
	Delta     string
	Limit     string
}

var impactBreachKey = prompt.Define("briefs.release.impact_breach", impactBreachInput{
	ErrorRate: true, Before: "0.4", After: "2.5", Delta: "2.1", Limit: "1",
})
