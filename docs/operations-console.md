---
title: Operations console
description: The cross-repository console for what your components do once they run: entities and their health, the Errors inbox, Logs, Changes, Issues, mobile releases and data sources, and how the top bar's environment and time range apply.
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
golden metrics are the exception: they stay empty until data sources are
bound to components, which is a later phase. Until then they show an empty
slot, and the tiles and charts link to [Data sources](data-sources.md).

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
- **Compare with previous period**, shown on an entity's Summary. Today it
  adds to the **Deploys** tile how many deploys the period of the same
  length just before had.
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
| All entities and the four type lists | Errors over the last 24 hours and the issues open now, whatever the range |
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
metric columns: response time, throughput and error rate for services; LCP
and page views for web; crash-free users and rollout for mobile; backlog
and consume rate for workers. They read "—" until a data source is bound to
the component.

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
| Backend, Frontend, Desktop | Summary, Errors, Logs, Changes, Issues |
| Worker | Summary, Queues, Errors, Logs, Changes, Issues |
| Mobile | Summary, Releases, Errors, Logs, Changes, Issues |
| Library, CLI, Infra, Data, Game, Other | Summary, Changes, Issues |

Errors, Logs, Changes and Issues are the console pages described below,
narrowed to this entity. An entity with no environment connected in the
environment picked has no errors or logs to read there, and those sections
say so. A link to a section the role does not have opens Summary.

**Summary** has three parts:

- **Golden metrics**: the role's golden signals, empty until a data source
  is bound, then the tiles TaskTrooper fills itself. **Errors (24 h)**,
  **Open issues** in this environment, **Deploys** in the range (with the
  previous period's count when compare is on), **Last deploy** (version,
  status and when) and **Runs on** (the provider, or Custom URL, and the
  resource).
- **Golden signals**: a lane with a marker for every deploy in the range,
  each opening that change, then the role's charts. Response time,
  throughput, error rate and CPU for a service; backlog, consume rate,
  oldest message and processing errors for a worker; LCP, page views, INP
  and JavaScript errors for a web app; crash-free users, cold start, ANR rate
  and rollout for a mobile app. The charts stay empty, with **Connect a data
  source**, until a data source is bound to the component. Other roles have
  no standard signals yet.
- **Activity**: the deploys and issues in the range, newest first, each
  opening its change or issue, with links to all changes and all issues. An
  issue that started before the range shows where it was last seen.

A worker's **Queues** waits on the broker's metrics the same way: until a
data source sends them, it shows only the way to connect one.

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

**Impact** is, for now, what the release's own verification recorded:
health samples ("Health 5/5"), smoke checks ("Smoke 3/3"), new error groups
since the deploy, whether verification stopped early, and what it could not
check. Before-and-after metric comparisons come once data sources feed them.
A release not checked yet reads "Not checked yet".

Click a release to open its panel: why it failed, if it did; its facts
(entity, repository, environment, commit, tag, trigger and executor,
started, finished, duration, tasks); its timeline (opened, cut, deploy
started, deployed, end of the verification window, rollback started,
finished); the deploy watch; what verification recorded; the rollback, if
there was one, with its reason, who decided, how, the revert commit, what
was restored and the steps nothing can undo on its own; the verdict; a
build run on this computer; store builds; and the release notes.

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
probe, failed deploy or opened by hand; the author is the rules engine's
first pass, an agent or a person.

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
where you connect the system that already collects your metrics, check it,
browse what it holds and try a query. See [Data sources](data-sources.md).

## See also

- [Production incidents](incidents.md): where issues come from, how they
  are deduplicated and what the remedy engine suggests
- [Deploy targets and recipes](deploy.md): the release flow behind Changes
- [Mobile devices and store releases](mobile-releases.md): store
  credentials, onboarding and release engines
- [Data sources](data-sources.md): connecting Prometheus-compatible sources,
  signals and recipes
