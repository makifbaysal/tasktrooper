---
title: Data sources
description: Connecting the system that already collects your metrics, what signals and recipes are, and checking a source by hand with verify, discovery and a test query.
---

# Data sources

A **data source** is a system that already collects telemetry about your
services — a Prometheus server, Grafana Mimir, a cloud provider's
monitoring — connected to TaskTrooper so the
[Operations console](operations-console.md) can read from it. TaskTrooper
does not scrape or store your metrics itself: it asks the source when it
needs an answer and keeps no copy.

Data sources live on **Operations → Settings → Data sources**
(`/operations/sources`), the last item in the console's own column. The
console's environment and time range do not apply on this page; **Try a
query** has a range of its own. Today one kind exists, **Prometheus
compatible**; other kinds (logs, error trackers, cloud monitoring) are
planned but not built yet.

Nothing else on the console reads from a data source yet. A source is not
bound to a component, so the golden metrics and metric charts on an
entity's Summary, the golden metric columns on Services, Web, Mobile and
Workers, a worker's Queues and a mobile app's Crash & launch tiles stay
empty; the tiles, charts and Queues link to this page. Binding sources to
components comes in a later phase; for now a source is connected, checked,
browsed and queried from here.

## Signals and recipes

Two ideas sit between a raw metric and a chart on the console:

- A **signal** is a measure that means the same thing everywhere, whatever
  collected it: `http.requests` (requests per second), `http.error_ratio`,
  `http.latency`, `queue.backlog`, `queue.consume_rate`, `web.lcp` and so on.
  The list is fixed and lives in `catalog/observability/signals.yaml`.
- A **recipe** says how to compute those signals from one exporter's metrics.
  `rabbitmq-prometheus`, for example, knows that a RabbitMQ queue's backlog is
  `rabbitmq_queue_messages_ready` plus `rabbitmq_queue_messages_unacked`. Each
  recipe also names one metric that gives its exporter away
  (`detect.metric`), which is how TaskTrooper notices that a source holds
  RabbitMQ data at all.

So a source answers "what do you have", a recipe answers "what does it mean",
and a signal is the common vocabulary between them. Supporting a new exporter
is a new recipe, not new code.

The **Recipes** tab on the Data sources page lists every recipe this version
knows: its id, the technology it covers, the source kind it is written for,
the signals it provides and the metric it is detected by. It is read-only.

These observability recipes are unrelated to the deploy recipes on
[Deploy targets and recipes](deploy.md).

## Adding a Prometheus-compatible source

**Add data source** opens a two-step dialog: pick the kind, then fill in its
settings. Anything that serves the Prometheus HTTP API works: Prometheus
itself, Grafana Mimir, Cortex, Thanos, VictoriaMetrics, Grafana Cloud, or
Amazon Managed Prometheus behind a SigV4 proxy.

| Setting | What to enter |
|---|---|
| Name | Optional. Left empty, the host name of the URL is used. |
| URL | The API base, **without** `/api/v1`: `http://localhost:9090` for a local Prometheus; Mimir serves it under `/prometheus`, Grafana Cloud under `/api/prom`. No user name or password in the URL itself. |
| Authentication | **None**, **Bearer token**, or **Basic** (user name and password). For Grafana Cloud use Basic: the user name is the instance ID, the password an access policy token with `metrics:read`. |
| Extra headers | Optional, one name and value per row, sent with every request. |

Extra headers are where a multi-tenant Mimir or Cortex gets its tenant
(`X-Scope-OrgID: tenant-1`), or where an `Authorization` header of your own
goes. Connection headers (`Host`, `Content-Length`, `Transfer-Encoding` and
the like) and cloud metadata headers (`Metadata-Flavor`,
`X-aws-ec2-metadata-token`) are refused.

### Credentials stay write-only

Tokens, passwords and extra headers are stored encrypted and are never shown
again — not even the header names. When you edit a source, a saved secret
reads "Saved. Leave it empty to keep it."; type a new value to replace it, or
**Remove** it. Headers you add while editing replace the whole saved set.

If an edit moves the URL to a different scheme, host or port, the saved
credentials are not carried over: the dialog asks you to enter each one again
or remove it before it saves. Changing only the path keeps them.

### Where a source may point

The URL is something you typed, and a self-hosted Prometheus usually runs on
this computer or your own network, so `localhost` and private addresses
(`10.x`, `192.168.x`, …) are allowed. Link-local and cloud metadata addresses
(`169.254.0.0/16`, `fe80::/10`, `fd00:ec2::254`, `metadata.google.internal`)
are always refused — when you save, and again on every request, which is
pinned to the address that was checked. A redirect may not leave the host.
See [The outbound URL guard](data-and-security.md#the-outbound-url-guard).

## Checking a source

Every save is followed by a connection check, and the dialog shows how it
went. A source that cannot be reached is **still saved**, with an **Error**
status and the reason, so you can fix the network or the credentials and
check again later. The list shows each source's status — **Connected** (with
the version it reported), **Error** or **Not checked** — and **Verify** runs
the check again.

For a Prometheus-compatible source the check reads
`/api/v1/status/buildinfo`, and falls back to a trivial query for servers
that do not serve it.

Click a source to open its details:

- **Try a query** runs a query (PromQL for this kind) over the time range you
  pick, from 15 minutes to 7 days, and draws the result, one line per series,
  with a table view. Warnings the source returns are shown above the chart; a
  query the source refuses shows its error. At most 500 series are drawn; past
  that a warning says how many there were.
- **Discovered metrics** lists the metric names the source has seen in the
  last hour, with a filter. Clicking a name puts it in the query box. Above
  the list, **Recipes that fit** shows every recipe whose telltale metric the
  source holds — for example `rabbitmq-prometheus` once RabbitMQ metrics are
  being scraped.

## Contributing a recipe

Custom recipes are not editable in the app yet. A recipe is a YAML file under
`catalog/observability/recipes/`, named after its id:

```yaml
id: rabbitmq-prometheus
source_kind: prometheus
applies_to:
  technology: rabbitmq
  messaging_system: rabbitmq
detect:
  metric: rabbitmq_queue_messages_ready
params: [queue, vhost]
signals:
  queue.backlog: >-
    sum(rabbitmq_queue_messages_ready{queue="$queue", vhost="$vhost"})
    + sum(rabbitmq_queue_messages_unacked{queue="$queue", vhost="$vhost"})
```

Every key under `signals` must be a signal from `signals.yaml`. A query may
use its declared `params` as `$name`, plus `$__interval` (the query step) and
`$__rate` (a rate window of twice the step, at least two minutes). The server
checks every recipe when it starts, and its tests render each one, so a
recipe that does not fit the rules is caught before it ships. Contributions
of recipes for exporters you run are welcome.
