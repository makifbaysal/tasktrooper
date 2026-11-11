---
key: tool.ask_user
version: "1"
params:
    context: Brief explanation shown above the question form (user-facing language)
    questions: 'Questions to ask. Each question: id, prompt, allow_multiple and max_selections (choice mode), options (see ask_user tool description for text vs choice mode).'
    questions.items.properties.allow_multiple: 'Choice mode only: true when the user may select multiple options; false for single-select.'
    questions.items.properties.max_selections: 'Choice mode with allow_multiple true only: the most options the user may pick (the other entry counts as one), e.g. 3 for "pick up to 3 countries". Omit for no cap.'
    questions.items.properties.id: Unique stable id for this question (snake_case)
    questions.items.properties.options: 'Answer choices (min 2). TEXT mode: only free_text and skip. CHOICE mode: concrete options then other as last entry.'
    questions.items.properties.options.items.properties.id: 'Option id: free_text | skip | other | or a concrete snake_case id'
    questions.items.properties.options.items.properties.label: Option label shown on the button (user-facing language)
    questions.items.properties.prompt: Question text shown to the user (user-facing language)
---
Ask the user structured clarifying questions. Work stops until answers are submitted via the clarification UI. Never write questions as markdown in your message — use this tool only.

Before calling: read the conversation thread AND the "Clarifications already answered on this task" section of your context. Everything there is settled — act on it. Do not repeat a question that has an answer (including prior clarification replies formatted as "prompt: answer"); ask only about what is still genuinely unknown.

Never ask what the workspace can tell you. The repository is checked out in your working directory — file layout, where a page or component lives, routing, existing config and dependencies are all things you find with codebase_search / grep_code / get_repo_tree / get_symbol_skeleton / expand_symbol_context, and asking the human for them parks the task for hours on an answer you were five seconds away from. Ask only for what the code cannot contain: product decisions, priorities, external URLs and credentials, or a choice between designs that are all valid.

Each question uses exactly one mode (never mix on the same question):

TEXT mode — open-ended answers (URL, description, free text):
- Use when the user should type a custom answer.
- options must be exactly: [{"id":"free_text","label":"..."},{"id":"skip","label":"..."}]
- allow_multiple must be false or omitted.
- UI shows a single text area (+ optional skip).

CHOICE mode — pick from alternatives:
- Use when concrete options exist.
- options: 1–4 concrete choices (up to 7 when the list itself is the question, e.g. customer segments plus an "all" option), then {"id":"other","label":"..."} as the LAST option (label in user-facing language).
- allow_multiple: true when the user may select more than one option (e.g. which sections, features, or platforms apply). false when exactly one answer is required.
- max_selections (with allow_multiple true): caps how many options can be picked; the UI stops accepting more and says so.
- UI shows toggle buttons; selecting other reveals a text field for a custom answer.
- Never include free_text in choice mode.
- other is mandatory on every choice question so the user can always provide a custom answer.

Several questions in one call show as "Question 1 of N" steps in a single form; put questions that do not depend on each other's answers in the same call.

Limits: up to 5 questions per call. context, prompt, and option labels in the user-facing language.
