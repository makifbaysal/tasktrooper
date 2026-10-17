import type { TelemetryDict, TelemetryKindsDict } from "@/locales/en/opsTelemetry";

// Chinese strings for the console's telemetry views; mirrors en/opsTelemetry.ts key for key.
export const telemetry: TelemetryDict = {
  table: {
    readAt: "{time}读取",
    readAtTitle: "读取于 {time}。服务器会将一次读取保留 30 秒，因此它可能比本页最近一次刷新早最多 30 秒。",
    window: "覆盖最近{span}",
    windowUnknown: "覆盖数据源自身的近期时间窗口",
    windowDocs: "各表的时间窗口",
    rows: "{count} 行",
    flag: "标记",
    sortBy: "按 {column} 排序",
    truncated: "数据源的行数多于其返回的行数：仅显示前 {count} 行。",
    showAll: "显示全部 {count} 行",
    noRows: "数据源目前没有相关内容。",
    noValue: "无值",
    unnamedSource: "数据源已删除",
    recipe: "配方 {recipe}",
    customQuery: "自定义查询",
    loadFailed: "无法读取此表",
    refreshFailed: "无法刷新，显示的是上一次的结果",
    errors: {
      failed: "无法读取此表",
      needsParams: "绑定缺少一个参数",
      needsParamsBody: "此表需要一个其绑定尚未提供的值。",
      notFound: "数据源已不存在",
      notFoundBody: "读取此表所用的数据源已不存在。请绑定其他数据源。",
      unsupported: "此数据源无法逐行列出",
      unsupportedBody: "此类数据源能读取该信号随时间的变化，但不能按条目逐行列出。带有此表的配方可以提供。",
      badQuery: "数据源拒绝了此表的查询",
      timeout: "数据源未及时响应",
      timeoutBody: "它可能正忙。请稍后重试。",
      upstream: "数据源未响应",
      openSignals: "打开“信号”页",
      retry: "重试",
    },
  },
  endpoints: {
    title: "端点",
    hint: "每张表覆盖其数据源自身的近期时间窗口，每分钟读取一次",
    scopeNote: "每张端点表覆盖其数据源自身的近期时间窗口（标注在表上）；时间范围在此不适用。",
    flags: {
      errors: "错误率最高",
      slowest: "p95 最慢",
    },
    empty: {
      title: "还没有端点表",
      body: "{name} 的端点（每个路由的吞吐量、错误率和 p95）来自配方会列出它们的数据源：Prometheus、New Relic 或 Datadog 中的 OpenTelemetry 指标。请在“信号”页绑定它的某个信号。",
      signals: "打开“信号”页",
      addSource: "添加数据源",
    },
  },
  pages: {
    scopeNote: "每张网页表覆盖其数据源自身的近期时间窗口（标注在表上）；时间范围在此不适用。",
    hint: "每张表覆盖其网站分析数据源自身的近期时间窗口",
    tabsLabel: "Web 数据表",
    tabs: {
      pages: "页面",
      referrers: "引荐来源",
      countries: "国家/地区",
      devices: "设备",
    },
    empty: {
      title: "还没有“{table}”表",
      body: "{name} 的{table}数据来自在“信号”页绑定的网站分析数据源：PostHog、Google Analytics 4 或 Plausible。",
      signals: "打开“信号”页",
      addSource: "添加数据源",
    },
  },
  crashes: {
    scopeNote: "每张崩溃表覆盖其数据源自身的近期时间窗口（标注在表上）；时间范围在此不适用。",
    openIssue: "在其控制台中打开",
    issues: {
      title: "崩溃和 ANR 问题",
      hint: "Google Play 的错误问题和 Sentry 的问题，按影响用户数从多到少排列",
    },
    signatures: {
      title: "诊断签名",
      hint: "App Store Connect 中最新构建的卡顿、启动和磁盘写入签名",
    },
    types: {
      crash: "崩溃",
      anr: "ANR",
      non_fatal: "非致命",
      hangs: "卡顿",
      launches: "启动",
      disk_writes: "磁盘写入",
      fatal: "致命",
      error: "错误",
      warning: "警告",
    },
    empty: {
      title: "还没有崩溃数据",
      body: "{name} 的崩溃数据来自 Google Play vitals、App Store Connect 或 Sentry。请在“信号”页查看建议：已关联的商店应用一步即可提供其数据源。",
      signals: "打开“信号”页",
      addSource: "添加数据源",
    },
  },
  traces: {
    minDuration: "最短耗时（ms）",
    minDurationHint: "只显示耗时不少于此值的链路。按 Enter 或离开输入框即可搜索。",
    manage: "管理链路绑定",
    loadFailed: "无法搜索链路",
    refreshFailed: "无法刷新链路，显示的是上一次的结果",
    capped: "仅显示最新的 {count} 条链路。缩小时间范围或提高最短耗时可查看其余链路。",
    openFromLog: "打开链路",
    clamped: {
      title: "搜索时间范围内的最后 7 天",
      body: "链路搜索最多覆盖 7 天，因此从 {since} 开始。",
    },
    unbound: {
      title: "连接链路数据源",
      body: "{name} 的链路来自 Grafana Tempo 等链路追踪后端。请把它的链路绑定到一个 Tempo 数据源，或先添加一个。",
      bind: "绑定链路",
      addSource: "添加数据源",
    },
    empty: {
      title: "此时间范围内没有链路",
      body: "自 {since} 以来，链路数据源中没有此组件的链路。",
      withMin: "此时间范围内没有耗时 {min} ms 或更长的链路。",
      clearMin: "清除最短耗时",
    },
    list: {
      label: "链路",
      count: "{count} 条链路",
      failedCount: "{count} 条有错误",
      order: "排序",
      orders: {
        newest: "最新优先",
        slowest: "最慢优先",
      },
      columns: {
        start: "开始",
        root: "根 Span",
        duration: "耗时",
        spans: "Span 数",
        status: "状态",
      },
      open: "打开链路 {name}",
      noRoot: "未找到根 Span",
      noService: "未知服务",
      error: "错误",
      ok: "OK",
    },
    detail: {
      back: "返回链路列表",
      untitled: "链路",
      copy: "复制 Trace ID",
      copied: "Trace ID 已复制",
      duration: "耗时",
      spans: "Span 数",
      errorCount: "错误",
      services: "服务",
      serviceSpans: "{count} 个 Span",
      serviceErrors: "{count} 个失败",
      viewLabel: "链路视图",
      views: {
        waterfall: "瀑布图",
        table: "表格",
      },
      hint: "选择一个 Span 查看其属性。",
      noSpans: "数据源发送的这条链路没有任何 Span。",
      refreshFailed: "无法重新读取链路，显示的是上一次的结果",
      truncated: {
        title: "并未显示所有 Span",
        body: "这条链路的 Span 数超过单次响应能携带的数量；仅显示前 {count} 个。",
      },
      elsewhere: {
        title: "通过另一个组件找到",
        body: "此组件没有链路绑定，因此在该环境的所有链路数据源中查找这条链路，并通过 {name} 找到了它。",
        action: "在 {name} 中打开",
      },
      errors: {
        notFound: "未找到链路",
        notFoundBody: "此环境中没有任何链路数据源包含链路 {id}。它可能早于数据源的保留期限。",
        unbound: "没有可供查找的链路数据源",
        unboundBody: "此环境中没有组件绑定链路，因此无法查找链路 {id}。",
        timeout: "链路数据源未及时响应",
        timeoutBody: "它可能正忙。请稍后重试。",
        failed: "无法读取链路",
      },
    },
    waterfall: {
      span: "Span",
      listLabel: "Span 列表",
      tableLabel: "以表格显示的 Span",
      rowLabel: "{service} {name}，开始于 +{offset}，耗时 {duration}",
      failed: "失败",
      noService: "未知服务",
      noName: "未命名 Span",
      expand: "显示 {name} 下的 Span",
      collapse: "隐藏 {name} 下的 Span",
      columns: {
        depth: "层级",
        service: "服务",
        operation: "操作",
        start: "开始于",
        duration: "耗时",
        status: "状态",
      },
      status: {
        unset: "未设置",
        ok: "OK",
        error: "错误",
      },
    },
    span: {
      close: "关闭 Span",
      duration: "耗时",
      offset: "开始于",
      started: "开始时间",
      id: "Span ID",
      parent: "父 Span",
      copy: "复制 Span ID",
      copied: "Span ID 已复制",
      attributes: "属性",
      resource: "资源",
      noAttributes: "没有属性。",
      statuses: {
        ok: "OK",
        error: "错误",
      },
      kinds: {
        server: "服务端",
        client: "客户端",
        internal: "内部",
        producer: "生产者",
        consumer: "消费者",
      },
    },
  },
  webVitals: {
    title: "核心网页指标",
    hint: "所选范围内页面加载的最新 p75，对照 Google 的阈值",
    traffic: "流量",
    bands: {
      good: "良好",
      needsImprovement: "需要改进",
      poor: "较差",
    },
    quantileP75: "p75",
    thresholds: "{good} 以内为良好，超过 {poor} 为较差",
    meterLabel: "{band}：{good} 以内为良好，超过 {poor} 为较差",
    bind: "绑定数据源",
    noData: "此时间范围内没有数据",
    readFailed: "无法读取",
    latest: "此范围内的最新值",
    live: "实时值，无历史",
  },
  mobileVitals: {
    window: "{env} 中的最近 7 天，与所选范围无关。",
    asOf: "截至 {date}",
    bind: "在“信号”页绑定",
    noData: "最近 7 天没有数据",
    notYet: "尚未发布",
    playLag: "Google Play 的数据会滞后 1–2 天，因此最近几天的数据会稍后补齐。",
    readFailed: "无法读取",
  },
  offers: {
    title: "可连接的数据源",
    found: "{count} 个可连接的数据源",
    bound: "已通过 {name} 绑定 {count} 个信号",
    titles: {
      playVitals: "为此应用连接 Google Play vitals",
      appStoreMetrics: "为此应用连接 App Store 指标",
      generic: "为此应用连接 {kind}",
    },
    bodies: {
      playVitals: "无崩溃用户、崩溃率、ANR 率和主要崩溃问题，使用工作区的 Google Play 凭据读取。无需安装任何东西。",
      appStoreMetrics: "各应用版本的启动时间，以及各构建的卡顿、启动和磁盘写入签名，使用工作区的 App Store Connect 密钥读取。无需安装任何东西。",
      generic: "{kind} 数据源可以为此应用读取这些信号。",
    },
    credentialMissing: {
      google_play: "请先保存 Google Play 凭据。",
      asc: "请先保存 App Store Connect 密钥。",
    },
    saveCredential: "设置 → 集成",
    connect: "连接",
    connecting: "正在连接…",
    notNow: "暂不",
    recipe: "配方 {recipe}",
  },
  binding: {
    noPreviewTitle: "此信号没有预览",
    noPreview: "{signal} 不是可绘制成图表的序列：日志、链路和错误问题分别在日志浏览器、“链路追踪”页和错误收件箱中查看。保存即可绑定。",
  },
  errors: {
    providers: {
      sentry: "Sentry",
    },
    fromProvider: "读取自 {provider}",
    noTask: "此分组未指明可用于创建任务的数据源。请改在 {provider} 中打开。",
    openSource: "{provider} 数据源",
    fixSource: "打开数据源",
  },
  sources: {
    storeCredentials: {
      google_play: "Google Play 服务账号",
      asc: "App Store Connect API 密钥",
    },
    storeCredentialWorkspace: "已为工作区保存",
    storeCredentialPlaceholder: "选择{credential}",
    noStoreCredential: "尚未保存{credential}。",
    saveStoreCredential: "前往 设置 → 集成 保存",
    namePlaceholderStore: "默认使用类型名称",
    errors: {
      store_credential: {
        not_found: "该凭据尚未保存。请先在 设置 → 集成 中保存。",
        wrong_provider: "该凭据属于另一个商店。",
        not_permitted: "当前环境下无法为数据源使用商店凭据。",
      },
      org: {
        invalid: "请输入组织 slug，即 sentry.io/organizations/<slug>/ 中的部分。",
      },
      index: {
        invalid: "请输入索引、数据流或模式，例如 logs-*；多个用逗号分隔。",
      },
    },
  },
};

