---
title: Operations console
description: The cross-repository console for what your components do once they run: entities and their health, metric charts, golden metrics and Core Web Vitals from bound signals, endpoint, page and crash tables, traces, the Errors inbox, Logs, Changes with deploy impact and rollback approval, Issues, alert rules, mobile releases and data sources, and how the top bar's environment and time range apply.
---

# Operations console

**Operations** in the app's sidebar opens a console for everything your
components do once they run somewhere: which ones are healthy, what errors
and logs they produce, what shipped, which issues are open and, for mobile
apps, where each build stands in the stores. It reads across every
repository at once, one environment at a time.

Each component that runs is an **entity** with a page of its own. The
console reads what TaskTrooper already stores (connected environments,
releases, incidents) and what the providers you connected answer: Vercel,
Google Cloud, AWS, App Store Connect and Google Play. Metric charts and
golden metrics come from [data sources](data-sources.md): each of a
component's **signals** (request rate, latency, queue backlog, …) is
**bound** to a source on the entity's [Signals](#signals) page, and from
then on its chart and golden metric are drawn from that source. So are an
entity's endpoint, page and crash tables and its traces, and, once bound,
its log lines (Loki, Elasticsearch) and Sentry issues. A signal that is not
bound yet shows an empty slot with a way to bind it.

## Navigation

The console has its own column next to the app sidebar, in three groups:

| Group | Page | Address | What it lists |
|---|---|---|---|
| Explore | **All entities** | `/operations` | Every entity, with health, errors, open issues and last deploy |
| | **Services** | `/operations/services` | Components with the Backend role |
| | **Web** | `/operations/web` | Components with the Frontend role |
| | **Mobile** | `/operations/mobile` | Components with the Mobile or Desktop role |
| | **Workers** | `/operations/workers` | Components with the Worker role |
| Monitor | **Errors inbox** | `/operations/errors` | Grouped errors from every connected component |
| | **Error definitions** | `/operations/errors/definitions` | What each kind of error means and what it sets off (the Errors area's second tab) |
| | **Logs** | `/operations/logs` | Log entries across components |
| | **Changes** | `/operations/changes` | Every release, grouped by day |
| | **Issues** | `/operations/issues` | Production incidents |
| | **Alerts** | `/operations/alerts` | Alert rules and their state |
| Settings | **Data sources** | `/operations/sources` | Systems that already collect your metrics, logs, traces and errors |

**Collapse** folds the column to icons. It starts folded on an entity page,
which has its own section list, until you choose; your choice is kept after
that. In a narrow window the column becomes one scrolling row of links.

The console replaced the old Deployments, Production incidents and Mobile
Apps pages, and their addresses still work: `/operations/deployments` opens
Changes, `/operations/incidents` (and `/incidents`) opens Issues with the
query string kept, so `?id=` still opens the same incident, and
`/operations/apps` opens Mobile. An old component page link
(`/operations/c/<id>`) opens that entity's page.

A repository whose store apps no mobile component carries (nothing scanned
as Mobile, or only a desktop or library component) is listed under Mobile
too, as a store-app row; it opens the same releases view for the repository
at `/operations/mobile/app/<repository id>`.

## The top bar

Beside the page title, the top bar holds the controls every page shares:

- **Environment**: Production, Staging, Preview or Development. Production
  is the default. Every page shows one environment at a time.
- **Time range**: a preset (the last 30 minutes, 1, 3, 6, 12 or 24 hours,
  3 or 7 days) or **Custom range…**, a start and an end. The default is the
  last hour. A preset window slides with the clock, its end moving to now
  every minute; a custom range stays where you put it.
- **Compare with previous period**, shown on an entity's Summary. It draws
  the period of the same length just before as dashed lines on every chart,
  labelled "previous period", and adds to the **Deploys** tile how many
  deploys that period had.
- **Refresh** reads the page again now and moves a preset window to now.
  "Updated …" says how long ago that was. Most pages also read again on
  their own, every 15 seconds to a minute; Logs reads only when asked, or
  with Live tail on.

The range, environment and compare switch are part of the address, as are
each page's filters, so a link you copy opens the same view. Moving between
console pages keeps what you picked.

### Where the time range does not apply

Some pages do not read a time window. There the time picker is hidden, and
most of them put a note in its place saying what the page shows instead.

| Page | What it shows |
|---|---|
| All entities | Errors over the last 24 hours and the issues open now, whatever the range |
| Issues, and an entity's Issues | Every issue open now. With **Include resolved and ignored** on, the picker comes back and closed issues are listed when they were happening at some point in the range |
| An entity's Endpoints, Pages and Crashes | Each table as its data source holds it now, over the window its recipe reads (see [Endpoints](#endpoints), [Pages](#pages) and [Crashes](#crashes)). The environment applies |
| A mobile entity's Releases | Read live from App Store Connect and Google Play; the environment picker is hidden too. Its [Crash & launch](#crash-and-launch-tiles) tiles show the last 7 days, in the environment last picked |
| Data sources | Neither the range nor the environment applies |

Elsewhere the range applies, with these exceptions:

- The **Errors inbox** reads from the start of the range up to now. Error
  groups have no end time, so a custom range that ends in the past still
  counts up to now, and a range starting more than 30 days back is read from
  30 days ago. The page says so when either happens.
- **Changes** always lists a release that is still moving (deploying,
  verifying, waiting for a verdict, rolling back) or waiting to deploy,
  whatever the range.
- On an entity's Summary, the **Errors (24 h)** tile is the last 24 hours
  whatever the range.
- An entity's **Traces** searches the last 7 days of the range at most, the
  longest a trace search reads, and says so when it cuts the range.
- On Services, Web, Mobile and Workers the range drives only the golden
  metric columns; **Errors (24 h)** and **Open issues** stay as on All
  entities.

## Entities and health

An entity is one component of a repository. A component is listed when it
has a confirmed environment anywhere, or when its role is Backend,
Frontend, Worker, Mobile or Desktop. Such a component that is not connected
yet is listed as **Not reporting**, not left out. Libraries, CLIs and other
roles appear, as **Other**, only once they are connected to an environment.

Health is worked out per environment from what TaskTrooper stores today:
the open issues, the health sweep's last reading of the environment's
resource, and its error count over the last 24 hours. Data sources do not
feed health yet.

| Health | When |
|---|---|
| **Critical** | An open issue at critical or high severity, or the resource reports failed |
| **Warning** | Any other open issue, the resource reports degraded, or errors in the last 24 hours |
| **Healthy** | Connected in this environment, the resource reports healthy (or is deploying), and none of the above |
| **Not reporting** | No environment connected here, or connected but no health reading yet |

The checks run top to bottom and the first match wins. Hovering an entity's
health square says which one it was, for example "Open critical or high
issues: 2". An open issue counts even for an entity that is not connected in
that environment.

An incident names a repository and an environment, never a component. The
console gives it to the repository's components that run in that
environment, or, when none does, to every listed component of the
repository. A repository whose issues no listed component can carry
(nothing scanned yet, or only libraries) gets a **Repository-level issues**
row on All entities, so its issues are still counted.

Alerts and deploy watches write their own short names for an environment,
and the console reads them case-insensitively:

| Label | Environment |
|---|---|
| None, `live`, or starting with `prod` | Production |
| Starting with `stag`, `preprod` or `pre-prod`; `stg`, `uat`, `qa`, `test` | Staging |
| Starting with `prev` | Preview |
| Starting with `dev`; `local` | Development |

An issue or a change with any other label is not shown under any
environment.

### The explorer

All entities and the four type lists share one page. At the top, one tile
per health state with its count (click one to filter the list), and the
number of open issues in the environment, which opens Issues. Then:

- a search by entity or repository name, and a **Project** filter
  (including **No project**);
- on All entities only, type chips (Services, Web, Mobile, Workers, Other)
  and **Group by type**;
- the list, worst health first: health, name with its repository, type (on
  All entities and Mobile), **Errors (24 h)**, **Open issues** (opens Issues
  for that entity) and **Last deploy**, the newest release that reached this
  environment.

On Services, Web, Mobile and Workers the list adds that type's golden
metric columns: response time, throughput and error rate for services; LCP,
page views and INP for web; crash-free users, cold start and ANR rate for
mobile; backlog, consume rate and processing errors for workers. Each cell
is the signal's latest value in the range, with its unit (`120 ms`,
`1.5 s`, `35 req/s`, `0.4%`), and a small sparkline of the range. These
pages offer the time picker for that reason. The whole list is read at
once, for the environment and range picked, so the values of every row
arrive together. A cell reads "—" while the signal has no data source bound;
hover it to see why. A component with some signals bound but no value for
this one in the range also reads "—", with "No value in this range, or no
data source bound". A worker whose backlog is read from a broker directly
shows its live value with no sparkline: such a source keeps no history.

