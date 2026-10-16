---
title: Operations console
description: The cross-repository console for what your components do once they run: entities and their health, metric charts and golden metrics from bound signals, the Errors inbox, Logs, Changes with deploy impact and rollback approval, Issues, alert rules, mobile releases and data sources, and how the top bar's environment and time range apply.
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
then on its chart and golden metric are drawn from that source. A signal
that is not bound yet shows an empty slot with a way to bind it.

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
| | **Logs** | `/operations/logs` | Log entries across components |
| | **Changes** | `/operations/changes` | Every release, grouped by day |
| | **Issues** | `/operations/issues` | Production incidents |
| | **Alerts** | `/operations/alerts` | Alert rules and their state |
| Settings | **Data sources** | `/operations/sources` | Systems that already collect your metrics |

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
| A mobile entity's Releases | Read live from App Store Connect and Google Play; neither the range nor the environment applies |
| A worker's Queues, and Data sources | Neither the range nor the environment applies |

Elsewhere the range applies, with three exceptions:

- The **Errors inbox** reads from the start of the range up to now. Error
  groups have no end time, so a custom range that ends in the past still
  counts up to now, and a range starting more than 30 days back is read from
  30 days ago. The page says so when either happens.
- **Changes** always lists a release that is still moving (deploying,
  verifying, waiting for a verdict, rolling back) or waiting to deploy,
  whatever the range.
- On an entity's Summary, the **Errors (24 h)** tile is the last 24 hours
  whatever the range.
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
data source bound".

An entity that is not connected in the environment picked has a
**Connect** link. It opens its repository's **Deploy & Runtime** tab, where
environments are connected.

## The entity page

Click an entity to open its page. The header shows its health, name and
role; its repository (which opens the repository); the environment picked,
with the provider and the resource or address it runs on, or **Not
connected in …** with **Connect**; and the last deploy there. The
environment is always the one in the top bar, connected or not.

The sections depend on the role:

| Role | Sections |
|---|---|
| Backend, Frontend, Desktop | Summary, Errors, Logs, Changes, Issues, Alerts, Signals |
| Worker | Summary, Queues, Errors, Logs, Changes, Issues, Alerts, Signals |
| Mobile | Summary, Releases, Errors, Logs, Changes, Issues, Alerts, Signals |
| Library, CLI, Infra, Data, Game, Other | Summary, Changes, Issues |

Errors, Logs, Changes, Issues and Alerts are the console pages described
below, narrowed to this entity. An entity with no environment connected in the
environment picked has no errors or logs to read there, and those sections
say so. A link to a section the role does not have opens Summary.

**Summary** has three parts:

- **Golden metrics**: the role's golden signals, each its latest value in
  the range with its unit, the change against the period just before (for
  example `+20% vs previous period`) and a sparkline. The change is coloured
  by what the signal means: up is bad for latency, error rate, backlog and
  errors, good for throughput, page views and crash-free users; under 1%
  reads "No change". A signal that is
  not bound reads "—" with **Bind a data source**, which opens Signals.
  Then come the tiles TaskTrooper fills itself. **Errors (24 h)**,
  **Open issues** in this environment, **Deploys** in the range (with the
  previous period's count when compare is on), **Last deploy** (version,
  status and when) and **Runs on** (the provider, or Custom URL, and the
  resource).
- **Golden signals**: a lane with a marker for every deploy in the range,
  each opening that change, then the role's charts. Response time,
  throughput, error rate and CPU for a service; backlog, consume rate,
  oldest message and processing errors for a worker; LCP, page views, INP
  and JavaScript errors for a web app; crash-free users, cold start, ANR rate
  and rollout for a mobile app; crash-free users, crash rate and cold start
  for a desktop app. Other roles have no standard signals yet. Once a signal
  is bound its chart draws the range from that source, with the deploy
  markers and a crosshair shared by every chart. Response time draws its
  p50, p95 and p99 as three lines with a legend when the binding reads
  quantiles; with **Compare with previous period** on, each line gets a
  dashed twin for the period before. Values carry their unit, and the y axis
  switches to seconds, minutes or KB…GB when the numbers get large. A chart
  that is not bound reads "No data source bound" with **Bind**, which opens
  the binding editor right there, and **Manage signals**; a chart whose read
  failed says why, with **Retry**; one that is bound but has nothing in the
  range says so.
