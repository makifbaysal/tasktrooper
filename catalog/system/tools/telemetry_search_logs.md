---
key: tool.telemetry_search_logs
version: "1"
params:
    before: 'Cursor for older lines, the `nextBefore` of the previous page.'
    env: 'Deployment environment to read; omit for all.'
    level: 'An exact level, not a minimum. Query error and warn separately; the counts in the result show which levels have lines.'
    limit: 'Maximum lines, default 50, at most 200.'
    q: 'Full-text search. Use a term from the exception or span, not a whole sentence.'
    range: 'How far back to look; it must cover the incident.'
    route: 'A route template such as /orders/:id. It matches only lines that carry a route, so an empty answer means no routed lines, not no logs.'
    service: 'A service name as telemetry reports it.'
    trace_id: 'A 32-hex trace id. The most precise filter there is; try it first when you have a trace.'
---
Search the production logs TaskTrooper Telemetry collected, newest first. Narrow by service, level and q or trace_id; an unfiltered week of logs is noise. Messages arrive with emails, IPs and keys masked, so a masked value is not a bug.
