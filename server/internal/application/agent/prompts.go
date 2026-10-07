package agent

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers every LLM-facing string this package renders — see
// catalog/system/prompts/agent_loop/**. Nothing here is a Go string literal;
// each Key's sample only has to be renderable, not realistic.

type budgetWarningInput struct{ Remaining int }

var budgetWarningKey = prompt.Define("agent_loop.budget_warning", budgetWarningInput{Remaining: 3})

type tokenBudgetWarningInput struct{ Used, Cap int }

var tokenBudgetWarningKey = prompt.Define("agent_loop.token_budget_warning", tokenBudgetWarningInput{Used: 136000, Cap: 160000})

type repeatNudgeInput struct {
	Name  string
	Count int
	Left  int
}

var repeatNudgeKey = prompt.Define("agent_loop.repeat_nudge", repeatNudgeInput{Name: "grep_code", Count: 2, Left: 3})

type sameCallNudgeInput struct {
	Name  string
	Execs int
	Left  int
}

var sameCallNudgeKey = prompt.Define("agent_loop.same_call_nudge", sameCallNudgeInput{Name: "run_terminal", Execs: 3, Left: 5})

type emptyResultNoteInput struct{ Name string }

var emptyResultNoteKey = prompt.Define("agent_loop.empty_result_note", emptyResultNoteInput{Name: "grep_code"})

type errorStreakNudgeInput struct {
	Streak int
	Left   int
}

var errorStreakNudgeKey = prompt.Define("agent_loop.error_streak_nudge", errorStreakNudgeInput{Streak: 3, Left: 5})

type toolErrorNoteInput struct {
	Name  string
	Count int
}

var toolErrorNoteKey = prompt.Define("agent_loop.tool_error_note", toolErrorNoteInput{Name: "write_file", Count: 5})

type attachmentIDsInput struct{ IDs string }

var attachmentIDsKey = prompt.Define("agent_loop.attachment_ids", attachmentIDsInput{IDs: "a1b2c3d4-0000-0000-0000-000000000000"})

var wrapUpKey = prompt.Define("agent_loop.wrap_up", struct{}{})

var emptyTurnPromptKey = prompt.Define("agent_loop.empty_turn_prompt", struct{}{})

var emptyTurnFallbackKey = prompt.Define("agent_loop.empty_turn_fallback", struct{}{})

var clarificationRefusalKey = prompt.Define("agent_loop.clarification_refusal", struct{}{})

var digestHeaderKey = prompt.Define("agent_loop.digest_header", struct{}{})

type outputTruncatedInput struct{ Limit int }

var outputTruncatedKey = prompt.Define("agent_loop.output_truncated", outputTruncatedInput{Limit: 16384})