- **Activity**: the deploys and issues in the range, newest first, each
  opening its change or issue, with links to all changes and all issues. An
  issue that started before the range shows where it was last seen.

A worker's **Queues** waits on the broker's metrics the same way: until a
data source sends them, it shows only the way to connect one.

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

- **Find suggestions** looks through your data sources for what fits. It
  lists the metric names of every metrics source and offers each recipe
  whose telltale metric is there, with its parameters filled from the label
  values that best name the component (its name, directory, package or cloud
  resource). When the environment runs on a connected cloud resource (a Cloud
  Run service, an ECS service, a Lambda function, …), the matching cloud
  recipe on a source that reads through the same cloud account is offered
  with its parameters taken from the resource. Suggestions appear under their
  signal.
- **Accept** binds a suggestion in one click. A suggestion that could not
  fill every parameter shows what it needs and offers **Review** instead,
  which opens the editor with what it has. **Dismiss** hides a suggestion for
  good: it is not offered again.
- **Bind** or **Edit** opens the editor: pick a metrics data source, then
  either a **Recipe** (only the recipes written for that kind of source that
  provide the signal are listed, with the recipe's query and one field per
  parameter) or a **Custom query** in the source's own language (PromQL for
  Prometheus and Google Cloud Monitoring, JSON for CloudWatch). **Preview**
  runs it over the console's range before anything is saved and draws it,
  with the query as it was sent and any warnings. **Save** makes it the
  signal's binding, replacing the one before and any waiting suggestions.
- **Remove** drops the binding after asking; the chart and golden metric go
  back to empty. The data source is not touched.

With no metrics data source connected yet, the page says so and links to
Data sources.

## Errors inbox

The Errors inbox reads error groups from every connected environment of the
environment picked, across all components, and lists them new first, then
the most recently seen, then the most frequent.

Errors come from where a component runs. An environment bound to Google
Cloud uses Error Reporting's own groups; one bound to AWS has its
error-level log lines grouped by message. Vercel environments and ones
connected by a custom URL have no error feed and are skipped; when nothing
connected in the environment has one, the empty page says so.

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

The Logs explorer lists the same way any environment it could not read.

## Logs explorer

Logs reads log entries, newest first, from the components connected in the
environment picked, between the start and the end of the range. An
environment connected by a custom URL has no logs to read.

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
(with a copy button), the source, what it runs on and any other fields.

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
whether it was breached. Below the table, a chart per signal spans the two
hours around the deploy, with the deploy marked. While the hour after the
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
rollback; a person approves it. The proposal is not stored on the release
(the server writes it as a comment on the release's newest task and sends a
`rollback_suggested` notification that links to
`/operations/changes?release=<id>`), so the panel reads it back from the
release itself. **Rollback suggested** shows at the top of a release's panel
when its delivery profile has auto rollback off, it can still be rolled back
(as in the table above), and one of these holds:

- it failed;
- it is awaiting a verdict and its verification failed: it stopped early
  (an impact threshold breached, for one), a smoke check failed, or more new
  error groups appeared than the profile allows;
- an open production issue began at most 15 minutes after it finished, the
  same rule the server uses to put an incident down to a release.

