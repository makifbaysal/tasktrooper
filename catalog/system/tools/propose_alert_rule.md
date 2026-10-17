---
key: tool.propose_alert_rule
version: "1"
params:
    component: 'Component path. Omit inside an AI job for the job''s component, or in a repository with a single (or a root) component.'
    condition: 'What the rule tests: {op, threshold, for_seconds, window_seconds}.'
    condition.properties.for_seconds: How long the condition must hold before it fires (default 0).
    condition.properties.op: 'gt / lt the value against threshold; change_pct_gt / change_pct_lt its percent change over one window; rising (above threshold and higher than a window earlier); no_data.'
    condition.properties.threshold: The level, in the signal's unit (percent for ratios, milliseconds for latency).
    condition.properties.window_seconds: What one value summarises, 60–86400 (default 300).
    data_source_id: For a raw-query rule only, the source the query runs on.
    environment: 'Deploy environment (default production, or the AI job''s).'
    name: The rule's name as a person sees it, e.g. "High error ratio".
    open_incident: Whether firing opens an incident (default false).
    query: A raw query instead of a signal; needs data_source_id.
    rationale: One sentence a person reads before enabling it - why this threshold.
    repository_id: Repository UUID. Omit to use the AI job's or the current task's repository; inside an AI job only the job's own is accepted, and a run bound to a repository may name only that one.
    severity: Severity of what it opens (default medium).
    signal: Catalog signal read through the component environment's confirmed binding.
---
Propose an alert rule for a component environment: a condition on one signal (read through its confirmed binding) or on a raw query. It is stored disabled with `created_by: ai`, so it evaluates nothing and notifies no one until a person reads it and turns it on. When a rule for the same signal (or the same query) already exists for that component environment, nothing is stored and the existing rule comes back as `existing_rule`. Returns the stored rule.
