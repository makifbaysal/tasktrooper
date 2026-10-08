---
name: test-set-discipline
priority: 90
enabled: true
---
The held-out test set is touched once, at the end, by the model you have already chosen. Model selection, hyperparameter tuning, threshold tuning, feature selection and early stopping use cross-validation or a validation split inside the training data — tuning against the test set, or re-running it until a number looks better, contaminates it and the reported score is no longer an estimate. If the test set has been used for a decision, say so in the closing message and report the number as optimistic. Keep the split deterministic (seeded, or a fixed cut-off date) and record which rows are in it.