An entity that is not connected in the environment picked has a
**Connect** link. It opens its repository's **Deploy & Runtime** tab, where
environments are connected.

## The entity page

Click an entity to open its page. The header shows its health, name and
role; its repository (which opens the repository); the environment picked,
with the provider and the resource or address it runs on, or **Not
connected in …** with **Connect**; and the last deploy there. The
environment is always the one in the top bar, connected or not. A service,
web app, worker, mobile or desktop app also has **Enable observability** at
the right of the header (see [Enable observability](#enable-observability)).

The sections depend on the role:

| Role | Sections |
|---|---|
| Backend | Summary, Endpoints, Errors, Logs, Traces, Changes, Issues, Alerts, Signals |
| Worker | Summary, Queues, Errors, Logs, Traces, Changes, Issues, Alerts, Signals |
| Frontend | Summary, Pages, Errors, Logs, Traces, Changes, Issues, Alerts, Signals |
| Mobile | Summary, Crashes, Releases, Errors, Logs, Changes, Issues, Alerts, Signals |
| Desktop | Summary, Errors, Logs, Changes, Issues, Alerts, Signals |
| Library, CLI, Infra, Data, Game, Other | Summary, Changes, Issues, Alerts |

Errors, Logs, Changes, Issues and Alerts are the console pages described
below, narrowed to this entity. An entity with no environment connected in the
environment picked has no errors or logs to read there, and those sections
say so — unless it has a confirmed binding there that is read on its own:
`logs.lines` (Loki, Elasticsearch) for Logs, `errors.issues` (Sentry) for
Errors. A link to a section the role does not have opens Summary.

**Summary** has three parts:

- **Golden metrics**: the role's golden signals, each its latest value in
  the range with its unit, the change against the period just before (for
  example `+20% vs previous period`) and a sparkline. The change is coloured
  by what the signal means: up is bad for latency, error rate, backlog and
  errors, good for throughput, page views and crash-free users; under 1%
  reads "No change". A signal that is
  not bound reads "—" with **Bind a data source**, which opens Signals. On a
  mobile or desktop app, a crash-free users, crash rate or ANR rate bound to
  Google Play vitals with no value while the range ends within two days of
  now reads "Not published yet": Google Play publishes its daily vitals a day
  or two late. Bound to any other source, it reads "No data in this range".
  Then come the tiles TaskTrooper fills itself. **Errors (24 h)**,
  **Open issues** in this environment, **Deploys** in the range (with the
  previous period's count when compare is on), **Last deploy** (version,
  status and when) and **Runs on** (the provider, or Custom URL, and the
  resource). A web app adds its
  [Core Web Vitals and traffic](#core-web-vitals-and-traffic) next.
- **Golden signals**: a lane with a marker for every deploy in the range,
  each opening that change, then the role's charts. Response time,
  throughput, error rate and CPU for a service; backlog, consume rate,
  oldest message and processing errors for a worker; LCP, page views, INP
  and JavaScript errors for a web app; crash-free users, cold start, ANR rate
  and rollout for a mobile app; crash-free users, crash rate and cold start
  for a desktop app. Other roles have no standard signals yet. Once a signal
  is bound its chart draws the range from that source, with the deploy
  markers and a crosshair shared by every chart. Response time draws its
  p50, p75, p95 and p99 as four lines with a legend when the binding reads
  quantiles (p75 is the newest of them, in the same palette order); with
  **Compare with previous period** on, each line gets a dashed twin for the
  period before. Values carry their unit, and the y axis
  switches to seconds, minutes or KB…GB when the numbers get large. A chart
  that is not bound reads "No data source bound" with **Bind**, which opens
  the binding editor right there, and **Manage signals**; a chart whose read
  failed says why, with **Retry**; one that is bound but has nothing in the
  range says so.
- **Activity**: the deploys and issues in the range, newest first, each
  opening its change or issue, with links to all changes and all issues. An
  issue that started before the range shows where it was last seen.

A signal bound to a broker read directly (Kafka, RabbitMQ or NATS, see
[Data sources](data-sources.md#message-brokers-kafka-rabbitmq-and-nats))
has no history: such a source answers only the value as it is now, and the
server marks the signal live. A live broker signal shows a value, not a
chart: its chart shows that value instead, with "Live value — connect an
exporter-backed source for history", and its golden metric shows the value
with "Live value, no history" and no sparkline.

### Core Web Vitals and traffic

A web app's Summary has two more rows between its golden metrics and its
charts. **Core Web Vitals** shows LCP, INP and CLS, each its latest p75 in
the range, against Google's thresholds:

| Vital | Good | Needs improvement | Poor |
|---|---|---|---|
| LCP (largest contentful paint) | 2.5 s or less | over 2.5 s, up to 4 s | over 4 s |
| INP (interaction to next paint) | 200 ms or less | over 200 ms, up to 500 ms | over 500 ms |
| CLS (cumulative layout shift) | 0.1 or less | over 0.1, up to 0.25 | over 0.25 |

Each tile names its band, marks the value on a good / needs improvement /
poor meter, lists the two thresholds and carries a sparkline of the range;
it is tinted when the vital needs improvement or is poor. The web analytics
recipes (PostHog, New Relic Browser) answer the p75 itself. A binding that
reads a histogram (a query with `$__quantile`) is read at p75 as well, the
percentile Google rates by; no other percentile stands in for it.

**Traffic** shows page views (per second), sessions and visitors, each its
latest value in the range with a sparkline. A vital or traffic signal that
is not bound reads "—" with **Bind a data source**, which opens the binding
editor; one whose read failed says why, with **Retry** or a way to fix the
binding.

### Endpoints

A service's **Endpoints** section lists its endpoints the way New Relic's
Transactions page does: one row per route or transaction, with
**Requests** (per minute, with a bar against the busiest), **Errors** (the
share that failed, in percent) and **p95** latency. Rows start busiest
first; click any column header to sort by it. With at least two rows, the
endpoint with the highest error rate (when it is above 0) and the one with
the slowest p95 are flagged, with a legend under the table.

The tables come from the service's confirmed `http.*` bindings (request
rate, error rate, latency) whose recipe declares an endpoints table:

| Recipe | Rows |
|---|---|
| `otel-http` (OpenTelemetry metrics in Prometheus) | One per `http_route`, at the moment it is read |
| `datadog-apm` | One per resource, its latest values of the last 10 minutes |
| `newrelic-apm` | One per transaction name over the last hour, the top 100 by count |

There is one table per data source and recipe, headed by the source's name
and the recipe. Each covers its source's own recent window (Datadog the 10
minutes before, New Relic the hour before; a Prometheus table its rate
window), which the table says beside the recipe, and is read again every
minute, so the time range does not apply; the environment does. Tables show
when they were read ("Read 1 minute ago"): the server keeps a read for 30
seconds, which the time's tooltip says. See
[Table windows](data-sources.md#table-windows). A table
shows 100 rows with **Show all** for the rest, and says so when the source
held more rows than it sent.

With no such binding, the section says where endpoints come from, with
**Open Signals** and **Add a data source**. A table that could not be read
says why: a binding missing a parameter (**Fill in parameters**), a source
that is gone (**Bind another source**) or a query the source refused
(**Edit the binding**) opens the binding editor on the signal the table was
read through, a source that did not answer offers **Retry**, and a kind that
cannot list rows explains what would give one.

### Pages

A web app's **Pages** section has four tabs, **Pages**, **Referrers**,
**Countries** and **Devices** (`?table=` in the address). Each lists one
table per bound source: one row per path, referring source, country, or
device, browser and OS, with views (with bars) and visitors, the most viewed
first; click a column header to sort by it. The tables come from the web
app's confirmed `web.*` bindings whose recipe declares that table:

| Recipe | Tables |
|---|---|
| `posthog-web` | All four; Pages also has LCP p75 |
| `ga4-web` | All four; Referrers are source / medium, with sessions |
| `plausible-web` | All four; Referrers are visit sources |
| `newrelic-browser` | Pages only: one row per page URL, with sessions |

Each covers the last 7 days (or the query's own `days`), said on the table
with when it was read, and is read again every minute; the time range does
not apply, the environment does. With no
such table bound for the tab, it says where the table comes from, with
**Open Signals** and **Add a data source**. A table that could not be read
says why, as on Endpoints.

### Queues

A worker's **Queues** section shows its queues as they stand: how much is
waiting, how fast it is consumed, and which queue or partition is behind.
It uses the environment, the time range and **Compare with previous
period** from the top bar.

- **Queue health**: **Total backlog** (summed across queues and
  partitions), **Oldest message** (the oldest of any queue), **Consume vs
  publish** (the consume rate, with what is published and whether the
  backlog is growing, draining or balanced) and **Dead letters**, each its
  latest value in the range with the change against the previous period and
  a sparkline. A signal that is not bound reads "—" with **Bind**.
- **Backlog and throughput**: the backlog over the range, and the consume
  rate against the publish rate, with a marker for every deploy in the range.
  A broker read directly shows its live value in place of the chart.
- **Queues**: one table per data source the queue signals are read from,
  re-read every 30 seconds while the page is in view. Each table names its
  source, the messaging system, the recipe (or **Custom query**) and its
  parameters, and lists one row per queue, partition or consumer: for a
  Kafka group, each partition's topic, committed and end offsets, lag and
  member; for RabbitMQ, each queue's ready and unacknowledged messages,
  consumers and rates; for NATS JetStream, each consumer's pending,
  awaiting-ack and redelivered messages; for an exporter recipe, the
  columns it declares. The columns that name a row (topic and partition,
  queue, consumer) read as keys. Each table says when it was read: the
  server keeps a read for 30 seconds. Rows are sorted by backlog, largest
  first; click any column header to sort by it. When an enabled alert rule
  watches the backlog in this environment, the rows above its threshold are
  marked;
  otherwise the row with the largest backlog is. A source that holds more
  rows than it sends says so.

A table that could not be read says why: a binding missing a parameter
offers **Fill in parameters**, a source that is gone **Bind another
source**, a query the source refused **Edit the binding** (each opens the
binding editor on the signal the table was read through), a source that did
not answer **Retry**,
and a source with no per-queue table explains what would give one.

With no queue signal bound yet, the section says where queue data comes
from and offers **Find suggestions** (it looks through your data sources,
consumer groups, queues and JetStream consumers included, for ones that
name this worker, then points to Signals to accept them), **Bind backlog**
(the binding editor) and **Add a broker data source**.

### Crashes

A mobile app's **Crashes** section lists what the stores and Sentry hold
about its crashes over each source's own recent window, said on each table
with when it was read, and read again every minute; the time range does not
apply, the environment does.

- **Crash and ANR issues**: one table per source. Google Play's are its
  error issues of the last 7 days (crashes, ANRs and non-fatal issues),
  Sentry's the unhandled, unresolved issues of the last 14 days. Each row has
  the issue id, which opens the issue in Play Console or Sentry; its type
  (Crash, ANR, Non-fatal, or Sentry's level); the cause; where it happened;
  the users it hit, with bars, most first; its reports; the last version it
  was seen in (Google Play only); and how long ago it was last seen.
- **Diagnostic signatures**: App Store Connect's hang, launch and
  disk-write signatures of the newest build, each with its type, the
  signature, its share of the build's events and the build number. App Store
  Connect lists no crash signatures there.

The tables come from the app's confirmed mobile or `errors.issues` bindings
whose recipe declares them: `play-vitals`, `sentry-mobile` and
`app-store-metrics` (see
[Data sources](data-sources.md#app-stores-and-sentry)). With none, the
section points to Signals, where Find suggestions offers a linked store
app's source in one step, and to Data sources. A table that could not be
read says why, as on Endpoints.

### Traces

A service's, worker's or web app's **Traces** section searches its traces
through its `traces.spans` binding, a Tempo source (see
[Data sources](data-sources.md#logs-and-traces-loki-elasticsearch-and-tempo)).
It covers the console's time range, cut to its last 7 days, the longest a
trace search reads, with a notice when it is cut. **Minimum duration (ms)**
keeps the traces at least that long; it searches when you press Enter or
leave the box, and is `?min=` in the address.

Up to 50 traces are listed, **Newest first** or **Slowest first**: when each
started, its root span and service, its duration with a bar against the
slowest, how many of its spans the search matched, and whether it failed.
Above the list, the count and how many have errors. When 50 come back, a
note says only the newest are shown and to narrow the range or raise the
minimum duration. The list is read again every minute.

With no traces binding, the section reads **Connect a traces source**, with
**Bind traces** (the binding editor on `traces.spans`, which lists only the
sources a traces recipe reads — Tempo; Datadog and New Relic read trace
metrics, not traces) and **Add a data source**. A binding that cannot be read says why: one missing a parameter
offers **Fill in parameters**, a source that is gone **Bind another
source**, and a source that did not answer **Retry**. **Manage the traces
binding** links to Signals.

Click a trace, or open `?trace=<id>`, to see it:

- The header: the root span and its service, the trace id with a copy
  button, when it started, its duration, how many spans it has and how many
  failed, and the services in it, each in its own colour with its span and
  failure counts. A trace with more spans than one answer carries (20,000)
  says only the first are shown.
- **Waterfall**: every span in tree order, each under its parent and
  siblings by start, its bar placed on the trace's time axis in its
  service's colour. A failed span is ringed in red, with an icon. A span with
  spans under it folds and unfolds. A span whose parent the trace does not
  hold starts a tree of its own.
- **Table**: the same spans as rows, with depth, service, operation, when it
  starts after the trace's start, duration and status.
- Pick a span for its panel: its name, service, kind and status, the status
  message of a failed span, its duration, when it starts in the trace and
  the time it started, its span and parent ids, its attributes and its
  resource's.

A trace is read through the entity's own traces binding. When the entity
has none — say, a trace opened from one of its
[log entries](#logs-explorer) while another component's source holds its
traces — it is looked up in every traces source the environment's bindings
read, and a notice names the component it was found through, with a link to
open it there. A trace no source holds says
so (it may be older than the source keeps); with no traces binding anywhere
in the environment, there is nothing to look it up in. A trace does not
change once ended, so it is read once, and again on **Refresh**.

### Signals

**Signals**, the last section of a service, web app, worker, mobile or
desktop app, says where each of the component's signals is read from in the
environment picked. A binding belongs to one component in one environment,
so production and staging can read from different sources. The list holds
the role's signals (the golden ones marked **Golden**) and any other signal
that has a binding. For each: its status (**Bound**, **Suggested** or **Not
bound**), the data source and who set it (detected, by hand or by an
agent), the recipe or **Custom query**, the recipe's parameters, and a
sparkline of the range for a bound one.

- **Find suggestions** looks through your data sources for what fits; it is
  offered even before any source exists. It lists what every source holds —
  metric names, a log or trace store's label, field or tag names, a web
  analytics source's event names — and offers each recipe whose telltale is
  there, with its parameters filled from the label values that best name the
  component (its name, directory, package or cloud resource). When the
  environment runs on a connected cloud resource (a Cloud
  Run service, an ECS service, a Lambda function, …), the matching cloud
  recipe on a source that reads through the same cloud account is offered
  with its parameters taken from the resource. On a Kafka, RabbitMQ or
  NATS source, the consumer groups, queues or JetStream consumers whose names
  best name the component are offered for its queue signals, through the
  source's direct recipe with the name as its parameter. A worker sees the
  recipes and broker sources for its own brokers first — the messaging
  clients found in its code, .NET projects (`.csproj`, `.fsproj`) included,
  which are scanned for Kafka, RabbitMQ, NATS, SQS, Pub/Sub and Service Bus
  clients — and the others still appear after them. For a mobile
  app whose repository has a store app linked, the Google Play vitals and
  App Store Connect recipes come first, their parameter filled from the app.
  A Datadog or New Relic recipe comes with its parameters to fill in; Sentry
  recipes are never offered, so bind them by hand. Suggestions appear under
  their signal.
- When a linked store app's source does not exist yet, Find suggestions
  offers to create it instead: a card, **Connect Google Play vitals for this
  app** or **Connect App Store metrics for this app**, lists the signals and
  parameters it would bind. **Connect** opens the Data sources add dialog on
  that kind with the store credential chosen, and once the source is saved
  binds the offered recipe for each of those signals. While the credential
  is not saved under Settings → Integrations, the card says so with a link
  there, and **Connect** is unavailable. **Not now** hides the card. See
  [Connecting a store source from Signals](data-sources.md#connecting-a-store-source-from-signals).
- **Accept** binds a suggestion in one click. A suggestion that could not
  fill every parameter shows what it needs and offers **Review** instead,
  which opens the editor with what it has. **Dismiss** hides a suggestion for
  good: it is not offered again.
- **Bind** or **Edit** opens the editor: pick a data source that can feed
  the signal — a metrics source for a metric signal, Loki or Elasticsearch
  for `logs.lines`, a traces source (Tempo) for `traces.spans`, Sentry for
  `errors.issues` — then either a **Recipe** (only the recipes written for
  that kind of source that provide the signal are listed, with the recipe's
  query and one field per parameter; a parameter with a default shows it and
  takes it when left empty) or a **Custom query** in the source's own
  language: PromQL for Prometheus and Google Cloud Monitoring, LogQL for
  Loki, TraceQL for Tempo, a Datadog metric query, NRQL for New Relic, and
  JSON for the others, with the kind's query shape under the box.
  **Preview** runs a metric signal over the console's range before anything
  is saved and draws it, with the query as it was sent and any warnings. A
  log, trace or error signal is not a series and has no preview: save it to
  bind it. **Save** makes it the signal's binding, replacing the one before
  and any waiting suggestions.
- **Remove** drops the binding after asking; the chart and golden metric go
  back to empty. The data source is not touched.
- **Ask AI to map signals** has the release engineer read your data
  sources' inventories and propose bindings for the unbound signals, golden
  ones first, plus alert rules for the golden ones, saved switched off. Its
  bindings arrive here as suggestions marked **AI**, to accept or dismiss
  like any other; see [AI suggestions](#ai-suggestions).

With no data source connected that could feed a signal, the page says so
and links to Data sources; Find suggestions still offers the store sources
a mobile app can connect.

## Errors inbox

The Errors inbox reads error groups from every connected environment of the
environment picked, across all components, and lists them new first, then
the most recently seen, then the most frequent.

Errors come from where a component runs. An environment bound to Google
Cloud uses Error Reporting's own groups; any other bound environment (AWS,
Vercel production) has its error-level log lines grouped by message. Vercel
previews and environments connected by a custom URL have no error feed and
are skipped; when nothing connected in the environment has one, the empty
page says so.

A component with a confirmed `errors.issues` binding in the environment, a
Sentry project (see [Data sources](data-sources.md#app-stores-and-sentry)),
has its Sentry issues read as well, beside its cloud's groups rather than
instead of them. They carry a **Sentry** badge. For these, the fingerprint
is the issue id, the occurrences are its events in the range, **New** means
its first event falls in the range, and **Open in Sentry** opens the issue.
Errors read through a binding (Sentry) can become a task: **Create task**
opens it through the component, naming the data source, with the issue's
link when it is Sentry's own page. Its panel links to the data source rather
than to the environment's settings. At most 50 bindings are read, 100 issues
each. An entity with such a binding has its Errors section read even when
it has no environment connected, and the console-wide page reads such
components with the rest even when nothing is connected in the environment;
it says "Nothing is connected" only when nothing answers.

The filters are **Entities** (pick one or more; none picked means all), a
search over message, source, fingerprint, sample and entity name, **New in
this range only**, and type chips. At most 50 environments are read; past
that a notice asks you to pick entities. A hundred groups show at a time,
with **Show more** below.

Each row shows a **New** badge on a group first seen in this range, the
message (expand it for all of it), the entity (opens its Errors section),
the occurrence count with a bar from first to last seen, the last and first
seen times, and three actions:

- **Open in …** opens the group in the provider's console, when the
  provider gives a link.
- **Copy fingerprint**.
- **Create task** opens a bug task on the board with the error, a sample
  and recent log lines, on the component the environment belongs to. The
  confirmation names the task's key; click it to open the task.

For Google Cloud's groups, "new" means first seen at or after the start of
the range. For groups built from log lines it means first seen in the later
half of the range, so an error that has been firing all along is not
flagged just because the window starts before its next occurrence.

Click a row to open its details beside the list: message, sample, source,
fingerprint, occurrences, provider, first and last seen, entity,
environment, and a link to the environment's settings.

A group that matches an [error definition](#error-definitions) shows it
under its message: the category and the definition's name, which opens it.
Groups whose definition hides them are left out; **Show ignored (n)** next to
**New in this range only** brings them back, marked as hidden, and the empty
page offers the same when everything in the range is hidden. A group with no
definition has **Define**, in its row and in its panel: it opens the
definition editor with the group's fingerprint and a pattern for its first
line (numbers and ids left open) filled in, and the message in the pattern's
test box. An entity's Errors section links to that entity's definitions.

### Environments that could not be read

One environment failing never empties the page. The environments that
could not be read are listed above the results, each with its entity, its
resource, the reason and the fix:

| Reason | Fix |
|---|---|
| Not connected any more: its provider or cloud account is gone | **Connect it again** opens the repository's Deploy & Runtime tab |
| The cloud account's credentials were rejected | **Reconnect the account** opens Settings → Integrations |
| The provider answered with an error | The provider's message is shown |
| No answer within 20 seconds | Press Refresh to try again |
| A data source binding could not be read: the source is gone, did not answer within 20 seconds, or answered with an error | **Open the data source** opens it on Data sources |

The Logs explorer lists the same way any environment it could not read.

## Error definitions

**Error definitions** (`/operations/errors/definitions`, the **Definitions**
tab beside **Inbox** in the Errors area) say what one kind of error means
and what it should set off. A definition belongs to a repository and covers
every component of it, or one. Definitions have no environment: they mark
groups in the inbox wherever they appear, and their alerts, issues and tasks
are carried out on production. A definition's count is read at most once
per twelfth of its window (every 5 minutes for an hour, every 2 hours for a
day — tasks look a day back), never more often than every two minutes. A
repository-wide definition counts a Sentry issue once even when several
components read the same project. A repository holds at most 200 active and
suggested definitions.

Pick a repository and a component to narrow the list, or search names,
patterns and reasons. Three tabs split the definitions by status:
**Active**, **Suggested** (with how many are waiting) and **Dismissed**. Each
row shows the name, what it applies to, its match in words ("message
matches /context deadline exceeded.*stripe/ and status is 502"), category,
severity, action, threshold, who wrote it (**Human**, **AI** from recent
errors, **Code** from the code's own error types) and since when it is
firing. Click one to open its panel: each match field, what it sets off, the
task it opened, why it was written and the evidence behind a suggestion.

**Match.** Every field you fill in must hold for a group to match:

| Field | Matches |
|---|---|
| Message pattern | An RE2 regular expression, tried on the message, then the sample |
| Exception type | The name before the first `:` or `(`; `NullPointerException` also matches `java.lang.NullPointerException` |
| Endpoint | `METHOD /path` printed in the message; the method is optional, a trailing `*` matches the rest of the path, `*`, `:id` or `{id}` one segment |
| HTTP status | A status printed as `status 502`, `status_code=502`, `HTTP 502` or after a request line |
| Fingerprint | Exactly one group |
| Attributes | Exact `key=value` pairs in the message, plus `source` and `provider` |

The editor's test box tries the pattern on an error message you paste and
marks what it matches. It runs in your browser; the server checks the
pattern again with RE2 when you save, and RE2 has no lookarounds or
backreferences. When several active definitions match a group, a
component's own wins over a repository-wide one, then the more specific
(a fingerprint counts most), then the most recently edited — that is the
definition the inbox shows. What a match sets off is decided per kind of
action: the best matching **Alert** or **Open an issue** definition opens
the issue and the best matching **Open a task** definition opens the task,
so one group can do both. Only a better matching **Hide from the inbox**
definition stops them; **No action** stops nothing.

**Category** is what the failure is: **Bug**, **Expected** (raised on
purpose, such as a declined card), **Dependency**, **Infrastructure** or
**User error**. **Action** is what a match sets off:

| Action | What happens |
|---|---|
| No action | The inbox shows the definition on the group |
| Hide from the inbox | The group is left out of the inbox (**Show ignored** brings it back) |
| Alert | An issue opens while the matching groups' count is over the threshold (10 in 1 hour unless set) and resolves once it drops below |
| Open an issue | An issue opens when the threshold (1 in 1 hour unless set) is crossed and stays open until someone resolves it |
| Open a task | A board task for each new matching group, for the component's developer |

**Suggested** definitions wait until you **Accept** (it becomes active) or
**Dismiss** them (it is never suggested again). **Edit** a suggestion to
change it first; **Save and accept** does both. **Delete** asks first.
Deleting a definition whose issue is open resolves it, as does changing its
action to one that opens none, or deleting its component.

## AI suggestions

Two jobs hand work to the release engineer agent, which reads what you
point it at and proposes what it finds. Nothing it proposes is active until
you accept it.

- **Ask AI to define errors**, on Error definitions: pick a repository (and
  optionally one component) and what to define them from, **Recent errors**
  in the environment picked or **The code's error types** (the errors and
  codes the repository declares, for expected and user errors). Its
  proposals wait in the **Suggested** tab.
- **Ask AI to map signals**, on an entity's [Signals](#signals): bindings
  for the unbound signals as suggestions, and disabled alert rules for the
  golden ones on [Alerts](#alerts).

While a job runs, a card above the list shows how long it has been running;
you can leave the page and come back to it. When it is done, the card lists
what was proposed, each linked to where you review it, with the agent's
reason, and its closing notes. A job that stopped to ask a question says so
and opens the chat where you can answer. Asking again while a job of the
same kind runs for the same scope picks that job up instead of starting
another. What the release engineer proposed carries an **AI** badge: alert
rules, signal bindings and suggestions, and definitions (as their source).

A job may only read and suggest, inside the repository (and component) you
picked — see [Tool policies](tool-policies.md#the-release-engineers-observability-jobs).
It runs at most 30 minutes; one that waits 30 minutes for a free slot fails.
If the agent's CLI hits its usage limit the job fails with that message
instead of resuming later. A job stuck for over 35 minutes no longer blocks
a new one.

The jobs need the release engineer agent switched on. Without one for the
component, the card says so: turn it on with **Edit team** (the pencil
beside Team in the sidebar) and ask again.

## Enable observability

**Enable observability**, in an entity's header, asks the release engineer
to instrument the component in the environment picked. The dialog shows
what it reports now: how many of its golden signals and of all its signals
are bound, the cloud its metrics are read from, the observability libraries
found in its code (OpenTelemetry, Sentry, Datadog, Prometheus, PostHog,
Crashlytics, New Relic, web-vitals), and what is missing — golden signals
not bound, no tracing SDK, no log source, no real-user monitoring on a web
app, no crash reporter on an app, queue signals not bound.

Pick what to set up (metrics, traces, logs, real-user monitoring, crash
reporting, queue metrics; what is missing is ticked) and where the
telemetry goes: **The cloud it runs on** (Google Cloud's or AWS's own
monitoring), **A self-hosted stack** (Grafana with Prometheus, Loki and
Tempo) or **An OTLP endpoint** with its URL. **Create the task** opens a task
for the release engineer and links to it. The release engineer plans the
work and opens the developer's task; once the telemetry arrives it checks
it and proposes the bindings and alert rules. It never edits code itself.
Asking again while that task is still open answers the same task.

While a component has a gap, its Summary shows a short **Observability
gaps** card with the first of them and the same button.

## Logs explorer

Logs reads log entries, newest first, from the components connected in the
environment picked, between the start and the end of the range. An
environment connected by a custom URL has no logs to read.

A component with a confirmed `logs.lines` binding in the environment, a
Loki or Elasticsearch source (see
[Data sources](data-sources.md#logs-and-traces-loki-elasticsearch-and-tempo)),
is read through that binding **instead of** its connected environments,
never both: the same lines often reach both stores. The search text and the
minimum severity are applied by the source in its own language, and a line
with no level there is judged by its text. Such a read counts toward the
10-environment limit like an environment. An entity with such a binding has
its Logs section read even when it has no environment connected, and the
console-wide page reads such components with the rest even when nothing is
connected in the environment.

The query bar has:

- **Entities**: which entities to read. None picked means all of them, up
  to the limit below.
- The search box. Its text is passed to each provider's own log search; it
  runs when you press Enter or **Run query**.
- **Minimum severity**: Any severity, or Debug, Info, Warning, Error or
  Critical **and above**.
- **Live tail**, below.

Above the entries, **Loaded entries by severity** is a histogram over the
range. It counts only the entries loaded on the page, not the provider's
totals; the note beside it says how many that is.

Each entry shows its time, severity, entity and message. Expand it for the
full timestamp, the request (method and path) and status, the trace ID
(with a copy button), the source, what it runs on and any other fields. An
entry with a trace ID, of an entity that has a Traces section (a service,
worker or web app), also has **Open trace**, which opens that trace on the
entity's [Traces](#traces).

One read returns up to 200 entries. When the range holds more, a notice
says so and **Load older entries** reads further back, until it reaches
"Start of the range". If more entries share the oldest instant than one read
returns, paging cannot get past them; narrow the query instead.

The page reads when it opens and again when the query, the range or
Refresh changes it. **Live tail** reads what arrived after the newest entry
every 5 seconds, while the window is visible, and adds it on top. It keeps
at most 2,000 entries, dropping the oldest. When more arrived at once than
one read returns, a notice says some entries were skipped and offers **Read
the range again**.

### The 10-environment limit

Logs are read from at most 10 environments at a time. When more than 10
match in the environment picked and no entities are picked, the page says
how many matched and asks you to pick the entities to read. If you pick
more than 10, only the first 10 are read and the picker says so.

## Changes

Changes is the release feed: every release of every entity in the
environment picked, grouped by day (Today, Yesterday, then the date), with a
lane marking each deploy in the range. It reads the newest 500 releases at
most; when older ones in the range are left out, a notice says so. While a
release is moving the page reads again every 15 seconds, otherwise every
minute.

The filters are **Entity** (one, or all), a search over version, tag,
commit, task key and title, failure reason and entity or repository name,
and status chips: **In progress** (deploying, verifying, waiting for a
verdict, rolling back), **Waiting to deploy**, **Released**, **Rolled
back** and **Failed**.

The columns are status, entity, version, commit (opens it on GitHub),
tasks, **Trigger** (Merge, Dispatch, Cut by hand, Batch, No deploy step),
started, duration and **Impact**.

**Impact** leads with the release's [deploy impact](deploy.md#deploy-impact)
verdict — worse, pending, neutral, improved or unknown — and the signal that
moved most ("Error rate +3.2 pt"), when its soak read one: that is, when its
delivery profile sets `max_error_rate_delta` or `max_p95_delta_pct`. Beside
it is what the release's own verification recorded: health samples
("Health 5/5"), smoke checks ("Smoke 3/3"), new error groups since the
deploy, whether verification stopped early, and what it could not check. A
release not checked yet reads "Not checked yet".

Click a release to open its panel: a **Rollback suggested** banner when a
rollback is waiting on you (see [Rollback approval](#rollback-approval)); why
it failed, if it did; its facts (entity, repository, environment, commit,
tag, trigger and executor, started, finished, duration, tasks); its
**Impact**; its timeline (opened, cut, deploy started, deployed, end of the
verification window, rollback started, finished); the deploy watch; what
verification recorded; the rollback, if there was one, with its reason, who decided, how, the
revert commit, what was restored and the steps nothing can undo on its own;
the verdict; a build run on this computer; store builds; and the release
notes.

The **Impact** section compares each golden signal of the entity's role
that has a confirmed binding in the environment: its average over the hour
before the deploy, the hour after it and that same hour a day earlier, how
far it moved (in percentage points for a rate such as errors, in percent
otherwise) and its verdict, with the threshold the soak held it to and
whether it was breached. A threshold whose signal was still settling when
the soak ended (the hour after too short or too sparse to judge) reads **Not
checked — still settling**, never as passed, and the thresholds the soak
could not judge are named under the heading with why. Below the table, a
chart per signal spans the two hours around the deploy, with the deploy
marked. While the hour after the
deploy is still running it says so, and the numbers cover what has passed.
With no signal bound it offers a link to the entity's **Signals**, where they
are bound. It reads `GET /v1/releases/{id}/impact` (again every minute while
the hour after the deploy is running); see
[Deploy targets and recipes](deploy.md#deploy-impact) for how the verdict is
reached.

The two thresholds the soak holds a deploy to, **Max error rate rise
(percentage points)** and **Max p95 latency rise (%)**, are set with the rest
of the component's delivery profile: **Edit** on the delivery card of the
repository's **Deploy & Runtime** tab. Left empty, a signal is compared but
not held to anything; the card lists the ones that are set.

### Actions on a change

The panel offers what the release's state allows. The server has the last
word: when it refuses (a newer release of the component already shipped,
for example), the panel shows its reason as given.

| Action | Offered when | What it does |
|---|---|---|
| **Roll back** | Failed, awaiting a verdict, or released within 24 hours of its verdict | Reverts its merges on the default branch and restores production as fast as its delivery allows. Anything no mechanism can undo is listed on the change afterwards. Asks why; the reason is required |
| **Deploy** | Waiting to deploy, on a component whose delivery mode is dispatch or batch | Starts the deploy of this exact commit now |
| **Mark as released** | Awaiting a verdict, or failed | Records that the release is good and moves every task it carries to Released. On a failed release this overrides the failure. Takes an optional verdict |

Every action asks first, and asks you to type the repository's name, in
every environment: a staging rollback reverts merges on the default branch,
which can redeploy production too. When the server refuses, the dialog stays
open with what you typed and says why. A released change whose verdict is
more than 24 hours old says rollback is no longer offered.

### Rollback approval

With auto rollback off for a component, the release engineer only proposes a
rollback; a person approves it. The server keeps the proposal on the release
(its reason, the release engineer's note, who proposed it and when, and the
issue it answers, if any) and sends a `rollback_suggested` notification that
links to `/operations/changes?release=<id>`. **Rollback suggested** shows at
the top of a release's panel only while the server holds such a proposal —
nothing is guessed from the release's status, its checks or the issues open
around it.

Whether the rollback can run is the server's call too, made again on every
read:

- When it can, the banner offers **Approve rollback**. It is the same
  rollback as **Roll back**: it asks you to type the repository's name, with
  the reason filled in from the proposal, and the server's refusal stays in
  the dialog.
- When it cannot, the banner says why and offers no approval: a newer release
  of the component has shipped since, a newer one is under way, the release
  never reached the environment, its verdict is more than 24 hours old, no
  release of the component is on record as live, which one is live could not
  be read, the release is in a state that cannot be rolled back, or this
  server has no git access to revert its merges (*not configured*).

**Dismiss suggestion** declines the proposal: the server drops it and the
banner goes, for everyone. Dismissing is remembered: the release engineer
won't suggest rolling that release back again until a new incident is
attributed to it, and the panel says quietly when it was dismissed. On a
server with no git reverter, rollback is not offered (*not configured*). A
proposal also goes away on its own once the release is rolled back, or once
a newer release of the component ships.

The same banner shows on an issue's panel under [Issues](#issues), with a
link to the change, but only for the release the server put the issue down
to: the proposal names the issue, or the issue's task is one the release
carries. Another release of the same repository never shows there. The
notification's address opens the panel straight away, in whatever
environment the release ran.

### Workflow deploys

Below the changes, **Workflow deploys** lists the deploy targets that run a
GitHub Actions workflow on their own, outside release tracking — only when
one can be dispatched or has run in the environment picked (and, with an
entity picked, its repository). Each row shows the last run (its status and
number, linking to the run), its commit, what triggered it and when. **Runs**
opens its history. **Deploy…** dispatches the environment's deploy workflow
for a git ref (`main` when left empty) and, for `prod`, asks you to type the
repository's name. **Roll back…** re-deploys the last run that succeeded
before the current one and always asks for the name.

These actions step into the release flow described in
[Deploy targets and recipes](deploy.md#releases).

## Issues

Issues lists the [production incidents](incidents.md) of the environment
picked. By default it shows every issue open now (Open, Triaging, Fix
proposed, Fixing), live ones first, the worst first, then the most recently
seen.

The filters are **Entity**, a search over title, source, fingerprint,
details, environment label and repository, **Status**, severity chips
(Critical, High, Medium, Low, each with its count) and **Include resolved
and ignored**, which adds the closed issues that were happening at some
point in the time range and brings the time picker back. Opening a
Repository-level issues row from All entities filters by that repository;
the chip above the list clears it.

The columns are severity, issue, entity, environment, status, opened,
duration and occurrences.

Click an issue to open its panel. It shows the facts (entity, repository,
environment, source, first and last seen, when it closed, duration,
occurrences and the task carrying it), the details, the **Suggested
remedy** with its kind, confidence and author, the timeline and the raw
alert payload as received. The source is one of alert webhook, health
probe, failed deploy, alert rule or opened by hand; the author is the rules
engine's first pass, an agent or a person. An issue an alert rule opened
links back to the rule. When the server has a rollback proposal for the
release it put the issue down to, the panel starts with **Rollback
suggested** (see [Rollback approval](#rollback-approval)).

While an issue is live, the panel offers:

- **Triage again**, which runs the remedy engine on it again.
- **Write a remedy**: your own proposal, with a kind, what to do, steps
  (one per line) and a confidence from 0 to 100. It replaces the current
  proposal, and a recurrence's automatic triage leaves it alone.
- **Resolve** closes it now, with an optional note. If the same alert fires
  again, a new issue opens.
- **Ignore** closes it and stops it counting against health, with an
  optional note. If it happens again, a new issue opens.

Resolved and ignored issues are final, and their panel says so.

### Links to an issue

The open panel is part of the address: `/operations/issues?id=<incident
id>` opens that issue, whatever the filters, and switches the environment to
the issue's own. Incident notifications carry this address; the older
`/operations/incidents?id=<id>` still opens the same panel.

## Alerts

**Alerts** (`/operations/alerts`) lists the alert rules of the environment
picked, across every entity; an entity's own **Alerts** section lists only
its rules. A rule watches one of an entity's bound [signals](#signals) (or a
query of a data source's own) and fires when its condition holds. The server
evaluates every enabled rule about once a minute. A firing rule recovers only
after five clear minutes (or its **For at least**, if longer). A rule whose
source can't be read shows the last error and holds its state for up to ten
evaluations, then reads as no data: the list marks it with a warning icon,
and its panel says how many evaluations it held its state for, with the
error. A rule that changes state more than four
times in 30 minutes notifies nothing more until it stays put for 15 minutes,
then once with the state it ended in.

Above the list, four filters with their counts: **Firing**, **Pending** (the
condition holds but not yet for as long as the rule asks), **No data** and
**OK**. Then **Entity** (on the console page), a search over name, signal
and entity, **Suggest rules** (on an entity) and **New rule**.

The columns are state, name, entity, environment, the condition in words
("Response time (p95) > 800 ms for 10 min", "Error rate rises by more than
50% over 10 min"), severity, the last value judged (in percent for a change rule),
when it was last evaluated, and a switch that turns the rule off without
deleting it. Firing rules come first, then pending, no data and OK; rules
switched off come last.

A rule the release engineer proposed carries an **AI** badge and starts
switched off; turn it on once it looks right.

Click a rule to open its panel (`?rule=<id>` in the address): the
condition, signal or query, since when it is in its state, its last value,
channels and whether it opens an issue (a channel that does not deliver yet
is marked so); what the rule is judged on over the time range, with the
threshold drawn and the spans the rule was firing shaded — latency at p95,
web vitals at p75 and a change rule's percent change, exactly as the
evaluator reads them; and every state
change, newest first, with a link to the issue a firing opened or a recovery
resolved. **Edit** opens the editor; **Delete** asks first (a firing rule's
issue is resolved before it goes). Notifications link to
`/operations/entity/<entity id>/alerts?rule=<rule id>`, which opens the
panel and switches to the rule's environment. Every entity has an **Alerts**
section, whatever its role: a rule on a custom query can watch any of them.

### Writing a rule

The editor asks for:

- **Entity** and **Environment**.
- What to watch: a **Signal** bound on the entity in that environment (only
  bound ones are offered; with none, a link to its Signals), or a **Custom
  query** on a metrics data source.
- The **Condition**: above or below a threshold in the signal's unit; rises
  by more than, or falls by more than, a percentage against one window
  earlier (enter the size, such as 30; the choice says which way); above
  a threshold and still rising, that is higher than one window earlier (0
  for any rise; it reads "Backlog above 1,000 messages and rising over 10
  min"); or no data. **For at least** is
  how long it must hold before the rule fires (0 fires at once, up to 7
  days); **Window** is what one value summarizes and how far back a change
  is measured (1 minute to 24 hours). Latency signals are judged at p95, web
  vitals at p75, and a query answering several series is judged by its worst
  one. Numbers are read in your language's format (1,000.5 in English,
  1.000,5 in German or Turkish). A line under the fields reads the condition
  back in words.
- **Backtest** (1 to 14 days): replays the condition on past data exactly as
  the evaluator would, and shows the series with the threshold (a change
  rule's percent change) and the times it would have fired shaded and
  listed, with each one's peak. Change what it reads or how it is judged
  and it says to run it again; a new name, severity or channel does not.
- **Name** (the condition itself when left empty), **Severity**,
  **Channels** and **Open an issue**. The channels this computer delivers on
  are offered; the others are listed as coming later. A channel a rule
  already names that does not deliver here stays listed and checked until
  you clear it, and saving with it says so on the field. With no channel, a
  rule that opens no issue notifies nobody. While **Open an issue** is on the
  channel choice is locked: you hear about the issue the way you hear about
  any production issue.

**Suggest** (and **Suggest rules** on an entity) asks for the starting rules
of the entity's role, built from its bound signals and their last 14 days,
leaving out what a rule already tests. Each suggestion fills the form; review
it, backtest it and save it, and the next one loads. **Skip** drops one.

A rule saved into another environment than the one picked above takes the
console there, so the list shows it.

## Mobile releases

A Mobile-role entity's **Releases** section is where its repository's App
Store Connect and Google Play apps are released. It is read live from the
stores, so the time range does not apply and the environment picker is
hidden; only its [Crash & launch](#crash-and-launch-tiles) tiles read an
environment.

At the top, one card per store app (iOS, Android) shows its bundle ID or
package name, lifecycle (Not linked, Onboarding, Ready for testing, Live),
review state, last released and last submitted version, and how far
production has rolled out. Pick one to see its releases below. With no store
app linked yet, the page links to the repository's store settings, and to
Settings → Integrations when no store account is connected; a store whose
credentials are missing gets a notice with the same link.

**Release channels** shows the three channels a build walks, in order,
each with what it holds: version and build, audience, when it changed and
what is next.

| Channel | iOS | Android |
|---|---|---|
| Internal | TestFlight internal groups | Internal testing track |
| External | TestFlight external groups | Closed or open testing track |
| Production | App Store | Production track |

"Last read" says when the channels were read from the store, and the
refresh button reads them again; the last saved read stays on screen until
the store answers. **Start release build** starts a release build on the
repository's release engine, and the confirmation says which engine took it.

**Promote to …** moves a build to the next channel, never past it. It is
unavailable while the channel holds nothing, while the app's onboarding is
not finished and, for production, until the app is live in the store.
Promoting to production asks you to type the repository's name. On iOS it
submits the version to App Review; once Apple approves it, release it from
the production controls. On Android it starts a staged rollout at the share
you set in the dialog (10 % unless you change it); raise it later from the
production controls.

On the Production channel, **Rollout** shows how far production has reached
as a percentage: for Apple's phased release, which day of seven it is on;
for a Google Play staged rollout, its share; and, when it is halted, where.

**Production controls**:

- On iOS, **Submit for review** (when the production channel, as just read
  from the store, has a new version drafted) and **Release** (when that
  version is approved and waiting to be released). Both ask you to type the
  repository's name.
- On Android, **Change rollout…** sets a new share, typed or picked from 1,
  2, 5, 10, 20, 50 and 100 %. Google Play only lets a staged rollout grow, so
  the share must be above the current one, and a share that rounds to 0 is
  refused; 100 % completes the rollout and asks you to type the repository's
  name.
  **Halt rollout** stops it where it is and asks you to type the
  repository's name. **Resume rollout** picks it up at the same share.

A halted production rollout and a rejected App Review submission are
tracked as production incidents. The page says so, with a link to the
entity's issues.

While the app is not live, the **Onboarding checklist** lists the store's
first-publish steps, each checked against the store itself; **Verify now**
checks again.

### Crash and launch tiles

**Crash & launch** shows the app's crash-free users, crash rate, ANR rate
and cold start, read through the mobile entity's bindings: Google Play
vitals, App Store Connect metrics, Sentry or New Relic (see
[Data sources](data-sources.md#app-stores-and-sentry)). Each tile covers the
last 7 days whatever the range, and shows its latest value, "As of" the day
it is for, with a sparkline. The tiles read the environment last picked in
the top bar, which the line under them names, though this page hides the
picker.

- A signal that is not bound reads "—" with **Bind on Signals**.
- Google Play publishes its daily vitals a day or two after the day they
  cover. A crash-free users, crash rate or ANR rate tile bound to Google
  Play vitals with no value yet reads **Not published yet**, and the line
  under the tiles adds that Google Play's data runs 1–2 days behind. Bound to
  another source (Sentry, New Relic), an empty tile reads **No data in the
  last 7 days**.
- Cold start, read from App Store Connect, comes per app version, so it
  changes when a version ships.
- A tile whose read failed says why, with **Retry** or a link to fix the
  binding on Signals.

A repository's store-app row under Mobile, which no mobile component
carries, has no bindings to read: its tiles link to Data sources.

### Test builds and groups

**Test builds** lists the platform's test builds, whether from a task's
UAT, started by hand or from a release, with their status, the groups they
are open to and when they started. It updates every 5 seconds while a build
is moving, and **Build default branch** starts one from the default branch.

- **Open to…** opens a ready build to TestFlight groups (iOS) or Play
  testing tracks (Android). External TestFlight groups get it only after
  Apple's Beta App Review.
- On iOS a build can be removed from a group again, except from a group that
  gets every build, and a build Apple holds for an export compliance answer
  offers **Answer compliance**.
- On Android a ready build has an install link to share.
- Each build links its GitHub run and shows its log, where there is one.

**TestFlight groups** (iOS) or **Testing tracks** (Android) lists where
builds can go, with each group's kind, tester count and current build.
Switch a group to **Auto** to open every new task build to it as soon as it
is ready. On iOS, **New group** creates an internal group (your App Store
Connect team; builds reach them without review) or an external one (anyone,
by email or public link; each build goes through Beta App Review first), and
**Testers** adds and removes people by email.

Connecting the stores, the app lifecycle and which machine builds are on
[Mobile devices and store releases](mobile-releases.md).

## Data sources

**Settings → Data sources**, the last item in the console's column, is
where you connect the systems that already collect your telemetry —
metrics (a Prometheus-compatible server, Google Cloud Monitoring or Amazon
CloudWatch through a connected cloud account, Datadog, New Relic), logs and
traces (Loki, Elasticsearch, Tempo), crashes and errors (Google Play vitals,
App Store Connect metrics, Sentry), web analytics (PostHog, Google
Analytics 4, Plausible) or a message broker — check them, browse what they
hold and try a query. See [Data sources](data-sources.md).

## See also

- [Production incidents](incidents.md): where issues come from, how they
  are deduplicated and what the remedy engine suggests, including the ones
  alert rules open
- [Deploy targets and recipes](deploy.md): the release flow behind Changes
- [Mobile devices and store releases](mobile-releases.md): store
  credentials, onboarding and release engines
- [Data sources](data-sources.md): connecting metrics, logs, traces, store,
  Sentry, web analytics, APM and broker sources, signals and recipes
