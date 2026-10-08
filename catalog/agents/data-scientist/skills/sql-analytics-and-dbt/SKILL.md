---
name: sql-analytics-and-dbt
category: sql
description: Use when writing or changing analytical SQL or a dbt model — CTE structure, window functions, grain and join correctness, dbt data tests, unit tests, contracts and incremental models, and how to run them
tech_stack: SQL & dbt
source: original; informed by the dbt documentation (data tests, unit tests, model contracts, incremental models, node selection) — cited, not reproduced
---
# SQL Analytics and dbt

## Overview

Analytical SQL fails quietly: a join that fans out, a NULL that drops rows from a `NOT IN`, a running total over the wrong frame. The query runs, the dashboard shows a number, the number is wrong. dbt gives you the tools to make SQL testable — use them the way you use pytest.

**Core principle:** every model declares its grain and tests it; logic is proven with a dbt unit test before it is trusted with a real table.

## 1. Detect the project's conventions

Read `dbt_project.yml`, `packages.yml` (is `dbt_utils` installed?), the adapter in `profiles.yml` or CI (Postgres, Snowflake, BigQuery, Databricks, DuckDB), any `.sqlfluff` config, and the existing folder and naming scheme (`staging/stg_<source>__<entity>`, `intermediate/`, `marts/fct_*`, `dim_*`). Follow them. SQLMesh projects follow SQLMesh's own model and audit conventions — never mix the two.

## 2. Model structure

```sql
with

orders as (
    select * from {{ ref('stg_shop__orders') }}
),

order_lines as (
    select * from {{ ref('stg_shop__order_lines') }}
),

line_totals as (
    select
        order_id,
        sum(amount) as order_value,
        count(*) as line_count
    from order_lines
    where status != 'cancelled'
    group by order_id
),

final as (
    select
        orders.order_id,
        orders.customer_id,
        orders.ordered_at,
        coalesce(line_totals.order_value, 0) as order_value,
        coalesce(line_totals.line_count, 0) as line_count
    from orders
    left join line_totals using (order_id)
)

select * from final
```

- `ref()` and `source()` only — never a hard-coded `schema.table`.
- Aggregate to the join key **before** joining (`line_totals`), so the join stays one-to-one.
- One transformation per CTE, named for what it holds; explicit column lists in the final select.

## 3. Window functions without surprises

```sql
-- latest row per key, deterministic tie-break
select *
from events
qualify row_number() over (partition by event_id order by loaded_at desc, _row_id desc) = 1

-- running total: say ROWS, or ties on order_date are summed together
sum(amount) over (
    partition by customer_id
    order by order_date, order_id
    rows between unbounded preceding and current row
) as running_spend

-- previous value
lag(order_date) over (partition by customer_id order by order_date, order_id) as previous_order_date
```

`QUALIFY` exists in Snowflake, BigQuery, Databricks and DuckDB; elsewhere filter `row_number() = 1` in an outer query. A window `ORDER BY` without a unique tie-breaker gives a different "latest row" on each run.

## 4. SQL pitfalls checklist

| Pitfall | Fix |
|---|---|
| `x not in (select y ...)` where `y` can be NULL returns no rows | `not exists (...)` |
| Integer division (`1 / 2 = 0` in Postgres, Redshift, SQL Server) | cast one side to numeric/float |
| Division by zero | `x / nullif(y, 0)` |
| `between` on timestamps includes the end instant | half-open: `>= start and < end` |
| Filter on the right table of a LEFT JOIN in `WHERE` turns it into an inner join | move the condition into `ON` |
| `count(col)` skips NULLs, `count(*)` does not | choose deliberately; `count(distinct key)` for entities through joins |
| `select distinct` masking a fan-out | fix the join grain instead |
| Date truncation in local time vs UTC | convert to one timezone first, state which |
| Average of per-group averages | aggregate from rows, or weight by group size |
| `union` silently deduplicating | `union all` unless dedup is intended |

## 5. Tests: grain, relationships, contracts

