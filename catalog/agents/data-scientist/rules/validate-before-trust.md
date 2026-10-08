---
name: validate-before-trust
priority: 85
enabled: true
---
Data from outside the code — a file, an API, a warehouse table, a user upload, a model input — passes a schema at the boundary before any logic uses it: types, nullability, ranges, allowed values, uniqueness of the key (pandera, Pydantic, Great Expectations or dbt tests, whichever the repository already uses). Check row counts before and after every join and declare the expected cardinality (`merge(validate="many_to_one")`, a dbt unique test on the key, COUNT(DISTINCT) on the entity) — a join that silently multiplies or drops rows is the most common wrong number. A failed check stops the pipeline with a message that names the column and the bad values; it is never caught and logged away. Load data-validation-contracts.
