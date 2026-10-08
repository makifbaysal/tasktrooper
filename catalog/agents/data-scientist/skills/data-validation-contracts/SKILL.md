---
name: data-validation-contracts
category: data
description: Use when data crosses a boundary into your code (file, API, warehouse table, upload, model input or output), when a join or cleaning step can change row counts, or when adding pandera, Great Expectations, Pydantic or dbt checks
tech_stack: Python
source: wshobson/agents data-quality-frameworks (MIT), adapted; pandera, Great Expectations GX Core 1.x and Pydantic 2 docs cited
---
# Data Validation and Contracts

## Overview

Code that trusts its input computes wrong numbers quietly. A contract states what the data must look like at a boundary and stops the pipeline, with a readable message, when it does not. EDA finds the facts (eda-and-data-profiling); contracts keep them true.

**Core principle:** validate at every boundary, fail loudly with the column and the bad values, and never catch a validation error just to keep going.

## 1. What to check — the dimensions

| Dimension | Question | Typical check |
|---|---|---|
| Completeness | Are required values present? | not-null on keys and required fields |
| Uniqueness | Is the grain what we think? | unique on the (composite) key |
| Validity | Are values in range and in the allowed set? | `ge`/`le`, `isin`, regex, dtype |
| Consistency | Do columns agree with each other? | `shipped_at` not null when `status == "shipped"` |
| Referential integrity | Do foreign keys point at something? | relationship test, anti-join count |
| Timeliness | Is the data fresh and the period complete? | max timestamp within N hours |
| Volume | Is the batch plausibly sized? | row count within a range of recent batches |

Check the columns your logic depends on, not every column — a contract nobody can maintain gets disabled.

## 2. Which tool where — follow the repo

| Boundary | Tool (if the repo has none yet) |
|---|---|
| DataFrames inside Python code and tests | pandera (`import pandera.pandas as pa`; `pandera.polars` for Polars) |
| Pipeline checkpoints with stored results and docs | Great Expectations (GX Core 1.x) — only if the repo already runs it |
| Single records: API payloads, configs, messages | Pydantic 2 models |
| Warehouse tables built by dbt | dbt data tests and model contracts (sql-analytics-and-dbt) |

Never add a second validation library beside the one the repository uses (rule stack-choice).

## 3. pandera schemas as code

```python
import pandas as pd
import pandera.pandas as pa
from pandera.typing import DataFrame, Series

class Orders(pa.DataFrameModel):
    order_id: Series[str] = pa.Field(unique=True)
    customer_id: Series[str]
    amount: Series[float] = pa.Field(ge=0)
    status: Series[str] = pa.Field(isin=["placed", "shipped", "refunded"])
    shipped_at: Series[pd.Timestamp] = pa.Field(nullable=True)

    class Config:
        strict = True
        coerce = True

    @pa.dataframe_check
    def shipped_orders_have_a_date(cls, df: pd.DataFrame) -> pd.Series:
        return ~((df["status"] == "shipped") & df["shipped_at"].isna())

@pa.check_types(lazy=True)
def clean_orders(raw: DataFrame[Orders]) -> DataFrame[Orders]:
    ...
```

- `lazy=True` collects every failure instead of stopping at the first; the `SchemaErrors.failure_cases` frame names column, check and value.
- `strict = True` rejects unexpected columns — a renamed upstream column fails here instead of turning into all-NaN later.
- `coerce = True` is a decision: it converts `"12"` to `12.0`, but a value that cannot be coerced still fails. Turn it off where a wrong type means a broken upstream.
- With pandas 3 the default string dtype is `str` (Arrow-backed when PyArrow is installed); keep pandera and pandas versions pinned together and run the schema tests after either moves.

## 4. Test the contract itself — TDD applies

```python
def test_orders_schema_rejects_negative_amount() -> None:
    bad = valid_orders().assign(amount=[-1.0, 5.0])
    with pytest.raises(pa.errors.SchemaErrors) as err:
        Orders.validate(bad, lazy=True)
    assert set(err.value.failure_cases["column"]) == {"amount"}
```

One failing-fixture test per rule you add: a rule that never failed a test may not be checking anything.

## 5. Joins: assert the cardinality you assume

```python
# ❌ silently duplicates orders when a customer has two rows
df = orders.merge(customers, on="customer_id", how="left")

# ✅ fails immediately if customers is not unique on the key
df = orders.merge(customers, on="customer_id", how="left", validate="many_to_one")
assert len(df) == len(orders)
```

In SQL, compare `COUNT(*)` with `COUNT(DISTINCT key)` before and after the join, or put a unique test on the dimension's key (analytics-result-qa).

## 6. Great Expectations (GX Core 1.x), when the repo uses it

```python
import great_expectations as gx

context = gx.get_context(mode="ephemeral")
batch = (
    context.data_sources.add_pandas("in_memory")
    .add_dataframe_asset("orders")
    .add_batch_definition_whole_dataframe("all")
    .get_batch(batch_parameters={"dataframe": df})
)
suite = gx.ExpectationSuite(name="orders")
suite.add_expectation(gx.expectations.ExpectColumnValuesToNotBeNull(column="order_id"))
suite.add_expectation(gx.expectations.ExpectColumnValuesToBeBetween(column="amount", min_value=0))
assert batch.validate(suite).success
```

GX 1.x removed the 0.x CLI (`great_expectations init`) and `DataContext.run_checkpoint` patterns; production runs use a `ValidationDefinition` inside a `Checkpoint`. Follow the version the repository pins.

## 7. Pydantic for records

```python
class ScoreRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    customer_id: str = Field(min_length=1)
    tenure_days: int = Field(ge=0, le=36_500)
    plan: Literal["free", "pro", "team"]
```

Use it for request bodies, job parameters and configuration (pydantic-settings) — not row-by-row over a large frame, which is slow; validate frames with a frame schema.

## 8. Where failures go

- Fail the run with the failing column, check and a few example values; the orchestrator marks the partition failed (data-pipeline-orchestration).
- Quarantining bad rows is a product decision: only where the repo already has a quarantine table or the task asks for it, and always with a count in the logs.
- Volume and freshness thresholds use ranges or recent-history baselines, not an exact row count that flakes every Monday.

## Common Mistakes

- Validating after the transformation that already broke on the bad value.
- `try: validate(df) except SchemaError: log.warning(...)` — the pipeline continues on bad data.
- A schema that allows every column to be nullable "to be safe".
- Row-level Pydantic validation in a loop over a million rows.
- A merge without `validate=` on a dimension you assumed unique.

## Red Flags

- Row count after a join differs from the left side on a left join you believed was many-to-one.
- A new upstream column or a renamed one that produced no failure anywhere.
- Validation code with no test that feeds it a bad fixture.
- Checks disabled with a comment instead of fixed.