const AUTH_OPTIONS = {
  none: "无",
  bearer: "Bearer 令牌",
  basic: "Basic（用户名和密码）",
};

export const telemetryKinds: TelemetryKindsDict = {
  google_play_vitals: {
    label: "Google Play vitals",
    description:
      "来自 Google Play 的 Android vitals：崩溃率和 ANR 率、启动缓慢以及主要崩溃问题，使用工作区的 Google Play 凭据读取。无需安装任何东西；Play 发布这些数据会晚一两天。",
    queryPlaceholder: '例如 {"metric_set": "crashRateMetricSet", "metric": "userPerceivedCrashRate", "as": "percent"}',
    queryHint:
      'JSON：{"package"?, "metric_set", "metric", "dimension"?, "filter"?, "period"?, "as"?}。metric_set 可为 crashRateMetricSet、anrRateMetricSet、slowStartRateMetricSet 或 errorCountMetricSet；period 可为 daily（默认）或 hourly；as 可为 value、percent 或 free_percent。每日数据滞后 1–2 天。',
    fields: {
      store_credential: {
        label: "Google Play 凭据",
        help: "在 设置 → 集成 → Google Play 下保存的服务账号。需要在其 Google Cloud 项目中启用 Play Developer Reporting API，并在 Play Console 中拥有查看应用信息的权限。",
      },
      package_name: {
        label: "包名",
        help: "查询未指定应用时读取的应用。组件的绑定会从其仓库关联的 Android 应用自动填入。",
        placeholder: "com.example.app",
      },
    },
  },
  app_store_metrics: {
    label: "App Store Connect 指标",
    description:
      "iOS 应用的启动时间、卡顿、内存、磁盘写入和耗电量（与 Xcode Organizer 中显示的一致，按应用版本划分），以及每个构建的卡顿和启动诊断，使用工作区的 App Store Connect 密钥读取。Apple 不提供无崩溃率。",
    queryPlaceholder: '例如 {"metric": "launchTime", "percentile": "p90"}',
    queryHint:
      'JSON：{"app"?, "metric", "percentile"?, "device"?, "version"?}。metric 是 Xcode Organizer 的某个指标（launchTime、hangRate 等）或整个类别（launch、hang、memory、disk、battery）；percentile 可为 p50 或 p90。数值按应用版本给出，以阶梯形式绘制。',
    fields: {
      store_credential: {
        label: "App Store Connect 凭据",
        help: "在 设置 → 集成 → App Store Connect 下保存的 API 密钥。",
      },
      app_id: {
        label: "应用 ID",
        help: "查询未指定应用时读取的应用，即它的 App Store Connect ID。组件的绑定会从其仓库关联的 iOS 应用自动填入。",
        placeholder: "1234567890",
      },
    },
  },
  sentry: {
    label: "Sentry",
    description:
      "来自 Sentry 的无崩溃用户和会话、错误数和问题，移动端、Web 和后端项目均适用。它的问题会进入错误收件箱。",
    queryPlaceholder: '例如 {"project": "ios-app", "metric": "crash_free_users"}',
    queryHint:
      'JSON：{"project", "metric"?, "environment"?, "release"?, "search"?, "days"?}。metric 可为 events（默认）、event_rate、crash_free_users、crash_free_sessions、crash_rate、crash_rate_users、sessions 或 users；崩溃类指标需要在 SDK 中开启会话跟踪。',
    fields: {
      url: {
        label: "Sentry URL",
        help: "Sentry 云服务填 https://sentry.io（欧盟区域的组织填 https://de.sentry.io），自托管的 Sentry 填它的地址。",
        placeholder: "https://sentry.io",
      },
      org: {
        label: "组织",
        help: "组织 slug，即 sentry.io/organizations/<slug>/ 中的部分。",
        placeholder: "acme",
      },
      token: {
        label: "Auth 令牌",
        help: "具有 org:read、project:read 和 event:read 权限范围的令牌：组织令牌或内部集成的令牌。加密保存。",
      },
    },
  },
  loki: {
    label: "Loki",
    description: "Grafana Loki 或 Grafana Cloud Logs：组件的日志行，以及用 LogQL 从中统计的错误率。",
    queryPlaceholder: '例如 {service_name="api"} |= "error"',
    queryHint:
      'LogQL。日志查询（{service_name="api"} |= "timeout"）供日志浏览器使用；指标查询（sum(rate({service_name="api"} | detected_level="error" [5m]))）用于绘制图表。',
    fields: {
      url: {
        help: "Loki 的基础 URL，不含 /loki/api/v1。对于 Grafana Cloud，填写 Logs URL（https://logs-prod-<n>.grafana.net）。",
        placeholder: "http://localhost:3100",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "对于 Grafana Cloud，填写 Logs 用户 ID。",
      },
      password: {
        help: "对于 Grafana Cloud，填写具有 logs:read 权限的 access policy 令牌。",
      },
    },
  },
  elasticsearch: {
    label: "Elasticsearch / OpenSearch",
    description: "保存在 Elasticsearch、OpenSearch 或 Elastic Cloud 中的日志行：读取一个索引模式，按字段筛选。",
    queryPlaceholder: '例如 {"filters": {"service.name": "billing"}}',
    queryHint: 'JSON：{"filters": {"<field>": "<value>", …}}，须匹配所有筛选条件，最多 32 个。keyword 字段精确匹配，text 字段须包含每个词。',
    fields: {
      url: {
        help: "Elasticsearch 或 OpenSearch 的基础 URL。对于 Elastic Cloud，填写部署的 Elasticsearch 端点。",
        placeholder: "http://localhost:9200",
      },
      index: {
        label: "索引",
        help: "保存日志的索引、数据流或模式；多个用逗号分隔（logs-*,app-*）。",
        placeholder: "logs-*",
      },
      time_field: {
        label: "时间字段",
        help: "每行日志排序和筛选所依据的日期字段。留空则为 @timestamp。",
        placeholder: "@timestamp",
      },
      message_field: {
        label: "消息字段",
        help: "保存日志行本身的字段；文本筛选在其中搜索。留空则为 message。",
        placeholder: "message",
      },
      level_field: {
        label: "级别字段",
        help: "保存日志级别的字段（ECS 的 log.level）。留空则为 log.level。",
        placeholder: "log.level",
      },
      auth: {
        options: {
          none: "无",
          basic: "Basic（用户名和密码）",
          api_key: "API 密钥",
        },
      },
      api_key: {
        label: "API 密钥",
        help: "编码后的 API 密钥（id:key 的 base64），即 Kibana 创建密钥时显示的值。加密保存。",
      },
    },
  },
  tempo: {
    label: "Tempo",
    description: "Grafana Tempo 或 Grafana Cloud Traces：组件的链路，用 TraceQL 搜索。",
    queryPlaceholder: '例如 { resource.service.name = "api" }',
    queryHint: 'TraceQL：组件链路起始的 Span，例如 { resource.service.name = "api" }。在“链路追踪”页查看，不会绘制成图表。',
    fields: {
      url: {
        help: "Tempo HTTP API 的基础地址，不含 /api。对于 Grafana Cloud，填写以 /tempo 结尾的 Traces URL。",
        placeholder: "http://localhost:3200",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "对于 Grafana Cloud，填写 Traces 用户 ID。",
      },
      password: {
        help: "对于 Grafana Cloud，填写具有 traces:read 权限的 access policy 令牌。",
      },
    },
  },
  posthog: {
    label: "PostHog",
    description:
      "来自 PostHog 的页面浏览量、会话、访客、JavaScript 异常和网页指标，以及热门页面、引荐来源、国家/地区和设备。除 posthog-js 外无需安装任何东西。",
    queryPlaceholder: '例如 {"metric": "lcp", "quantile": 0.75}',
    queryHint:
      'JSON：{"metric"?, "event"?, "quantile"?, "host"?}。metric 可为 page_views（默认）、sessions、visitors、js_errors、events（需同时指定要统计的 event）或网页指标 lcp、inp、cls、fcp，未指定 quantile 时取 0.75。host 只保留一个主机名。',
    fields: {
      url: {
        label: "PostHog URL",
        help: "PostHog Cloud 填 https://us.posthog.com 或 https://eu.posthog.com，自托管的 PostHog 填它的地址。",
        placeholder: "https://us.posthog.com",
      },
      project_id: {
        label: "项目 ID",
        help: "项目 URL 中的数字（/project/<id>），也可在 Project settings 下找到。",
        placeholder: "12345",
      },
      personal_api_key: {
        label: "个人 API 密钥",
        help: "对此项目具有 Query Read 权限范围的个人 API 密钥（phx_…）。加密保存。",
      },
    },
  },
  ga4: {
    label: "Google Analytics 4",
    description:
      "来自 Google Analytics 4 媒体资源的页面浏览量、会话和活跃用户，以及热门页面、引荐来源、国家/地区和设备。GA4 处理数据最多需要两天，因此最新的数据点可能仍会上升。",
    queryPlaceholder: '例如 {"metric": "page_views"}',
    queryHint:
      'JSON：{"metric"?, "event"?, "host"?, "property"?}。metric 可为 page_views（默认）、sessions、visitors 或 events（需同时指定要统计的 event，即 GA4 的 eventName）。property 用于读取此数据源之外的另一个媒体资源。',
    fields: {
      property_id: {
        label: "媒体资源 ID",
        help: "管理 → 媒体资源详情 下的数字（不是 G-… 开头的衡量 ID）。",
        placeholder: "123456789",
      },
      auth: {
        label: "登录方式",
        help: "已连接的 Google Cloud 账号，或仅供此数据源使用的服务账号密钥。",
        options: {
          cloud_account: "已连接的 Google Cloud 账号",
          service_account: "服务账号密钥",
        },
      },
      cloud_account_id: {
        label: "Google Cloud 账号",
        help: "其服务账号必须是该媒体资源的查看者（管理 → 媒体资源访问权限管理），并且其项目中已启用 Google Analytics Data API。",
      },
      service_account_json: {
        label: "服务账号密钥（JSON）",
        help: "某个服务账号的密钥，该服务账号须是此媒体资源的查看者，且其项目中已启用 Google Analytics Data API。粘贴整个 JSON 文件。加密保存。",
      },
    },
  },
  plausible: {
    label: "Plausible",
    description: "来自 Plausible Analytics 的页面浏览量、访问次数和访客，以及热门页面、来源、国家/地区和设备。",
    queryPlaceholder: '例如 {"metric": "visitors"}',
    queryHint: 'JSON：{"metric"?, "host"?, "site"?}。metric 可为 page_views（默认）、sessions（访问次数）或 visitors。site 用于读取此数据源之外的另一个站点。',
    fields: {
      url: {
        label: "Plausible URL",
        help: "https://plausible.io，或自托管的 Plausible 的地址。",
        placeholder: "https://plausible.io",
      },
      site_id: {
        label: "站点",
        help: "站点在 Plausible 中所用的域名。",
        placeholder: "example.com",
      },
      api_key: {
        label: "Stats API 密钥",
        help: "拥有该站点的团队的 Stats API 密钥（Account settings → API keys）。加密保存。",
      },
    },
  },
  datadog: {
    label: "Datadog",
    description: "来自 Datadog 指标 API 的 APM 请求速率、错误和延迟，以及主机或容器指标。",
    queryPlaceholder: "例如 sum:trace.http.request.hits{service:api,env:prod}.as_rate()",
    queryHint:
      "Datadog 指标查询，写法与仪表板中相同：avg:system.cpu.user{service:web} by {host}。表格读取每个标签组的最新值。",
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
        label: "站点",
        help: "你的 Datadog URL 对应的站点：app.datadoghq.com 对应 datadoghq.com，app.datadoghq.eu 对应 datadoghq.eu，us5.datadoghq.com 对应 us5.datadoghq.com。",
      },
      api_key: {
        label: "API 密钥",
        help: "Organization settings → API keys。加密保存。",
      },
      app_key: {
        label: "应用密钥",
        help: "Organization settings → Application keys；限定范围的密钥需要 timeseries_query 和 metrics_read。加密保存。",
      },
    },
  },
  newrelic: {
    label: "New Relic",
    description: "通过 NRQL 从 New Relic 读取 APM 吞吐量、错误和延迟，浏览器页面浏览量和网页指标，以及移动端崩溃率。",
    queryPlaceholder: "例如 SELECT percentile(duration * 1000, 95) FROM Transaction WHERE appName = 'api'",
    queryHint:
      "NRQL，不含 SINCE、UNTIL 或 TIMESERIES：此类数据源会按时间范围自动添加。表格使用 FACET 查询，每个 facet 一行。",
    fields: {
      region: {
        label: "区域",
        help: "账号数据所在的位置：one.eu.newrelic.com 选 eu，否则选 us。",
        options: {
          us: "US",
          eu: "EU",
        },
      },
      account_id: {
        label: "账号 ID",
        help: "账号的编号，显示在账号选择器中账号名称的下方。",
        placeholder: "1234567",
      },
      api_key: {
        label: "用户密钥",
        help: "在 API keys 中创建的用户密钥（NRAK-…），其所属用户须能查询该账号。加密保存。",
      },
    },
  },
};
