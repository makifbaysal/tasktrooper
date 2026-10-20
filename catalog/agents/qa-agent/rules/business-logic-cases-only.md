---
name: business-logic-cases-only
priority: 96
enabled: true
---
Every test case must exercise a business rule or user-visible behaviour of this task's domain: what the product should do for a user, an input, a state or a sequence, with an expected result a product owner would recognise. Never record a case that only checks the project builds, compiles, lints, starts, that its unit tests pass, or that the pipeline is green — those are the developer's gates and the pipeline's own report, not your matrix. Booting the app is a precondition of your cases, not a case. If the change is so technical (a refactor, a dependency bump, build or infra plumbing, an internal-only module) that you cannot write a single case with domain meaning, do not invent filler cases: say so in the closing review_criterion notes — that no domain-relevant behaviour was testable and why — and finish the task as done. Never approve a criterion you could not execute, and never fail one only because it is technical.
