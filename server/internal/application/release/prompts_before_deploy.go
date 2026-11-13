package release

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers every LLM-facing string before_deploy.go renders —
// see catalog/system/guards/release_*.md and
// catalog/system/prompts/notices/release_*.md. Nothing here is a Go string
// literal; each Key's sample only has to be renderable, not realistic.

type releaseNameInput struct{ Name string }

var deliveryUnconfirmedKey = prompt.Define("guard.release_delivery_unconfirmed", releaseNameInput{Name: "api"})
var deliveryUnconfirmedCommentKey = prompt.Define("notices.release_delivery_unconfirmed_comment", struct{}{})

type releaseNameStepsInput struct{ Name, Steps string }

var beforeDeployPendingMergeKey = prompt.Define("guard.release_before_deploy_pending_merge", releaseNameStepsInput{Name: "api", Steps: "Flip the flag."})
var beforeDeployPendingMergeCommentKey = prompt.Define("notices.release_before_deploy_pending_merge_comment", releaseNameStepsInput{Name: "api", Steps: "Flip the flag."})

var beforeDeployPendingIntroKey = prompt.Define("guard.release_before_deploy_pending_intro", struct{}{})

type releaseMessageInput struct{ Message string }

var beforeDeployPendingDeployCommentKey = prompt.Define("notices.release_before_deploy_pending_deploy_comment", releaseMessageInput{Message: "x"})

type releaseInFlightInput struct{ Name, Release, Status string }

var releaseInFlightKey = prompt.Define("guard.release_in_flight", releaseInFlightInput{Name: "api", Release: "T-1 (abc1234)", Status: "verifying"})
var releaseInFlightCommentKey = prompt.Define("notices.release_in_flight_comment", releaseInFlightInput{Name: "api", Release: "T-1 (abc1234)", Status: "verifying"})
