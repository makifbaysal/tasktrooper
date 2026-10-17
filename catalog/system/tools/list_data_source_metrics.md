---
key: tool.list_data_source_metrics
version: "1"
params:
    data_source_id: The data source to list the metrics of. Omit to list the data sources themselves.
    limit: Most metric names to return (default 200, at most 1000).
    match: Case-insensitive substring a metric name must contain, e.g. the service name, "http", "latency" or "lag".
---
Read what the observability data sources hold. Without `data_source_id` it lists them: `{id, name, kind, status}` each — inside an AI job only the ones its repository's components read through or no component uses yet. With one, it lists the metric names that source holds (filtered by `match`, capped by `limit`; `truncated` says there were more), the `matching_recipes` whose detect metric it holds, and every recipe of the source's kind (`{id, signals, params, detect_metric}`) you could bind a signal through. Read-only: use it before `propose_signal_binding` to find the metric a signal is measured by.
