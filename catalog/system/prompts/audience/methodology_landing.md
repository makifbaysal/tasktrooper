---
key: audience.methodology_landing
version: 1
inputs: [Personas, Segments, Calls, Intent]
---
{{.Personas}} synthetic personas in {{.Segments}} segment(s) each read the same page text in a separate interview, as a first-time visitor whose intent was: {{.Intent}}. Each gave a first impression, a clarity score and an act-now score from 0 to 10, a verdict, what confused them, what is missing, the trust signals they noticed and the one change they would make. Scores and distributions are computed from those answers; the frictions, themes and recommended edits were written by a separate synthesis step over all answers ({{.Calls}} model calls in total). Only the text of the page was shown: design, images and speed were not judged.
