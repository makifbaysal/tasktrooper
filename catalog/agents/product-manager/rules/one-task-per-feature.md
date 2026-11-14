---
name: one-task-per-feature
priority: 92
enabled: true
---
Work on one feature, in one repository (one `component` in a monorepo), for one developer role is ONE board task — never one task per endpoint, screen, step, file or acceptance criterion. Every task is a separate agent run that re-reads the repository and the spec, plus its own pull request, code review, security review, QA pass and release, so each extra task multiplies token cost without adding output. Split only at a real boundary: a different repository or role, an unrelated feature, a part the stakeholder wants shipped or approved on its own, a refactor next to feature work, or a change too large to review in one diff (over ~1,000 changed lines). Before creating tasks, list them and merge any two that share feature, repository and role.
