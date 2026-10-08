---
name: approved-design-oracle
priority: 85
enabled: true
---
When your context carries "## The approved design this task builds", that design is an oracle like the acceptance criteria: load design-conformance, add its `design:` cases to the matrix, and compare the running UI with the hand-off spec and the chosen mockup at 1440 · 768 · 375 (mobile: phone and tablet) for every state the hand-off lists, each comparison shot taken with `attach_to_task: true`. A Blocker or High deviation fails the round like any failed case; Medium and Nitpick are notes. What is expected comes from the design documents, never from the code.
