---
title: Data sources
description: Connecting the systems that already collect your telemetry — metrics stores (Prometheus-compatible servers, Google Cloud Monitoring, Amazon CloudWatch, Datadog, New Relic), logs and traces (Loki, Elasticsearch, Tempo), Google Play vitals, App Store Connect metrics and Sentry, web analytics (PostHog, Google Analytics 4, Plausible) or a message broker read directly (Kafka, RabbitMQ, NATS JetStream) — what signals and recipes are, binding them to components, and checking a source by hand with verify, discovery and a test query.
---

# Data sources

A **data source** is a system that already collects telemetry about your
services — a Prometheus server, a cloud provider's monitoring, a log or
trace store, Sentry, the app stores, your web analytics or APM, or the
message broker itself — connected to TaskTrooper so the
[Operations console](operations-console.md) can read from it. TaskTrooper
does not scrape or store your telemetry itself: it asks the source when it
needs an answer and keeps no copy.

Data sources live on **Operations → Settings → Data sources**
(`/operations/sources`), the last item in the console's own column. The
console's environment and time range do not apply on this page; **Try a
query** has a range of its own. Seventeen kinds exist today:

| Kind | Reads | Query language |
|---|---|---|
| **Prometheus compatible** | Prometheus, Grafana Mimir, Cortex, Thanos, VictoriaMetrics, Grafana Cloud, … | PromQL |
| **Google Cloud Monitoring** | Every Google Cloud metric, through a connected Google Cloud account | PromQL |
| **Amazon CloudWatch** | Every CloudWatch metric, through a connected AWS account | CloudWatch JSON |
| **Kafka** | A cluster's consumer groups: lag per partition, members — read directly, live only | JSON |
| **RabbitMQ** | A broker's queues through its management plugin — read directly, live only | JSON |
| **NATS JetStream** | A server's JetStream streams and consumers through its monitoring endpoint — read directly, live only | JSON |
| **Loki** | Grafana Loki or Grafana Cloud Logs: a component's log lines, and error rates counted from them | LogQL |
| **Elasticsearch / OpenSearch** | Log lines kept in Elasticsearch, OpenSearch or Elastic Cloud, from one index pattern | JSON |
| **Tempo** | Grafana Tempo or Grafana Cloud Traces: a component's traces | TraceQL |
| **Google Play vitals** | An Android app's crash and ANR rates and its top error issues, with the workspace's Google Play credential | JSON |
| **App Store Connect metrics** | An iOS app's launch time and other Xcode Organizer metrics per version, and its builds' hang, launch and disk-write diagnostic signatures, with the workspace's App Store Connect key | JSON |
| **Sentry** | Crash-free users and sessions, error counts and issues, from sentry.io or a self-hosted Sentry | JSON |
| **PostHog** | Page views, sessions, visitors, exceptions and web vitals, and the top pages, referrers, countries and devices | JSON |
| **Google Analytics 4** | A property's page views, sessions and active users, and its top pages, referrers, countries and devices | JSON |
| **Plausible** | A site's page views, visits and visitors, and its top pages, sources, countries and devices | JSON |
| **Datadog** | Any Datadog metric: APM trace metrics, host and container metrics | Datadog metric query |
| **New Relic** | APM transactions, Browser page views and web vitals, Mobile crashes | NRQL |

