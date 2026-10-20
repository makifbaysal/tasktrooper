---
name: design-variants
category: design
description: Use when a screen design needs variants - how many, variants as side-by-side rows on the one canvas, what makes two variants genuinely different, recommending one, how the human's choice is read, and narrowing to the chosen one in revision
---
# Design Variants

## Overview

Variants exist so the human can choose a direction with the alternatives in front of them — not to show effort. Two variants that differ only in colour make the human do the designer's job; five variants for a field change waste the review.

**Core principle:** Variants differ in structure, share the design system, are each complete as a row of the one canvas, and arrive with a recommendation.

## How many

Rule variant-count decides: the number the human names (description, comment or task chat), exactly; otherwise two for a new screen or flow and one for a small change to an existing screen. A design system task has none.

## What makes them different

Pick the one or two axes that matter for THIS screen's main task, and vary those:

| Axis | Variant A | Variant B |
|---|---|---|
| Hierarchy — what comes first | the summary (totals, what needs attention) | the list itself |
| Layout | single column | master–detail / split view |
| Disclosure | one long page | tabs, or a stepper |
| Density | comfortable rows with secondary lines | a compact table |
| Interaction model | edit inline | edit in a dialog, or on its own page |

Never a variant axis: palette, radius, shadow, icon set, font — those belong to the design system, and all variants use the same one. Copy is the same in every variant except where the structure forces a different label.

## What each variant carries

- Its own row on the screen's page of the ONE canvas document `design: <feature>` (screen-mockup-html): `Variant A — table first` above `Variant B — summary first`, frames in the same order and left-aligned, so the same frame of two variants sits in the same column and the eye compares them directly. Never one document per variant: separate files cannot be laid side by side.
- A name that states the idea, in its row title and `#notes`: `Variant A — table first`, not `Option 1`.
- One line: the idea, whom it serves best, what it costs (e.g. "needs a new InvoiceSummary organism; the table is reused").
- Both widths and every state (every-state-designed), at the same finish (professional-grade-visuals). An incomplete or rougher variant cannot be fairly compared, and an approved one with missing states becomes guesswork in the hand-off.

## Recommend one

Say which variant you recommend and why — the brief's primary action, the audience's main task, consistency with the existing screens, and how much it reuses from the inventory — in the notes and in your summary comment, naming it by screen and variant. Then record the choice as ONE non-blocking product question with `record_open_questions`:

```
prompt: "Which variant of the invoice list should be built?"
kind: product, blocking: false
recommended_answer: "Invoices — list, Variant A — table first: finance staff scan 50+ invoices a day, and it reuses DataTable as is."
```

## How the choice is read

The human compares the variants on the canvas — page by page, commenting on any of them — and picks one per screen that has variants: by answering the variant question, or with a comment that names it ("Invoices — list: Variant B"). A choice for one screen says nothing about another; a screen drawn once is never offered. Any further lines are the human's note on the choice ("keep B's empty state"): act on it like a review comment — a request to take parts of another variant is a combination, so a new variant (see In revision). Read the choice in this order:

1. The latest human comment naming one of that screen's variants (the legacy `Chosen variant: <document title>` marker counts the same) — it overrides the question, answered or not.
2. Else the answered variant question.
3. Else, at approval, its recommended answer.

## For a small change

One variant, one row on the canvas, drawn next to what is there today: `Today` and `Proposed` frames side by side at the same width, so the reviewer sees the delta without hunting for it. Draw "today" from the code you read, labelled as such. With one variant there is nothing to choose: no variant question.

## In revision

Once a variant is chosen, revise only its row, keeping the canvas title, page headings and ids; the other rows stay as they were, marked `not chosen` in their row title, and the hand-off names the chosen one so they are never built. `update` or `withdraw` the variant question. A request to combine ("A's table with B's summary") is one new row with the next letter (`Variant C — …`), drawn in full, named for what it is.

## Common Mistakes

- Variants in separate documents — the human cannot lay them side by side. They are rows of the one canvas.
- Two variants that differ only in colour or corner radius.
- A third, half-drawn variant "for inspiration".
- No recommendation — the human is left to guess what you would build.
- Recording the variant choice as a blocking question; there is always a recommended answer.
- Building from the recommended answer when a later comment or answer names another variant.

## Red Flags

- A variant missing its 1440 frame or its error state.
- Variant names that say nothing ("Option 1", "Modern").
