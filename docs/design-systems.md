---
title: Design systems
description: One design system per project, an optional layer per repository, and the designer agent that derives them from your code and designs screens before they are built.
---

## What it is

A **design system** is the set of colors, type, spacing, radius, elevation and
components every screen of a project is built from. TaskTrooper keeps it in
two layers:

| Layer | Belongs to | Holds |
|---|---|---|
| Base | A project | What every repository of the project shares |
| Repository layer | One repository, optional | Only what that repository deliberately does differently, each with its reason |

An agent working in a repository follows the base of its project with the
repository's layer on top. A layer adds tokens or changes their values; it
never removes one, so every repository speaks the same token names
(`color.primary`, `color.surface`). A repository that belongs to no project
with a design system can still have one of its own: its layer is then the
whole design system.

Each version holds three parts:

- **DESIGN.md** — principles, colors, typography, layout, elevation, shapes,
  components, and the do's and don'ts.
- **Tokens** — the values as a [DTCG](https://www.designtokens.org/) token tree.
- **Component inventory** — the components, their variants and states, and
  where they live in the code.

## Create one from the code you already have

Open the project and go to the **Design System** tab. On a project with no
design system, **Create from the project's code** opens a `design` task for
the `ui-designer` agent. The agent reads the theme, token and component code
of every repository in the project, and then:

1. proposes the base: what the repositories share;
2. proposes a layer for each repository that differs on purpose (a marketing
   site with its own palette, iOS touch targets);
3. lists the differences that are accidental — the same value copied by hand
   and then changed — as drift for implementation tasks to fix;
4. attaches an HTML report with the palette, type scale, components and the
   per-repository comparison.

The task moves to **Analiz Review** on its own. Read the report there, comment
on any passage, or approve it. Approving the task approves every version it
proposed. Once a design system exists the button reads **Update from code**.

A repository's own **Design System** tab does the same for one repository: it
opens a task that derives that repository's layer, or its whole design system
when it has no project base.

## Checks

Every version is checked as it is proposed and whenever you open it. The
Design System tab lists what the checks found:

| Check | Severity | Means |
|---|---|---|
| Contrast below AA | Error | A color and its text color (`primary` and `primary-foreground`, `surface` and `on-surface`) are below 4.5:1 |
| Unresolved alias | Error | A token points at `{a.token}` that does not exist |
| Invalid color | Error | A color token's value is not a color |
| Empty group | Warning | A token group holds no tokens |
| Missing section | Warning | The base's DESIGN.md lacks Overview, Colors, Typography, Components or Do's and Don'ts |

The designer fixes every error before it sends the design system to review.

## Files in the repository

A repository keeps its design system in four generated files, rendered from
the approved versions: `DESIGN.md`, `design/tokens.json`, `design/tokens.css`
(one CSS variable per token) and `design/INVENTORY.md`. The repository's
Design System tab shows them under **Files**. When a design system is approved,
the designer opens an **Apply design system** task for each repository; its
developer writes these files as they are and builds the app's theme from them.
A new repository created in a project that has a design system gets the same
step in its setup task.

## A repository in several projects

When a repository belongs to more than one project that has a design system,
TaskTrooper cannot pick a base for it. The repository's Design System tab
shows a warning and a **Base project** picker; until you choose, only the
repository's own layer applies.

## How agents use it

- Every run in a repository with a design system carries a short design system
  block naming the versions and the start of DESIGN.md.
- Every agent can read the full design system with the `get_design_system`
  tool.
- Only the `ui-designer` agent proposes changes, and only from a `design` task;
  nothing changes until you approve that task.
- Frontend and mobile developers build with the design system's tokens and
  components only. When a change needs something it does not have, they say
  so in their hand-off instead of inventing a value.

## Designing a screen first

When the look of a piece of work is unclear, a `design` task comes before the
implementation tasks. You can open one yourself; the product manager and the
architect open one when the work needs a new look or no approved design
covers the screen.

If the project has no design system yet, the designer does not invent one on
the way: it opens a separate design system task and the screen design waits
for it, then continues with the approved design system.

The designer draws each variant as its own HTML document with every state
side by side — two variants for a new screen, one for a small change, or
exactly as many as you ask for — and saves its screenshots on the task. In
Analiz Review, **Compare variants** shows two of them side by side; **Choose
this variant** records your choice. Approving the task approves the design.

The frontend and mobile tasks are blocked by the design task. They start only
once the design task is **released** — after the designer has written the
hand-off spec and opened the implementation work — and every run on them
carries the approved design. The task's detail shows the same design under
the task: the hand-off spec, the mockups and the designer's screenshots.

## Design review

QA compares a built screen with its approved design at desktop, tablet and
phone widths and saves the screenshots on the task; a large difference fails
the QA round like any other defect. You can also open a design review task
for the designer to compare a built screen with its design.
