// Chinese dictionary for code-health findings. Must mirror
// en/findings.ts keys exactly.
export const findings = {
  intro: "每次扫描都会从代码中读取：反模式、缺失或配置错误的日志、进入生产环境的调试设置、密钥等。你接受或标记为误报的发现，在之后的扫描中保持关闭。",
  allComponents: "全部组件",
  repositoryWide: "整个仓库",
  openCount: "{count} 个未处理",
  allCategories: "全部类别",
  occurrences: "{count} 处",
  lines: "{count} 行",
  moreLocations: "还有 {count} 处",
  showLocations: "显示位置",
  hideLocations: "隐藏位置",
  createTask: "创建任务",
  createTaskSelected: "为 {count} 项创建任务",
  selectAll: "全选",
  clearSelection: "清除",
  accept: "接受",
  falsePositive: "误报",
  reopen: "重新打开",
  openTask: "打开 {key}",
  reason: "原因：{reason}",
  resolvedHint: "最近一次扫描已不再发现此问题。",
  updateFailed: "无法更新此发现",
  componentSummary: {
    open: "{count} 个未处理的代码健康发现",
    none: "没有未处理的代码健康发现",
    view: "查看发现",
  },
  status: {
    open: "未处理",
    accepted: "已接受",
    false_positive: "误报",
    resolved: "已解决",
  },
  severity: {
    high: "高",
    medium: "中",
    low: "低",
  },
  category: {
    security: "安全",
    configuration: "配置",
    logging: "日志",
    error_handling: "错误处理",
    observability: "可观测性",
    reliability: "可靠性",
    quality: "代码质量",
  },
  empty: {
    noScanTitle: "尚未扫描",
    noScanDescription: "仓库首次扫描后会显示发现。",
    cleanTitle: "未发现问题",
    cleanDescription: "最近一次扫描在这里没有发现需要修复的内容。",
    filtered: "没有符合此筛选条件的发现。",
  },
  verdict: {
    acceptTitle: "接受风险",
    acceptDescription: "该发现将被关闭，即使代码中仍存在，之后的扫描也保持关闭。",
    falsePositiveTitle: "标记为误报",
    falsePositiveDescription: "告诉扫描它在这里判断有误。该发现在之后的扫描中保持关闭。",
    reasonLabel: "原因（可选）",
    reasonPlaceholder: "为什么这里没问题",
    save: "保存",
  },
  task: {
    title: "创建任务",
    description: "一个覆盖 {count} 个发现的看板任务。智能体会拿到每个位置以及各自的修复方法。",
    titleLabel: "标题",
    notesLabel: "给智能体的说明（可选）",
    notesPlaceholder: "智能体需要先知道的事项",
    submit: "创建任务",
    created: "已创建任务 {key}",
    failed: "无法创建任务",
    defaultTitleMany: "修复 {component} 中的 {count} 个代码健康发现",
    severalComponents: "多个组件",
  },
  rules: {
    hardcoded_secret: {
      title: "代码中提交了密钥",
      description: "仓库中存在已知格式的密钥或令牌。请先轮换，再改为从配置读取。",
    },
    hardcoded_credential: {
      title: "硬编码凭据",
      description: "密码、密钥或令牌以字面量写在代码中，而不是从配置读取。",
    },
    committed_env_file: {
      title: "含密钥的 env 文件未被忽略",
      description: "包含真实密钥的 .env 文件被版本控制或未被 git 忽略。",
    },
    tls_verification_disabled: {
      title: "TLS 校验已关闭",
      description: "证书或主机名校验被关闭，流量可能被拦截。",
    },
    cleartext_traffic_allowed: {
      title: "允许明文 HTTP",
      description: "应用允许对所有主机使用未加密的 HTTP。",
    },
    sql_injection_risk: {
      title: "可能存在 SQL 注入",
      description: "值被拼接进 SQL 字符串，而不是作为参数传入。",
    },
    dynamic_code_execution: {
      title: "动态执行代码",
      description: "eval、exec 或不安全的反序列化会执行输入中的任何内容。",
    },
    unsafe_html_injection: {
      title: "未转义的 HTML",
      description: "原始 HTML 未经清理直接渲染，可能导致跨站脚本攻击。",
    },
    permissive_cors: {
      title: "CORS 允许任意来源",
      description: "任何网站都能从访问者的浏览器调用此 API。",
    },
    insecure_token_storage: {
      title: "令牌明文存储",
      description: "令牌或密码保存在 localStorage、UserDefaults 等处。",
    },
    http_body_logging: {
      title: "记录了请求体或请求头",
      description: "请求体和请求头会把令牌和个人数据带进日志。",
    },
    error_details_exposed: {
      title: "错误详情返回给客户端",
      description: "堆栈跟踪或开发者错误页会暴露给用户。",
    },
    public_source_maps: {
      title: "公开的 source map",
      description: "生产构建会发布原始源代码。",
    },
    debug_mode_in_production: {
      title: "生产环境开启调试模式",
      description: "生产配置开启了调试或开发模式。",
    },
    debug_log_level_in_production: {
      title: "生产环境日志级别为 debug",
      description: "生产配置以 debug 或 trace 级别记录日志。",
    },
    debuggable_release_build: {
      title: "Release 构建可调试",
      description: "任何人都能把调试器附加到 Android release 构建上。",
    },
    release_minification_disabled: {
      title: "Release 构建未压缩混淆",
      description: "release 构建关闭了 R8/ProGuard。",
    },
    unconditional_debug_logging: {
      title: "日志级别在代码中写死为 debug",
      description: "日志级别在代码中设为 debug，忽略了环境配置。",
    },
    console_logs_in_production: {
      title: "生产环境中的调试控制台输出",
      description: "console.log/debug/info 调用被打包进产物，构建也没有移除它们。",
    },
    mobile_debug_prints: {
      title: "Release 构建中的调试打印",
      description: "print、NSLog 或 Log.d 在调试保护之外执行。",
    },
    print_instead_of_logger: {
      title: "用 print 代替日志",
      description: "服务代码直接写 stdout，而没有使用日志器。",
    },
    print_stack_trace: {
      title: "堆栈被打印而非记录",
      description: "printStackTrace 或 print_exc 绕过了日志系统。",
    },
    no_structured_logging: {
      title: "没有日志库",
      description: "服务没有日志器，因此没有分级的结构化日志。",
    },
    no_request_logging: {
      title: "没有请求日志",
      description: "HTTP 服务不记录请求（方法、状态码、耗时）。",
    },
    catch_without_logging: {
      title: "错误被吞掉且未记录",
      description: "捕获的错误既没有记录，也没有重新抛出或上报。",
    },
    empty_catch_block: {
      title: "空的错误处理",
      description: "空的 catch、except 或 err 块会隐藏该路径上的所有失败。",
    },
    bare_except: {
      title: "裸 except",
      description: "except: 还会捕获 KeyboardInterrupt 和 SystemExit。",
    },
    no_error_boundary: {
      title: "没有 React 错误边界",
      description: "一次渲染错误就会让整个界面变成空白，且没有任何上报。",
    },
    no_crash_reporting: {
      title: "没有崩溃或错误上报",
      description: "用户在生产环境遇到的错误无从得知。",
    },
    no_health_endpoint: {
      title: "没有健康检查端点",
      description: "探针和部署检查无法判断服务是否正常。",
    },
    http_client_without_timeout: {
      title: "HTTP 调用没有超时",
      description: "一个缓慢的依赖就可能让请求无限期挂起。",
    },
    force_unwrap: {
      title: "大量强制解包",
      description: "try!、as! 和 !! 遇到意外的 nil 会崩溃。",
    },
    mutable_default_argument: {
      title: "可变默认参数",
      description: "list 或 dict 默认值会在多次调用间共享。",
    },
    debug_leftovers: {
      title: "遗留的调试语句",
      description: "代码中残留 debugger、断点或 var_dump/dd。",
    },
    oversized_source_file: {
      title: "源文件过大",
      description: "超过 1,500 行的文件难以审查和修改。",
    },
    typescript_any_overuse: {
      title: "大量 any / @ts-ignore",
      description: "很多地方关闭了类型检查。",
    },
    tech_debt_markers: {
      title: "大量 TODO/FIXME",
      description: "已知问题没有在任何地方跟踪。",
    },
    no_automated_tests: {
      title: "没有自动化测试",
      description: "组件有源代码但没有测试文件。",
    },
    no_linter_configured: {
      title: "未配置 linter",
      description: "风格偏差和常见错误模式无人检查。",
    },
    wildcard_import: {
      title: "通配符导入",
      description: "from x import * 让名称来源变得不清楚。",
    },
  },
};
