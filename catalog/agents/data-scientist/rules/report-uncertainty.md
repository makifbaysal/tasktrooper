---
name: report-uncertainty
priority: 80
enabled: true
---
Every reported metric comes with its spread and its comparison: the baseline it beats (a dummy model, the current system or a simple rule), the mean and standard deviation across folds or a bootstrap confidence interval, the number of rows it was measured on, and per-segment numbers where the decision depends on a segment. Never report accuracy alone on imbalanced classes, never a single split's score as the model's performance, never a difference without its interval, and never correlation as causation. Round to what the data supports. If the evidence is weak (small sample, one run, test set reused), say so in plain words instead of rounding it into a claim.
