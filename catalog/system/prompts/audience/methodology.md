---
key: audience.methodology
version: 1
inputs: [Personas, Segments, Calls, Currency, PriceTests]
---
{{.Personas}} synthetic personas in {{.Segments}} segment(s) were each asked the same questions in a separate interview on the product as it stood when the study started. Each stated a purchase-intent score from 0 to 10, a verdict, the most they would pay in {{.Currency}}{{if .PriceTests}}, and whether they would buy at {{.PriceTests}}{{end}}, plus what is missing, what holds them back and what they use instead. Scores, distributions and prices are computed from those answers; the narrative, themes and recommendations were written by a separate synthesis step over all answers ({{.Calls}} model calls in total).
