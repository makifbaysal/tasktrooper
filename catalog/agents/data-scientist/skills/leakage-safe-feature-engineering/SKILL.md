---
name: leakage-safe-feature-engineering
category: ml
description: Use when building or changing features, preprocessing, encoders, resampling or train/validation/test splits — keep every fitted transform inside a Pipeline fitted on training folds only, and build time and group features without seeing the future or the same entity twice
tech_stack: Python
source: original; informed by scikit-learn Common pitfalls and recommended practices and the cross-validation user guide (cited, not reproduced)
---
# Leakage-Safe Feature Engineering

## Overview

Leakage is information in training that will not exist at prediction time. It produces a great offline score and a model that fails in production — and no test fails, because the code does exactly what it was told. The three sources: transforms fitted on rows they later score, features computed from the future, and the same entity on both sides of a split.

**Core principle:** split first; everything that learns from data (scalers, imputers, encoders, selectors, resamplers) is fitted inside the training fold only — in practice, inside one scikit-learn `Pipeline` that is cross-validated and shipped as a single artifact.

## 1. Fit inside the Pipeline, never before the split

```python
# ❌ the scaler and the selector have seen the test rows
X_scaled = StandardScaler().fit_transform(X)
X_sel = SelectKBest(k=20).fit_transform(X_scaled, y)
X_train, X_test, y_train, y_test = train_test_split(X_sel, y)

# ✅ split once, then everything learned lives in the pipeline
X_train, X_test, y_train, y_test = train_test_split(
    X, y, test_size=0.2, stratify=y, random_state=seed
)
preprocess = ColumnTransformer([
    ("num", make_pipeline(SimpleImputer(strategy="median"), StandardScaler()), numeric_cols),
    ("cat", OneHotEncoder(handle_unknown="ignore", min_frequency=20), categorical_cols),
])
model = make_pipeline(preprocess, SelectKBest(k=20), LogisticRegression(max_iter=1000))
scores = cross_validate(model, X_train, y_train, cv=cv, scoring="average_precision")
```

`cross_validate` refits the whole pipeline per fold, so imputation medians, category vocabularies and selected features are learned from that fold's training part only. Use `set_output(transform="pandas")` when you need column names downstream.

## 2. Encoders and resampling

- **Target encoding:** scikit-learn's `TargetEncoder.fit_transform` uses internal cross-fitting; `fit(X, y).transform(X)` on the same rows does not and leaks the target. Inside a `Pipeline`, `fit` calls `fit_transform`, which is the safe path. Never compute per-category target means with a `groupby` over the full table.
- **Frequency/count encoding, aggregates, PCA, clustering features:** fitted transforms like any other — inside the pipeline.
- **Class imbalance:** prefer `class_weight="balanced"` or threshold tuning. Oversampling (SMOTE) goes inside an `imblearn.pipeline.Pipeline` only if the repository already depends on imbalanced-learn; oversampling before the split copies near-identical rows into validation.

## 3. Time: only what was known at prediction time

When rows are ordered in time, the split, the features and the label each respect it.

```python
cv = TimeSeriesSplit(n_splits=5, gap=horizon_rows)
```

Point-in-time joins: attach the latest feature value strictly before the prediction timestamp.

```python
# ❌ joins the customer's current tier, which may have changed after the snapshot
df = snapshots.merge(customers[["customer_id", "tier"]], on="customer_id")

# ✅ as-of join against the tier history (both frames sorted by the `on` key)
df = pd.merge_asof(
    snapshots.sort_values("snapshot_ts"),
    tier_history.sort_values("changed_at"),
    left_on="snapshot_ts", right_on="changed_at",
    by="customer_id", direction="backward", allow_exact_matches=False,
)
```

Rolling features exclude the current row and anything after it:

```python
def spend_7d(events: pd.DataFrame) -> pd.Series:
    events = events.sort_values(["user_id", "ts"])
    rolled = (
        events.set_index("ts")
        .groupby("user_id")["amount"]
        .rolling("7D", closed="left")
        .sum()
        .fillna(0.0)
    )
    return pd.Series(rolled.to_numpy(), index=events.index, name="spend_7d")
```

The feature window ends before the label window starts; a label that becomes final 60 days later means the last 60 days of snapshots cannot be training rows yet.

## 4. Groups: one entity, one side

If a user, patient, device or store appears in many rows, a random split puts the same entity in train and validation and the model memorises it.

```python
cv = StratifiedGroupKFold(n_splits=5, shuffle=True, random_state=seed)
cross_validate(model, X, y, groups=df["user_id"], cv=cv, scoring="average_precision")
```

Also deduplicate exact and near-exact rows before the split; duplicates across the split are the same leak in a different form.

## 5. Guard tests

A future-rows test pins the time logic:

```python
def test_spend_7d_ignores_rows_after_prediction_time() -> None:
    events = pd.DataFrame({
        "user_id": ["a", "a", "a"],
        "ts": pd.to_datetime(["2026-01-01", "2026-01-03", "2026-01-05"]),
        "amount": [10.0, 20.0, 30.0],
    })
    later = pd.DataFrame({"user_id": ["a"], "ts": pd.to_datetime(["2026-01-06"]), "amount": [1e6]})

    before = spend_7d(events)
    after = spend_7d(pd.concat([events, later], ignore_index=True))

    assert before.tolist() == [0.0, 10.0, 30.0]
    pd.testing.assert_series_equal(after.loc[before.index], before)
```

A shuffled-label test catches leaks you did not think of: train the real pipeline on permuted labels with the real CV; the score must fall to the baseline. If it does not, something in the features encodes the row order, an ID or the label.

```python
def test_pipeline_cannot_learn_shuffled_labels(train_fixture) -> None:
    X, y = train_fixture
    y_shuffled = np.random.default_rng(0).permutation(y)
    score = cross_val_score(build_model(seed=0), X, y_shuffled, cv=5, scoring="roc_auc").mean()
    assert score < 0.6
```

## 6. Training-serving parity

The fitted pipeline is the artifact. Serving calls `pipeline.predict` on the same raw columns training used — it never re-implements the preprocessing in SQL or in the API handler. A test feeds one fixture row through the batch path and the serving path and asserts identical output (model-serving-and-monitoring).

## Common Mistakes

- `fit_transform` on the full dataset "just for scaling".
- Feature selection or hyperparameter search run on all rows, then cross-validated.
- Aggregates (`user_total_spend`) computed over the whole history, including after the snapshot.
- Random `KFold` on time-ordered or repeated-entity data.
- `train_test_split` without `random_state`, so the split differs every run.

## Red Flags

- Validation score far above what the domain suggests is possible, or above the production score of the current system by a wide margin.
- A single feature with near-perfect importance.
- Validation much better than the most recent out-of-time period.
- Preprocessing code that exists twice — once for training, once for serving.