**Approve rollback** is the same rollback as **Roll back**: it asks you to
type the repository's name, with the reason filled in from what triggered the
suggestion, and the server's refusal stays in the dialog. The same banner
shows on that issue's panel under [Issues](#issues), with a link to the
change. The notification's address opens the panel straight away, in
whatever environment the release ran.

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
links back to the rule. When the issue began within 15 minutes of a
production release that waits on a person to be rolled back, the panel
starts with **Rollback suggested** and **Approve rollback** (see
[Rollback approval](#rollback-approval)).

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
evaluates every enabled rule about once a minute.

Above the list, four filters with their counts: **Firing**, **Pending** (the
condition holds but not yet for as long as the rule asks), **No data** and
**OK**. Then **Entity** (on the console page), a search over name, signal
and entity, **Suggest rules** (on an entity) and **New rule**.

The columns are state, name, entity, environment, the condition in words
("Response time (p95) > 800 ms for 10 min", "Error rate change over 10 min
> +50%"), severity, the last value judged (in percent for a change rule),
when it was last evaluated, and a switch that turns the rule off without
deleting it. Firing rules come first, then pending, no data and OK; rules
switched off come last.

Click a rule to open its panel (`?rule=<id>` in the address): the
condition, signal or query, since when it is in its state, its last value,
channels and whether it opens an issue; the signal over the time range with
the threshold drawn and the spans the rule was firing shaded; and every state
change, newest first, with a link to the issue a firing opened or a recovery
resolved. **Edit** opens the editor; **Delete** asks first (a firing rule's
issue is resolved before it goes). Notifications link to
`/operations/entity/<entity id>/alerts?rule=<rule id>`, which opens the
panel and switches to the rule's environment.

### Writing a rule

The editor asks for:

- **Entity** and **Environment**.
- What to watch: a **Signal** bound on the entity in that environment (only
  bound ones are offered; with none, a link to its Signals), or a **Custom
  query** on a metrics data source.
- The **Condition**: above or below a threshold in the signal's unit; a
  change against one window earlier, in percent (negative for a drop); above
  a threshold and still rising, that is higher than one window earlier (0
  for any rise; it reads "Backlog above 1,000 messages and rising over 10
  min"); or no data. **For at least** is
  how long it must hold before the rule fires (0 fires at once, up to 7
  days); **Window** is what one value summarizes and how far back a change
  is measured (1 minute to 24 hours). Latency signals are judged at p95, web
  vitals at p75, and a query answering several series is judged by its worst
  one. A line under the fields reads the condition back in words.
- **Backtest** (1 to 14 days): replays the condition on past data exactly as
  the evaluator would, and shows the series with the threshold (a change
  rule's percent change) and the times it would have fired shaded and
  listed, with each one's peak. Change the rule and it says to run it again.
- **Name** (the condition itself when left empty), **Severity**,
  **Channels** and **Open an issue**. Only **Desktop notification** delivers
  for now; the other channels are listed as coming later. With no channel, a
  rule that opens no issue notifies nobody. While **Open an issue** is on the
  channel choice is locked: you hear about the issue the way you hear about
  any production issue.

**Suggest** (and **Suggest rules** on an entity) asks for the starting rules
of the entity's role, built from its bound signals and their last 14 days,
leaving out what a rule already tests. Each suggestion fills the form; review
it, backtest it and save it, and the next one loads. **Skip** drops one.

## Mobile releases

A Mobile-role entity's **Releases** section is where its repository's App
Store Connect and Google Play apps are released. It is read live from the
stores, so neither the time range nor the environment applies.

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
checks again. **Crash & launch** has tiles for crash-free users, cold start
and ANR rate, empty until a data source is bound.

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
where you connect the system that already collects your metrics (a
Prometheus-compatible server, Google Cloud Monitoring or Amazon CloudWatch
through a connected cloud account), check it, browse what it holds and try
a query. See [Data sources](data-sources.md).

## See also

- [Production incidents](incidents.md): where issues come from, how they
  are deduplicated and what the remedy engine suggests, including the ones
  alert rules open
- [Deploy targets and recipes](deploy.md): the release flow behind Changes
- [Mobile devices and store releases](mobile-releases.md): store
  credentials, onboarding and release engines
- [Data sources](data-sources.md): connecting Prometheus-compatible
  sources, Google Cloud Monitoring and Amazon CloudWatch, signals and recipes