A source feeds the console once its signals are **bound** to a component on
the entity's [Signals](operations-console.md#signals) page. A bound metric
signal is read for the entity's Summary charts and golden metrics, the
golden metric columns on Services, Web, Mobile and Workers, a worker's
[Queues](operations-console.md#queues) section, a web app's
[Core Web Vitals and traffic](operations-console.md#core-web-vitals-and-traffic)
tiles and a mobile app's
[Crash & launch](operations-console.md#crash-and-launch-tiles) tiles. Three
signals are never drawn; they say where something else is read:
`logs.lines` (the [Logs explorer](operations-console.md#logs-explorer)
reads the component's lines from Loki or Elasticsearch), `traces.spans` (its
[Traces](operations-console.md#traces) section searches Tempo) and
`errors.issues` (the [Errors inbox](operations-console.md#errors-inbox)
adds its Sentry issues). Some recipes also declare tables, which the
[Endpoints](operations-console.md#endpoints),
[Pages](operations-console.md#pages) and
[Crashes](operations-console.md#crashes) sections show.

## Signals and recipes

Two ideas sit between a raw metric and a chart on the console:

- A **signal** is a measure that means the same thing everywhere, whatever
  collected it: `http.requests` (requests per second), `http.error_ratio`,
  `http.latency`, `queue.backlog`, `queue.consume_rate`, `web.lcp` and so on.
  The list is fixed and lives in `catalog/observability/signals.yaml`. Most
  are metrics; `logs.lines`, `traces.spans` and `errors.issues` are not
  series but say where a component's log lines, traces and error issues are
  read.
- A **recipe** says how to compute those signals from one exporter's metrics.
  `rabbitmq-prometheus`, for example, knows that a RabbitMQ queue's backlog is
  `rabbitmq_queue_messages_ready` plus `rabbitmq_queue_messages_unacked`. Each
  recipe also names one metric that gives its exporter away
  (`detect.metric`), which is how TaskTrooper notices that a source holds
  RabbitMQ data at all; on a log or trace store it is a label, field or tag
  name, on a web analytics source an event name. A recipe may also declare
  **tables**: one row per endpoint, page, crash issue or queue, read when
  they are shown over a recent window of the source's own (see
  [Table windows](#table-windows)).

So a source answers "what do you have", a recipe answers "what does it mean",
and a signal is the common vocabulary between them. Supporting a new exporter
is a new recipe, not new code.

### Table windows

A table is not read over the console's time range: each source reads a
fixed window before the moment it is asked, and each table on the console
says which ("Covers the last 7 days"):

| Source | Window |
|---|---|
| PostHog, Google Analytics 4, Plausible (Pages, Referrers, Countries, Devices) | The last 7 days, or the query's own `days` |
| New Relic Browser (`newrelic-browser`) | The last 7 days (its NRQL says `SINCE 7 days ago`) |
| Google Play vitals (crash and ANR issues) | The last 7 days, or the query's own `days` (1–30) |
| Sentry (crash issues) | The last 14 days, or the query's own `days` (1–90) |
| Datadog (endpoints) | The 10 minutes before |
| New Relic APM (endpoints) | The hour before, unless the NRQL has a `SINCE` of its own |
| Queues, Prometheus and CloudWatch tables, App Store Connect signatures | The value as the source holds it now |

A table whose window cannot be told (a Prometheus rate, the newest build's
signatures) says it covers the source's own recent window. Tables are kept
30 seconds on the server, so each card also says when it was read ("Read 1
minute ago").

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
fit, **Accept** binds one, and the editor lets you pick a source that can
feed the signal — a metrics source for a metric signal, Loki or
Elasticsearch for `logs.lines`, a traces source (Tempo) for `traces.spans`,
Sentry for `errors.issues` — then a recipe (only those written for that kind
of source that provide the signal) or a custom query. **Preview** draws a
metric signal over the console's range before saving; a log, trace or error
signal is not a series and has no preview, so you save it to bind it. See
[Signals](operations-console.md#signals). For a mobile app, Find suggestions
can also offer to create the store source it needs; see
[Connecting a store source from Signals](#connecting-a-store-source-from-signals).

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

Tokens, passwords, API keys, pasted key files and extra headers are stored
encrypted and are never shown again — not even the header names. When you
edit a source, a saved secret reads "Saved. Leave it empty to keep it.";
type a new value to replace it, or **Remove** it. Headers you add while
editing replace the whole saved set.

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
The kinds without a URL of yours — Google Play vitals, App Store Connect
metrics, Google Analytics 4, Datadog (whose site is picked from Datadog's
own list) and New Relic (by region) — call their product's fixed API
address, which goes through the same checks.

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
certificate again, as moving a URL does; while the source holds them, the
field says so.

**Kafka without TLS.** With SASL **PLAIN** and TLS **off**, a source whose
bootstrap or advertised brokers are not on this computer (not `localhost` or
a loopback address) still saves, but its check shows a warning: PLAIN then
sends the password unencrypted. Turn TLS on, or use SCRAM.

**Queries** are one small JSON object each; an absent `metric` is the
backlog:

| Kind | Query | `metric` |
|---|---|---|
| Kafka | `{"group": "billing", "topic": "invoices", "metric": "lag"}` (`topic` optional) | `lag` (the group's total), `lag_by_partition` (one series per partition), `consumers` (members assigned a partition), `members` |
| RabbitMQ | `{"queue": "orders", "metric": "ready"}` (a `*` in `queue` matches any run of characters: `orders.*`) | `messages` (ready plus unacknowledged), `ready`, `unacked`, `consumers`, `publish_rate`, `deliver_rate`, `ack_rate` (per second) |
| NATS | `{"stream": "ORDERS", "consumer": "billing", "metric": "pending"}` (without `consumer`, every consumer of the stream) | `pending` (not yet delivered), `ack_pending`, `redelivered`, `waiting` |

**Find suggestions** on a worker's Signals page lists a broker's consumer
groups, queues or JetStream consumers and offers the ones whose names best
name the component through the catalog's `kafka-direct`, `rabbitmq-direct`
and `nats-direct` recipes, the name filled in as the recipe's parameter
(`group`; `queue`; `stream` and `consumer`), which escapes it for the query.
A RabbitMQ queue whose name holds `*` is not offered, since a query reads
`*` as a pattern; under **Discovered metrics** such a queue is listed but
cannot be put in the query box. Bind it through `rabbitmq-direct`'s `queue`
parameter by hand.

## Logs and traces: Loki, Elasticsearch and Tempo

These three kinds read a component's log lines and traces from a store of
their own, rather than from the cloud it runs on. Bound to a component,
`logs.lines` makes the [Logs explorer](operations-console.md#logs-explorer)
read that component's lines from Loki or Elasticsearch instead of its
connected environment, and `traces.spans` gives its
[Traces](operations-console.md#traces) section a Tempo source to search.

| Kind | Setting | What to enter |
|---|---|---|
| Loki | URL | The base URL, without `/loki/api/v1`: `http://localhost:3100`. For Grafana Cloud, the Logs URL (`https://logs-prod-<n>.grafana.net`). |
| | Authentication | **None**, **Bearer token** or **Basic**. For Grafana Cloud use Basic: the Logs user ID and an access policy token with `logs:read`. |
| | Extra headers | Optional, as for Prometheus; a multi-tenant Loki takes its tenant here (`X-Scope-OrgID: tenant-1`). |
| Elasticsearch / OpenSearch | URL | The cluster's base URL, such as `http://localhost:9200`. For Elastic Cloud, the deployment's Elasticsearch endpoint. |
| | Index | The index, data stream, alias or pattern holding the logs; several separated by commas, a leading `-` leaving one out (`logs-*,-logs-debug`). |
| | Time field, Message field, Level field | Optional. Left empty, `@timestamp`, `message` and `log.level` (ECS). |
| | Authentication | **None**, **Basic** (user name and password), or **API key**: the encoded key (base64 of `id:key`), as Kibana shows it when the key is created. |
| | Extra headers | Optional. |
| Tempo | URL | The HTTP API base, without `/api`: `http://localhost:3200`. For Grafana Cloud, the Traces URL ending in `/tempo`. |
| | Authentication | **None**, **Bearer token** or **Basic**. For Grafana Cloud use Basic: the Traces user ID and an access policy token with `traces:read`. |
| | Extra headers | Optional (`X-Scope-OrgID` for a multi-tenant Tempo). |

**Permissions.** Grafana Cloud's tokens need `logs:read` or `traces:read`.
For Elasticsearch or OpenSearch (Amazon OpenSearch Service works behind
basic auth), the user or API key must be able to search the indices the
pattern matches and list their fields (`_field_caps`): the check refuses a
pattern that matches no index, or no index with the time field.

**Queries.** Loki takes LogQL. A `logs.lines` binding is a log query; a
chart, and **Try a query**, take a metric query, which is how an error rate
is counted from lines:

```
{service_name="api"}
sum(rate({service_name="api"} | detected_level="error" [5m]))
```

A Loki query may end in a `# comment`: the Logs explorer's own filters go on
a line of their own after the query, so the comment cannot swallow them.

Elasticsearch takes one JSON object of filters, every one of which must
match (exact on a keyword field, every word on a text one), at most 32:

```json
{"filters": {"service.name": "billing", "kubernetes.namespace": "prod"}}
```

Tempo takes TraceQL, the spans a component's traces are found by:

```
{ resource.service.name = "api" }
```

The Logs explorer's own filters are added in the store's language: its
search text becomes a Loki line filter (`|= "…"`) or a phrase match on the
message field, and **Minimum severity** drops the lower levels at the
source, by Loki's `detected_level`, `level` or `severity` label or by the
level field. A line with no level there is judged by its own text, so a page
can hold fewer lines than asked. Each line's level, service and trace id are
read from its labels or fields (Loki's `trace_id`, Elasticsearch's ECS
`trace.id`); the trace id is what gives an entry its **Open trace** link.

A Tempo search covers 7 days at most, Tempo's own ceiling, newest first, and
marks a trace failed when it also matches `status = error`. One trace is
read with up to 20,000 spans.

**Recipes.** `loki-service` (param `service`) is the primary Loki recipe:
`logs.lines` from streams labelled `service_name`, which Loki 3 sets on
every stream, and `process.errors` from the lines Loki 3 detected as error
or worse. `loki-app` (`app`) reads streams a Loki 2 shipper such as Promtail
labelled `app`. `elasticsearch-service` (`service`) reads lines carrying ECS
`service.name`, and `tempo-service` (`service`) traces by their resource
`service.name`. Their telltale is a label, field or tag name
(`service_name`, `app`, `service.name`, `resource.service.name`), and
**Find suggestions** fills the parameter with the value of it, seen over
the last hour, that best names the component.

## App stores and Sentry

Google Play vitals, App Store Connect metrics and Sentry read what the
stores and Sentry already measure about an app, with nothing more to
install in it. The two store kinds read the workspace's store credential
(see [Store credential fields](#store-credential-fields)); Sentry takes a
token of its own.

| Kind | Setting | What to enter |
|---|---|---|
| Google Play vitals | Google Play credential | The service account saved under Settings → Integrations → Google Play. |
| | Package name | Optional. The app read when a query names none (`com.example.app`); a component's binding fills it from its repository's linked Android app. |
| App Store Connect metrics | App Store Connect credential | The API key saved under Settings → Integrations → App Store Connect. |
| | App id | Optional. The app read when a query names none: its App Store Connect id (`1234567890`); a component's binding fills it from its repository's linked iOS app. |
| Sentry | Sentry URL | `https://sentry.io` (the default), `https://de.sentry.io` for an organization in the EU region, or your self-hosted Sentry's address. |
| | Organization | The organization slug, as in `sentry.io/organizations/<slug>/`. |
| | Auth token | An organization token or an internal integration's, with the `org:read`, `project:read` and `event:read` scopes. |

**Permissions.** The Google Play service account needs the Play Developer
Reporting API enabled in its Google Cloud project and, in Play Console,
permission to view app information; the token TaskTrooper mints from its
key asks only for the `playdeveloperreporting` scope. The App Store Connect
key's role must be allowed to read the app's metrics; a refusal says so. A
Sentry token refused by Sentry is reported with the three scopes it needs.

**Google Play vitals** queries are JSON, `{"package"?, "metric_set",
"metric", "dimension"?, "filter"?, "period"?, "as"?}`:

```json
{"metric_set": "anrRateMetricSet", "metric": "userPerceivedAnrRate", "as": "percent"}
```

- `metric_set` is `crashRateMetricSet`, `anrRateMetricSet`,
  `slowStartRateMetricSet` or `errorCountMetricSet`, and `metric` one of its
  metrics (`crashRate`, `userPerceivedCrashRate`, `anrRate`,
  `userPerceivedAnrRate`, `slowStartRate`, `errorReportCount`,
  `distinctUsers`, and the 7- and 28-day user-weighted averages); Play
  refuses one it does not know.
- `dimension` breaks the answer down, one series per value (`versionCode`,
  `deviceModel`, `countryCode`, `apiLevel`, …); `filter` is Play's own filter
  on those dimensions.
- `period` is `daily` (the default: calendar days in America/Los_Angeles,
  Play's only daily time zone) or `hourly` (UTC; the crash and ANR sets
  only, not their rolling averages).
- `as` scales the fraction of users Play answers: `value` keeps it,
  `percent` multiplies it by 100, `free_percent` is 100 minus that percent.

Play publishes daily vitals a day or two after the day they cover, so the
newest point is never today's. A read reaching past the latest day Play
holds stops there and warns "Google Play publishes vitals late"; the
connection check says how far the data goes (`data through` and the day).

**App Store Connect metrics** queries are JSON, `{"app"?, "metric",
"percentile"?, "device"?, "version"?}`:

```json
{"metric": "launchTime", "percentile": "p90"}
```

`metric` is an Xcode Organizer metric identifier (`launchTime`, `hangRate`,
…) or a whole category, one series per metric in it: `launch`, `hang`,
`memory`, `disk`, `battery`, `termination`, `animation` or `storage`.
`percentile` is `p50` or `p90`, the only two Apple reports (both when left
out); `device` is Apple's device filter (`all_iphones` when left out);
`version` keeps one app version. Apple reports these values **per app
version**, not over time, so a series is a staircase: at each point, the
value of the newest version created by then, with a warning saying so. An
app with no metrics yet answers nothing, with a warning. Apple reports **no
crash-free rate**: bind crash-free users to Sentry or New Relic instead.

**Sentry** queries are JSON, `{"project", "metric"?, "environment"?,
"release"?, "search"?, "days"?}`:

```json
{"project": "ios-app", "metric": "crash_free_users", "environment": "production"}
```

`project` is the project's slug or id. `metric` is `events` when left out
(error events per interval) or `event_rate` (per second); from release
health, which needs the SDK's session tracking, it is `crash_free_users`,
`crash_free_sessions`, `crash_rate` (crashed sessions) or
`crash_rate_users`, all in percent, or `sessions` and `users` per interval.
Release health is read at whole-hour intervals (1 to 24 hours), events at
whole minutes. `environment` narrows every read, `release` keeps one
release, and `search` adds Sentry search syntax to an events or issues read
(issues default to `is:unresolved`). Bound to `errors.issues`, a project's
issues join the [Errors inbox](operations-console.md#errors-inbox), beside
what the component's cloud reports, and each can be made a task from there.

**Links.** A Play issue's link and a Sentry issue's permalink are shown only
when they are the service's own https pages: Play Console on
`play.google.com`, and your configured Sentry (for Sentry's cloud, `sentry.io`
or a subdomain of it). Any other link the source answers is left out.

**Recipes.**

| Recipe | Param | Signals | Table |
|---|---|---|---|
| `play-vitals` | `package` | `mobile.crash_free_users` (100 minus the day's crash rate: the share of that day's users who did not crash), `mobile.crash_rate` and `mobile.anr_rate` (the user-perceived rates Play's core vitals judge) | `crashes`: the app's error issues of the last 7 days — crashes, ANRs and non-fatal ones — most affected users first |
| `app-store-metrics` | `app` | `mobile.cold_start` (launch time at p90) | `signatures`: the newest build's hang, launch and disk-write signatures, by their share of its events |
| `sentry-mobile` | `project` | `mobile.crash_free_users`, `mobile.crash_rate`, `errors.issues` | `crashes`: unhandled, unresolved issues of the last 14 days, most frequent first |
| `sentry-web` | `project` | `web.js_errors` (per second), `errors.issues` | — |
| `sentry-backend` | `project` | `process.errors` (per second), `errors.issues` | — |

App Store Connect metrics lists **Crashes** among what it provides: its
`signatures` table is what a mobile app's
[Crashes](operations-console.md#crashes) shows from it.

Play does not map cold start: it measures the share of slow starts, not how
long a start takes. A Sentry recipe reads every environment the project
reports; to read one, bind a custom query with `environment`. Sentry
recipes have no telltale, so **Find suggestions** never offers them: bind
them by hand. The store recipes are offered from the repository's linked
store apps instead, as below.

### Store credential fields

The two store kinds keep no credential of their own. Their credential field
names the workspace's store credential, the one per store saved under
**Settings → Integrations** that the release flow already uses (see
[Connecting the stores](mobile-releases.md#connecting-the-stores)), and the
source reads it on every call, so a key replaced there is used from the next
read on. In the add dialog the field is a choice of that one credential,
already picked for a new source. With none saved, the field says "No Google
Play service account is saved yet" (or App Store Connect API key) and links
to Settings → Integrations, and saving is refused until it is there. Left
empty, such a source's name is its kind's.

### Connecting a store source from Signals

For a mobile app whose repository has a store app linked, **Find
suggestions** on its Signals page offers the store recipes first, with the
parameter filled in from the app: the Android package name for
`play-vitals`, the App Store Connect app id for `app-store-metrics`. Where no
source of that kind exists yet, it offers one instead, as a card: **Connect
Google Play vitals for this app** or **Connect App Store metrics for this
app**, with the signals and parameters it would bind. Find suggestions is
offered for this even before any data source exists.

- **Connect** opens the add dialog on that kind with the store credential
  already chosen. Once the source is saved, the offered recipe is bound to
  each of the card's signals.
- While the store credential is not saved, the card says so with a link to
  Settings → Integrations, and **Connect** is unavailable.
- **Not now** hides the card; the next Find suggestions offers it again.

## Web analytics: PostHog, Google Analytics 4 and Plausible

These kinds read what a site's own analytics already counts: traffic for the
console's charts and tiles, and the top pages, referrers, countries and
devices for a web app's [Pages](operations-console.md#pages) section.

| Kind | Setting | What to enter |
|---|---|---|
| PostHog | PostHog URL | `https://us.posthog.com` (the default) or `https://eu.posthog.com` for PostHog Cloud, or your self-hosted PostHog's address. |
| | Project ID | The number in the project's URL (`/project/<id>`) or under Project settings. |
| | Personal API key | A personal API key (`phx_…`) with the Query Read scope on this project. |
| Google Analytics 4 | Property ID | The number under Admin → Property details, not the `G-…` measurement ID. |
| | Sign in with | **A connected Google Cloud account** (the default) or **A service account key** for this source alone. |
| | Google Cloud account | With the first: one of your connected Google Cloud accounts. |
| | Service account key (JSON) | With the second: the whole key file, pasted into the text area. |
| Plausible | Plausible URL | `https://plausible.io` (the default), or your self-hosted Plausible's address. |
| | Site | The site's domain as Plausible names it. |
| | Stats API key | A Stats API key (Account settings → API keys) of the team that owns the site. |

**Permissions.** GA4's service account, the connected account's or the
pasted one, must be a Viewer of the property (Admin → Property access
management), with the Google Analytics Data API enabled in its project; its
token asks only for the `analytics.readonly` scope.

**Queries** are JSON, the same shape for the three. A series names a
`metric`; `host` keeps one hostname, which is how a project, property or site
shared by several sites is narrowed:

```json
{"metric": "page_views", "host": "www.example.com"}
```

| `metric` | Means | PostHog | GA4 | Plausible |
|---|---|---|---|---|
| `page_views` (the default) | Page views per second | yes | yes | yes |
| `sessions`, `visitors` | Distinct sessions and visitors per interval | yes | yes (`activeUsers`) | yes (visits, visitors) |
| `events` | Events per interval, of the `event` named | yes | yes | — |
| `js_errors` | JavaScript exceptions per second | yes | — | — |
| `lcp`, `inp`, `fcp`, `cls` | A web vital at `quantile` (0.75 when left out), in ms; CLS is a score | yes | — | — |

GA4's queries also take `property`, and Plausible's `site`, to read another
property or site than the source's.

**Data delay.** Google Analytics keeps processing a day's data for up to 48
hours: a window ending less than 48 hours ago warns that the newest points
may still rise, and an answer GA4 thresholded or folded into "(other)" warns
too. GA4 and Plausible answer by the hour below a one-day step, else by the
day, in the property's or site's own time zone. PostHog answers in buckets
of at least a minute.

**Recipes.** None takes a parameter: the source's project, property or site
is the site.

| Recipe | Detected by | Signals | Tables |
|---|---|---|---|
| `posthog-web` | `$pageview` | `web.page_views`, `web.sessions`, `web.visitors`, `web.js_errors` (from PostHog's exception autocapture) | pages (with LCP p75), referrers, countries, devices |
| `posthog-web-vitals` | `$web_vitals` | `web.lcp`, `web.inp`, `web.cls`, `web.fcp`, at p75, from posthog-js' web vitals events | — |
| `ga4-web` | `page_view` | `web.page_views`, `web.sessions`, `web.visitors` | pages, referrers (source / medium, with sessions), countries, devices |
| `plausible-web` | `pageview` | `web.page_views`, `web.sessions`, `web.visitors` | pages, referrers (visit sources), countries, devices |

The tables cover the last 7 days, the most viewed first. **Find
suggestions** offers each recipe once the source has seen the event it is
detected by.

## APM: Datadog and New Relic

| Kind | Setting | What to enter |
|---|---|---|
| Datadog | Site | The site in your Datadog URL, picked from Datadog's own list: US1 (`datadoghq.com`, the default), US3, US5, EU1 (`datadoghq.eu`), AP1 or US1-FED (`ddog-gov.com`). There is no free-form host, so the keys only ever go to Datadog's API. |
| | API key | Organization settings → API keys. |
| | Application key | Organization settings → Application keys. A scoped key needs `timeseries_query` and `metrics_read`. |
| New Relic | Region | `us` (the default), or `eu` for an account on one.eu.newrelic.com. |
| | Account ID | The account's number, shown under its name in the account picker. |
| | User key | A user key (`NRAK-…`) of a user who can query the account. |

**Datadog** queries are metric queries as a dashboard writes them; Datadog
picks the rollup for the window:

```
sum:trace.http.request.hits{service:api,env:prod}.as_rate()
avg:system.cpu.user{service:web} by {host}
```

Read as a table, a query answers each tag group's latest value over the 10
minutes before, so `… by {resource_name}` is one row per resource.

**New Relic** queries are NRQL without `SINCE`, `UNTIL`, `TIMESERIES` or
`COMPARE WITH`, and without comments (`--`, `//`, `/* */` would swallow what
the kind appends, so a query holding one outside a string is refused): the
kind adds them for the range, in steps of whole minutes, 366 at most:

```
SELECT percentile(duration, 95) * 1000 FROM Transaction WHERE appName = 'api'
```

Read as a table, a `FACET` query is one row per facet, over the hour before
unless it has a `SINCE` of its own.

**Recipes.**

| Recipe | Detected by | Params | Signals | Table |
|---|---|---|---|---|
| `datadog-apm` | `trace.http.request.hits` | `service`, `env`, `metric_prefix` | `http.requests`, `http.error_ratio`, `http.latency` | endpoints: one row per resource, its latest values of the last 10 minutes |
| `datadog-system` | `system.cpu.user` | `host` | `infra.cpu`, `infra.memory` | — |
| `datadog-container` | `container.cpu.usage` | `container` | `infra.cpu`, `infra.memory` | — |
| `newrelic-apm` | `Transaction` | `app` | `http.requests`, `http.error_ratio`, `http.latency` | endpoints: one row per transaction name over the last hour, the top 100 by count |
| `newrelic-browser` | `PageView` | `app` | `web.page_views`, `web.sessions`, `web.js_errors`, `web.lcp`, `web.inp`, `web.cls` (p75) | pages: one row per page URL over the last 7 days, the top 100 by views |
| `newrelic-mobile` | `MobileSession` | `app` | `mobile.crash_rate` (crashes per hundred sessions), `mobile.crash_free_users` | — |

`datadog-apm`'s `metric_prefix` is `trace.<span name>`, which depends on the
tracer's integration. Left empty it is `trace.http.request` (Go's net/http);
set it to the one Datadog's service page lists, such as
`trace.flask.request` or `trace.express.request`. New Relic's `app` is the
application's `appName`; a value holding `'` or `\` is refused rather than
escaped. **Find suggestions** offers these recipes once the source holds
their telltale (a metric name for Datadog, an event type for New Relic),
with the parameters left for you to fill in: **Review** opens the editor.

Neither kind provides **traces**: Datadog provides metrics, New Relic
metrics, RUM and crashes, and the console reads traces from Tempo only — a
`traces.spans` binding needs a Tempo source.

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
server's `/varz` (its details say so when JetStream is off). Loki and Tempo
read their build info (or `/ready`), and Elasticsearch its version and
whether the index pattern matches an index with the time field. A store
source reads the app it names, or else the apps its credential sees; a
Google Play source also says the last day Play has data for. Sentry reads
the organization, PostHog the last day's event count, Google Analytics 4
and Plausible the last 7 days' visitors, Datadog validates its keys and
counts the metrics active in the last hour, and New Relic reads the
account's name.

Click a source to open its details:

- **Try a query** runs a query (PromQL for Prometheus and Google Cloud
  Monitoring, JSON for CloudWatch, Kafka, RabbitMQ and NATS, and each other
  kind's own language; the box shows the kind's query shape) over the time range you pick, from 15 minutes to 7 days, and draws the result, one line per series,
  with a table view. An answer of single points, as a broker source gives,
  is listed as values, largest first, instead of a chart. Warnings the source returns are shown above the chart; a
  query the source refuses shows its error. At most 500 series are drawn; past
  that a warning says how many there were. It is offered for sources that
  read metrics, so not for Elasticsearch or Tempo; on Loki it takes a metric
  query.
- **Discovered metrics** lists the metric names the source has seen in the
  last hour, with a filter. Clicking a name puts it in the query box. Above
  the list, **Recipes that fit** shows every recipe whose telltale metric the
  source holds — for example `rabbitmq-prometheus` once RabbitMQ metrics are
  being scraped. A broker source lists what it holds instead: Kafka's
  `group/<name>` and `topic/<name>`, RabbitMQ's `queue/<name>`, NATS's
  `stream/<name>` and `consumer/<stream>/<name>`; clicking a group, queue,
  stream or consumer puts its JSON query in the box. The other kinds list
  their own inventory: Loki its label names, Elasticsearch the pattern's
  field names, Tempo its tags as `<scope>.<tag>`, Sentry the organization's
  projects as `project/<slug>` (clicking one puts its query in the box),
  PostHog the last 7 days' event names, GA4 the last 28 days', Plausible
  `pageview` once the site recorded one in 28 days, Datadog the metrics
  active in the last day and New Relic its event types. The two store kinds
  list nothing.

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
refused: a live read has no step and no quantile. The other kinds hold to
their own languages:

- **LogQL** (Loki): as PromQL, a param only inside a double-quoted stream
  selector value matched with `=` or `!=`, never in a line filter or a label
  filter stage.
- **TraceQL** (Tempo): a param only as the double-quoted value an attribute
  is compared to with `=` or `!=` inside `{...}`; every built-in is refused.
- **Elasticsearch JSON**: a param only inside a filter's value; every
  built-in is refused.
- **Store, Sentry and web analytics JSON**: as for the broker kinds, a param
  only in the value naming what is read (Play `package`; App Store Connect
  `app`, `version`, `build`; Sentry `project`, `environment`, `release`;
  `host`, `event`, GA4's `property`, Plausible's `site`), and every built-in
  refused.
- **Datadog**: a param is checked, not escaped. As a whole tag value inside
  `{...}` (`service:$service`) it may hold only letters, digits and `_.:/-`;
  outside, only as part of a metric name (`$metric_prefix.hits`).
  `$__quantile` goes only as the aggregator before a `:` (it becomes `p95`),
  `$__interval` is the step in seconds, and `$__rate` is refused (use
  `.as_rate()`).
- **NRQL** (New Relic): a param only inside a single-quoted string compared
  with `=` or `!=` (`appName = '$app'`); `$__quantile` is a percent where a
  number goes (`percentile(duration, $__quantile)` becomes `95`), and
  `$__rate` and `$__interval` are refused.

A recipe may give a param a default under `defaults:`, the value it takes
when a binding leaves it empty (`datadog-apm`'s `metric_prefix`).

A recipe may also declare **tables**, by name: `queues` (a worker's Queues
section), `endpoints` (a service's Endpoints: the source's own label for the
route as the key column, then `rpm`, `error_pct` and `p95_ms`), `pages`,
`referrers`, `countries` and `devices` (a web app's Pages), and `crashes`
and `signatures` (a mobile app's Crashes). A table has its columns (`key`,
`label`, `unit`), the `key_labels` that name a row, and one instant query
per other column, joined on those labels:

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

A kind that answers every column by name from one read — the store kinds,
Sentry, the web analytics kinds and New Relic — takes a single `query:` for
all of a table's value columns instead.

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
`kafka-direct`, `rabbitmq-direct` and `nats-direct` for the broker kinds.
`otel-http` reads OpenTelemetry's HTTP server metrics in Prometheus (param
`job`): request rate, error rate and latency, and an endpoints table of one
row per `http_route`. For logs and traces it ships `loki-service`,
`loki-app`, `elasticsearch-service` and `tempo-service`; for the stores and
Sentry `play-vitals`, `app-store-metrics`, `sentry-mobile`, `sentry-web` and
`sentry-backend`; for web analytics `posthog-web`, `posthog-web-vitals`,
`ga4-web` and `plausible-web`; and for APM `datadog-apm`, `datadog-system`,
`datadog-container`, `newrelic-apm`, `newrelic-browser` and
`newrelic-mobile`. The server
checks every recipe when it starts, and its tests render each one, so a
recipe that does not fit the rules is caught before it ships. Contributions
of recipes for exporters you run are welcome.
