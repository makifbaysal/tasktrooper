---
name: eda-and-data-profiling
category: data
description: Use when you meet a new dataset, table, file or source, before building features or a model on it, or when a number looks wrong — profile grain, keys, nulls, cardinality, time coverage and target balance, and persist what you learn as code
tech_stack: Python
source: anthropics/knowledge-work-plugins explore-data (Apache-2.0), adapted; DuckDB SUMMARIZE and pandas docs cited
---
# EDA and Data Profiling

## Overview

Exploration answers "what is actually in this data?" before anything is built on it. In this role exploration has a deliverable: the facts you learn become a schema, a test or a documented assumption in the repository — not a notebook cell nobody re-runs.

**Core principle:** every surprise you find in EDA ends up as a check that would catch it next time (data-validation-contracts) or a written assumption in your closing message.

## 1. Structure first

Answer these before looking at any distribution:

- **Grain:** one row is one what? (an order, an order line, a customer-day)
- **Key:** which column(s) identify a row — and are they actually unique?
- **Size and span:** row count, earliest and latest timestamp, any missing days.
- **Freshness:** when was it last updated; is the latest period complete?
- **Column roles:** identifier, dimension, metric, timestamp, free text, boolean, nested.

```python
def assert_grain(df: pd.DataFrame, key: list[str]) -> None:
    dupes = df.duplicated(subset=key, keep=False)
    if dupes.any():
        raise ValueError(f"{int(dupes.sum())} rows share a {key} key, e.g. {df.loc[dupes, key].head(3).to_dict('records')}")
```

## 2. Profile every column

| Column kind | Look at |
|---|---|
| All | null rate, distinct count, cardinality ratio, top and bottom values |
| Numeric | min, p1, p25, median, mean, p75, p99, max; zeros; negatives where impossible |
| String / category | empty strings, whitespace, case variants ("US", "us", "USA"), unexpected categories |
| Timestamp | min/max, future dates, gaps per day/week, timezone (naive vs aware) |
| Target | class balance or distribution, by time period |

Fast paths:

```python
duckdb.sql("SUMMARIZE SELECT * FROM 'data/events/*.parquet'").df()
```

```python
def profile(df: pd.DataFrame) -> pd.DataFrame:
    return pd.DataFrame({
        "dtype": df.dtypes.astype(str),
        "null_rate": df.isna().mean(),
        "n_unique": df.nunique(dropna=True),
        "example": df.apply(lambda s: s.dropna().iloc[0] if s.notna().any() else None),
    })
```

Profile on the full data or a documented sample — a 1,000-row head of a file sorted by date tells you about one day.

## 3. Quality signals that need a decision

| Signal | Likely cause | Decide |
|---|---|---|
| Null rate > 5% in a key column | upstream gap, late arrival, optional field | impute in the pipeline, exclude with a reason, or flag |
| Placeholder values (0, -1, 999999, "N/A", "test", 1970-01-01) | defaults standing in for missing | convert to NA at ingestion, with a test |
| One value with suspicious frequency | a default or a backfill | confirm with the source owner, or note it |
| ID with far fewer distinct values than rows expected | wrong join key or truncated extract | stop and trace before modelling |
| Status says done but the done-timestamp is null | cross-column inconsistency | a schema check across columns |
| Values all ending in 0 or 5 | estimates, not measurements | note it as a precision limit |
| Distribution shift between old and recent months | drift, a tracking change, a definition change | evaluate on the recent period separately |

Do not silently drop outliers: decide whether each group is an error (fix or remove, with a count), a genuine extreme (keep, use robust statistics) or another population (segment it).

## 4. Relationships and joins

- For every join you plan, measure coverage: what share of left rows find a match, and how many matches each gets.
- Check foreign keys that point at nothing (referential gaps) and keys that appear many times on the side you expected to be unique.
- Look for derived or redundant columns (a total next to its parts) and columns that are suspiciously predictive of the target — a single-feature ROC-AUC near 1.0 is usually leakage.

```python
merged = events.merge(users, on="user_id", how="left", validate="many_to_one", indicator=True)
unmatched = (merged["_merge"] == "left_only").mean()
```

## 5. Persist what you learned

- A schema at the ingestion boundary encoding the facts: key uniqueness, allowed ranges, allowed categories, nullable columns (data-validation-contracts).
- A test for every cleaning rule EDA motivated ("placeholder -1 in `age` becomes NA").
- Plots and long profiles go to `/tmp/tt-<task key>/` while you work; commit them only where the repository keeps reports.
- Your closing message states the key numbers: rows, span, null rates that mattered, what you excluded and how many rows that was.

## Common Mistakes

- Profiling a head or a non-random sample and generalising.
- Assuming a key is unique because its name ends in `_id`.
- Treating the latest, incomplete day or month as a real drop.
- Fixing a quality issue in a notebook and never in the pipeline.
- Comparing timestamps from sources in different timezones.

## Red Flags

- Row count changes after a join you believed was one-to-one.
- A target rate that jumps on a specific date — check for a definition or tracking change before modelling it.
- Exact round totals or 0%/100% rates — usually a filter or a default value.
- "The data looks fine" without a single number to show for it.
