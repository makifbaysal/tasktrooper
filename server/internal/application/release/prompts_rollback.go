package release

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers every LLM-facing string rollback.go renders — see
// catalog/system/guards/release_rollback_*.md. Nothing here is a Go string
// literal; each Key's sample only has to be renderable, not realistic.

type rollbackNotAllowedReasonInput struct {
	Case    string
	Version string
	Status  string
	Err     string
}

var rollbackNotAllowedReasonKey = prompt.Define("guard.release_rollback_not_allowed_reason", rollbackNotAllowedReasonInput{Case: "too_old"})

type rollbackNotAllowedInput struct{ Why string }

var rollbackNotAllowedKey = prompt.Define("guard.release_rollback_not_allowed", rollbackNotAllowedInput{Why: "this release finished more than 24 hours ago"})

type rollbackInvalidReasonInput struct{ Reason string }

var rollbackInvalidReasonKey = prompt.Define("guard.release_rollback_invalid_reason", rollbackInvalidReasonInput{Reason: `"weird"`})

var rollbackNeedsHumanKey = prompt.Define("guard.release_rollback_needs_human", struct{}{})

type rollbackProposalDismissedInput struct{ IncidentID string }

var rollbackProposalDismissedKey = prompt.Define("guard.release_rollback_proposal_dismissed", rollbackProposalDismissedInput{
	IncidentID: "3f2b8c1e-5d4a-4e9b-8c7d-1a2b3c4d5e6f",
})
var rollbackNotConfiguredKey = prompt.Define("guard.release_rollback_not_configured", struct{}{})
