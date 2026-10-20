// English strings for the console's telemetry views — Endpoints, Pages,
// Crashes, Traces, the Core Web Vitals and mobile vitals tiles, data source
// offers — and the data source kinds that feed them. Spread into
// en/operations.ts: `telemetry` as `operations.telemetry`, `telemetryKinds`
// into `operations.sources.kinds`. No `as const`, as there.
export const telemetry = {
  table: {
    readAt: "Read {time}",
    readAtTitle: "Read at {time}. The server keeps a read for 30 seconds, so it can be up to 30 s older than this page's last refresh.",
    window: "Covers the last {span}",
    windowUnknown: "Covers the source's own recent window",
    windowDocs: "Table windows",
    rows: "{count} rows",
    flag: "Flag",
    sortBy: "Sort by {column}",
    truncated: "The source holds more rows than it sent: the first {count} are shown.",
    showAll: "Show all {count} rows",
    noRows: "The source has nothing for this right now.",
    noValue: "No value",
    unnamedSource: "Data source removed",
    recipe: "Recipe {recipe}",
    customQuery: "Custom query",
    loadFailed: "Couldn't read this table",
    refreshFailed: "Couldn't refresh, so the last answer is shown",
    errors: {
      failed: "Couldn't read this table",
      needsParams: "The binding is missing a parameter",
      needsParamsBody: "The table needs a value its binding doesn't have yet.",
      notFound: "The data source is gone",
      notFoundBody: "The data source this table was read from no longer exists. Bind another one.",
      unsupported: "This source can't list rows",
      unsupportedBody: "Its kind can read the signal over time, but not one row per item. A recipe with this table gives one.",
      badQuery: "The source refused the table's query",
      timeout: "The source didn't answer in time",
      timeoutBody: "It may be busy. Try again in a moment.",
      upstream: "The source didn't answer",
      openSignals: "Open Signals",
      retry: "Retry",
    },
  },
  endpoints: {
    title: "Endpoints",
    hint: "Each table covers its source's own recent window, read every minute",
    scopeNote: "Each endpoints table covers its source's own recent window, shown on the table, so the time range doesn't apply.",
    flags: {
      errors: "Highest error rate",
      slowest: "Slowest p95",
    },
    empty: {
      title: "No endpoint table yet",
      body: "{name}'s endpoints — throughput, error rate and p95 per route — come from a source whose recipe lists them: OpenTelemetry metrics in Prometheus, New Relic or Datadog. Bind one of its signals on Signals.",
      signals: "Open Signals",
      addSource: "Add a data source",
    },
  },
  pages: {
    scopeNote: "Each web table covers its source's own recent window, shown on the table, so the time range doesn't apply.",
    hint: "Each table covers its web analytics source's own recent window",
    tabsLabel: "Web tables",
    tabs: {
      pages: "Pages",
      referrers: "Referrers",
      countries: "Countries",
      devices: "Devices",
    },
    empty: {
      title: "No {table} table yet",
      body: "{name}'s {table} come from a web analytics source — PostHog, Google Analytics 4 or Plausible — bound on Signals.",
      signals: "Open Signals",
      addSource: "Add a data source",
    },
  },
  crashes: {
    scopeNote: "Each crash table covers its source's own recent window, shown on the table, so the time range doesn't apply.",
    openIssue: "Open in its console",
    issues: {
      title: "Crash and ANR issues",
      hint: "Google Play's error issues and Sentry's issues, most users hit first",
    },
    signatures: {
      title: "Diagnostic signatures",
      hint: "App Store Connect's hang, launch and disk-write signatures of the newest build",
    },
    types: {
      crash: "Crash",
      anr: "ANR",
      non_fatal: "Non-fatal",
      hangs: "Hang",
      launches: "Launch",
      disk_writes: "Disk writes",
      fatal: "Fatal",
      error: "Error",
      warning: "Warning",
    },
    empty: {
      title: "No crash data yet",
      body: "{name}'s crashes come from Google Play vitals, App Store Connect or Sentry. Find suggestions on Signals: a linked store app offers its source in one step.",
      signals: "Open Signals",
      addSource: "Add a data source",
    },
  },
  traces: {
    minDuration: "Minimum duration (ms)",
    minDurationHint: "Only traces at least this long. Enter or leave the box to search.",
    manage: "Manage the traces binding",
    loadFailed: "Couldn't search traces",
    refreshFailed: "Couldn't refresh the traces, so the last answer is shown",
    capped: "The newest {count} traces are shown. Narrow the range or raise the minimum duration to see the rest.",
    openFromLog: "Open trace",
    clamped: {
      title: "Searching the last 7 days of the range",
      body: "A trace search covers 7 days at most, so it starts at {since}.",
    },
    unbound: {
      title: "Connect a traces source",
      body: "{name}'s traces come from a tracing backend such as Grafana Tempo. Bind its traces to a Tempo source, or add one first.",
      bind: "Bind traces",
      addSource: "Add a data source",
    },
    empty: {
      title: "No traces in this range",
      body: "The traces source holds none for this component since {since}.",
      withMin: "No trace in this range took {min} ms or longer.",
      clearMin: "Clear the minimum duration",
    },
    list: {
      label: "Traces",
      count: "{count} traces",
      failedCount: "{count} with errors",
      order: "Order",
      orders: {
        newest: "Newest first",
        slowest: "Slowest first",
      },
      columns: {
        start: "Started",
        root: "Root span",
        duration: "Duration",
        spans: "Spans",
        status: "Status",
      },
      open: "Open trace {name}",
      noRoot: "Root span not found",
      noService: "Unknown service",
      error: "Error",
      ok: "OK",
    },
    detail: {
      back: "Back to traces",
      untitled: "Trace",
      copy: "Copy trace id",
      copied: "Trace id copied",
      duration: "Duration",
      spans: "Spans",
      errorCount: "Errors",
      services: "Services",
      serviceSpans: "{count} spans",
      serviceErrors: "{count} failed",
      viewLabel: "Trace view",
      views: {
        waterfall: "Waterfall",
        table: "Table",
      },
      hint: "Pick a span to see its attributes.",
      noSpans: "The source sent this trace without spans.",
      refreshFailed: "Couldn't read the trace again, so the last answer is shown",
      truncated: {
        title: "Not every span is shown",
        body: "The trace has more spans than one answer carries; the first {count} are shown.",
      },
      elsewhere: {
        title: "Found through another component",
        body: "This component has no traces binding, so the trace was looked up in every traces source of the environment and found through {name}.",
        action: "Open it on {name}",
      },
      errors: {
        notFound: "Trace not found",
        notFoundBody: "No traces source of this environment holds trace {id}. It may be older than the source keeps.",
        unbound: "No traces source to look in",
        unboundBody: "No component in this environment has a traces binding, so trace {id} can't be looked up.",
        timeout: "The traces source didn't answer in time",
        timeoutBody: "It may be busy. Try again in a moment.",
        failed: "Couldn't read the trace",
      },
    },
    waterfall: {
      span: "Span",
      listLabel: "Spans",
      tableLabel: "Spans as a table",
      rowLabel: "{service} {name}, starts at +{offset}, takes {duration}",
      failed: "failed",
      noService: "Unknown service",
      noName: "Unnamed span",
      expand: "Show the spans under {name}",
      collapse: "Hide the spans under {name}",
      columns: {
        depth: "Depth",
        service: "Service",
        operation: "Operation",
        start: "Starts at",
        duration: "Duration",
        status: "Status",
      },
      status: {
        unset: "Unset",
        ok: "OK",
        error: "Error",
      },
    },
    span: {
      close: "Close the span",
      duration: "Duration",
      offset: "Starts at",
      started: "Started",
      id: "Span id",
      parent: "Parent span",
      copy: "Copy span id",
      copied: "Span id copied",
      attributes: "Attributes",
      resource: "Resource",
      noAttributes: "No attributes.",
      statuses: {
        ok: "OK",
        error: "Error",
      },
      kinds: {
        server: "Server",
        client: "Client",
        internal: "Internal",
        producer: "Producer",
        consumer: "Consumer",
      },
    },
  },
  webVitals: {
    title: "Core Web Vitals",
    hint: "The latest p75 of page loads in the range, against Google's thresholds",
    traffic: "Traffic",
    bands: {
      good: "Good",
      needsImprovement: "Needs improvement",
      poor: "Poor",
    },
    quantileP75: "p75",
    thresholds: "Good up to {good}, poor past {poor}",
    meterLabel: "{band}: good up to {good}, poor past {poor}",
    bind: "Bind a data source",
    noData: "No data in this range",
    readFailed: "Couldn't read",
    latest: "Latest in the range",
    live: "Live value, no history",
  },
  mobileVitals: {
    window: "Last 7 days in {env}, whatever the range.",
    asOf: "As of {date}",
    bind: "Bind on Signals",
    noData: "No data in the last 7 days",
    notYet: "Not published yet",
    playLag: "Google Play's data runs 1–2 days behind, so the newest days fill in later.",
    readFailed: "Couldn't read",
  },
  offers: {
    title: "Data sources to connect",
    found: "{count} data sources to connect",
    bound: "Bound {count} signals through {name}",
    titles: {
      playVitals: "Connect Google Play vitals for this app",
      appStoreMetrics: "Connect App Store metrics for this app",
      generic: "Connect {kind} for this app",
    },
    bodies: {
      playVitals: "Crash-free users, crash rate, ANR rate and the top crash issues, read with the workspace's Google Play credential. Nothing to install.",
      appStoreMetrics: "Launch time per app version, and each build's hang, launch and disk-write signatures, read with the workspace's App Store Connect key. Nothing to install.",
      generic: "A {kind} source would read these signals for this app.",
    },
    credentialMissing: {
      google_play: "Save the Google Play credential first.",
      asc: "Save the App Store Connect key first.",
    },
    saveCredential: "Settings → Integrations",
    connect: "Connect",
    connecting: "Connecting…",
    notNow: "Not now",
    recipe: "Recipe {recipe}",
  },
  binding: {
    noPreviewTitle: "No preview for this signal",
    noPreview: "{signal} is no series to chart: logs, traces and error issues are read in the Logs explorer, on Traces and in the Errors inbox. Save to bind it.",
  },
  errors: {
    providers: {
      sentry: "Sentry",
    },
    fromProvider: "Read from {provider}",
    noTask: "This group names no data source to make a task from. Open it in {provider} instead.",
    openSource: "{provider} data source",
    fixSource: "Open the data source",
  },
  sources: {
    storeCredentials: {
      google_play: "Google Play service account",
      asc: "App Store Connect API key",
    },
    storeCredentialWorkspace: "saved for the workspace",
    storeCredentialPlaceholder: "Choose the {credential}",
    noStoreCredential: "No {credential} is saved yet.",
    saveStoreCredential: "Save it in Settings → Integrations",
    namePlaceholderStore: "Defaults to the kind's name",
    errors: {
      store_credential: {
        not_found: "That credential isn't saved yet. Save it in Settings → Integrations first.",
        wrong_provider: "That credential belongs to the other store.",
        not_permitted: "Store credentials can't be used for data sources in this setup.",
      },
      org: {
        invalid: "Enter the organization slug, as in sentry.io/organizations/<slug>/.",
      },
      index: {
        invalid: "Enter an index, data stream or pattern, such as logs-*; several separated by commas.",
      },
    },
  },
};

