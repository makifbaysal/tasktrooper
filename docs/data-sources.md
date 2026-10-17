---
title: Data sources
description: Connecting the system that already collects your metrics (Prometheus-compatible servers, Google Cloud Monitoring, Amazon CloudWatch) or a message broker read directly (Kafka, RabbitMQ, NATS JetStream), what signals and recipes are, binding them to components, and checking a source by hand with verify, discovery and a test query.
---

# Data sources

A **data source** is a system that already collects telemetry about your
services — a Prometheus server, Grafana Mimir, a cloud provider's
monitoring, or the message broker itself — connected to TaskTrooper so the
[Operations console](operations-console.md) can read from it. TaskTrooper
does not scrape or store your metrics itself: it asks the source when it
needs an answer and keeps no copy.

Data sources live on **Operations → Settings → Data sources**
(`/operations/sources`), the last item in the console's own column. The
console's environment and time range do not apply on this page; **Try a
query** has a range of its own. Six kinds exist today:

| Kind | Reads | Query language |
|---|---|---|
| **Prometheus compatible** | Prometheus, Grafana Mimir, Cortex, Thanos, VictoriaMetrics, Grafana Cloud, … | PromQL |
| **Google Cloud Monitoring** | Every Google Cloud metric, through a connected Google Cloud account | PromQL |
| **Amazon CloudWatch** | Every CloudWatch metric, through a connected AWS account | CloudWatch JSON |
| **Kafka** | A cluster's consumer groups: lag per partition, members — read directly, live only | JSON |
| **RabbitMQ** | A broker's queues through its management plugin — read directly, live only | JSON |
| **NATS JetStream** | A server's JetStream streams and consumers through its monitoring endpoint — read directly, live only | JSON |

Logs, error trackers and other kinds are planned but not built yet.

A source feeds the console once its signals are **bound** to a component on
the entity's [Signals](operations-console.md#signals) page: from then on
the entity's Summary charts and golden metrics, and the golden metric
columns on Services, Web, Mobile and Workers, are read from it, and so is a
worker's [Queues](operations-console.md#queues) section. A mobile app's
Crash & launch tiles do not read bindings yet.

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

## Binding signals to components