```yaml
models:
  - name: fct_orders
    config:
      contract:
        enforced: true
    columns:
      - name: order_id
        data_type: varchar
        constraints:
          - type: not_null
        data_tests:
          - unique
          - not_null
      - name: customer_id
        data_type: varchar
        data_tests:
          - relationships:
              to: ref('dim_customers')
              field: customer_id
      - name: order_value
        data_type: numeric
```

- Every model: `unique` + `not_null` on its key. A composite key uses `dbt_utils.unique_combination_of_columns` when the repo has dbt_utils, otherwise a surrogate key column with the two tests.
- Enforced contracts on marts that other teams or dashboards consume: a column rename or type change fails the build instead of the dashboard.
- Newer dbt versions accept generic-test arguments nested under `arguments:` — write tests in the form the repository's YAML already uses.

## 6. dbt unit tests — TDD for SQL (dbt 1.8+)

```yaml
unit_tests:
  - name: fct_orders_excludes_cancelled_lines
    model: fct_orders
    given:
      - input: ref('stg_shop__orders')
        rows:
          - {order_id: o1, customer_id: c1, ordered_at: '2026-01-05'}
          - {order_id: o2, customer_id: c1, ordered_at: '2026-01-06'}
      - input: ref('stg_shop__order_lines')
        rows:
          - {order_id: o1, amount: 10, status: ok}
          - {order_id: o1, amount: 5, status: cancelled}
    expect:
      rows:
        - {order_id: o1, order_value: 10, line_count: 1}
        - {order_id: o2, order_value: 0, line_count: 0}
```

Write the unit test first, watch `dbt test --select fct_orders` fail, then change the SQL. Unit tests need the model's parents to exist as relations; `dbt run --select +fct_orders --empty` creates them without data in the dev target.

## 7. Incremental models

```sql
{{ config(
    materialized='incremental',
    unique_key='order_id',
    incremental_strategy='merge',
    on_schema_change='append_new_columns'
) }}

select ...
from {{ ref('stg_shop__orders') }}
{% if is_incremental() %}
where updated_at > (select {{ dbt.dateadd('day', -3, 'max(updated_at)') }} from {{ this }})
{% endif %}
```

- A `unique_key` so re-processed rows update instead of duplicating; a lookback window for late-arriving rows.
- Prove it: build twice in dev and the `unique` test still passes; a `--full-refresh` build and an incremental build agree on row counts for the same period.
- A required full refresh or backfill in production is deploy-time work: record it in `before_deploy` / `after_deploy` (rule deploy-runbook-fields).

## 8. Running it

| Goal | Command |
|---|---|
| Parse without a warehouse | `dbt parse` |
| See the compiled SQL | `dbt compile --select fct_orders` (then read `target/compiled/...`) |
| Look at rows | `dbt show --select fct_orders --limit 20` |
| Build + test what changed and its children | `dbt build --select state:modified+ --state <prod artifacts>` (only when the repo keeps state artifacts) |
| Otherwise | `dbt build --select fct_orders+` against the dev target |

Credentials come from `env_var()` in `profiles.yml`, never from the repo. With no warehouse access here, run what you can (`dbt parse`, a DuckDB target if the project has one) and say exactly which tests did not run.

## 9. SQL from Python

Bind parameters — never format values into SQL strings:

```python
# ❌ injection and quoting bugs
pd.read_sql(f"select * from orders where country = '{country}'", engine)

# ✅
pd.read_sql(text("select * from orders where country = :country"), engine, params={"country": country})
```

## Common Mistakes

- Joining before aggregating, then "fixing" the inflated sum with `distinct`.
- A model without a key test.
- Incremental filter with no lookback, silently dropping late rows.
- Window functions ordered by a non-unique column.
- Contract enabled with column types that differ by warehouse.

## Red Flags

- Row count of a model larger than its driving table on a one-to-one design.
- A key test disabled or set to `severity: warn` without a reason.
- `select *` from a source in a mart.
- Results that change between two runs on unchanged data.