export type TelemetryDict = typeof telemetry;

const AUTH_OPTIONS = {
  none: "None",
  bearer: "Bearer token",
  basic: "Basic (username and password)",
};

export const telemetryKinds = {
  google_play_vitals: {
    label: "Google Play vitals",
    description:
      "Android vitals from Google Play — crash and ANR rates, slow starts and the top crash issues — read with the workspace's Google Play credential. Nothing to install; Play publishes them a day or two late.",
    queryPlaceholder: 'e.g. {"metric_set": "crashRateMetricSet", "metric": "userPerceivedCrashRate", "as": "percent"}',
    queryHint:
      'JSON: {"package"?, "metric_set", "metric", "dimension"?, "filter"?, "period"?, "as"?}. metric_set is crashRateMetricSet, anrRateMetricSet, slowStartRateMetricSet or errorCountMetricSet; period daily (the default) or hourly; as value, percent or free_percent. Daily data runs 1–2 days behind.',
    fields: {
      store_credential: {
        label: "Google Play credential",
        help: "The service account saved under Settings → Integrations → Google Play. It needs the Play Developer Reporting API enabled in its Google Cloud project and, in Play Console, permission to view app information.",
      },
      package_name: {
        label: "Package name",
        help: "The app read when a query names none. A component's binding fills it from its repository's linked Android app.",
        placeholder: "com.example.app",
      },
    },
  },
  app_store_metrics: {
    label: "App Store Connect metrics",
    description:
      "An iOS app's launch time, hangs, memory, disk writes and battery use as Xcode Organizer shows them, per app version, and each build's hang and launch diagnostics — read with the workspace's App Store Connect key. Apple reports no crash-free rate.",
    queryPlaceholder: 'e.g. {"metric": "launchTime", "percentile": "p90"}',
    queryHint:
      'JSON: {"app"?, "metric", "percentile"?, "device"?, "version"?}. metric is an Xcode Organizer metric (launchTime, hangRate, …) or a whole category (launch, hang, memory, disk, battery); percentile p50 or p90. Values come per app version, drawn as steps.',
    fields: {
      store_credential: {
        label: "App Store Connect credential",
        help: "The API key saved under Settings → Integrations → App Store Connect.",
      },
      app_id: {
        label: "App id",
        help: "The app read when a query names none: its App Store Connect id. A component's binding fills it from its repository's linked iOS app.",
        placeholder: "1234567890",
      },
    },
  },
  sentry: {
    label: "Sentry",
    description:
      "Crash-free users and sessions, error counts and issues from Sentry, for mobile, web and backend projects alike. Its issues join the Errors inbox.",
    queryPlaceholder: 'e.g. {"project": "ios-app", "metric": "crash_free_users"}',
    queryHint:
      'JSON: {"project", "metric"?, "environment"?, "release"?, "search"?, "days"?}. metric is events (the default), event_rate, crash_free_users, crash_free_sessions, crash_rate, crash_rate_users, sessions or users; the crash metrics need session tracking in the SDK.',
    fields: {
      url: {
        label: "Sentry URL",
        help: "https://sentry.io for Sentry's cloud (https://de.sentry.io for an organization in the EU region), or your self-hosted Sentry's address.",
        placeholder: "https://sentry.io",
      },
      org: {
        label: "Organization",
        help: "The organization slug, as in sentry.io/organizations/<slug>/.",
        placeholder: "acme",
      },
      token: {
        label: "Auth token",
        help: "A token with the org:read, project:read and event:read scopes: an organization token or an internal integration's. Stored encrypted.",
      },
    },
  },
  loki: {
    label: "Loki",
    description: "Grafana Loki or Grafana Cloud Logs: a component's log lines, and error rates counted from them with LogQL.",
    queryPlaceholder: 'e.g. {service_name="api"} |= "error"',
    queryHint:
      'LogQL. A log query ({service_name="api"} |= "timeout") feeds the Logs explorer; a metric query (sum(rate({service_name="api"} | detected_level="error" [5m]))) draws a chart.',
    fields: {
      url: {
        help: "The Loki base URL, without /loki/api/v1. For Grafana Cloud, the Logs URL (https://logs-prod-<n>.grafana.net).",
        placeholder: "http://localhost:3100",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "For Grafana Cloud, the Logs user ID.",
      },
      password: {
        help: "For Grafana Cloud, an access policy token with logs:read.",
      },
    },
  },
  elasticsearch: {
    label: "Elasticsearch / OpenSearch",
    description: "Log lines kept in Elasticsearch, OpenSearch or Elastic Cloud: one index pattern, filtered by field.",
    queryPlaceholder: 'e.g. {"filters": {"service.name": "billing"}}',
    queryHint: 'JSON: {"filters": {"<field>": "<value>", …}}, every filter matching, at most 32. Exact on a keyword field, every word on a text one.',
    fields: {
      url: {
        help: "The Elasticsearch or OpenSearch base URL. For Elastic Cloud, the deployment's Elasticsearch endpoint.",
        placeholder: "http://localhost:9200",
      },
      index: {
        label: "Index",
        help: "The index, data stream or pattern holding the logs; several separated by commas (logs-*,app-*).",
        placeholder: "logs-*",
      },
      time_field: {
        label: "Time field",
        help: "The date field each line is sorted and filtered by. Left empty, @timestamp.",
        placeholder: "@timestamp",
      },
      message_field: {
        label: "Message field",
        help: "The field holding the line itself; the text filter searches it. Left empty, message.",
        placeholder: "message",
      },
      level_field: {
        label: "Level field",
        help: "The field holding the level (ECS log.level). Left empty, log.level.",
        placeholder: "log.level",
      },
      auth: {
        options: {
          none: "None",
          basic: "Basic (username and password)",
          api_key: "API key",
        },
      },
      api_key: {
        label: "API key",
        help: "The encoded API key (base64 of id:key), as Kibana shows it when the key is created. Stored encrypted.",
      },
    },
  },
  tempo: {
    label: "Tempo",
    description: "Grafana Tempo or Grafana Cloud Traces: a component's traces, searched with TraceQL.",
    queryPlaceholder: 'e.g. { resource.service.name = "api" }',
    queryHint: 'TraceQL: the spans a component\'s traces start from, such as { resource.service.name = "api" }. Read on Traces, never as a chart.',
    fields: {
      url: {
        help: "The Tempo HTTP API base, without /api. For Grafana Cloud, the Traces URL ending in /tempo.",
        placeholder: "http://localhost:3200",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "For Grafana Cloud, the Traces user ID.",
      },
      password: {
        help: "For Grafana Cloud, an access policy token with traces:read.",
      },
    },
  },
  posthog: {
    label: "PostHog",
    description:
      "Page views, sessions, visitors, JavaScript exceptions and web vitals from PostHog, with the top pages, referrers, countries and devices. Nothing to install beyond posthog-js.",
    queryPlaceholder: 'e.g. {"metric": "lcp", "quantile": 0.75}',
    queryHint:
      'JSON: {"metric"?, "event"?, "quantile"?, "host"?}. metric is page_views (the default), sessions, visitors, js_errors, events (with the event it counts) or a web vital: lcp, inp, cls, fcp, at quantile 0.75 unless given. host keeps one hostname.',
    fields: {
      url: {
        label: "PostHog URL",
        help: "https://us.posthog.com or https://eu.posthog.com for PostHog Cloud, or your self-hosted PostHog's address.",
        placeholder: "https://us.posthog.com",
      },
      project_id: {
        label: "Project ID",
        help: "The number in the project's URL (/project/<id>) or under Project settings.",
        placeholder: "12345",
      },
      personal_api_key: {
        label: "Personal API key",
        help: "A personal API key (phx_…) with the Query Read scope on this project. Stored encrypted.",
      },
    },
  },
  ga4: {
    label: "Google Analytics 4",
    description:
      "Page views, sessions and active users from a Google Analytics 4 property, with the top pages, referrers, countries and devices. GA4 processes data for up to two days, so the newest points may still rise.",
    queryPlaceholder: 'e.g. {"metric": "page_views"}',
    queryHint:
      'JSON: {"metric"?, "event"?, "host"?, "property"?}. metric is page_views (the default), sessions, visitors or events (with the event it counts, GA4\'s eventName). property reads another property than the source\'s.',
    fields: {
      property_id: {
        label: "Property ID",
        help: "The number under Admin → Property details (not the G-… measurement ID).",
        placeholder: "123456789",
      },
      auth: {
        label: "Sign in with",
        help: "A connected Google Cloud account, or a service account key for this source alone.",
        options: {
          cloud_account: "A connected Google Cloud account",
          service_account: "A service account key",
        },
      },
      cloud_account_id: {
        label: "Google Cloud account",
        help: "Its service account must be a Viewer of the property (Admin → Property access management), with the Google Analytics Data API enabled in its project.",
      },
      service_account_json: {
        label: "Service account key (JSON)",
        help: "A key of a service account that is a Viewer of the property, with the Google Analytics Data API enabled in its project. Paste the whole JSON file. Stored encrypted.",
      },
    },
  },
  plausible: {
    label: "Plausible",
    description: "Page views, visits and visitors from Plausible Analytics, with the top pages, sources, countries and devices.",
    queryPlaceholder: 'e.g. {"metric": "visitors"}',
    queryHint: 'JSON: {"metric"?, "host"?, "site"?}. metric is page_views (the default), sessions (visits) or visitors. site reads another site than the source\'s.',
    fields: {
      url: {
        label: "Plausible URL",
        help: "https://plausible.io, or your self-hosted Plausible's address.",
        placeholder: "https://plausible.io",
      },
      site_id: {
        label: "Site",
        help: "The site's domain as it is named in Plausible.",
        placeholder: "example.com",
      },
      api_key: {
        label: "Stats API key",
        help: "A Stats API key (Account settings → API keys) of the team that owns the site. Stored encrypted.",
      },
    },
  },
  vercel: {
    label: "Vercel",
    description: "Page views, visitors and Core Web Vitals of your Vercel projects, from Web Analytics and Speed Insights, read with a connected Vercel account. Nothing to install.",
    queryPlaceholder: 'e.g. {"metric": "lcp", "project": "prj_…"}',
    queryHint:
      'JSON: {"metric"?, "project"}. project is the Vercel project\'s id (prj_…). metric is page_views (the default), visitors, lcp, inp, cls or fcp; a vital is its p75 unless quantile (0.5, 0.75, 0.9, 0.95 or 0.99) says otherwise. Web Analytics and Speed Insights must be switched on for the project.',
  },
  datadog: {
    label: "Datadog",
    description: "APM request rates, errors and latency, and host or container metrics, from Datadog's metrics API.",
    queryPlaceholder: "e.g. sum:trace.http.request.hits{service:api,env:prod}.as_rate()",
    queryHint:
      "A Datadog metric query, as a dashboard writes it: avg:system.cpu.user{service:web} by {host}. A table reads each tag group's latest value.",
    fields: {
      site: {
        options: {
          datadoghq_com: "US1 · datadoghq.com",
          us3_datadoghq_com: "US3 · us3.datadoghq.com",
          us5_datadoghq_com: "US5 · us5.datadoghq.com",
          datadoghq_eu: "EU1 · datadoghq.eu",
          ap1_datadoghq_com: "AP1 · ap1.datadoghq.com",
          "ddog-gov_com": "US1-FED · ddog-gov.com",
        },
        label: "Site",
        help: "The site in your Datadog URL: app.datadoghq.com is datadoghq.com, app.datadoghq.eu is datadoghq.eu, us5.datadoghq.com is us5.datadoghq.com.",
      },
      api_key: {
        label: "API key",
        help: "Organization settings → API keys. Stored encrypted.",
      },
      app_key: {
        label: "Application key",
        help: "Organization settings → Application keys; a scoped key needs timeseries_query and metrics_read. Stored encrypted.",
      },
    },
  },
  newrelic: {
    label: "New Relic",
    description: "APM throughput, errors and latency, browser page views and web vitals, and mobile crash rates, from New Relic with NRQL.",
    queryPlaceholder: "e.g. SELECT percentile(duration * 1000, 95) FROM Transaction WHERE appName = 'api'",
    queryHint:
      "NRQL, without SINCE, UNTIL or TIMESERIES: the kind adds them for the range. A table is a FACET query, one row per facet.",
    fields: {
      region: {
        label: "Region",
        help: "Where the account's data lives: eu for one.eu.newrelic.com, else us.",
        options: {
          us: "US",
          eu: "EU",
        },
      },
      account_id: {
        label: "Account ID",
        help: "The account's number, shown under the account name in the account picker.",
        placeholder: "1234567",
      },
      api_key: {
        label: "User key",
        help: "A user key (NRAK-…) from API keys, of a user who can query the account. Stored encrypted.",
      },
    },
  },
};

export type TelemetryKindsDict = typeof telemetryKinds;
