---
name: experiment-tracking-and-reproducibility
category: mlops
description: Use when a change trains a model, reports a metric, or touches seeds, data or model versions, the lockfile, MLflow or DVC — what every run must log, seeding, environment pinning, data versioning and keeping tests off the shared tracking server
tech_stack: MLOps
source: original; informed by the MLflow 3 tracking and model registry docs, the DVC pipelines docs and the PyTorch reproducibility notes (cited, not reproduced)
---
# Experiment Tracking and Reproducibility

## Overview

A metric nobody can reproduce is an anecdote. Reproducing a run needs five things: the code (git SHA), the data (a version or hash), the parameters, the environment (lockfile) and the seed. Log all five with every run, and make the training entrypoint a deterministic function of them.

**Core principle:** if two runs with the same five inputs can produce different models, the difference must be explainable (GPU non-determinism, thread scheduling) and bounded — never "randomness" from an unseeded call.

## 1. What every run logs

| Item | How |
|---|---|
| Code | git SHA and a dirty flag |
| Data | DVC hash from `dvc.lock`, a snapshot date plus query, or a content hash of the training frame |
| Parameters | the full resolved config, including defaults |
| Environment | hash of the lockfile (`uv.lock`, `poetry.lock`), Python version, key library versions |
| Seed | the one seed the run derived everything from |
| Metrics | mean and spread across folds, the baseline's score, the test score once (rule test-set-discipline) |
| Artifact | the fitted pipeline in a safe format (rule safe-model-serialization), with an input example |

```python
def data_fingerprint(df: pd.DataFrame) -> str:
    return hashlib.sha256(pd.util.hash_pandas_object(df, index=True).to_numpy().tobytes()).hexdigest()[:16]

def code_and_env() -> dict[str, str]:
    sha = subprocess.run(["git", "rev-parse", "HEAD"], capture_output=True, text=True, check=True).stdout.strip()
    dirty = bool(subprocess.run(["git", "status", "--porcelain"], capture_output=True, text=True, check=True).stdout)
    lock = hashlib.sha256(Path("uv.lock").read_bytes()).hexdigest()[:16]
    return {"git_sha": sha, "git_dirty": str(dirty), "lock_sha": lock, "python": platform.python_version()}
```

## 2. MLflow 3

```python
with mlflow.start_run(run_name=f"churn-{cfg.seed}"):
    mlflow.log_params(cfg.model_dump())
    mlflow.set_tags({**code_and_env(), "data_fingerprint": data_fingerprint(train_df), "seed": str(cfg.seed)})
    mlflow.log_input(mlflow.data.from_pandas(train_df, name="train", targets="churned"), context="training")

    cv = cross_validate(pipe, X_train, y_train, cv=splitter, scoring="average_precision")
    mlflow.log_metrics({
        "cv_ap_mean": cv["test_score"].mean(),
        "cv_ap_std": cv["test_score"].std(),
        "baseline_ap": y_train.mean(),
    })

    pipe.fit(X_train, y_train)
    info = mlflow.sklearn.log_model(
        sk_model=pipe,
        name="model",
        input_example=X_train.head(5),
        skops_trusted_types=REVIEWED_SKLEARN_TYPES,
    )
```

- MLflow 3 logs models as first-class `LoggedModel`s: `log_model(..., name=...)` replaces the 2.x `artifact_path=` argument, and `info.model_uri` points at the logged model.
- Use registry **aliases** (`champion`, `challenger`) and load `models:/churn@champion`; registry stages are deprecated.
- Follow the repository's pinned MLflow and its existing logging helpers; W&B or another tracker in the repo stays the tracker (rule stack-choice).
- Recent MLflow 3 releases save scikit-learn models with skops (outside Databricks) and refuse types that are not on a trusted list: `REVIEWED_SKLEARN_TYPES` names only the types you checked (a `HistGradientBoosting*` model needs its `TreePredictor`), never everything `get_untrusted_types()` reports. Older pins and Databricks use cloudpickle — load those only from the repository's own tracking server or registry (rule safe-model-serialization).

## 3. Tests never touch the shared tracking server

```python
@pytest.fixture(autouse=True)
def local_mlflow(tmp_path, monkeypatch):
    monkeypatch.setenv("MLFLOW_TRACKING_URI", f"sqlite:///{tmp_path / 'mlflow.db'}")
```

A test asserts what the run logged (params, the metric keys, the data fingerprint tag) by reading it back from this local store. Your own end-to-end exercise of the pipeline logs to `/tmp/tt-<task key>/` the same way.

## 4. Seeds

```python
# ❌ global, legacy, invisible
np.random.seed(42)
idx = np.random.permutation(n)

# ✅ one generator, passed down; integer seeds for estimators and splitters
rng = np.random.default_rng(cfg.seed)
idx = rng.permutation(n)
splitter = StratifiedKFold(n_splits=5, shuffle=True, random_state=cfg.seed)
model = HistGradientBoostingClassifier(random_state=cfg.seed)
```

- Python's `random.seed(cfg.seed)` too if any dependency uses it.
- LightGBM: `seed=`, plus `deterministic=True` and a fixed `num_threads` when bit-identical results matter. XGBoost: `random_state=`.
- PyTorch has its own list (deep-learning-pytorch).
- A determinism test (two fits, same seed, identical predictions) belongs in the suite (ml-testing-strategy). Across different machines, compare with a tolerance — floating-point sums depend on thread scheduling.

## 5. Environment

- The lockfile is the environment; `uv sync --locked` (or the repo's equivalent) builds it. Never `pip install` something ad hoc to get a run to work.
- Pin CPU wheels for CI and tests if the repository separates CPU and GPU dependencies.
- A container image used for training is referenced by digest, not by a moving tag.

## 6. DVC for data and pipelines

```yaml
stages:
  features:
    cmd: uv run python -m churn.pipelines.features --out data/features.parquet
    deps:
      - src/churn/features
      - data/raw/events.parquet
    outs:
      - data/features.parquet
  train:
    cmd: uv run python -m churn.pipelines.train --features data/features.parquet --out models/
    deps:
      - src/churn/models
      - data/features.parquet
    params:
      - train.seed
      - train.learning_rate
    outs:
      - models/model.skops
    metrics:
      - reports/metrics.json:
          cache: false
```

- `dvc repro` re-runs only stages whose deps or params changed; `dvc.lock` records the hashes — commit it.
- `dvc exp run -S train.learning_rate=0.05` and `dvc exp show` compare runs; `dvc metrics diff` compares against the default branch.
- `dvc pull`/`dvc push` need remote credentials. If the workspace has none, say so and run the stage on the fixture instead; never commit the data to git to work around it.

## 7. Idempotent stages

Each stage writes its full output to a deterministic location for its inputs (`features/date=2026-10-01/`) and overwrites it on re-run; appending to a shared file makes a retry double the data (data-pipeline-orchestration).

## Common Mistakes

- Logging the metric but not the data version, so a better score cannot be told apart from a different dataset.
- A `random_state` on the model but not on the splitter.
- Tests that log to the team's tracking server.
- Comparing two runs trained on different seeds and calling the difference an improvement.
- A dirty working tree recorded as a clean SHA.

## Red Flags

- `np.random.seed` or `random.random()` in library code.
- A metric in the closing message that no logged run contains.
- A model in the registry with no link to the run, data and code that produced it.
- `uv.lock` changed in a diff with no dependency change in `pyproject.toml`.
