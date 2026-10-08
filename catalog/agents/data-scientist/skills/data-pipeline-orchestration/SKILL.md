---
name: data-pipeline-orchestration
category: mlops
description: Use when adding or changing a scheduled data or ML pipeline — Dagster assets, Prefect flows, Airflow DAGs or a cron'd entrypoint — idempotent partitioned stages, retries, backfills, quality gates, and testing the pipeline without the scheduler
tech_stack: MLOps
source: wshobson/agents ml-pipeline-workflow (MIT), adapted; Dagster, Prefect and Airflow documentation cited
---
# Data Pipeline Orchestration

## Overview

A pipeline runs unattended, gets retried, gets backfilled for last March, and runs twice when someone clicks the button twice. Each stage therefore has to produce the same result for the same inputs no matter how often it runs, and fail loudly when its input is wrong.

**Core principle:** business logic lives in plain, tested functions; the orchestrator layer is a thin wrapper that wires partitions, resources and retries around them.

## 1. Detect the orchestrator — and keep it

| You find | It is |
|---|---|
| `@dg.asset`, `Definitions(...)`, `dagster.yaml`, `workspace.yaml` | Dagster (asset-based) |
| `@flow`, `@task` from `prefect`, `prefect.yaml` | Prefect |
| `dags/` folder, `@dag` / `DAG(...)`, `from airflow` | Airflow (Airflow 3 imports authoring APIs from `airflow.sdk`) |
| `dvc.yaml` stages | DVC pipeline (experiment-tracking-and-reproducibility) |
| a cron entry or a CI schedule calling a CLI | plain entrypoint |

Never add a second orchestrator (rule stack-choice). A plain CLI entrypoint called by the existing scheduler is often the right size.

## 2. Thin orchestration, testable logic

```python
# src/churn/features.py — pure, tested with fixtures
def build_daily_features(orders: pd.DataFrame, day: date) -> pd.DataFrame: ...

# src/churn/orchestration/assets.py — wiring only
import dagster as dg

daily = dg.DailyPartitionsDefinition(start_date="2025-01-01")

@dg.asset(partitions_def=daily)
def daily_features(context: dg.AssetExecutionContext, warehouse: WarehouseResource) -> None:
    day = date.fromisoformat(context.partition_key)
    features = build_daily_features(warehouse.orders_for(day), day)
    warehouse.overwrite_partition("features.daily", day, features)
```

The asset never computes anything itself; `build_daily_features` has unit tests that know nothing about Dagster.

## 3. Idempotency

| ❌ | ✅ |
|---|---|
| `INSERT INTO features SELECT ...` on every run | delete-then-insert the partition in one transaction, `MERGE` on the key, or `INSERT OVERWRITE` the partition |
| `df.to_parquet("out/features.parquet", append...)` | write `out/day=2026-10-01/part.parquet` to a temp path, then rename |
| `window_end = datetime.now()` | window from the partition key or the run's data interval |
| file names with a timestamp of when it ran | paths keyed by what the data is for (partition, model version) |

Test it: run the stage twice on the fixture; the output is identical and the row count does not double.

## 4. Retries, failures and gates

- Retries are for transient I/O (network, a warehouse timeout): `@task(retries=3, retry_delay_seconds=30)` in Prefect, a `RetryPolicy` in Dagster, `retries=` in Airflow. A validation failure is not transient — retrying it three times just delays the alert.
- Quality gates fail the run: a pandera/GX check at ingestion (data-validation-contracts), Dagster `@dg.asset_check`, dbt tests after `dbt build`. Downstream stages do not run on a failed gate.
- A training pipeline ends with an evaluation gate: the candidate is registered or promoted only if it beats the current model and the baseline on the agreed metric, per slice where it matters (model-training-and-evaluation). The gate's numbers are logged.
- Log stage-level metrics: rows in, rows out, rows rejected, duration — so a silent drop is visible.

## 5. Backfills and partitions

- Partition by the natural time grain (day, hour) or by entity set; every stage takes its partition as input.
- A backfill is the same code over a range of partitions — never a separate script.
- Dry-run one partition first and compare its output with the existing data for that partition.
- A production backfill or full rebuild is deploy-time work: record it in `before_deploy` / `after_deploy`, with how long it takes and how to stop it (rule deploy-runbook-fields).

## 6. Testing without the scheduler

```python
def test_daily_features_overwrites_its_partition(tmp_warehouse) -> None:
    for _ in range(2):
        result = dg.materialize(
            [daily_features],
            partition_key="2026-10-01",
            resources={"warehouse": tmp_warehouse},
        )
        assert result.success
    assert tmp_warehouse.row_count("features.daily", day=date(2026, 10, 1)) == 3
```

- Dagster: `dg.materialize([...], partition_key=..., resources=...)` with a local resource (DuckDB file in `tmp_path`, an in-memory fake).
- Prefect: call the flow function directly inside `prefect_test_harness()`.
- Airflow: a DagBag import test (no import errors, no cycles) plus `dag.test()` for one run; keep task bodies as calls into your package.
- Every pipeline test uses fixtures and local resources — never production credentials or buckets.

## 7. Secrets and configuration

Credentials come from the orchestrator's resources, connections or environment — never in code, never in DAG parameters. A new variable the deployed pipeline reads is declared with declare_env_vars. In Airflow, never render user-supplied parameters into Jinja-templated SQL or shell commands; pass them as bound parameters.

## 8. Rollout

A changed pipeline runs side by side (shadow output to a separate table or path) before it replaces the old output, when the task's risk warrants it; the cut-over and the rollback (point consumers back, re-run the old version for affected partitions) go in `after_deploy` and `rollback_plan`.

## Common Mistakes

- Transformation logic written inline in a DAG file, testable only by running Airflow.
- Appending output, so every retry duplicates rows.
- `now()` inside a stage, so a backfill computes today's window for last year's partition.
- Retrying data-quality failures.
- A schedule change that silently overlaps two runs writing the same partition.

## Red Flags

- Row counts that double after a re-run.
- A stage with no rows-in/rows-out logging.
- A backfill script that is not the production code path.
- A credential in a DAG file, a flow parameter or a notebook cell.
