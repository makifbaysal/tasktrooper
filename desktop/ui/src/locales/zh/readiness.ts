// Chinese dictionary for the repository setup checklist. Must mirror
// en/readiness.ts keys exactly.
export const readiness = {
  title: "让这个项目就绪",
  intro: "智能体要在这个项目里顺利工作还缺什么，按合理的顺序列出。不需要的可以跳过。",
  progress: "已完成 {done} / {total}",
  skippedCount: "已跳过 {count} 项",
  complete: "这个项目已就绪。",
  show: "显示清单",
  hide: "隐藏",
  expand: "展开清单",
  collapse: "收起清单",
  skip: "跳过",
  unskip: "撤销跳过",
  doIt: "执行",
  open: "打开",
  install: "安装",
  viewTask: "查看 {key}",
  taskOpen: "任务 {key} 进行中。",
  skippedNote: "已跳过。",
  waitingFor: "等待：{step}。",
  started: "已开始。",
  taskOpened: "任务 {key} 已在看板上创建。",
  allStarted: "已启动 {runs} 项，创建 {tasks} 个任务。",
  actionFailed: "无法完成此操作",
  runAll: "按顺序全部执行",
  runAllTitle: "按顺序全部执行？",
  runAllTasks: "将在看板上创建 {count} 个任务，每个都等待前一个完成。",
  runAllRuns: "{count} 项会立即开始，不创建任务。",
  runAllCost: "每个任务都会占用你的智能体的时间。",
  runAllConfirm: "全部执行",
  status: {
    done: "已完成",
    todo: "待办",
    running: "进行中",
    blocked: "等待前面的步骤",
    skipped: "已跳过",
    unknown: "未检查",
  },
  missing: {
    readme: "README",
    agent_guide: "智能体指南",
  },
  steps: {
    scan: {
      title: "扫描项目",
      why: "扫描会找出组件、命令和 CI，其他所有步骤都依赖这些信息。",
    },
    toolchain: {
      title: "安装此技术栈所需的工具",
      why: "智能体在这台电脑上构建和测试，所以它需要智能体调用的编译器。",
    },
    index: {
      title: "为代码建立索引",
      why: "有了索引，智能体就能直接找到正确的文件，而不必通读全部代码。",
    },
    guide: {
      title: "添加 README 和智能体指南",
      why: "智能体每个任务都从这些文件开始，它们最能节省时间、减少错误。",
    },
    tests: {
      title: "让测试可以运行",
      why: "测试运行通过，你和智能体才能确信一次改动没有破坏任何东西。",
    },
    ci: {
      title: "在 CI 中运行检查",
      why: "CI 会在每次改动进入主分支之前重复构建和测试。",
    },
    delivery: {
      title: "选择改动如何上线",
      why: "交付目标告诉 TaskTrooper 完成的改动去向哪里，以及如何验证。",
    },
    observability: {
      title: "在生产环境中监控",
      why: "指标、日志和链路追踪会在某次发布出问题时提醒你。",
    },
    findings: {
      title: "查看代码健康发现",
      why: "扫描会标出调试设置、密钥和缺失的日志，这些问题越早修复成本越低。",
    },
    design: {
      title: "建立设计系统",
      why: "设计系统让新页面与现有页面保持一致。",
    },
    store: {
      title: "关联商店应用",
      why: "关联 App Store 或 Google Play 应用后，TaskTrooper 可以发布构建并跟踪评价。",
    },
  },
  detail: {
    scan: {
      no_scan: "尚未扫描。",
      scan_failed: "上一次扫描失败。",
      no_components: "扫描没有找到任何组件。",
      running: "正在扫描…",
      done: "找到 {cmp} 个组件。",
    },
    toolchain: {
      unknown: "需要：{languages}。",
      no_bridge: "只有桌面应用可以检查这台电脑上的工具。",
      missing: "这台电脑缺少：{tools}。",
      installed: "此技术栈所需的一切都已安装。",
    },
    index: {
      not_indexed: "尚未建立索引。",
      failed: "上一次索引失败。",
      unavailable: "代码索引不可用。",
      running: "{processed} / {total} 个文件。",
      done: "已索引 {files} 个文件。",
    },
    guide: {
      missing: "缺少：{missing}。",
      unreadable: "无法读取工作副本。",
      done: "已找到 README 和智能体指南。",
    },
    tests: {
      gaps: "没有测试命令：{components}。",
      done: "每个组件都有测试命令。",
    },
    ci: {
      no_ci: "没有找到 CI 工作流。",
      done: "找到 {checks} 项 CI 检查。",
    },
    delivery: {
      gaps: "没有已确认的交付方式：{components}。",
      done: "每个可部署的组件都有交付目标。",
    },
    observability: {
      gaps: "存在缺口：{components}。",
      done: "没有发现缺口。",
    },
    findings: {
      open: "{count} 项未处理（{high} 项高危）。",
      done: "没有未处理的发现。",
    },
    design: {
      none: "还没有设计系统。",
      unavailable: "无法读取设计系统。",
      done: "已有设计系统。",
    },
    store: {
      no_app: "尚未关联商店应用。",
      unavailable: "无法读取商店应用。",
      done: "已关联商店应用。",
    },
  },
};
