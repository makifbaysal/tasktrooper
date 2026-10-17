---
key: tool.propose_signal_binding
version: "1"
params:
    component: 'Component path (e.g. "services/api"). Omit inside an AI job for the job''s component, or in a repository with a single (or a root) component.'
    data_source_id: The data source to read the signal from (from list_data_source_metrics).
    environment: 'Deploy environment of the binding (default production, or the AI job''s).'
    params: Values for the recipe's params, e.g. {"job":"api"}. Ignored with a raw query.
    query: 'A raw query in the source''s own language (PromQL, CloudWatch JSON, …), used when no recipe fits; it wins over recipe_id. It must read only this component in this environment. Built-ins: $__rate (inside [...]), $__interval, $__quantile (where a number goes).'
    rationale: One sentence a person reads before confirming it - which metric and which labels tie it to this component.
    recipe_id: A recipe of the source's kind that maps this signal.
    repository_id: Repository UUID. Omit to use the AI job's or the current task's repository; inside an AI job only the job's own is accepted, and a run bound to a repository may name only that one.
    signal: 'Catalog signal id, e.g. http.error_ratio, http.latency, http.throughput, queue.backlog, mobile.crash_free_users, web.lcp.'
---
Propose where one signal of a component environment is read: a data source plus a recipe and its params, or a raw query. It is checked exactly as a person's binding is (the signal, the source, the recipe's params, the query parsing) and stored as a suggestion (`status: suggested, source: ai`); a person confirms it before anything reads it. Proposing a target already offered, or one dismissed, for that signal answers that binding as it stands instead of adding another. Returns the binding with its `rendered_query` and any `needs_params` still missing.
