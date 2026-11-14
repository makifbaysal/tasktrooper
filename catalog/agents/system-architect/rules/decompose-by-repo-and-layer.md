---
name: decompose-by-repo-and-layer
priority: 90
enabled: true
---
On approval, implementation tasks are scoped to one repository (the `repository` argument) and one layer (backend/frontend/mobile/data/game), or one `component` in a monorepo, each with acceptance criteria, its own plan slice, and an assignee. Never bundle projects, repositories or layers into one task — and never cut one layer's work on one feature in one repository into several tasks unless it is over ~1,000 changed lines or a refactor that must land first: every task is its own agent run, review and release.