A binding says where one signal of one component is read from in one
environment: a data source plus a recipe and its parameters, or a query of
your own in the source's language. Bindings are made on each entity's
**Signals** page, where **Find suggestions** offers the recipes your sources
fit, **Accept** binds one, and the editor lets you pick a source, a recipe
(only those written for that kind of source that provide the signal) or a
custom query, and **Preview** it over the console's range before saving. See
[Signals](operations-console.md#signals).

A latency signal whose query uses `$__quantile` is read three times, at
p50, p95 and p99, and drawn as three lines; its golden metric is the p95.

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

## Google Cloud Monitoring and Amazon CloudWatch

These two kinds read through a cloud account you already connected under
**Settings → Integrations**; they store no credential of their own and need
nothing installed. When such an account is connected but no source reads
through it yet, the Data sources page offers **Use your connected Google
Cloud account for metrics** (or AWS): it opens the add dialog on the right
kind with the account already picked. Left empty, a source's name becomes
the kind and the account's name, such as `Google Cloud Monitoring · Acme
production`.

| Kind | Setting | What to enter |
|---|---|---|
| Google Cloud Monitoring | Google Cloud account | One of your connected Google Cloud accounts. With none, the field links to Settings → Integrations. |
| | Project | Optional. Left empty, the account's own project. A scoping project also reads the projects in its metrics scope. |
| Amazon CloudWatch | AWS account | One of your connected AWS accounts. |
| | Region | Optional. Left empty, the account's own region. |

**Permissions.** The Google Cloud account's service account needs the
**Monitoring Viewer** role (`roles/monitoring.viewer`) on the project; the
token TaskTrooper mints from its key asks only for the `monitoring.read`
scope. The AWS account's access key needs `cloudwatch:GetMetricData` and
`cloudwatch:ListMetrics`.

**Google Cloud Monitoring** is read through its Prometheus-compatible API,
so its queries are PromQL. A metric type such as
`run.googleapis.com/request_count` is `run_googleapis_com:request_count`, its
metric and resource labels are labels (`service_name`, `location`,
`response_code_class`), the resource type is the `monitored_resource` label,
and a distribution is a histogram (`_bucket`, `_count`, `_sum`).

**Amazon CloudWatch** queries are JSON, either one metric:

```json
{"namespace": "AWS/SQS", "metric": "ApproximateNumberOfMessagesVisible",
 "stat": "Maximum", "dimensions": {"QueueName": "orders"}}
```

or an expression over named metrics, for metric math, `SEARCH(...)` or a
Metrics Insights `SELECT`:

```json
{"expression": "100 * errors / invocations",
 "metrics": {"errors": {"namespace": "AWS/Lambda", "metric": "Errors", "stat": "Sum", "dimensions": {"FunctionName": "api"}},
             "invocations": {"namespace": "AWS/Lambda", "metric": "Invocations", "stat": "Sum", "dimensions": {"FunctionName": "api"}}}}
```

`stat` is any statistic (`Sum`, `Average`, `Maximum`, `p95`, …). `period`
(seconds: 1, 5, 10, 30 or a multiple of 60) is optional and follows the
chart's step when left out. **Discovered metrics** lists CloudWatch metrics
active in the last three hours as `<namespace>/<metric>`.

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

## Message brokers: Kafka, RabbitMQ and NATS

These three kinds read a broker's own state, with no exporter in between.
They answer **the live value only**: a query is read at the instant it is
made, so every series is one point, now, whatever the range, and a window
that ended more than five minutes ago (the previous period of a comparison)
has no data and a warning. That is enough for alert rules, which judge the
latest value, for golden metrics, and for a worker's
[Queues](operations-console.md#queues) tables. A chart shows the live value
instead of a line, with a pointer to history: for that, also connect an
exporter-backed Prometheus source (`kafka-exporter`, `kminion`,
`rabbitmq-prometheus`, `nats-exporter`) and bind the chart's signal there.

| Kind | Setting | What to enter |
|---|---|---|
| Kafka | Bootstrap servers | One or more brokers as `host:port`, separated by commas (`broker-1:9092,broker-2:9092`); the port defaults to 9092. Not a URL. |
| | SASL | **None**, **PLAIN**, **SCRAM-SHA-256** or **SCRAM-SHA-512**, with a user name and password. |
| | TLS | **On** for a listener that expects TLS. The brokers' certificates are always checked; there is no way to skip that. |
| | CA certificate | Shown with TLS on. The PEM of a private CA that signed the brokers' certificates, pasted whole (a PEM pasted on one line also works). |
| RabbitMQ | Management URL | The management plugin's base URL, such as `http://localhost:15672`, with its path prefix if it has one. |
| | Username, Password | A management user (basic auth). |
| | Virtual host | Optional. Left empty, `/`. |
| NATS JetStream | Monitoring URL | The server's HTTP monitoring endpoint, such as `http://localhost:8222`. |
| | Bearer token | Optional, only for a monitoring endpoint behind a proxy that asks for one. |

**Permissions.** The Kafka user needs **Describe** on the consumer groups it
reads (their members and committed offsets) and on their topics (end
offsets), and **Describe** on the cluster to list every group for
suggestions and discovery. A RabbitMQ user with the **monitoring** tag is
enough. NATS needs its monitoring port enabled (`nats-server -m 8222`, or
`http_port` in its configuration) and JetStream on; in a cluster, point the
source at a server that holds the streams to watch.

**Where they may point.** The bootstrap servers are checked like a URL: an
address that is not allowed is refused when you save, and every broker the
cluster advertises is checked again when TaskTrooper connects to it — so
those advertised addresses must be reachable from this computer too. Moving
the bootstrap servers to other hosts asks for the saved password and CA
certificate again, as moving a URL does.

**Queries** are one small JSON object each; an absent `metric` is the
backlog:

| Kind | Query | `metric` |
|---|---|---|
| Kafka | `{"group": "billing", "topic": "invoices", "metric": "lag"}` (`topic` optional) | `lag` (the group's total), `lag_by_partition` (one series per partition), `consumers` (members assigned a partition), `members` |
| RabbitMQ | `{"queue": "orders", "metric": "ready"}` (a `*` in `queue` matches any run of characters: `orders.*`) | `messages` (ready plus unacknowledged), `ready`, `unacked`, `consumers`, `publish_rate`, `deliver_rate`, `ack_rate` (per second) |
| NATS | `{"stream": "ORDERS", "consumer": "billing", "metric": "pending"}` (without `consumer`, every consumer of the stream) | `pending` (not yet delivered), `ack_pending`, `redelivered`, `waiting` |

**Find suggestions** on a worker's Signals page lists a broker's consumer
groups, queues or JetStream consumers and offers the ones whose names best
name the component, already written as these queries. The catalog's
`kafka-direct`, `rabbitmq-direct` and `nats-direct` recipes do the same with
a parameter you fill in (`group`; `queue`; `stream` and `consumer`).

## Checking a source

Every save is followed by a connection check, and the dialog shows how it
went. A source that cannot be reached is **still saved**, with an **Error**
status and the reason, so you can fix the network or the credentials and
check again later. The list shows each source's status — **Connected** (with
the version it reported), **Error** or **Not checked** — and **Verify** runs
the check again.

For a Prometheus-compatible source the check reads
`/api/v1/status/buildinfo`, and falls back to a trivial query for servers
that do not serve it. A Kafka source reads the cluster's metadata (its
details then name the broker count and the cluster id), a RabbitMQ source
the management API's overview and the virtual host, and a NATS source the
server's `/varz` (its details say so when JetStream is off).

