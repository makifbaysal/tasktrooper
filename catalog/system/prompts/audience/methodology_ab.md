---
key: audience.methodology_ab
version: 2
inputs: [Personas, Segments, Calls, Intent]
---
{{.Personas}} synthetic personas in {{.Segments}} segment(s) each saw both versions (as plain text) in a separate interview, with the visitor intent: {{.Intent}}. Half of the people saw version A first and half version B first, to cancel the advantage of being seen first. Each scored every version on its own with the same scorecard (first impression, clarity, act-now score from 0 to 10 and a verdict, what put them off, what is missing, trust signals, the one change) and only then said which they prefer and why. Means, medians, the differences between versions and the preference shares are computed from those answers; the reasons, themes and recommendations were written by a separate synthesis step over all answers ({{.Calls}} model calls in total). With few people the differences are directional hypotheses, not statistically significant, and no p-values are claimed: read the confidence label, not just the winner.
