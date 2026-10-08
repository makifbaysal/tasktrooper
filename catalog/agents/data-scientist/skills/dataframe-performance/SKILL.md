---
name: dataframe-performance
category: data
description: Use when dataframe code is slow or runs out of memory, when data outgrows pandas, or when writing pandas 3, Polars or DuckDB code — vectorisation, dtypes, Copy-on-Write, lazy and out-of-core execution, and measuring before and after
tech_stack: Polars & DuckDB
source: original; informed by the pandas 3 Copy-on-Write and string dtype guides, the Polars user guide and the DuckDB Python API docs (cited, not reproduced)
---
# DataFrame Performance

## Overview

Most slow data code is Python looping over rows, reading columns it never uses, or holding strings as generic objects. Fix those in the library the repository already uses before reaching for a new one; when the data truly outgrows memory, DuckDB and Polars process it lazily and out of core.

**Core principle:** measure first, pin behaviour with a test, optimise, measure again — and report both numbers.

## 1. Measure, then pin

```python
start = time.perf_counter()
result = build_features(sample)
print(f"{time.perf_counter() - start:.2f}s, {result.memory_usage(deep=True).sum() / 1e6:.0f} MB")
```

Measure on a realistic sample (not 20 rows, not the full warehouse). Before rewriting a slow function, make sure a fixture test pins its output; the faster version must pass the same test. Put "before → after" timings and memory in your closing message.

## 2. pandas 3 semantics you must write for

pandas 3 makes Copy-on-Write the only mode and a dedicated string dtype the default. Code written for pandas 1.x/2.x can silently stop working:

```python
# ❌ chained assignment — never updates df under Copy-on-Write
df["discount"][df["plan"] == "pro"] = 0.1
# ❌ inplace on a selected column — never updates df
df["age"].fillna(0, inplace=True)

# ✅ one indexing operation on the object you mean to change
df.loc[df["plan"] == "pro", "discount"] = 0.1
df["age"] = df["age"].fillna(0)
```

- A filtered frame behaves as a copy: `sub = df[mask]; sub["x"] = 1` changes `sub` only, with no `SettingWithCopyWarning` and no `.copy()` needed.
- Strings default to the `str` dtype (Arrow-backed when PyArrow is installed; missing values are `NaN`). `df["s"].dtype == object` is now `False` — use `pd.api.types.is_string_dtype`.
- The repository's pinned pandas wins; if it is still on 2.x, the patterns above are also the correct 2.x style.

## 3. Vectorise instead of looping

```python
# ❌ Python function per row
df["tier"] = df.apply(lambda r: "high" if r.spend > 1000 else ("mid" if r.spend > 100 else "low"), axis=1)

# ✅ vectorised
df["tier"] = np.select([df["spend"] > 1000, df["spend"] > 100], ["high", "mid"], default="low")
# ✅ or as an ordered categorical
df["tier"] = pd.cut(df["spend"], bins=[-np.inf, 100, 1000, np.inf], labels=["low", "mid", "high"])
```

| Instead of | Use |
|---|---|
| `iterrows`, `itertuples`, `apply(axis=1)` | column arithmetic, `np.where`, `np.select`, `pd.cut` |
| a loop doing dict lookups | `series.map(mapping)` or a merge |
| groupby, then merge the result back | `groupby(...)[col].transform("sum")` |
| Python string functions per value | the `.str` accessor (fast on the Arrow-backed dtype) |
| `pd.concat` inside a loop | collect frames in a list, concat once |

## 4. Read less, store smaller

```python
pd.read_parquet("events.parquet", columns=["user_id", "ts", "amount"], filters=[("ts", ">=", start)])
pd.read_csv(path, usecols=["user_id", "plan"], dtype={"plan": "category"})
```

- Parquet over CSV for anything read more than once; select columns and push filters into the read.
- `category` for low-cardinality strings; `float32` for model features where the precision is enough.
- Check `df.memory_usage(deep=True)` before and after.
- Drop intermediate frames you no longer need inside long functions; do not keep five versions of the same table alive.

## 5. DuckDB: SQL over files, out of core

```python
con = duckdb.connect()
con.execute("SET memory_limit = '4GB'")
daily = con.execute(
    """
    select user_id, date_trunc('day', ts) as day, sum(amount) as spend
    from read_parquet('data/events/*/*.parquet', hive_partitioning = true)
    where ts >= ?
    group by all
    """,
    [start],
).df()
```

- Reads Parquet/CSV directly, spills to disk past the memory limit, and can query an in-scope pandas or Polars frame by its variable name.
- `.df()` returns pandas, `.pl()` Polars; `COPY (select ...) TO 'out.parquet' (FORMAT parquet)` writes without a round-trip through Python.
- Bind values with `?` parameters; `EXPLAIN ANALYZE` shows where time goes.

## 6. Polars: lazy expressions

```python
spend = (
    pl.scan_parquet("data/events/*.parquet")
    .filter(pl.col("ts") >= start)
    .group_by("user_id")
    .agg(pl.col("amount").sum().alias("spend"), pl.len().alias("n_events"))
    .collect(engine="streaming")
)
```

- `scan_*` + `collect()` lets the optimiser push filters and column selection into the read; `.explain()` prints the plan.
- `pl.when(...).then(...).otherwise(...)` and `.over("user_id")` replace row loops and groupby-merge-back.
- `map_elements` runs Python per value — the same trap as `apply(axis=1)`.
- Polars 2.0 shipped in October 2026 with breaking changes. A repository pinned to `polars<2` stays there; the code above is written against the 1.x API — check the changelog before using it on 2.x.

## 7. Choosing — the repository decides first

| Situation | Do |
|---|---|
| Repo uses pandas, data fits in memory | optimise the pandas code (sections 2–4) |
| Repo already uses DuckDB or Polars | use it for the heavy step |
| Data outgrows memory and the repo has neither | say so in a comment and propose DuckDB or Polars; do not add a second dataframe library in a feature task (rule stack-choice) |
| Heavy aggregation already in the warehouse | push it into SQL or dbt instead of pulling raw rows |

Convert between libraries once, at a boundary — not back and forth inside a loop.

## Common Mistakes

- Optimising without a before number, so nobody knows whether it helped.
- `apply(axis=1)` "because it is readable".
- Reading a whole wide Parquet file to use three columns.
- Chained assignment carried over from pandas 1.x code into a pandas 3 repository.
- Converting pandas ↔ Polars per batch inside a loop.

## Red Flags

- A function whose runtime grows faster than its input size.
- Memory several times the on-disk Parquet size.
- `object` dtype columns full of numbers.
- A `ChainedAssignmentError` warning in the test output.