Click a source to open its details:

- **Try a query** runs a query (PromQL for Prometheus and Google Cloud
  Monitoring, JSON for CloudWatch, Kafka, RabbitMQ and NATS; the box shows
  the kind's query shape) over the time range you pick, from 15 minutes to 7 days, and draws the result, one line per series,
  with a table view. An answer of single points, as a broker source gives,
  is listed as values, largest first, instead of a chart. Warnings the source returns are shown above the chart; a
  query the source refuses shows its error. At most 500 series are drawn; past
  that a warning says how many there were.
- **Discovered metrics** lists the metric names the source has seen in the
  last hour, with a filter. Clicking a name puts it in the query box. Above
  the list, **Recipes that fit** shows every recipe whose telltale metric the
  source holds — for example `rabbitmq-prometheus` once RabbitMQ metrics are
  being scraped. A broker source lists what it holds instead: Kafka's
  `group/<name>` and `topic/<name>`, RabbitMQ's `queue/<name>`, NATS's
  `stream/<name>` and `consumer/<stream>/<name>`; clicking a group, queue,
  stream or consumer puts its JSON query in the box.

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
use its declared `params` as `$name`, plus `$__interval` (the query step),
`$__rate` (a rate window of twice the step, at least two minutes) and
`$__quantile` (the quantile a latency signal is read at: 0.5, 0.95 and 0.99
for the three chart lines). In PromQL a param may sit only inside a
double-quoted label value and `$__quantile` where a number goes; in
CloudWatch JSON a param may sit only in a dimension value and `$__quantile`
only as a whole `stat` (it becomes `p95`), and `$__rate`/`$__interval` are
refused (the period follows the step). In a broker kind's JSON a param may
sit only in the value naming what is read (Kafka `group`/`topic`,
RabbitMQ `queue`, NATS `stream`/`consumer`), and every built-in is
refused: a live read has no step and no quantile.

A recipe may also declare a **queues table**, which a worker's Queues
section shows: its columns (`key`, `label`, `unit`), the `key_labels` that
name a row, and one instant query per other column, joined on those labels:

```yaml
tables:
  queues:
    key_labels: [topic, partition]
    columns:
      - {key: topic, label: Topic}
      - {key: partition, label: Partition}
      - {key: lag, label: Lag, unit: messages}
    queries:
      lag: >-
        sum by (topic, partition) (kafka_consumergroup_lag{consumergroup="$group"})
```

A cloud recipe can also say which cloud resource it fits and where its
params come from, so a component whose environment runs on that resource is
offered it already filled in:

```yaml
applies_to:
  technology: cloud_run
  resource_kind: cloud_run_service
  resource_params: {service: name, location: region}
```

The catalog ships `gcp-cloud-run`, `gcp-gke-workload`,
`gcp-https-loadbalancer` and `gcp-pubsub` for Google Cloud Monitoring, and
`aws-alb`, `aws-ecs`, `aws-lambda`, `aws-sqs` and `aws-msk` for CloudWatch. For queues it
ships `kafka-exporter`, `kminion`, `rabbitmq-prometheus`, `nats-exporter`,
`redis-exporter` and `redis-exporter-lists` for Prometheus, and
`kafka-direct`, `rabbitmq-direct` and `nats-direct` for the broker kinds. The server
checks every recipe when it starts, and its tests render each one, so a
recipe that does not fit the rules is caught before it ships. Contributions
of recipes for exporters you run are welcome.
