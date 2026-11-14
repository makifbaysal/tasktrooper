package claudecode

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers this adapter's LLM-facing strings — see
// catalog/system/prompts/cli/claude_*.md.

type maxTurnsNoteInput struct{ MaxTurns int }

var maxTurnsNoteKey = prompt.Define("cli.claude_max_turns_note", maxTurnsNoteInput{MaxTurns: 40})

type continuePromptInput struct{ Task string }

var continuePromptKey = prompt.Define("cli.claude_continue_prompt", continuePromptInput{Task: "tt-123 Add a link"})

var earlierTurnLabelKey = prompt.Define("cli.claude_earlier_turn_label", struct{}{})

var compactFocusKey = prompt.Define("cli.claude_compact_focus", struct{}{})
