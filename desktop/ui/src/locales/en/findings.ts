// English dictionary for code-health findings: the repository page's
// Findings tab, its dialogs and every rule's title and one-line
// description. Rule keys match server findings.Rules(). Source of truth
// for every other language's copy.
export const findings = {
  intro: "Read from the code at every scan: anti-patterns, missing or misconfigured logging, debug settings that reach production, secrets and more. Findings you accept or mark as a false positive stay closed on later scans.",
  allComponents: "All components",
  repositoryWide: "Repository-wide",
  openCount: "{count} open",
  allCategories: "All categories",
  occurrences: "{count} occurrences",
  lines: "{count} lines",
  moreLocations: "and {count} more",
  showLocations: "Show locations",
  hideLocations: "Hide locations",
  createTask: "Create task",
  createTaskSelected: "Create task for {count}",
  selectAll: "Select all",
  clearSelection: "Clear",
  accept: "Accept",
  falsePositive: "False positive",
  reopen: "Reopen",
  openTask: "Open {key}",
  reason: "Reason: {reason}",
  resolvedHint: "The latest scan no longer finds this.",
  updateFailed: "Could not update the finding",
  componentSummary: {
    open: "{count} open code health findings",
    none: "No open code health findings",
    view: "View findings",
  },
  status: {
    open: "Open",
    accepted: "Accepted",
    false_positive: "False positive",
    resolved: "Resolved",
  },
  severity: {
    high: "High",
    medium: "Medium",
    low: "Low",
  },
  category: {
    security: "Security",
    configuration: "Configuration",
    logging: "Logging",
    error_handling: "Error handling",
    observability: "Observability",
    reliability: "Reliability",
    quality: "Code quality",
  },
  empty: {
    noScanTitle: "Not scanned yet",
    noScanDescription: "Findings appear after the repository's first scan.",
    cleanTitle: "No problems found",
    cleanDescription: "The latest scan found nothing to fix here.",
    filtered: "No findings match this filter.",
  },
  verdict: {
    acceptTitle: "Accept the risk",
    acceptDescription: "The finding closes and stays closed on later scans, even while the code still has it.",
    falsePositiveTitle: "Mark as false positive",
    falsePositiveDescription: "Tell the scan it is wrong here. The finding stays closed on later scans.",
    reasonLabel: "Reason (optional)",
    reasonPlaceholder: "Why this is fine here",
    save: "Save",
  },
  task: {
    title: "Create a task",
    description: "One board task covering {count} findings. The agent gets every location and how to fix each one.",
    titleLabel: "Title",
    notesLabel: "Notes for the agent (optional)",
    notesPlaceholder: "Anything the agent should know first",
    submit: "Create task",
    created: "Task {key} created",
    failed: "Could not create the task",
    defaultTitleMany: "Fix {count} code health findings in {component}",
    severalComponents: "several components",
  },
  rules: {
    hardcoded_secret: {
      title: "Secret committed in code",
      description: "A key or token in a known format is in the tree. Rotate it, then load it from configuration.",
    },
    hardcoded_credential: {
      title: "Hard-coded credential",
      description: "A password, key or token is assigned as a literal instead of read from configuration.",
    },
    committed_env_file: {
      title: "Env file with secrets not ignored",
      description: "An .env file holding real secrets is tracked, or not ignored by git.",
    },
    tls_verification_disabled: {
      title: "TLS verification disabled",
      description: "Certificate or hostname checks are off, so traffic can be intercepted.",
    },
    cleartext_traffic_allowed: {
      title: "Plain HTTP allowed",
      description: "The app permits unencrypted HTTP for every host.",
    },
    sql_injection_risk: {
      title: "Possible SQL injection",
      description: "A value is spliced into an SQL string instead of passed as a parameter.",
    },
    dynamic_code_execution: {
      title: "Dynamic code execution",
      description: "eval, exec or unsafe deserialization runs whatever the input says.",
    },
    unsafe_html_injection: {
      title: "Unescaped HTML",
      description: "Raw HTML is rendered without sanitizing, which allows cross-site scripting.",
    },
    permissive_cors: {
      title: "CORS allows any origin",
      description: "Any website can call this API from a visitor's browser.",
    },
    insecure_token_storage: {
      title: "Tokens in plain storage",
      description: "Tokens or passwords are kept in localStorage, UserDefaults or similar.",
    },
    http_body_logging: {
      title: "Request bodies or headers logged",
      description: "Bodies and headers carry tokens and personal data into the logs.",
    },
    error_details_exposed: {
      title: "Error details sent to clients",
      description: "Stack traces or developer error pages reach users.",
    },
    public_source_maps: {
      title: "Public source maps",
      description: "The production build publishes the original source code.",
    },
    debug_mode_in_production: {
      title: "Debug mode in production",
      description: "Production configuration turns on debug or development mode.",
    },
    debug_log_level_in_production: {
      title: "Debug log level in production",
      description: "Production configuration logs at debug or trace level.",
    },
    debuggable_release_build: {
      title: "Debuggable release build",
      description: "Anyone can attach a debugger to the Android release build.",
    },
    release_minification_disabled: {
      title: "Release build not minified",
      description: "R8/ProGuard is off for the release build.",
    },
    unconditional_debug_logging: {
      title: "Logger hard-coded to debug",
      description: "The log level is set to debug in code, ignoring the environment.",
    },
    console_logs_in_production: {
      title: "Debug console output in production",
      description: "console.log/debug/info calls ship in the bundle and the build does not strip them.",
    },
    mobile_debug_prints: {
      title: "Debug prints in release builds",
      description: "print, NSLog or Log.d run outside debug-only guards.",
    },
    print_instead_of_logger: {
      title: "Prints instead of logging",
      description: "Service code writes to stdout instead of using the logger.",
    },
    print_stack_trace: {
      title: "Stack traces printed, not logged",
      description: "printStackTrace or print_exc bypass the logging pipeline.",
    },
    no_structured_logging: {
      title: "No logging library",
      description: "The service has no logger, so there are no levelled, structured logs.",
    },
    no_request_logging: {
      title: "No request logging",
      description: "The HTTP service does not log requests (method, status, duration).",
    },
    catch_without_logging: {
      title: "Errors swallowed without logging",
      description: "Caught errors are neither logged, rethrown nor reported.",
    },
    empty_catch_block: {
      title: "Empty error handler",
      description: "An empty catch, except or err block hides every failure on that path.",
    },
    bare_except: {
      title: "Bare except",
      description: "except: also catches KeyboardInterrupt and SystemExit.",
    },
    no_error_boundary: {
      title: "No React error boundary",
      description: "One render error blanks the whole UI and nothing is reported.",
    },
    no_crash_reporting: {
      title: "No crash or error reporting",
      description: "Errors users hit in production stay invisible.",
    },
    no_health_endpoint: {
      title: "No health endpoint",
      description: "Probes and deploy checks cannot tell whether the service is up.",
    },
    http_client_without_timeout: {
      title: "HTTP calls without a timeout",
      description: "One slow dependency can hang requests indefinitely.",
    },
    force_unwrap: {
      title: "Many force unwraps",
      description: "try!, as! and !! crash on an unexpected nil.",
    },
    mutable_default_argument: {
      title: "Mutable default argument",
      description: "A list or dict default is shared between calls.",
    },
    debug_leftovers: {
      title: "Debugging statements left in",
      description: "debugger, breakpoints or var_dump/dd remain in the code.",
    },
    oversized_source_file: {
      title: "Very large source files",
      description: "Files over 1,500 lines are hard to review and change.",
    },
    typescript_any_overuse: {
      title: "Many any / @ts-ignore",
      description: "Type checking is switched off in many places.",
    },
    tech_debt_markers: {
      title: "Many TODO/FIXME markers",
      description: "Known problems that are tracked nowhere.",
    },
    no_automated_tests: {
      title: "No automated tests",
      description: "The component has source code but no test files.",
    },
    no_linter_configured: {
      title: "No linter configured",
      description: "Style drift and common bug patterns go unchecked.",
    },
    wildcard_import: {
      title: "Wildcard imports",
      description: "from x import * hides where names come from.",
    },
  },
};
