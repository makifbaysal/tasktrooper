---
name: model-training-and-evaluation
category: ml
description: Use when training, tuning, comparing or evaluating a model — choosing the cross-validation scheme, baselines, tuning inside CV, thresholds and calibration, per-slice metrics, feature importance and error analysis
tech_stack: Python
source: original; informed by the scikit-learn model selection, model evaluation and probability calibration guides and the Optuna and SHAP documentation (cited, not reproduced)
---
# Model Training and Evaluation

## Overview

A model's score is an estimate, and its quality depends on how it was estimated: the split that matches the data's structure, the baseline it is compared with, the tuning kept away from the final test, and the spread around the number. Gradient-boosted trees on tabular data are the strong default; anything more complex has to beat them on the same folds.

**Core principle:** every number you report answers "compared with what, measured how, and how sure" — baseline, CV scheme, spread.

## 1. Pick the CV scheme from the data

| Data | Splitter |
|---|---|
| Independent rows, classification | `StratifiedKFold(n_splits=5, shuffle=True, random_state=seed)` |
| Independent rows, regression | `KFold(n_splits=5, shuffle=True, random_state=seed)` |
| Repeated entities (users, patients, stores) | `GroupKFold` / `StratifiedGroupKFold` with `groups=` |
| Time-ordered, forecasting or scoring the future | `TimeSeriesSplit(n_splits=5, gap=horizon)` or a rolling-origin split by date |
| Small data (< a few thousand rows) | `RepeatedStratifiedKFold` for a stabler mean and spread |

The scheme mirrors production: a model that scores next month is validated on a later month than it trained on. Leakage rules apply throughout (leakage-safe-feature-engineering).

## 2. Baselines on the same folds

```python
scoring = "average_precision"
baseline = cross_validate(DummyClassifier(strategy="prior"), X_train, y_train, cv=cv, scoring=scoring)
linear = cross_validate(make_linear(seed), X_train, y_train, cv=cv, scoring=scoring)
gbm = cross_validate(make_gbm(seed), X_train, y_train, cv=cv, scoring=scoring)
```

Always include the dummy and the simple rule from ml-problem-framing. A complex model that beats the linear one by less than the fold-to-fold spread is not better.

## 3. Tune inside cross-validation

Hyperparameter search runs on the training data with the same CV; the test set is not involved (rule test-set-discipline).

```python
def objective(trial: optuna.Trial) -> float:
    params = {
        "learning_rate": trial.suggest_float("learning_rate", 1e-3, 0.3, log=True),
        "max_leaf_nodes": trial.suggest_int("max_leaf_nodes", 15, 255, log=True),
        "l2_regularization": trial.suggest_float("l2_regularization", 1e-8, 10.0, log=True),
    }
    return cross_val_score(make_gbm(seed, **params), X_train, y_train, cv=cv, scoring=scoring).mean()

study = optuna.create_study(direction="maximize", sampler=optuna.samplers.TPESampler(seed=seed))
study.optimize(objective, n_trials=50)
```

- The best CV score of a search is optimistically biased; the honest estimate is the untouched test set, or nested CV when there is no test set.
- Early stopping takes its validation rows from the training fold (`HistGradientBoosting*(early_stopping=True, validation_fraction=0.1)`, or an `eval_set` carved from the training fold for XGBoost/LightGBM) — never from the test set.
- Keep search spaces small and log-scaled; 50 sensible trials beat 1,000 over nonsense ranges.

## 4. Report with spread

```python
res = cross_validate(model, X_train, y_train, cv=cv, scoring=scoring)
print(f"{scoring}: {res['test_score'].mean():.3f} ± {res['test_score'].std():.3f} over {cv.get_n_splits()} folds")
```

For the final test set, a bootstrap interval:

```python
def bootstrap_ci(y_true, y_score, metric, n=1000, seed=0, alpha=0.05):
    rng = np.random.default_rng(seed)
    stats = []
    for _ in range(n):
        i = rng.integers(0, len(y_true), len(y_true))
        if np.unique(y_true[i]).size > 1:
            stats.append(metric(y_true[i], y_score[i]))
    return np.quantile(stats, [alpha / 2, 1 - alpha / 2])
```

Report baseline and model side by side, with the number of rows and positives (rule report-uncertainty).

## 5. Thresholds and calibration

- The decision threshold is tuned, not assumed: `TunedThresholdClassifierCV(model, scoring=..., cv=cv)` optimises it on CV folds for the metric or cost the decision cares about.
- When probabilities are consumed as probabilities (expected value, pricing, a risk shown to people), check calibration on held-out data: `calibration_curve`, Brier score. Fix with `CalibratedClassifierCV(model, method="isotonic" | "sigmoid", cv=5)`; isotonic needs plenty of data (roughly a thousand or more calibration rows), sigmoid otherwise.
- Class weights or resampling change the predicted probabilities — calibrate after them.

## 6. Slices

An average hides the segment where the model fails. Report the metric per meaningful slice — plan, country, new vs tenured, device, most recent period — with `n` per slice:

```python
def metrics_by_slice(df: pd.DataFrame, slice_col: str) -> pd.DataFrame:
    rows = []
    for value, g in df.groupby(slice_col, observed=True):
        has_both = g["y"].nunique() > 1
        rows.append({
            slice_col: value,
            "n": len(g),
            "positive_rate": g["y"].mean(),
            "pr_auc": average_precision_score(g["y"], g["score"]) if has_both else np.nan,
        })
    return pd.DataFrame(rows).sort_values("pr_auc")
```

Where the model affects people, include the fairness-relevant attributes the task or policy names, and report gaps rather than hiding them.

## 7. Importance and explanations

- `permutation_importance(model, X_val, y_val, scoring=scoring, n_repeats=10, random_state=seed)` on held-out data — not on training data, where an overfit feature looks important.
- SHAP for tree models: `shap.TreeExplainer(pipe[-1])` on `pipe[:-1].transform(X_val)`; use it for per-prediction reasons when the task asks for them.
- Correlated features split or swap importance; importance is association with the model's output, not a causal effect. Say so when reporting.

## 8. Error analysis

Before calling a model done, read the 20 most confident false positives and false negatives. Look for label noise, a missing feature, a segment, a data bug. Each finding is either a fix (with a test) or a documented limitation in your closing message.

## 9. The final evaluation

1. Choose the model and parameters from CV only.
2. Refit on the full training data.
3. Evaluate once on the test set; report it with its interval next to the baseline.
4. Log everything (experiment-tracking-and-reproducibility) and save the fitted pipeline as one artifact in a safe format (rule safe-model-serialization).

## Common Mistakes

- Comparing models on different splits or different metrics.
- Accuracy on an imbalanced target; ROC-AUC when only the top of the ranking matters.
- Early stopping on the test set.
- Reporting the best of 200 search trials as the expected performance.
- Feature importance from training data.

## Red Flags

- Fold scores that vary wildly — too little data, leakage in some folds, or a time effect.
- A test score clearly higher than the CV score.
- A complex model "wins" by less than one standard deviation.
- No per-slice view for a model that will be used on different populations.
