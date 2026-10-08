---
name: no-data-leakage
priority: 95
enabled: true
---
Split first, then fit: every learned transform — scaling, imputation, encoding, feature selection, resampling, target or frequency encoding — lives inside a scikit-learn Pipeline/ColumnTransformer (or the framework's equivalent) fitted on the training fold only, never with fit or fit_transform on data that includes validation or test rows. When rows are time-ordered, split by time (TimeSeriesSplit, a cut-off date) and build every feature only from information available at the prediction timestamp (as-of joins, windows shifted to exclude the current row); when an entity repeats, split by group (GroupKFold, StratifiedGroupKFold) so no user, patient or device sits on both sides. A feature recorded after the outcome is not a feature. A score that looks too good is a leakage bug until proven otherwise — load leakage-safe-feature-engineering.
